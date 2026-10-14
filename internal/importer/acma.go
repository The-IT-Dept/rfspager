package importer

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
)

// ACMA Register of Radiocommunications Licences: a ~70 MB weekly zip of CSVs
// covering every licensed device in Australia. We keep the paging
// transmitters on the two NSW fire paging channels.
// https://www.acma.gov.au/radiocomms-licence-data
const DefaultACMARRLURL = "https://web.acma.gov.au/rrl-updates/spectra_rrl.zip"

// Paging channel frequencies in Hz, as ACMA lists them.
const (
	acmaFreqRFS   = 148_587_500
	acmaFreqFRNSW = 148_962_500
)

// acmaLicensees are the licensees whose transmitters carry fire paging: RFS
// owns most sites on 148.5875; the Telco Authority runs an overlay on it and
// every FRNSW site on 148.9625.
var acmaLicensees = map[string]bool{
	"NSW RURAL FIRE SERVICE":                                  true,
	"NEW SOUTH WALES GOVERNMENT TELECOMMUNICATIONS AUTHORITY": true,
}

// acmaMinTransmitters guards the replace-all upsert: a parse that finds
// fewer than this is a broken or reshaped file, not 400 decommissioned sites.
const acmaMinTransmitters = 100

// ACMATransmitter is one paging transmitter device from the register.
type ACMATransmitter struct {
	SddID        int64
	Channel      string // "RFS" | "FRNSW"
	FreqHz       int64
	LicenceNo    string
	Licensee     string
	SiteID       string
	Name         string
	Lat, Lng     float64
	ElevationM   *float64
	HeightM      *float64
	EIRPW        *float64
	AuthorisedOn *time.Time
}

// ImportACMA downloads the register to a temporary file, extracts the
// paging transmitters and replaces paging_transmitters with them. Admin
// district overrides survive: only the ACMA columns are updated.
func ImportACMA(ctx context.Context, db *sqlx.DB, url string) (upserted, removed int, err error) {
	if url == "" {
		url = DefaultACMARRLURL
	}
	start := time.Now()
	path, err := downloadToTemp(ctx, url)
	if err != nil {
		return 0, 0, err
	}
	defer os.Remove(path)

	txs, err := ParseACMA(path)
	if err != nil {
		return 0, 0, err
	}
	upserted, removed, err = UpsertTransmitters(ctx, db, txs)
	if err != nil {
		return 0, 0, err
	}
	slog.Info("ACMA paging transmitters imported", "upserted", upserted, "removed", removed, "took", time.Since(start).Round(time.Second))
	return upserted, removed, nil
}

// downloadToTemp streams url to a temp file (archive/zip needs a ReaderAt)
// and returns its path. The caller removes it.
func downloadToTemp(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "rfspager-importer/1.0 (https://rfspager.app)")
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching ACMA register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ACMA register HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", "acma-rrl-*.zip")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("downloading ACMA register: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// acmaSource opens the register's CSVs from an unzipped directory or the zip.
type acmaSource interface {
	open(name string) (io.ReadCloser, error)
	Close() error
}

type dirSource string

func (d dirSource) open(name string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(string(d), name))
}
func (dirSource) Close() error { return nil }

type zipSource struct{ *zip.ReadCloser }

func (z zipSource) open(name string) (io.ReadCloser, error) {
	for _, f := range z.File {
		if strings.EqualFold(filepath.Base(f.Name), name) {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("%s not found in ACMA zip", name)
}

func openACMASource(path string) (acmaSource, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return dirSource(path), nil
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("opening ACMA zip: %w", err)
	}
	return zipSource{z}, nil
}

// ParseACMA extracts the paging transmitters from the register at path, an
// unzipped directory or the zip itself. Order matters for memory: the small
// client and licence tables are indexed first, the 390 MB device table is
// streamed keeping only matching rows, then only their sites are read.
func ParseACMA(path string) ([]ACMATransmitter, error) {
	src, err := openACMASource(path)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	// client.csv: CLIENT_NO -> licensee, for the licensees we want.
	clients := map[string]string{}
	err = eachCSV(src, "client.csv", func(row csvRow) error {
		name := strings.ToUpper(strings.TrimSpace(row.get("LICENCEE")))
		if acmaLicensees[name] {
			clients[row.get("CLIENT_NO")] = name
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// licence.csv: granted licences held by those clients.
	licences := map[string]string{}
	err = eachCSV(src, "licence.csv", func(row csvRow) error {
		licensee, ok := clients[row.get("CLIENT_NO")]
		if ok && strings.EqualFold(strings.TrimSpace(row.get("STATUS_TEXT")), "Granted") {
			licences[row.get("LICENCE_NO")] = licensee
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// device_details.csv: transmitters on the paging channels.
	var txs []ACMATransmitter
	err = eachCSV(src, "device_details.csv", func(row csvRow) error {
		if strings.TrimSpace(row.get("DEVICE_TYPE")) != "T" {
			return nil
		}
		licensee, ok := licences[row.get("LICENCE_NO")]
		if !ok {
			return nil
		}
		freq, ok := parseHz(row.get("FREQUENCY"))
		if !ok {
			return nil
		}
		var channel string
		switch freq {
		case acmaFreqRFS:
			channel = "RFS"
		case acmaFreqFRNSW:
			channel = "FRNSW"
		default:
			return nil
		}
		sdd, err := strconv.ParseInt(strings.TrimSpace(row.get("SDD_ID")), 10, 64)
		if err != nil {
			return nil
		}
		t := ACMATransmitter{
			SddID:     sdd,
			Channel:   channel,
			FreqHz:    freq,
			LicenceNo: strings.TrimSpace(row.get("LICENCE_NO")),
			Licensee:  licensee,
			SiteID:    strings.TrimSpace(row.get("SITE_ID")),
			HeightM:   parseOptFloat(row.get("HEIGHT")),
			EIRPW:     eirpWatts(row.get("EIRP"), row.get("EIRP_UNIT")),
		}
		if d, err := time.Parse("2006-01-02", strings.TrimSpace(row.get("AUTHORISATION_DATE"))); err == nil {
			t.AuthorisedOn = &d
		}
		txs = append(txs, t)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// site.csv: location, name and ground elevation of those devices' sites.
	type site struct {
		name      string
		lat, lng  float64
		elevation *float64
	}
	wanted := map[string]*site{}
	for _, t := range txs {
		wanted[t.SiteID] = nil
	}
	err = eachCSV(src, "site.csv", func(row csvRow) error {
		id := strings.TrimSpace(row.get("SITE_ID"))
		if _, ok := wanted[id]; !ok {
			return nil
		}
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(row.get("LATITUDE")), 64)
		lng, err2 := strconv.ParseFloat(strings.TrimSpace(row.get("LONGITUDE")), 64)
		if err1 != nil || err2 != nil {
			return nil
		}
		wanted[id] = &site{
			name:      cleanText(row.get("NAME")),
			lat:       lat,
			lng:       lng,
			elevation: parseOptFloat(row.get("ELEVATION")),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := txs[:0]
	for _, t := range txs {
		s := wanted[t.SiteID]
		if s == nil {
			// Area-licensed devices (transportables) have no site and so no
			// position to draw; two NSWTA overlay units on 148.5875 are.
			slog.Info("skipping ACMA transmitter without a site position", "sdd_id", t.SddID, "site_id", t.SiteID)
			continue
		}
		t.Name, t.Lat, t.Lng, t.ElevationM = s.name, s.lat, s.lng, s.elevation
		if t.Name == "" {
			t.Name = "ACMA site " + t.SiteID
		}
		out = append(out, t)
	}
	return out, nil
}

// UpsertTransmitters replaces paging_transmitters with txs in one
// transaction: rows are upserted by sdd_id and rows not in txs are deleted.
// agency_id, district_source and district_confidence are left alone so
// manual overrides survive; new rows start unmapped and auto.
func UpsertTransmitters(ctx context.Context, db *sqlx.DB, txs []ACMATransmitter) (upserted, removed int, err error) {
	if len(txs) < acmaMinTransmitters {
		return 0, 0, fmt.Errorf("ACMA register yielded %d paging transmitters, refusing to replace the table (want at least %d)", len(txs), acmaMinTransmitters)
	}
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	var now time.Time
	if err := tx.GetContext(ctx, &now, `SELECT NOW()`); err != nil {
		return 0, 0, err
	}
	for _, t := range txs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO paging_transmitters
			    (sdd_id, channel, freq_hz, licence_no, licensee, acma_site_id, name,
			     lat, lng, elevation_m, height_m, eirp_w, authorised_on, imported_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (sdd_id) DO UPDATE SET
			    channel = EXCLUDED.channel,
			    freq_hz = EXCLUDED.freq_hz,
			    licence_no = EXCLUDED.licence_no,
			    licensee = EXCLUDED.licensee,
			    acma_site_id = EXCLUDED.acma_site_id,
			    name = EXCLUDED.name,
			    lat = EXCLUDED.lat,
			    lng = EXCLUDED.lng,
			    elevation_m = EXCLUDED.elevation_m,
			    height_m = EXCLUDED.height_m,
			    eirp_w = EXCLUDED.eirp_w,
			    authorised_on = EXCLUDED.authorised_on,
			    imported_at = EXCLUDED.imported_at,
			    updated_at = NOW()`,
			t.SddID, t.Channel, t.FreqHz, t.LicenceNo, t.Licensee, t.SiteID, t.Name,
			t.Lat, t.Lng, t.ElevationM, t.HeightM, t.EIRPW, t.AuthorisedOn, now,
		); err != nil {
			return 0, 0, fmt.Errorf("upsert transmitter %d: %w", t.SddID, err)
		}
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM paging_transmitters WHERE imported_at < $1`, now)
	if err != nil {
		return 0, 0, err
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return len(txs), int(n), nil
}

// csvRow is one record with header-name lookup.
type csvRow struct {
	rec []string
	idx map[string]int
}

func (r csvRow) get(col string) string {
	if i, ok := r.idx[col]; ok && i < len(r.rec) {
		return r.rec[i]
	}
	return ""
}

// eachCSV streams one register CSV, calling fn per record. Columns are found
// by header name, never position; quoting is lenient because the register
// has stray quotes in free-text fields.
func eachCSV(src acmaSource, name string, fn func(csvRow) error) error {
	f, err := src.open(name)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	r.ReuseRecord = true

	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("%s: reading header: %w", name, err)
	}
	idx := make(map[string]int, len(header))
	for i, h := range header {
		h = strings.TrimPrefix(h, "\ufeff")
		idx[strings.ToUpper(strings.TrimSpace(h))] = i
	}
	for line := 2; ; line++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// One malformed record must not sink the import; csv.Reader
			// resumes at the next line.
			var perr *csv.ParseError
			if errors.As(err, &perr) {
				slog.Debug("skipping malformed ACMA record", "file", name, "line", line, "error", err)
				continue
			}
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := fn(csvRow{rec: rec, idx: idx}); err != nil {
			return err
		}
	}
}

// parseHz parses a frequency in Hz, tolerating "148587500.0".
func parseHz(s string) (int64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	return int64(math.Round(f)), true
}

func parseOptFloat(s string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}

// eirpWatts converts the register's EIRP to watts. Almost every paging row is
// in W; the others are converted rather than dropped.
func eirpWatts(value, unit string) *float64 {
	v := parseOptFloat(value)
	if v == nil {
		return nil
	}
	var w float64
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "W", "":
		w = *v
	case "MW":
		// ACMA writes milliwatts as "mW"; upper-casing makes it "MW". Nobody
		// licenses a megawatt pager.
		w = *v / 1000
	case "KW":
		w = *v * 1000
	case "DBW":
		w = math.Pow(10, *v/10)
	case "DBM":
		w = math.Pow(10, (*v-30)/10)
	default:
		return nil
	}
	if w <= 0 {
		return nil
	}
	return &w
}

// cleanText trims s and makes it valid UTF-8, reading bytes that are not as
// Latin-1 (the register mixes encodings in free text).
func cleanText(s string) string {
	s = strings.TrimSpace(s)
	if utf8.ValidString(s) {
		return s
	}
	runes := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			r = rune(s[i])
		}
		runes = append(runes, r)
		i += size
	}
	return string(runes)
}
