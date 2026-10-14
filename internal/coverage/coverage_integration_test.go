package coverage

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/importer"
)

// coverageTestDB migrates a fresh schema on RFSPAGER_TEST_DATABASE_URL,
// like the db and admin integration tests.
func coverageTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	rawURL := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(rawURL)
	if err != nil || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "rfspager_test") {
		t.Fatalf("RFSPAGER_TEST_DATABASE_URL must name a rfspager_test* database")
	}
	d, err := db.New(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()
	if _, err := d.Conn().ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d.Conn()
}

const (
	gosfordID = 1
	hornsbyID = 2
	frnswID   = 3
	beachesID = 4
)

func seedCoverage(t *testing.T, conn *sqlx.DB, now time.Time) {
	t.Helper()
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, user_id, email, password) VALUES (1, gen_random_uuid(), 'o@example.invalid', '!')`)
	exec(`INSERT INTO systems (id, system_id, user_id, name, lat, lng, located_since, approved_at, deleted_at, created_at, updated_at) VALUES
		(1, gen_random_uuid(), 1, 'Budgewoi',  -33.23, 151.56, NULL, NOW(), NULL, NOW(), NOW()),
		(2, gen_random_uuid(), 1, 'Newcastle', -32.93, 151.75, $1,   NOW(), NULL, NOW(), NOW()),
		(3, gen_random_uuid(), 1, 'Unlocated', NULL,   NULL,   NULL, NOW(), NULL, NOW(), NOW()),
		(4, gen_random_uuid(), 1, 'Deleted',   -33.70, 151.10, NULL, NOW(), NOW(), NOW(), NOW()),
		(5, gen_random_uuid(), 1, 'Null Island', 0, 0,         NULL, NOW(), NULL, NOW(), NOW())`,
		now.Add(-48*time.Hour))
	exec(`INSERT INTO agencies (id, agency, coverage_district) VALUES
		(1, 'Gosford', true), (2, 'Hornsby', true), (3, 'FRNSW', false), (4, 'Northern Beaches', true)`)
	exec(`INSERT INTO capcodes (id, address, alias, agency, agency_id, icon, colour, "ignore", created_at, updated_at) VALUES
		(1, '1001001', 'Kincumber Brigade', 'Gosford', 1, 'fire', 'red', false, NOW(), NOW()),
		(2, '1001002', 'Erina Brigade', 'Gosford', 1, 'fire', 'red', false, NOW(), NOW()),
		(3, '2002001', 'Berowra Brigade', 'Hornsby', 2, 'fire', 'red', false, NOW(), NOW()),
		(4, '1370001', 'Avalon Brigade', 'Northern Beaches', 4, 'fire', 'red', false, NOW(), NOW()),
		(5, '0107211', 'Station 1', 'FRNSW', 3, 'fire', 'red', false, NOW(), NOW())`)
	exec(`INSERT INTO emergency_stations (source, source_id, category, name, lat, lng, raw) VALUES
		('ga_emfd', '1', 'rfs', 'KINCUMBER', -33.47, 151.38, '{}'),
		('ga_emfd', '2', 'rfs', 'BEROWRA', -33.62, 151.15, '{}'),
		('ga_emfd', '3', 'rfs', 'AVALON', -33.63, 151.33, '{}')`)
	// Geocoded incidents: Central Coast pages for Gosford, one Northern
	// Beaches page on 2026-09-09 (its last).
	exec(`INSERT INTO messages (system_id, capcode_id, address, message, md5, lat, lng, created_at, updated_at) VALUES
		(1, 1, '1001001', 'a', 'a', '-33.45', '151.40', NOW() - INTERVAL '10 days', NOW()),
		(1, 2, '1001002', 'b - [151.43,-33.42]', 'b', NULL, NULL, NOW() - INTERVAL '20 days', NOW()),
		(1, 4, '1370001', 'c', 'c', '-33.64', '151.32', '2026-09-09T03:00:00Z', NOW())`)

	// Transmitters: two on the Central Coast, one at Hornsby, one at the
	// Northern Beaches, one out west with no labels, one FRNSW.
	exec(`INSERT INTO paging_transmitters (id, sdd_id, channel, freq_hz, licence_no, licensee, acma_site_id, name, lat, lng, elevation_m, height_m, eirp_w) VALUES
		(101, 1, 'RFS',   148587500, 'L1', 'NSW RURAL FIRE SERVICE', 'S1', 'Kincumber', -33.46, 151.39, 100, 15, 331),
		(102, 2, 'RFS',   148587500, 'L2', 'NSW RURAL FIRE SERVICE', 'S2', 'Erina',     -33.43, 151.42, 150, 15, 331),
		(103, 3, 'RFS',   148587500, 'L3', 'NSW RURAL FIRE SERVICE', 'S3', 'Berowra',   -33.62, 151.16, 200, 15, 331),
		(104, 4, 'RFS',   148587500, 'L4', 'NSW RURAL FIRE SERVICE', 'S4', 'Avalon',    -33.63, 151.32, 100, 15, 331),
		(105, 5, 'RFS',   148587500, 'L5', 'NSW RURAL FIRE SERVICE', 'S5', 'Bourke',    -30.09, 145.94, 110, 15, 331),
		(106, 6, 'FRNSW', 148962500, 'L6', 'NEW SOUTH WALES GOVERNMENT TELECOMMUNICATIONS AUTHORITY', 'S6', 'Sugarloaf', -32.89, 151.54, 410, 30, 331)`)

	// Receptions. m1: Budgewoi and Newcastle both hear 1001001 (one page),
	// Budgewoi again 20 s later (re-send, same minute). m2: Budgewoi hears
	// 1001002. Hornsby: Newcastle before it moved, and the unlocated system.
	m1 := now.Add(-3 * time.Hour).Truncate(time.Minute)
	m2 := now.Add(-2 * time.Hour).Truncate(time.Minute)
	rec := func(system int, at time.Time, address string, lat, lng any, key any, source string) {
		t.Helper()
		exec(`INSERT INTO page_receptions (system_id, heard_at, address, lat, lng, incident_key, source) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			system, at, address, lat, lng, key, source)
	}
	rec(1, m1.Add(5*time.Second), "1001001", -33.451, 151.401, "rfs:26-000001:aaa", "live")
	rec(2, m1.Add(6*time.Second), "1001001", -33.451, 151.401, "rfs:26-000001:aaa", "live")
	rec(1, m1.Add(25*time.Second), "1001001", -33.451, 151.401, "rfs:26-000001:aaa", "live")
	// The stop message for the same incident, same cell: still one incident.
	rec(1, m2.Add(time.Second), "1001002", -33.449, 151.399, "rfs:26-000001:bbb", "live")
	// A third Gosford page for Budgewoi, so it reaches minLinkPages.
	rec(1, now.Add(-90*time.Minute), "1001002", nil, nil, nil, "live")
	rec(2, now.Add(-72*time.Hour), "2002001", -33.62, 151.15, nil, "logs")
	rec(3, now.Add(-time.Hour), "2002001", -33.62, 151.15, nil, "messages")
	// FRNSW, outside the RFS report.
	rec(1, now.Add(-time.Hour), "0107211", nil, nil, "frinc:1-13102026", "live")
	// Outside a 30-day window.
	rec(1, now.Add(-40*24*time.Hour), "1001001", nil, nil, nil, "messages")
}

func TestCoverageEndToEnd(t *testing.T) {
	conn := coverageTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	seedCoverage(t, conn, now)

	res, err := AssignDistricts(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if res.Assigned != 4 || res.Unmapped != 1 || res.FRNSW != 1 {
		t.Errorf("assign = %+v, want 4 assigned, 1 unmapped, 1 FRNSW", res)
	}
	districtOf := func(txID int64) (agency *int64, source string) {
		t.Helper()
		if err := conn.QueryRowContext(ctx, `SELECT agency_id, district_source FROM paging_transmitters WHERE id = $1`, txID).Scan(&agency, &source); err != nil {
			t.Fatal(err)
		}
		return agency, source
	}
	for txID, want := range map[int64]int64{101: gosfordID, 102: gosfordID, 103: hornsbyID, 104: beachesID, 106: frnswID} {
		if got, _ := districtOf(txID); got == nil || *got != want {
			t.Errorf("transmitter %d district = %v, want %d", txID, got, want)
		}
	}
	if got, _ := districtOf(105); got != nil {
		t.Errorf("Bourke transmitter district = %d, want unmapped", *got)
	}

	svc := NewService(conn, Options{ImportDisabled: true})
	svc.now = func() time.Time { return now }
	p, _ := Params{}.Normalize()
	r, err := svc.Report(ctx, p)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, rx := range r.Receivers {
		names = append(names, rx.Name)
	}
	if strings.Join(names, ",") != "Budgewoi,Newcastle" {
		t.Errorf("receivers = %v (unlocated, deleted and 0,0 excluded)", names)
	}
	bud, newc := r.Receivers[0], r.Receivers[1]
	if bud.Pages != 3 || bud.DistrictsHeard != 1 {
		t.Errorf("Budgewoi = %+v, want 3 pages (re-send in the minute counts once), 1 district", bud)
	}
	if newc.Pages != 1 || newc.LocatedSince == nil {
		t.Errorf("Newcastle = %+v, want 1 attributable page", newc)
	}

	d := map[string]District{}
	for _, x := range r.Districts {
		d[x.Agency] = x
	}
	if len(r.Districts) != 3 || r.Districts[0].Agency != "Gosford" {
		t.Errorf("districts = %+v", r.Districts)
	}
	if g := d["Gosford"]; g.Pages != 3 || g.CapcodesHeard != 2 || g.CapcodesKnown != 2 || g.Transmitters != 2 || len(g.Receivers) != 2 {
		t.Errorf("Gosford = %+v (the page both receivers heard counts once)", g)
	}
	if h := d["Hornsby"]; h.Pages != 2 || len(h.Receivers) != 0 {
		t.Errorf("Hornsby = %+v, want 2 pages and no attributable receiver", h)
	}
	if nb := d["Northern Beaches"]; nb.Pages != 0 || nb.LastHeardAt == nil || !nb.LastHeardAt.Equal(time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("Northern Beaches = %+v", nb)
	}

	status := map[int64]string{}
	for _, tx := range r.Transmitters {
		status[tx.ID] = tx.Status
	}
	// Budgewoi heard 3 Gosford pages: probable link. Newcastle heard only 1.
	if status[101] != StatusProbable && status[102] != StatusProbable {
		t.Errorf("no probable Gosford transmitter: %v", status)
	}
	if status[103] != StatusDistrictHeard || status[104] != StatusNotHeard || status[105] != StatusUnmapped {
		t.Errorf("statuses = %v", status)
	}
	if _, ok := status[106]; ok {
		t.Error("FRNSW transmitter in an RFS report")
	}
	if len(r.Links) == 0 || r.Links[0].SystemID != 1 || r.Links[0].Kind != StatusProbable || r.Links[0].Pages != 3 {
		t.Errorf("links = %+v", r.Links)
	}

	// Heat: the call and stop message of one incident in one cell weigh 1;
	// Hornsby's two unkeyed pages at different minutes weigh 2.
	heat := map[[2]float64]float64{}
	for _, h := range r.Heat {
		heat[[2]float64{h[0], h[1]}] = h[2]
	}
	if heat[[2]float64{151.4, -33.45}] != 1 || heat[[2]float64{151.15, -33.62}] != 2 || len(heat) != 2 {
		t.Errorf("heat = %v", r.Heat)
	}
	if r.ReceptionsSince == nil {
		t.Error("receptions_since missing")
	}

	// Cached until something changes.
	again, _ := svc.Report(ctx, p)
	if again != r {
		t.Error("second report was rebuilt, want cached")
	}

	// One receiver.
	one, err := svc.Report(ctx, Params{Days: 30, Channel: ChannelRFS, SystemID: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Receivers) != 1 || one.Receivers[0].ID != 2 {
		t.Errorf("filtered receivers = %+v", one.Receivers)
	}
	for _, x := range one.Districts {
		if x.Agency == "Gosford" && x.Pages != 1 {
			t.Errorf("Gosford for Newcastle only = %d pages, want 1", x.Pages)
		}
	}

	// FRNSW channel.
	fr, err := svc.Report(ctx, Params{Days: 30, Channel: ChannelFRNSW})
	if err != nil {
		t.Fatal(err)
	}
	if len(fr.Districts) != 1 || fr.Districts[0].Pages != 1 || len(fr.Transmitters) != 1 || len(fr.Heat) != 0 {
		t.Errorf("FRNSW report: districts %+v, %d transmitters, heat %v", fr.Districts, len(fr.Transmitters), fr.Heat)
	}

	// Manual override survives reassignment and invalidates the cache.
	hornsby := int64(hornsbyID)
	if err := svc.SetManualDistrict(ctx, 101, &hornsby); err != nil {
		t.Fatal(err)
	}
	if _, err := AssignDistricts(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if got, src := districtOf(101); got == nil || *got != hornsbyID || src != "manual" {
		t.Errorf("after override + reassign: %v %s", got, src)
	}
	if fresh, _ := svc.Report(ctx, p); fresh == r {
		t.Error("override did not invalidate the cache")
	}
	if err := svc.SetManualDistrict(ctx, 101, nil); err != nil {
		t.Fatal(err)
	}
	if got, src := districtOf(101); got != nil || src != "manual" {
		t.Errorf("manual unmapped: %v %s", got, src)
	}
	if err := svc.ResetDistrict(ctx, 101); err != nil {
		t.Fatal(err)
	}
	if got, src := districtOf(101); got == nil || *got != gosfordID || src != "auto" {
		t.Errorf("after reset: %v %s", got, src)
	}
	bogus := int64(999)
	if err := svc.SetManualDistrict(ctx, 101, &bogus); !errors.Is(err, ErrUnknownAgency) {
		t.Errorf("unknown agency: %v", err)
	}
	if err := svc.SetManualDistrict(ctx, 9999, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown transmitter: %v", err)
	}

	// Retention.
	if _, err := conn.ExecContext(ctx, `INSERT INTO page_receptions (system_id, heard_at, address, source) VALUES (1, $1, '1', 'live')`,
		now.Add(-401*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n, err := pruneReceptions(ctx, conn, now); err != nil || n != 1 {
		t.Errorf("prune = %d, %v; want 1", n, err)
	}
}

// Imports the real ACMA register when it is unpacked locally, runs the
// district assignment over the seeded labels, and checks a manual override
// survives a re-import. Reports the counts in the test log.
func TestCoverageRealACMAImport(t *testing.T) {
	const dir = "/tmp/acma-rrl/data"
	if _, err := os.Stat(filepath.Join(dir, "device_details.csv")); err != nil {
		t.Skipf("real ACMA register not unpacked at %s", dir)
	}
	conn := coverageTestDB(t)
	ctx := context.Background()
	seedCoverage(t, conn, time.Now().UTC())
	if _, err := conn.ExecContext(ctx, `DELETE FROM paging_transmitters`); err != nil {
		t.Fatal(err)
	}

	txs, err := importer.ParseACMA(dir)
	if err != nil {
		t.Fatal(err)
	}
	n, removed, err := importer.UpsertTransmitters(ctx, conn, txs)
	if err != nil {
		t.Fatal(err)
	}
	res, err := AssignDistricts(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("imported %d transmitters (%d removed); districts: %+v", n, removed, res)
	if res.FRNSW < 30 || res.Assigned+res.Unmapped < 400 {
		t.Errorf("assignment = %+v", res)
	}

	var pinned int64
	if err := conn.GetContext(ctx, &pinned, `SELECT id FROM paging_transmitters WHERE channel = 'RFS' ORDER BY id LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(conn, Options{})
	beaches := int64(beachesID)
	if err := svc.SetManualDistrict(ctx, pinned, &beaches); err != nil {
		t.Fatal(err)
	}
	if _, _, err := importer.UpsertTransmitters(ctx, conn, txs); err != nil {
		t.Fatal(err)
	}
	var agency int64
	var source string
	if err := conn.QueryRowContext(ctx, `SELECT agency_id, district_source FROM paging_transmitters WHERE id = $1`, pinned).Scan(&agency, &source); err != nil {
		t.Fatal(err)
	}
	if agency != beachesID || source != "manual" {
		t.Errorf("override after re-import = %d %s", agency, source)
	}
	if _, _, err := importer.UpsertTransmitters(ctx, conn, txs[:10]); err == nil {
		t.Error("a 10-row import replaced the table")
	}
}

func TestPublicCoverageEndToEnd(t *testing.T) {
	conn := coverageTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	seedCoverage(t, conn, now)
	if _, err := AssignDistricts(ctx, conn); err != nil {
		t.Fatal(err)
	}
	// Stored to more precision than the public map shows.
	if _, err := conn.ExecContext(ctx, `UPDATE systems SET lat = -33.234567, lng = 151.556789 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	svc := NewService(conn, Options{ImportDisabled: true})
	clock := now
	svc.now = func() time.Time { return clock }
	r, err := svc.PublicReport(ctx, 7) // not offered: served as 90
	if err != nil {
		t.Fatal(err)
	}
	if r.Window.Days != 90 {
		t.Errorf("window = %+v, want 90 days", r.Window)
	}
	var names []string
	for _, rx := range r.Receivers {
		names = append(names, rx.Name)
	}
	if strings.Join(names, ",") != "Budgewoi,Newcastle" {
		t.Errorf("receivers = %v (unlocated, deleted and 0,0 excluded)", names)
	}
	if bud := r.Receivers[0]; bud.Lat != -33.23 || bud.Lng != 151.56 || bud.LastHeardAt == nil {
		t.Errorf("Budgewoi = %+v, want rounded position and a last heard time", bud)
	}
	if len(r.Transmitters) != 5 {
		t.Errorf("transmitters = %d, want the 5 RFS sites", len(r.Transmitters))
	}
	heard := map[string]bool{}
	for _, tx := range r.Transmitters {
		if tx.District != nil {
			heard[*tx.District] = heard[*tx.District] || tx.Heard
		} else if tx.Heard {
			t.Errorf("unmapped transmitter heard: %+v", tx)
		}
	}
	if !heard["Gosford"] || !heard["Hornsby"] || heard["Northern Beaches"] {
		t.Errorf("heard by district = %v", heard)
	}
	var districts []string
	for _, d := range r.Districts {
		districts = append(districts, d.Name)
	}
	if strings.Join(districts, ",") != "Gosford,Hornsby,Northern Beaches" {
		t.Errorf("districts = %v (RFS only)", districts)
	}
	if r.TotalPages == 0 || len(r.Heat) == 0 {
		t.Errorf("total pages %d, heat %v", r.TotalPages, r.Heat)
	}

	// Cached per window for 15 minutes, separately from the admin report.
	if again, _ := svc.PublicReport(ctx, 90); again != r {
		t.Error("second request within 15 minutes was rebuilt")
	}
	if short, _ := svc.PublicReport(ctx, 30); short == r || short.Window.Days != 30 {
		t.Errorf("30-day window = %+v, want its own report", short.Window)
	}
	svc.Invalidate()
	if again, _ := svc.PublicReport(ctx, 90); again != r {
		t.Error("an admin invalidation dropped the public cache")
	}
	clock = now.Add(16 * time.Minute)
	if later, _ := svc.PublicReport(ctx, 90); later == r {
		t.Error("public report served past 15 minutes")
	}
}
