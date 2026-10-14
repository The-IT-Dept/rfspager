package handler

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// receptionTestDB migrates a fresh schema on RFSPAGER_TEST_DATABASE_URL,
// like the db and admin integration tests.
func receptionTestDB(t *testing.T) *db.DB {
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
	return d
}

// Every receiver's copy of a page is recorded, including the copies dedupe
// throws away; ignored capcodes and stale pages are not.
func TestCreateMessageRecordsEveryReception(t *testing.T) {
	d := receptionTestDB(t)
	ctx := context.Background()
	conn := d.Conn()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, user_id, email, password) VALUES (1, gen_random_uuid(), 'o@example.invalid', '!')`)
	exec(`INSERT INTO systems (id, system_id, user_id, name, api_key, approved_at, created_at, updated_at) VALUES
		(1, gen_random_uuid(), 1, 'Budgewoi', 'k1', NOW(), NOW(), NOW()),
		(2, gen_random_uuid(), 1, 'Figtree', 'k2', NOW(), NOW(), NOW())`)
	exec(`INSERT INTO agencies (id, agency) VALUES (1, 'Gosford'), (2, 'Rubish')`)
	exec(`INSERT INTO capcodes (id, address, alias, agency, agency_id, icon, colour, "ignore", created_at, updated_at) VALUES
		(1, '1001001', 'Kincumber Brigade', 'Gosford', 1, 'fire', 'red', false, NOW(), NOW()),
		(2, '9990001', 'Noise', 'Rubish', 2, 'x', 'x', true, NOW(), NOW())`)

	h := NewMessageHandler(d)
	budgewoi := &models.System{ID: 1, Name: "Budgewoi"}
	figtree := &models.System{ID: 2, Name: "Figtree"}
	at := time.Now().Truncate(time.Second)
	page := "14 October 2026 01:54:54 CCKINC - 26-131331 - Grass Fire - 1 SMITH ST - [151.38,-33.47]"

	if status, err := h.IngestPage(ctx, budgewoi, "1001001", page, at, &rxproto.PageQuality{Codewords: 20}); status != "ok" || err != nil {
		t.Fatalf("first copy: %s %v", status, err)
	}
	// Figtree's damaged copy of the same page is deduped but still heard.
	if status, _ := h.IngestPage(ctx, figtree, "1001001", page, at.Add(time.Second), &rxproto.PageQuality{Codewords: 20, Uncorrectable: 2}); status != "dup" {
		t.Fatalf("second copy status = %s, want dup", status)
	}
	// A PagerMon-style repeat from Budgewoi hits the cheap early exit.
	if result, _ := h.createMessage(ctx, budgewoi, "1001001", page, at.Add(30*time.Second), nil, nil); result != "Duplicate message." {
		t.Fatalf("repeat = %q", result)
	}
	// Unknown address: recorded without a capcode.
	if status, _ := h.IngestPage(ctx, budgewoi, "1370555", "unknown capcode page text", at, nil); status != "ok" {
		t.Fatalf("unknown capcode status = %s", status)
	}
	// Ignored capcode and a stale page: not recorded.
	if result, _ := h.createMessage(ctx, budgewoi, "9990001", "noise page text", at, nil, nil); result != "Ignored capcode." {
		t.Fatalf("ignored = %q", result)
	}
	if _, msg := h.createMessage(ctx, budgewoi, "1001001", "stale page", at.Add(-time.Hour), nil, nil); msg != nil {
		t.Fatal("stale page stored")
	}

	var rows []struct {
		SystemID  int64    `db:"system_id"`
		Address   string   `db:"address"`
		CapcodeID *int64   `db:"capcode_id"`
		Lat       *float64 `db:"lat"`
		Damaged   bool     `db:"damaged"`
		Source    string   `db:"source"`
		Key       *string  `db:"incident_key"`
	}
	if err := conn.SelectContext(ctx, &rows, `SELECT system_id, address, capcode_id, lat, damaged, source, incident_key
		FROM page_receptions ORDER BY address, heard_at, system_id`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("receptions = %+v, want 4", rows)
	}
	wantSystems := []int64{1, 2, 1, 1}
	for i, r := range rows {
		if r.SystemID != wantSystems[i] || r.Source != "live" {
			t.Errorf("reception %d = %+v", i, r)
		}
	}
	if r := rows[0]; r.CapcodeID == nil || *r.CapcodeID != 1 || r.Lat == nil || *r.Lat != -33.47 || r.Damaged || r.Key == nil || *r.Key != IncidentKey(page) {
		t.Errorf("Budgewoi reception = %+v", r)
	}
	if r := rows[1]; !r.Damaged {
		t.Errorf("Figtree reception = %+v, want damaged", r)
	}
	if r := rows[3]; r.Address != "1370555" || r.CapcodeID != nil {
		t.Errorf("unknown capcode reception = %+v", r)
	}

	var stored int
	if err := conn.GetContext(ctx, &stored, `SELECT COUNT(*) FROM messages`); err != nil {
		t.Fatal(err)
	}
	if stored != 2 {
		t.Errorf("stored messages = %d, want 2 (dedupe unchanged)", stored)
	}
}
