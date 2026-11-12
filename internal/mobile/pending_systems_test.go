package mobile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
)

// Someone else's pending application (often named after the applicant, with
// their suburb) must not be listed or fetchable; the applicant still sees
// their own, and approved systems stay public.
func TestPendingSystemsArePrivateToTheApplicant(t *testing.T) {
	conn := hostCompMobileDB(t)
	ctx := context.Background()

	user := func(email string) *AuthUser {
		t.Helper()
		var u AuthUser
		if err := conn.Get(&u, `INSERT INTO users (user_id, name, email, password) VALUES (gen_random_uuid(), 'U', $1, '!')
			RETURNING `+authUserColumns, email); err != nil {
			t.Fatal(err)
		}
		return &u
	}
	system := func(owner int64, name string, approved bool) string {
		t.Helper()
		var uuid string
		if err := conn.QueryRowContext(ctx, `INSERT INTO systems (system_id, user_id, name, suburb, approved_at)
			VALUES (gen_random_uuid(), $1, $2, 'Somewhere NSW', CASE WHEN $3 THEN NOW() END)
			RETURNING system_id::text`, owner, name, approved).Scan(&uuid); err != nil {
			t.Fatal(err)
		}
		return uuid
	}

	applicant, other := user("applicant@example.invalid"), user("other@example.invalid")
	approvedUUID := system(applicant.ID, "Approved Receiver", true)
	pendingUUID := system(applicant.ID, "Jane Citizen", false)

	h := NewHandlers(conn)
	sh := NewSystemHandlers(conn)
	list := func(u *AuthUser) map[string]bool {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/systems", nil).WithContext(context.WithValue(ctx, UserContextKey, u))
		rec := httptest.NewRecorder()
		h.ListSystems(rec, req)
		var rows []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("list: %v (%s)", err, rec.Body)
		}
		names := map[string]bool{}
		for _, r := range rows {
			names[r["name"].(string)] = true
		}
		return names
	}
	get := func(u *AuthUser, uuid string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/systems/"+uuid, nil)
		rc := chi.NewRouteContext()
		rc.URLParams.Add("uuid", uuid)
		req = req.WithContext(context.WithValue(context.WithValue(ctx, chi.RouteCtxKey, rc), UserContextKey, u))
		rec := httptest.NewRecorder()
		sh.GetSystem(rec, req)
		return rec.Code
	}

	if got := list(other); !got["Approved Receiver"] || got["Jane Citizen"] {
		t.Fatalf("other user's list = %v, want only the approved system", got)
	}
	if got := list(applicant); !got["Approved Receiver"] || !got["Jane Citizen"] {
		t.Fatalf("applicant's list = %v, want both", got)
	}
	if code := get(other, pendingUUID); code != http.StatusNotFound {
		t.Fatalf("other user GET pending = %d, want 404", code)
	}
	if code := get(applicant, pendingUUID); code != http.StatusOK {
		t.Fatalf("applicant GET own pending = %d, want 200", code)
	}
	if code := get(other, approvedUUID); code != http.StatusOK {
		t.Fatalf("other user GET approved = %d, want 200", code)
	}
}

// The app's incident screen calls GET /major-incidents/{id}; it 404'd for
// every incident because the route did not exist.
func TestGetMajorIncident(t *testing.T) {
	conn := hostCompMobileDB(t)
	var id int64
	if err := conn.QueryRow(`INSERT INTO majorincidents (upstream_id, title, category, geometry, data, created_at, updated_at)
		VALUES ('guid-1', 'THE HORSLEY DR, HORSLEY PARK', 'Advice', '{}', '{"status":"Under control"}', NOW(), NOW())
		RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Get("/major-incidents/{id}", NewHandlers(conn).GetMajorIncident)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/major-incidents/"+jsonNumber(id), nil))
	var got map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got["title"] != "THE HORSLEY DR, HORSLEY PARK" {
		t.Fatalf("GET existing = %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/major-incidents/999999", "/major-incidents/abc"} {
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }

// Migration 040 moves only rows the Go importer inserted (id > 172573) by
// the Sydney offset in force at the time, and leaves Laravel's rows alone.
func TestMigration040ShiftsGoImportedIncidents(t *testing.T) {
	conn := hostCompMobileDB(t)
	sqlText, err := os.ReadFile("../db/migrations/040_majorincidents_pubdate_utc.sql")
	if err != nil {
		t.Fatal(err)
	}
	// As stored by the buggy importer for pubDate "12/11/2026 2:16:00 AM"
	// (UTC): 02:16 read as Sydney time = 2026-11-11 15:16 UTC.
	if _, err := conn.Exec(`INSERT INTO majorincidents (id, upstream_id, title, category, geometry, data, created_at, updated_at) VALUES
		(100,    'laravel', 'old', 'x', '{}', '{}', '2026-09-01 03:00:00+00', NOW()),
		(172574, 'go',      'new', 'x', '{}', '{}', '2026-11-11 15:16:00+00', NOW())`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(string(sqlText)); err != nil {
		t.Fatal(err)
	}
	var oldAt, newAt string
	if err := conn.QueryRow(`SELECT to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI') FROM majorincidents WHERE id = 100`).Scan(&oldAt); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI') FROM majorincidents WHERE id = 172574`).Scan(&newAt); err != nil {
		t.Fatal(err)
	}
	if oldAt != "2026-09-01 03:00" || newAt != "2026-11-12 02:16" {
		t.Fatalf("laravel row = %s (want unchanged 2026-09-01 03:00), go row = %s (want 2026-11-12 02:16)", oldAt, newAt)
	}
}
