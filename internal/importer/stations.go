package importer

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// Geoscience Australia — Emergency Management Facilities Database.
// National coverage, CC-BY 4.0, last bulk refresh ~2018-2022. Includes
// police, ambulance, metro fire, rural fire, SES, other.
// https://ecat.ga.gov.au/geonetwork/srv/api/records/015c1b8c-7b47-4780-8206-1bd39ad2120b
const gaEmfdGeoJSONURL = "https://d28rz98at9flks.cloudfront.net/147776/147776_00_2.json"

// Fire and Rescue NSW — official station GeoRSS feed. Newer urban stations
// that may not be in the GA snapshot. Refreshed by FRNSW daily.
// https://www.fire.nsw.gov.au/page.php?id=9376
const frnswGeoRSSURL = "https://www.fire.nsw.gov.au/feeds/stations-georss.php"

// ----- Geoscience Australia GeoJSON shapes -----

type gaCollection struct {
	Features []gaFeature `json:"features"`
}

type gaFeature struct {
	Properties gaProps        `json:"properties"`
	Geometry   map[string]any `json:"geometry"`
}

type gaProps struct {
	ObjectID      json.Number `json:"OBJECTID"`
	Class         string      `json:"CLASS"`
	FacilityName  string      `json:"FACILITY_NAME"`
	Status        string      `json:"FACILITY_OPERATIONALSTATUS"`
	Address       string      `json:"FACILITY_ADDRESS"`
	Suburb        string      `json:"ABS_SUBURB"`
	State         string      `json:"FACILITY_STATE"`
	Postcode      string      `json:"ABS_POSTCODE"`
	Source        string      `json:"FACILITY_SOURCE"`
	Revised       string      `json:"FACILITY_REVISED"` // YYYYMMDD
	Lat           string      `json:"FACILITY_LAT"`
	Lng           string      `json:"FACILITY_LONG"`
	GNAFFormatted string      `json:"GNAF_FORMATTED_ADDRESS"`
}

// ----- FRNSW GeoRSS shapes -----

type frnswRSS struct {
	Channel frnswChannel `xml:"channel"`
}
type frnswChannel struct {
	Items []frnswItem `xml:"item"`
}
type frnswItem struct {
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Lat         string `xml:"http://www.w3.org/2003/01/geo/wgs84_pos# lat"`
	Long        string `xml:"http://www.w3.org/2003/01/geo/wgs84_pos# long"`
}

// classToCategory maps GA's CLASS field to our short category code.
func classToCategory(class string) string {
	c := strings.ToUpper(class)
	switch {
	case strings.Contains(c, "RURAL") || strings.Contains(c, "COUNTRY"):
		return "rfs"
	case strings.Contains(c, "METRO") && strings.Contains(c, "FIRE"):
		return "frnsw"
	case strings.Contains(c, "POLIC"):
		return "police"
	case strings.Contains(c, "AMBULANCE"):
		return "ambulance"
	case strings.Contains(c, "SES"):
		return "ses"
	default:
		return "other"
	}
}

// operatorFromSource turns GA's FACILITY_SOURCE into a readable operator name.
func operatorFromSource(src, category string) string {
	s := strings.ToUpper(src)
	switch {
	case strings.Contains(s, "RURAL FIRE"):
		return "NSW Rural Fire Service"
	case strings.Contains(s, "FIRE & RESCUE") || strings.Contains(s, "FIRE AND RESCUE"):
		return "Fire and Rescue NSW"
	case strings.Contains(s, "POLICE"):
		return "NSW Police Force"
	case strings.Contains(s, "AMBULANCE"):
		return "NSW Ambulance"
	case strings.Contains(s, "SES"):
		return "NSW State Emergency Service"
	}
	// fall back to category default
	switch category {
	case "rfs":
		return "NSW Rural Fire Service"
	case "frnsw":
		return "Fire and Rescue NSW"
	case "police":
		return "NSW Police Force"
	case "ambulance":
		return "NSW Ambulance"
	case "ses":
		return "NSW State Emergency Service"
	}
	return ""
}

// parseGaRevised converts "20180914" → 2018-09-14.
func parseGaRevised(s string) *time.Time {
	if len(s) < 8 {
		return nil
	}
	t, err := time.Parse("20060102", s[:8])
	if err != nil {
		return nil
	}
	return &t
}

func importStations(ctx context.Context, db *sqlx.DB) error {
	gaCount, gaErr := importStationsFromGA(ctx, db)
	if gaErr != nil {
		slog.Error("GA EMFD station import failed", "error", gaErr)
	}

	frnswCount, frnswErr := importStationsFromFRNSW(ctx, db)
	if frnswErr != nil {
		slog.Error("FRNSW GeoRSS station import failed", "error", frnswErr)
	}

	slog.Info("stations imported",
		"ga_emfd", gaCount,
		"frnsw_supplement", frnswCount,
	)
	if gaErr != nil && frnswErr != nil {
		return fmt.Errorf("both station sources failed: ga=%v frnsw=%v", gaErr, frnswErr)
	}
	return nil
}

func importStationsFromGA(ctx context.Context, db *sqlx.DB) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", gaEmfdGeoJSONURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "rfspager-importer/1.0 (https://rfspager.app)")

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetching GA EMFD: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("GA EMFD HTTP %d", resp.StatusCode)
	}

	var coll gaCollection
	if err := json.NewDecoder(resp.Body).Decode(&coll); err != nil {
		return 0, fmt.Errorf("decoding GA EMFD: %w", err)
	}

	count := 0
	for _, f := range coll.Features {
		p := f.Properties
		if !strings.EqualFold(p.State, "NEW SOUTH WALES") {
			continue
		}
		lat, err1 := strconv.ParseFloat(p.Lat, 64)
		lng, err2 := strconv.ParseFloat(p.Lng, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		category := classToCategory(p.Class)
		operator := operatorFromSource(p.Source, category)
		name := strings.TrimSpace(p.FacilityName)
		if name == "" {
			continue
		}
		address := strings.TrimSpace(p.Address)
		if address == "" {
			address = strings.TrimSpace(p.GNAFFormatted)
		}

		rawJSON, _ := json.Marshal(p)
		_, err := db.ExecContext(ctx, `
			INSERT INTO emergency_stations
			    (source, source_id, category, name, operator, address,
			     suburb, postcode, state, status, lat, lng, revised_at, raw, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'NSW', $9, $10, $11, $12, $13, NOW())
			ON CONFLICT (source, source_id) DO UPDATE SET
			    category = EXCLUDED.category,
			    name = EXCLUDED.name,
			    operator = EXCLUDED.operator,
			    address = EXCLUDED.address,
			    suburb = EXCLUDED.suburb,
			    postcode = EXCLUDED.postcode,
			    status = EXCLUDED.status,
			    lat = EXCLUDED.lat,
			    lng = EXCLUDED.lng,
			    revised_at = EXCLUDED.revised_at,
			    raw = EXCLUDED.raw,
			    updated_at = NOW()`,
			"ga_emfd", p.ObjectID.String(), category, name, operator, address,
			strings.TrimSpace(p.Suburb), strings.TrimSpace(p.Postcode),
			strings.TrimSpace(p.Status), lat, lng, parseGaRevised(p.Revised), rawJSON,
		)
		if err != nil {
			slog.Error("upsert station failed", "source", "ga_emfd", "id", p.ObjectID.String(), "error", err)
			continue
		}
		count++
	}
	return count, nil
}

func importStationsFromFRNSW(ctx context.Context, db *sqlx.DB) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", frnswGeoRSSURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "rfspager-importer/1.0 (https://rfspager.app)")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetching FRNSW GeoRSS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("FRNSW GeoRSS HTTP %d", resp.StatusCode)
	}

	var rss frnswRSS
	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		return 0, fmt.Errorf("decoding FRNSW GeoRSS: %w", err)
	}

	count := 0
	for _, it := range rss.Channel.Items {
		// Title is "208, Aberdeen Fire Station" — split into station number + name.
		stationNum, name, ok := strings.Cut(it.Title, ",")
		if !ok {
			name = it.Title
			stationNum = ""
		}
		stationNum = strings.TrimSpace(stationNum)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		lat, err1 := strconv.ParseFloat(it.Lat, 64)
		lng, err2 := strconv.ParseFloat(it.Long, 64)
		if err1 != nil || err2 != nil {
			continue
		}

		// Description = "20 Moray St, Aberdeen NSW 2336" — extract suburb + postcode.
		address := strings.TrimSpace(it.Description)
		suburb, postcode := parseFrnswAddress(address)

		// source_id: prefer station number, fall back to name hash for robustness.
		sourceID := stationNum
		if sourceID == "" {
			sourceID = name
		}

		raw := map[string]string{
			"title":       it.Title,
			"description": it.Description,
			"station_no":  stationNum,
		}
		rawJSON, _ := json.Marshal(raw)

		_, err := db.ExecContext(ctx, `
			INSERT INTO emergency_stations
			    (source, source_id, category, name, operator, address,
			     suburb, postcode, state, status, lat, lng, raw, updated_at)
			VALUES ($1, $2, 'frnsw', $3, 'Fire and Rescue NSW', $4, $5, $6, 'NSW', 'OPERATIONAL', $7, $8, $9, NOW())
			ON CONFLICT (source, source_id) DO UPDATE SET
			    name = EXCLUDED.name,
			    address = EXCLUDED.address,
			    suburb = EXCLUDED.suburb,
			    postcode = EXCLUDED.postcode,
			    lat = EXCLUDED.lat,
			    lng = EXCLUDED.lng,
			    raw = EXCLUDED.raw,
			    updated_at = NOW()`,
			"frnsw", sourceID, name, address, suburb, postcode, lat, lng, rawJSON,
		)
		if err != nil {
			slog.Error("upsert station failed", "source", "frnsw", "id", sourceID, "error", err)
			continue
		}
		count++
	}
	return count, nil
}

// parseFrnswAddress extracts suburb + postcode from "20 Moray St, Aberdeen NSW 2336".
func parseFrnswAddress(s string) (suburb, postcode string) {
	parts := strings.Split(s, ",")
	if len(parts) < 2 {
		return "", ""
	}
	tail := strings.TrimSpace(parts[len(parts)-1])
	// tail looks like "Aberdeen NSW 2336"
	words := strings.Fields(tail)
	if len(words) >= 1 {
		last := words[len(words)-1]
		if len(last) == 4 {
			postcode = last
			words = words[:len(words)-1]
		}
	}
	// drop trailing "NSW"
	if len(words) > 0 && strings.EqualFold(words[len(words)-1], "NSW") {
		words = words[:len(words)-1]
	}
	suburb = strings.Join(words, " ")
	return suburb, postcode
}
