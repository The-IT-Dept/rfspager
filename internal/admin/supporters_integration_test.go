package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

func supporterRequest(t *testing.T, handler http.HandlerFunc, admin *AdminUser, method string, systemID int64, userID int64, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/", &buf)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.FormatInt(systemID, 10))
	if userID != 0 {
		rctx.URLParams.Add("userId", strconv.FormatInt(userID, 10))
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if admin != nil {
		ctx = context.WithValue(ctx, AdminUserContextKey, admin)
	}
	rec := httptest.NewRecorder()
	handler(rec, req.WithContext(ctx))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func listSupporters(t *testing.T, h *supporterHandlers, systemID int64) []Supporter {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.FormatInt(systemID, 10))
	rec := httptest.NewRecorder()
	h.List(rec, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Data []Supporter `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Data
}

// gone2 adds another approved receiver owned by owner.
func gone2(t *testing.T, conn *sqlx.DB, owner int64) int64 {
	t.Helper()
	return insertID(t, conn, `INSERT INTO systems (system_id, user_id, name, approved_at, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, 'Second', NOW() - INTERVAL '10 days', NOW(), NOW()) RETURNING id`, owner)
}

type hostComp struct {
	Since *time.Time `db:"host_comp_since"`
	Until *time.Time `db:"host_comp_until"`
}

func hostCompOf(t *testing.T, conn *sqlx.DB, userID int64) hostComp {
	t.Helper()
	var c hostComp
	if err := conn.Get(&c, `SELECT host_comp_since, host_comp_until FROM users WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	return c
}

// Supporters are added by email with a note, listed with their Pro status,
// and removed; removing one ends their free Pro unless they still host or
// support a receiver.
func TestSystemSupporters(t *testing.T) {
	conn := messageDetailTestDB(t)
	adminID := insertID(t, conn, `INSERT INTO admin_users (email, password_hash, name, role) VALUES ('nick@example.invalid', '!', 'Nick', 'superadmin') RETURNING id`)
	admin := &AdminUser{ID: adminID, Email: "nick@example.invalid", Role: "superadmin"}
	user := func(name, email string) int64 {
		return insertID(t, conn, `INSERT INTO users (user_id, name, email, password) VALUES (gen_random_uuid(), $1, $2, '!') RETURNING id`, name, email)
	}
	system := func(owner int64, name string, deleted bool) int64 {
		return insertID(t, conn, `INSERT INTO systems (system_id, user_id, name, approved_at, deleted_at, created_at, updated_at)
			VALUES (gen_random_uuid(), $1, $2, NOW() - INTERVAL '10 days', CASE WHEN $3 THEN NOW() END, NOW(), NOW()) RETURNING id`, owner, name, deleted)
	}
	owner := user("Alex", "alex@example.invalid")
	hilltop := system(owner, "Hilltop Brigade", false)
	gone := system(owner, "Old", true)
	ownerless := insertID(t, conn, `INSERT INTO systems (system_id, user_id, name, approved_at, created_at, updated_at)
		VALUES (gen_random_uuid(), NULL, 'Ownerless', NOW(), NOW(), NOW()) RETURNING id`)
	sam := user("Sam", "sam@example.invalid")
	// Same address in another case: the exact address wins.
	user("Sam (old)", "SAM@example.invalid")
	h := newSupporterHandlers(conn)

	// Validation.
	for name, tc := range map[string]struct {
		system int64
		body   map[string]any
		want   int
	}{
		"blank note":     {hilltop, map[string]any{"email": "sam@example.invalid", "note": " "}, http.StatusBadRequest},
		"no email":       {hilltop, map[string]any{"email": "", "note": "x"}, http.StatusBadRequest},
		"unknown email":  {hilltop, map[string]any{"email": "nobody@example.invalid", "note": "x"}, http.StatusNotFound},
		"the owner":      {hilltop, map[string]any{"email": "alex@example.invalid", "note": "x"}, http.StatusConflict},
		"deleted system": {gone, map[string]any{"email": "sam@example.invalid", "note": "x"}, http.StatusNotFound},
		"no such system": {999999, map[string]any{"email": "sam@example.invalid", "note": "x"}, http.StatusNotFound},
		// The job only comps receivers with an owner, so this would do nothing.
		"no owner": {ownerless, map[string]any{"email": "sam@example.invalid", "note": "x"}, http.StatusConflict},
		// The limit is characters, as the form counts them, not bytes.
		"1001-character note": {hilltop, map[string]any{"email": "sam@example.invalid", "note": strings.Repeat("é", 1001)}, http.StatusBadRequest},
	} {
		if code, out := supporterRequest(t, h.Add, admin, http.MethodPost, tc.system, 0, tc.body); code != tc.want {
			t.Errorf("%s: %d %v, want %d", name, code, out, tc.want)
		}
	}
	if got := listSupporters(t, h, hilltop); len(got) != 0 {
		t.Fatalf("rejected adds left rows: %+v", got)
	}

	// A 1000-character note fits however many bytes it takes.
	jules := user("Jules", "jules@example.invalid")
	if code, out := supporterRequest(t, h.Add, admin, http.MethodPost, gone2(t, conn, owner), 0, map[string]any{"email": "jules@example.invalid", "note": strings.Repeat("é", 1000)}); code != http.StatusCreated {
		t.Fatalf("1000-character note: %d %v", code, out)
	}

	// Add, case-insensitively, and the exact address is the account added.
	code, out := supporterRequest(t, h.Add, admin, http.MethodPost, hilltop, 0, map[string]any{"email": " sam@example.invalid ", "note": "Co-funded the gear"})
	if code != http.StatusCreated || out["user_id"] != float64(sam) || out["email"] != "sam@example.invalid" {
		t.Fatalf("add: %d %v", code, out)
	}
	if code, out := supporterRequest(t, h.Add, admin, http.MethodPost, hilltop, 0, map[string]any{"email": "sam@example.invalid", "note": "again"}); code != http.StatusConflict ||
		!strings.Contains(out["error"].(string), "already") {
		t.Fatalf("duplicate add: %d %v", code, out)
	}
	// Two accounts match in a third case: refused rather than guessed.
	if code, out := supporterRequest(t, h.Add, admin, http.MethodPost, hilltop, 0, map[string]any{"email": "Sam@Example.invalid", "note": "x"}); code != http.StatusConflict ||
		!strings.Contains(out["error"].(string), "exactly") {
		t.Fatalf("ambiguous add: %d %v", code, out)
	}
	got := listSupporters(t, h, hilltop)
	if len(got) != 1 || got[0].UserID != sam || got[0].Name != "Sam" || got[0].Note != "Co-funded the gear" ||
		got[0].AdminEmail == nil || *got[0].AdminEmail != "nick@example.invalid" {
		t.Fatalf("list = %+v", got)
	}

	// The job comps him; removing him ends it now, since he has no receiver
	// of his own and supports no other.
	mustExec(t, conn, `UPDATE users SET host_comp_since = NOW() - INTERVAL '1 day', host_comp_until = NOW() + INTERVAL '6 days' WHERE id = $1`, sam)
	before := time.Now()
	code, out = supporterRequest(t, h.Remove, admin, http.MethodDelete, hilltop, sam, nil)
	if code != http.StatusOK || out["comp_ended"] != true {
		t.Fatalf("remove: %d %v", code, out)
	}
	if c := hostCompOf(t, conn, sam); c.Since != nil || c.Until == nil || c.Until.After(time.Now()) || c.Until.Before(before.Add(-time.Minute)) {
		t.Fatalf("comp after removal = %+v, want ended now", c)
	}
	if got := listSupporters(t, h, hilltop); len(got) != 0 {
		t.Fatalf("still listed: %+v", got)
	}
	if code, _ := supporterRequest(t, h.Remove, admin, http.MethodDelete, hilltop, sam, nil); code != http.StatusNotFound {
		t.Fatalf("removing a non-supporter: %d", code)
	}

	// Someone who hosts a receiver of their own keeps their comp: the job
	// decides it from that receiver.
	host := user("Jo", "jo@example.invalid")
	system(host, "Riverside", false)
	// The only account with that address, in any case, is the one added.
	if code, out := supporterRequest(t, h.Add, admin, http.MethodPost, hilltop, 0, map[string]any{"email": "Jo@Example.invalid", "note": "x"}); code != http.StatusCreated || out["user_id"] != float64(host) {
		t.Fatalf("add host: %d %v", code, out)
	}
	mustExec(t, conn, `UPDATE users SET host_comp_since = NOW() - INTERVAL '1 day', host_comp_until = NOW() + INTERVAL '6 days' WHERE id = $1`, host)
	code, out = supporterRequest(t, h.Remove, admin, http.MethodDelete, hilltop, host, nil)
	if code != http.StatusOK || out["comp_ended"] != false {
		t.Fatalf("remove host: %d %v", code, out)
	}
	if c := hostCompOf(t, conn, host); c.Since == nil || c.Until == nil || !c.Until.After(time.Now()) {
		t.Fatalf("host's own comp was ended: %+v", c)
	}

	// Jules still supports another approved receiver: removing them from
	// Hilltop leaves the comp to the job.
	if code, out := supporterRequest(t, h.Add, admin, http.MethodPost, hilltop, 0, map[string]any{"email": "jules@example.invalid", "note": "x"}); code != http.StatusCreated {
		t.Fatalf("add jules: %d %v", code, out)
	}
	mustExec(t, conn, `UPDATE users SET host_comp_since = NOW() - INTERVAL '1 day', host_comp_until = NOW() + INTERVAL '6 days' WHERE id = $1`, jules)
	code, out = supporterRequest(t, h.Remove, admin, http.MethodDelete, hilltop, jules, nil)
	if code != http.StatusOK || out["comp_ended"] != false {
		t.Fatalf("remove jules: %d %v", code, out)
	}
	if c := hostCompOf(t, conn, jules); c.Since == nil || !c.Until.After(time.Now()) {
		t.Fatalf("comp from the other receiver was ended: %+v", c)
	}
}

// The routes exist under /systems and are superadmin only: supporters are
// people's accounts and free Pro.
func TestSupporterRoutes(t *testing.T) {
	conn := messageDetailTestDB(t)
	for role, want := range map[string]int{"admin": http.StatusForbidden, "superadmin": http.StatusNotFound} {
		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), AdminUserContextKey, &AdminUser{ID: 1, Role: role})))
			})
		})
		r.Route("/systems", func(r chi.Router) {
			mountSupporterRoutes(r, newSupporterHandlers(conn), NewAuthHandler(nil, "test-secret").RequireSuperadmin)
		})
		for _, req := range []*http.Request{
			httptest.NewRequest(http.MethodGet, "/systems/999/supporters", nil),
			httptest.NewRequest(http.MethodPost, "/systems/999/supporters", strings.NewReader(`{"email":"a@example.invalid","note":"x"}`)),
			httptest.NewRequest(http.MethodDelete, "/systems/999/supporters/5", nil),
		} {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != want {
				t.Errorf("%s %s as %s = %d, want %d", req.Method, req.URL.Path, role, w.Code, want)
			}
		}
	}
}
