package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/the-it-dept/rfspager/internal/coverage"
)

// The coverage endpoints' request parsing and status codes, against a real
// schema, plus the system location columns the coverage map relies on.
func TestCoverageEndpoints(t *testing.T) {
	conn := messageDetailTestDB(t)
	ctx := context.Background()
	user := insertID(t, conn, `INSERT INTO users (user_id, email, password) VALUES (gen_random_uuid(), 'o@example.invalid', '!') RETURNING id`)
	sys := insertID(t, conn, `INSERT INTO systems (system_id, user_id, name, approved_at, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, 'Budgewoi', NOW(), NOW(), NOW()) RETURNING id`, user)
	agency := insertID(t, conn, `INSERT INTO agencies (agency) VALUES ('Gosford') RETURNING id`)
	mustExec(t, conn, `INSERT INTO paging_transmitters (id, sdd_id, channel, freq_hz, licence_no, licensee, acma_site_id, name, lat, lng)
		VALUES (7, 1, 'RFS', 148587500, 'L1', 'NSW RURAL FIRE SERVICE', 'S1', 'Kincumber', -33.46, 151.39)`)

	// System location: PUT /systems/{id} passes lat, lng, located_since and
	// GET returns them; a cleared form field becomes NULL.
	store := NewStore(conn)
	if err := store.UpdateSystem(ctx, sys, map[string]any{"lat": -33.23, "lng": 151.56, "located_since": "2026-10-06T22:53:00Z"}); err != nil {
		t.Fatalf("update system location: %v", err)
	}
	row, err := store.GetSystem(ctx, sys)
	if err != nil {
		t.Fatal(err)
	}
	if row["lat"] != -33.23 || row["lng"] != 151.56 || row["located_since"] == nil {
		t.Errorf("system row lat=%v lng=%v located_since=%v", row["lat"], row["lng"], row["located_since"])
	}
	if err := store.UpdateSystem(ctx, sys, map[string]any{"located_since": ""}); err != nil {
		t.Fatalf("clear located_since: %v", err)
	}
	if err := store.UpdateAgency(ctx, agency, map[string]any{"coverage_district": false}); err != nil {
		t.Fatalf("update coverage_district: %v", err)
	}
	mustExec(t, conn, `UPDATE agencies SET coverage_district = true`)

	r := chi.NewRouter()
	mountCoverageRoutes(r, coverage.NewService(conn, coverage.Options{ImportDisabled: true}))
	do := func(method, target, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	code, body := do("GET", "/coverage?days=120&channel=RFS", "")
	if code != http.StatusOK {
		t.Fatalf("GET coverage = %d %v", code, body)
	}
	if w := body["window"].(map[string]any); w["days"] != float64(120) {
		t.Errorf("window = %v", w)
	}
	if rx := body["receivers"].([]any); len(rx) != 1 {
		t.Errorf("receivers = %v", rx)
	}
	if tx := body["transmitters"].([]any); len(tx) != 1 || tx[0].(map[string]any)["status"] != "unmapped" {
		t.Errorf("transmitters = %v", tx)
	}
	if ds := body["districts"].([]any); len(ds) != 1 {
		t.Errorf("districts = %v", ds)
	}
	for _, bad := range []string{"/coverage?days=0x", "/coverage?days=400", "/coverage?channel=VHF", "/coverage?system_id=-1"} {
		if code, _ := do("GET", bad, ""); code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", bad, code)
		}
	}

	for _, tc := range []struct {
		target, body string
		want         int
	}{
		{"/coverage/transmitters/7", `{"agency_id": ` + itoa(agency) + `}`, http.StatusOK},
		{"/coverage/transmitters/7", `{"agency_id": null}`, http.StatusOK},
		{"/coverage/transmitters/7", `{"auto": true}`, http.StatusOK},
		{"/coverage/transmitters/7", `{"auto": false}`, http.StatusBadRequest},
		{"/coverage/transmitters/7", `{"agency_id": "x"}`, http.StatusBadRequest},
		{"/coverage/transmitters/7", `{"agency_id": 99999}`, http.StatusUnprocessableEntity},
		{"/coverage/transmitters/7", `{}`, http.StatusBadRequest},
		{"/coverage/transmitters/7", `not json`, http.StatusBadRequest},
		{"/coverage/transmitters/8", `{"agency_id": null}`, http.StatusNotFound},
		{"/coverage/transmitters/x", `{"agency_id": null}`, http.StatusBadRequest},
	} {
		if code, body := do("PUT", tc.target, tc.body); code != tc.want {
			t.Errorf("PUT %s %s = %d %v, want %d", tc.target, tc.body, code, body, tc.want)
		}
	}
}

func itoa(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
