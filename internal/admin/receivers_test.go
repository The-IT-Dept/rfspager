package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// fakeReceiverStore is an in-memory ReceiverStore. System 1 exists; every
// other id is unknown.
type fakeReceiverStore struct {
	revisions  []db.ReceiverConfigRevision // ascending version, system 1
	conn       *db.SystemConnection
	commands   []db.SystemCommand
	captures   []db.SystemCapture
	stats      []db.SystemStatsRow
	statsSince time.Duration
	statsStep  time.Duration
}

func (f *fakeReceiverStore) GetSystemByID(_ context.Context, id int64) (*models.System, error) {
	if id != 1 {
		return nil, sql.ErrNoRows
	}
	return &models.System{ID: 1, Name: "Budgewoi"}, nil
}

func (f *fakeReceiverStore) CurrentReceiverConfig(_ context.Context, _ int64) (int, []byte, bool, error) {
	if len(f.revisions) == 0 {
		return 0, nil, false, nil
	}
	rev := f.revisions[len(f.revisions)-1]
	return rev.Version, rev.Config, true, nil
}

func (f *fakeReceiverStore) GetReceiverConfigRevision(_ context.Context, _ int64, version int) (*db.ReceiverConfigRevision, error) {
	for i := range f.revisions {
		if f.revisions[i].Version == version {
			return &f.revisions[i], nil
		}
	}
	return nil, db.ErrNotFound
}

func (f *fakeReceiverStore) InsertReceiverConfigRevision(_ context.Context, systemID int64, raw []byte, source, note string) (int, error) {
	rev := db.ReceiverConfigRevision{ID: int64(len(f.revisions) + 1), SystemID: systemID, Version: len(f.revisions) + 1, Config: raw, Source: source, CreatedAt: time.Now()}
	if note != "" {
		rev.Note = &note
	}
	f.revisions = append(f.revisions, rev)
	return rev.Version, nil
}

func (f *fakeReceiverStore) ListReceiverConfigRevisions(_ context.Context, _ int64, limit int) ([]db.ReceiverConfigRevision, error) {
	out := []db.ReceiverConfigRevision{}
	for i := len(f.revisions) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, f.revisions[i])
	}
	return out, nil
}

func (f *fakeReceiverStore) GetSystemConnection(_ context.Context, _ int64) (*db.SystemConnection, error) {
	if f.conn == nil {
		return nil, sql.ErrNoRows
	}
	return f.conn, nil
}

func (f *fakeReceiverStore) QuerySystemStats(_ context.Context, _ int64, since, step time.Duration) ([]db.SystemStatsRow, error) {
	f.statsSince, f.statsStep = since, step
	return f.stats, nil
}

func (f *fakeReceiverStore) InsertSystemCommand(_ context.Context, cmd *db.SystemCommand) error {
	cmd.Status, cmd.CreatedAt, cmd.UpdatedAt = db.CommandQueued, time.Now(), time.Now()
	f.commands = append(f.commands, *cmd)
	return nil
}

func (f *fakeReceiverStore) InsertSystemCaptureCommand(ctx context.Context, capture *db.SystemCapture, cmd *db.SystemCommand) error {
	if err := f.InsertSystemCommand(ctx, cmd); err != nil {
		return err
	}
	capture.CommandID = &cmd.ID
	capture.Status, capture.CreatedAt = db.CaptureRequested, time.Now()
	f.captures = append(f.captures, *capture)
	return nil
}

func (f *fakeReceiverStore) ListSystemCommands(_ context.Context, _ int64, limit int) ([]db.SystemCommand, error) {
	out := []db.SystemCommand{}
	for i := len(f.commands) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, f.commands[i])
	}
	return out, nil
}

func (f *fakeReceiverStore) GetSystemCapture(_ context.Context, id string) (*db.SystemCapture, error) {
	for i := range f.captures {
		if f.captures[i].ID == id {
			return &f.captures[i], nil
		}
	}
	return nil, db.ErrNotFound
}

func (f *fakeReceiverStore) ListSystemCaptures(_ context.Context, _ int64, _ int) ([]db.SystemCapture, error) {
	return f.captures, nil
}

type fakeReleases struct {
	version string
	err     error
}

func (f fakeReleases) LatestVersion(context.Context) (string, error) { return f.version, f.err }

// newReceiverTestRouter mounts the receiver routes the way Router does,
// with a signed-in admin injected instead of the JWT middleware.
func newReceiverTestRouter(store ReceiverStore, releases ReleaseLookup) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), AdminUserContextKey, &AdminUser{ID: 1, Email: "ops@example.invalid", Role: "admin"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	rx := newReceiverHandlers(nil, ReceiverDeps{Store: store, Releases: releases})
	r.Route("/systems", func(r chi.Router) { mountReceiverRoutes(r, rx) })
	r.Get("/receivers/latest-version", rx.LatestVersion)
	return r
}

func call(t *testing.T, router http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	var out map[string]any
	if rr.Body.Len() > 0 {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: non-JSON response %q", method, path, rr.Body.String())
		}
	}
	return rr, out
}

func errorsOf(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	errs, ok := out["errors"].(map[string]any)
	if !ok {
		t.Fatalf("response has no errors map: %v", out)
	}
	return errs
}

func TestPutConfigRejectsInvalidValuesWithFieldErrors(t *testing.T) {
	store := &fakeReceiverStore{}
	router := newReceiverTestRouter(store, nil)
	body := `{"config": {"tuner_gain_tenths_db": 900, "log_level": "loud", "sample_rate": 1920001,
	           "channels": [{"label":"RFS","freq_hz":148587500,"enabled":true},{"label":"RFS","freq_hz":148962500,"enabled":true}]}}`
	rr, out := call(t, router, http.MethodPut, "/systems/1/config", body)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	errs := errorsOf(t, out)
	for _, field := range []string{"tuner_gain_tenths_db", "log_level", "sample_rate", "channels[1].label"} {
		if _, ok := errs[field]; !ok {
			t.Errorf("missing error for %s in %v", field, errs)
		}
	}
	if len(store.revisions) != 0 {
		t.Fatal("invalid config was stored")
	}
}

func TestPutConfigStoresNormalisedRevisionWithAdminSource(t *testing.T) {
	store := &fakeReceiverStore{}
	router := newReceiverTestRouter(store, nil)

	// A bare config object: unspecified keys take their defaults.
	rr, out := call(t, router, http.MethodPut, "/systems/1/config", `{"tuner_gain_tenths_db": 280}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if out["version"] != float64(1) || out["source"] != "admin:ops@example.invalid" || out["note"] != nil {
		t.Fatalf("current = %v", out)
	}
	cfg := out["config"].(map[string]any)
	if cfg["tuner_gain_tenths_db"] != float64(280) || cfg["sample_rate"] != float64(1_920_000) || len(cfg["channels"].([]any)) != 2 {
		t.Fatalf("config = %v", cfg)
	}
	var stored rxproto.RuntimeConfig
	if err := json.Unmarshal(store.revisions[0].Config, &stored); err != nil || stored.SampleRate != 1_920_000 || stored.LogLevel != "info" {
		t.Fatalf("stored revision is not the full normalised config: %s (%v)", store.revisions[0].Config, err)
	}

	// The wrapper form carries a note and bumps the version.
	rr, out = call(t, router, http.MethodPut, "/systems/1/config",
		`{"config": {"tuner_gain_tenths_db": 0, "ppm": 3}, "note": "back to AGC, trim ppm"}`)
	if rr.Code != http.StatusOK || out["version"] != float64(2) || out["note"] != "back to AGC, trim ppm" {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPutConfigRejectsMalformedBodies(t *testing.T) {
	store := &fakeReceiverStore{}
	router := newReceiverTestRouter(store, nil)
	for _, body := range []string{"", "{", `{"note": "config missing"}`, `{"config": "not an object"}`, `[1,2]`} {
		rr, _ := call(t, router, http.MethodPut, "/systems/1/config", body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q: status=%d, want 400", body, rr.Code)
		}
	}
	if len(store.revisions) != 0 {
		t.Fatal("malformed body was stored")
	}
	rr, _ := call(t, router, http.MethodPut, "/systems/42/config", `{"ppm": 1}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown system: status=%d", rr.Code)
	}
}

func TestGetConfigShape(t *testing.T) {
	store := &fakeReceiverStore{}
	router := newReceiverTestRouter(store, nil)

	rr, out := call(t, router, http.MethodGet, "/systems/1/config", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if out["current"] != nil || out["applied"] != nil || out["receiver"] != nil {
		t.Fatalf("empty system should have null current/applied/receiver: %v", out)
	}
	if defaults := out["defaults"].(map[string]any); defaults["sample_rate"] != float64(1_920_000) {
		t.Fatalf("defaults = %v", defaults)
	}
	if revs := out["revisions"].([]any); len(revs) != 0 {
		t.Fatalf("revisions = %v", revs)
	}

	for _, body := range []string{`{"ppm": 1}`, `{"ppm": 2}`} {
		if rr, _ := call(t, router, http.MethodPut, "/systems/1/config", body); rr.Code != http.StatusOK {
			t.Fatalf("seed revision: status=%d", rr.Code)
		}
	}
	store.conn = &db.SystemConnection{
		SystemID: 1, LastPingAt: time.Now(), Proto: 2,
		ReceiverVersion: sql.NullString{String: "5.5.0", Valid: true},
		ConfigVersion:   sql.NullInt64{Int64: 2, Valid: true},
		ConfigStatus:    sql.NullString{String: "rejected", Valid: true},
	}
	_, out = call(t, router, http.MethodGet, "/systems/1/config", "")
	current := out["current"].(map[string]any)
	if current["version"] != float64(2) || current["config"].(map[string]any)["ppm"] != float64(2) {
		t.Fatalf("current = %v", current)
	}
	if revs := out["revisions"].([]any); len(revs) != 2 || revs[0].(map[string]any)["version"] != float64(2) {
		t.Fatalf("revisions should be newest first: %v", revs)
	}
	applied := out["applied"].(map[string]any)
	if applied["version"] != float64(2) || applied["status"] != "rejected" {
		t.Fatalf("applied = %v", applied)
	}
	receiver := out["receiver"].(map[string]any)
	if receiver["online"] != true || receiver["proto"] != float64(2) || receiver["version"] != "5.5.0" {
		t.Fatalf("receiver = %v", receiver)
	}

	rr, _ = call(t, router, http.MethodGet, "/systems/42/config", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown system: status=%d", rr.Code)
	}
}

func TestPostCommandValidation(t *testing.T) {
	store := &fakeReceiverStore{}
	router := newReceiverTestRouter(store, nil)
	cases := []struct {
		body   string
		status int
		fields []string
	}{
		{`{"type": "reboot"}`, http.StatusUnprocessableEntity, []string{"type"}},
		{`{"type": "update", "params": {"version": "5.5.0"}}`, http.StatusUnprocessableEntity, []string{"version"}},
		{`{"type": "update", "params": {"version": "main"}}`, http.StatusUnprocessableEntity, []string{"version"}},
		{`{"type": "update"}`, http.StatusUnprocessableEntity, []string{"version"}},
		{`{"type": "capture", "params": {"kind": "audio"}}`, http.StatusUnprocessableEntity, []string{"channel", "seconds"}},
		{`{"type": "capture", "params": {"kind": "iq", "channel": "RFS", "seconds": 31}}`, http.StatusUnprocessableEntity, []string{"seconds"}},
		{`{"type": "capture", "params": {"kind": "audio", "channel": "VHF", "seconds": 5}}`, http.StatusUnprocessableEntity, []string{"channel"}},
		{`{"type": "capture", "params": {"kind": "waterfall"}}`, http.StatusUnprocessableEntity, []string{"kind"}},
		{`{"type": "capture", "params": "nope"}`, http.StatusBadRequest, nil},
		{`{`, http.StatusBadRequest, nil},
	}
	for _, tc := range cases {
		rr, out := call(t, router, http.MethodPost, "/systems/1/commands", tc.body)
		if rr.Code != tc.status {
			t.Errorf("%s: status=%d want %d (%s)", tc.body, rr.Code, tc.status, rr.Body.String())
			continue
		}
		if tc.fields != nil {
			errs := errorsOf(t, out)
			for _, field := range tc.fields {
				if _, ok := errs[field]; !ok {
					t.Errorf("%s: missing error for %s in %v", tc.body, field, errs)
				}
			}
		}
	}
	if len(store.commands) != 0 || len(store.captures) != 0 {
		t.Fatalf("invalid commands were stored: %d commands, %d captures", len(store.commands), len(store.captures))
	}
}

func TestPostCommandQueuesTypedCommands(t *testing.T) {
	store := &fakeReceiverStore{}
	router := newReceiverTestRouter(store, nil)

	rr, out := call(t, router, http.MethodPost, "/systems/1/commands", `{"type": "restart"}`)
	if rr.Code != http.StatusCreated || out["status"] != "queued" || out["requested_by"] != "ops@example.invalid" || out["type"] != "restart" {
		t.Fatalf("restart: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if params := out["params"].(map[string]any); len(params) != 0 {
		t.Fatalf("restart params = %v", params)
	}

	rr, out = call(t, router, http.MethodPost, "/systems/1/commands", `{"type": "update", "params": {"version": " v5.5.0 "}}`)
	if rr.Code != http.StatusCreated || out["params"].(map[string]any)["version"] != "v5.5.0" {
		t.Fatalf("update: status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr, out = call(t, router, http.MethodPost, "/systems/1/commands", `{"type": "capture", "params": {"kind": "spectrum", "channel": "ignored", "seconds": 99}}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("spectrum: status=%d body=%s", rr.Code, rr.Body.String())
	}
	capture := out["capture"].(map[string]any)
	params := out["params"].(map[string]any)
	if capture["kind"] != "spectrum" || capture["channel"] != nil || capture["seconds"] != nil || capture["status"] != "requested" {
		t.Fatalf("spectrum capture = %v", capture)
	}
	if params["capture_id"] != capture["id"] || params["kind"] != "spectrum" || params["seconds"] != nil || params["channel"] != nil {
		t.Fatalf("spectrum params = %v", params)
	}
	if len(store.captures) != 1 || *store.captures[0].CommandID != out["id"] {
		t.Fatalf("capture row not linked to its command: %+v", store.captures)
	}

	rr, out = call(t, router, http.MethodPost, "/systems/1/commands", `{"type": "capture", "params": {"kind": "audio", "channel": "RFS", "seconds": 10}}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("audio: status=%d body=%s", rr.Code, rr.Body.String())
	}
	capture = out["capture"].(map[string]any)
	if capture["channel"] != "RFS" || capture["seconds"] != float64(10) {
		t.Fatalf("audio capture = %v", capture)
	}

	rr, out = call(t, router, http.MethodGet, "/systems/1/commands?limit=2", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("list: status=%d", rr.Code)
	}
	data := out["data"].([]any)
	if len(data) != 2 || data[0].(map[string]any)["type"] != "capture" {
		t.Fatalf("list = %v", data)
	}
	rr, out = call(t, router, http.MethodGet, "/systems/1/captures", "")
	if rr.Code != http.StatusOK || len(out["data"].([]any)) != 2 {
		t.Fatalf("captures: status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGetStatsClampsRangeAndShapesRows(t *testing.T) {
	at := time.Date(2026, 10, 13, 9, 30, 0, 0, time.UTC)
	store := &fakeReceiverStore{stats: []db.SystemStatsRow{{TS: at, Stats: []byte(`{"interval_s":15,"channels":[{"label":"RFS","snr_db":12.5}]}`)}}}
	router := newReceiverTestRouter(store, nil)

	rr, out := call(t, router, http.MethodGet, "/systems/1/stats", "")
	if rr.Code != http.StatusOK || store.statsSince != 6*time.Hour || store.statsStep != time.Minute {
		t.Fatalf("defaults: status=%d since=%s step=%s", rr.Code, store.statsSince, store.statsStep)
	}
	data := out["data"].([]any)
	row := data[0].(map[string]any)
	if row["ts"] != float64(at.UnixMilli()) || row["stats"].(map[string]any)["interval_s"] != float64(15) {
		t.Fatalf("row = %v", row)
	}

	rr, out = call(t, router, http.MethodGet, "/systems/1/stats?since=30d&step=0s", "")
	if rr.Code != http.StatusOK || store.statsSince != 7*24*time.Hour || store.statsStep != time.Second {
		t.Fatalf("clamped: status=%d since=%s step=%s", rr.Code, store.statsSince, store.statsStep)
	}
	if out["since"] != "168h0m0s" || out["step"] != "1s" {
		t.Fatalf("echoed range = %v / %v", out["since"], out["step"])
	}

	rr, _ = call(t, router, http.MethodGet, "/systems/1/stats?since=yesterday", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad since: status=%d", rr.Code)
	}
}

func TestLatestVersionEndpoint(t *testing.T) {
	rr, out := call(t, newReceiverTestRouter(&fakeReceiverStore{}, fakeReleases{version: "v5.4.0"}), http.MethodGet, "/receivers/latest-version", "")
	if rr.Code != http.StatusOK || out["version"] != "v5.4.0" {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	rr, _ = call(t, newReceiverTestRouter(&fakeReceiverStore{}, nil), http.MethodGet, "/receivers/latest-version", "")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured: status=%d", rr.Code)
	}
	rr, _ = call(t, newReceiverTestRouter(&fakeReceiverStore{}, fakeReleases{err: errors.New("github down")}), http.MethodGet, "/receivers/latest-version", "")
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("upstream error: status=%d", rr.Code)
	}
}
