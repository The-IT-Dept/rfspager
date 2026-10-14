package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Migration 034 backfills page_receptions from receiver log lines (every
// receiver's copy) and, before each system's first log line, from the stored
// messages. Applied to a database migrated up to 033 with realistic rows,
// then re-run to prove it is idempotent.
func TestCoverageMigrationBackfill(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)
	ctx := context.Background()

	all, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var before []migration
	var cov migration
	for _, m := range all {
		switch {
		case m.name < "034":
			before = append(before, m)
		case strings.HasPrefix(m.name, "034_"):
			cov = m
		}
	}
	if cov.name == "" {
		t.Fatal("migration 034 not found")
	}
	if err := d.applyMigrations(ctx, before); err != nil {
		t.Fatalf("migrate to 033: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	firstLog := now.Add(-2 * time.Hour)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.conn.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, user_id, email, password) VALUES (1, gen_random_uuid(), 'o@example.invalid', '!')`)
	for _, s := range []struct {
		id   int
		name string
	}{{1, "Budgewoi"}, {2, "Figtree"}, {3, "PagerMon"}} {
		exec(`INSERT INTO systems (id, system_id, user_id, name, created_at, updated_at) VALUES ($1, gen_random_uuid(), 1, $2, NOW(), NOW())`, s.id, s.name)
	}
	exec(`INSERT INTO agencies (id, agency) VALUES (1, 'Gosford'), (2, 'FRNSW'), (3, 'SES'), (4, 'Rubish')`)
	exec(`INSERT INTO capcodes (id, address, alias, agency, agency_id, icon, colour, "ignore") VALUES
		(1, '1001001', 'Kincumber Brigade', 'Gosford', 1, 'fire', 'red', false),
		(2, '0107211', 'Station 1', 'FRNSW', 2, 'fire', 'red', false),
		(3, '9990001', 'Noise', 'Rubish', 4, 'x', 'x', true)`)

	rfs := "14 October 2026 01:54:54 CCKINC - 26-131331 - Grass Fire - 1 SMITH ST KINCUMBER - [151.38,-33.47]"
	logLine := func(system int, ts time.Time, address, msg string, posted bool, extra string) {
		t.Helper()
		exec(`INSERT INTO system_logs (system_id, ts, level, source, message, raw) VALUES ($1, $2, 'info', 'pocsag', 'page',
			jsonb_build_object('channel', 'RFS', 'address', $3::text, 'msg', $4::text, 'posted', $5::bool) || $6::jsonb)`,
			system, ts, address, msg, posted, extra)
	}
	// Both Go receivers logged the same page; Figtree's copy was damaged.
	pageAt := now.Add(-time.Hour)
	logLine(1, pageAt.Add(300*time.Millisecond), "1001001", rfs, true, `{"corrected": 2, "uncorrectable": 0}`)
	logLine(2, pageAt.Add(800*time.Millisecond), "1001001", rfs, true, `{"uncorrectable": 3}`)
	// The earliest log line for system 1 (sets its messages cut-off).
	logLine(1, firstLog, "0107211", "FRINC TYPE: AFA INC: 203357-13102026 TURNOUT: 72", true, `{"truncated": true}`)
	// Not posted (noise), ignored capcode, no address: none recorded.
	logLine(1, pageAt, "0521839", "beacon", false, `{}`)
	logLine(1, pageAt, "9990001", "noise page text", true, `{}`)
	exec(`INSERT INTO system_logs (system_id, ts, message, raw) VALUES (1, NOW(), 'page', '{"posted": true}')`)
	// A non-page log line.
	exec(`INSERT INTO system_logs (system_id, ts, message, raw) VALUES (1, NOW(), 'connected', '{"posted": true, "address": "1"}')`)

	msg := func(system int, capcode any, address, text string, at time.Time, lat, lng any, deleted bool, incidentKey any) {
		t.Helper()
		exec(`INSERT INTO messages (system_id, capcode_id, address, message, md5, lat, lng, incident_key, created_at, updated_at, deleted_at)
			VALUES ($1, $2, $3, $4, md5($4), $5, $6, $7, $8, $8, CASE WHEN $9 THEN NOW() END)`,
			system, capcode, address, text, lat, lng, incidentKey, at, deleted)
	}
	// The stored copy of the logged page (system 1 won), after system 1's
	// first log line: covered by the logs, not re-added.
	msg(1, 1, "1001001", rfs, pageAt, "-33.47", "151.38", false, "rfs:26-131331:abc")
	// Older pages: credited to the stored copy's receiver.
	msg(1, 1, "1001001", "older page no coords 26-120000 Grass Fire", now.Add(-10*24*time.Hour), "", "", false, nil)
	msg(3, nil, "1370555", "unknown capcode page - [151.30,-33.65]", now.Add(-20*24*time.Hour), nil, nil, false, nil)
	// PagerMon system never logs: its recent message counts too.
	msg(3, 2, "0107211", "FRINC TYPE: AFA INC: 203358-13102026", now.Add(-30*time.Minute), nil, nil, false, nil)
	// Deleted, too old, garbage coordinates.
	msg(1, 1, "1001001", "deleted page", now.Add(-5*24*time.Hour), nil, nil, true, nil)
	msg(1, 1, "1001001", "ancient page", now.Add(-100*24*time.Hour), nil, nil, false, nil)
	msg(2, 1, "1001001", "bad coords", now.Add(-6*24*time.Hour), "abc", "-", false, nil)

	if err := d.applyMigrations(ctx, []migration{cov}); err != nil {
		t.Fatalf("apply 034: %v", err)
	}

	type row struct {
		SystemID    int64     `db:"system_id"`
		Address     string    `db:"address"`
		CapcodeID   *int64    `db:"capcode_id"`
		Lat         *float64  `db:"lat"`
		Lng         *float64  `db:"lng"`
		IncidentKey *string   `db:"incident_key"`
		Damaged     bool      `db:"damaged"`
		Source      string    `db:"source"`
		HeardAt     time.Time `db:"heard_at"`
	}
	var rows []row
	if err := d.conn.SelectContext(ctx, &rows, `SELECT system_id, address, capcode_id, lat, lng, incident_key, damaged, source, heard_at
		FROM page_receptions ORDER BY source, system_id, heard_at`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 7 {
		t.Fatalf("got %d receptions, want 7 (3 logs + 4 messages)", len(rows))
	}
	logs, msgs := rows[:3], rows[3:]
	for _, r := range logs {
		if r.Source != "logs" {
			t.Fatalf("expected logs rows first: %+v", rows)
		}
	}
	// System 1: FRNSW first log line, then the RFS page; system 2: the RFS page.
	if r := logs[0]; r.SystemID != 1 || r.Address != "0107211" || !r.Damaged || deref(r.IncidentKey) != "frinc:203357-13102026" || r.CapcodeID == nil || *r.CapcodeID != 2 {
		t.Errorf("FRNSW log reception = %+v", r)
	}
	if r := logs[1]; r.SystemID != 1 || r.Damaged || !r.HeardAt.Equal(pageAt) || deref(r.IncidentKey) != "rfs:26-131331:abc" ||
		r.Lat == nil || *r.Lat != -33.47 || *r.Lng != 151.38 || r.CapcodeID == nil || *r.CapcodeID != 1 {
		t.Errorf("Budgewoi log reception = %+v (key from the stored copy, ts truncated)", r)
	}
	if r := logs[2]; r.SystemID != 2 || !r.Damaged || !r.HeardAt.Equal(pageAt) {
		t.Errorf("Figtree log reception = %+v", r)
	}

	if r := msgs[0]; r.SystemID != 1 || deref(r.IncidentKey) != "rfs:26-120000" || r.Lat != nil {
		t.Errorf("older message reception = %+v", r)
	}
	if r := msgs[1]; r.SystemID != 2 || r.Lat != nil || r.Lng != nil {
		t.Errorf("bad-coords message reception = %+v", r)
	}
	if r := msgs[2]; r.SystemID != 3 || r.Address != "1370555" || r.Lat == nil || *r.Lat != -33.65 || *r.Lng != 151.30 || r.CapcodeID != nil {
		t.Errorf("unknown-capcode message reception = %+v", r)
	}
	if r := msgs[3]; r.SystemID != 3 || r.Address != "0107211" || deref(r.IncidentKey) != "frinc:203358-13102026" {
		t.Errorf("PagerMon recent message reception = %+v", r)
	}

	// Non-district agencies switched off, the rest on.
	var off []string
	if err := d.conn.SelectContext(ctx, &off, `SELECT agency FROM agencies WHERE NOT coverage_district ORDER BY agency`); err != nil {
		t.Fatal(err)
	}
	if strings.Join(off, ",") != "FRNSW,Rubish,SES" {
		t.Errorf("non-district agencies = %v", off)
	}

	// Re-running the file (a partial first run) changes nothing and keeps
	// admin edits to coverage_district.
	exec(`UPDATE agencies SET coverage_district = true WHERE agency = 'SES'`)
	if _, err := d.conn.ExecContext(ctx, cov.sql); err != nil {
		t.Fatalf("re-run 034: %v", err)
	}
	var n int
	if err := d.conn.GetContext(ctx, &n, `SELECT COUNT(*) FROM page_receptions`); err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Errorf("after re-run: %d receptions, want 7", n)
	}
	var ses bool
	if err := d.conn.GetContext(ctx, &ses, `SELECT coverage_district FROM agencies WHERE agency = 'SES'`); err != nil {
		t.Fatal(err)
	}
	if !ses {
		t.Error("re-run reset an admin's coverage_district edit")
	}

	// Live recording is idempotent with the backfill on the unique key.
	if err := d.RecordPageReception(ctx, PageReception{SystemID: 1, HeardAt: pageAt, Address: "1001001"}); err != nil {
		t.Fatal(err)
	}
	if err := d.conn.GetContext(ctx, &n, `SELECT COUNT(*) FROM page_receptions`); err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Errorf("live repeat of a backfilled page added a row: %d", n)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
