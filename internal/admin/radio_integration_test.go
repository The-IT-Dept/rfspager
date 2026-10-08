package admin

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestRadioAdminImportAndEdit(t *testing.T) {
	rawURL := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse RFSPAGER_TEST_DATABASE_URL: %v", err)
	}
	if !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "rfspager_test") {
		t.Fatalf("refusing to reset non-test database %q", u.Path)
	}
	db, err := sqlx.Connect("postgres", rawURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public; CREATE TABLE systems (id BIGSERIAL PRIMARY KEY);`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../db/migrations/032_radio.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO radio_talkgroups (sysid, tgid, calls_clear, calls_encrypted, last_seen_at)
	                      VALUES (721, 10047, 12, 3, NOW())`); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(NewStore(db), nil)
	r := chi.NewRouter()
	r.Post("/radio/talkgroups/import", h.ImportRadioTalkgroups)
	r.Put("/radio/talkgroups/{id}", h.UpdateRadioTalkgroup)
	r.Put("/radio/sites/{id}", h.UpdateRadioSite)
	do := func(req *http.Request) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}

	// Raw CSV body, default sysid.
	rr := do(httptest.NewRequest(http.MethodPost, "/radio/talkgroups/import",
		strings.NewReader("tgid,alpha_tag,description,agency,category\n10047,RFS CC A,Central Coast,RFS,Fire\nnope,,,,\n")))
	var res struct {
		Inserted, Updated, Skipped int
		Errors                     []string
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil || rr.Code != http.StatusOK {
		t.Fatalf("import = %d %s", rr.Code, rr.Body)
	}
	if res.Updated != 1 || res.Inserted != 0 || res.Skipped != 1 || len(res.Errors) != 1 {
		t.Errorf("import result = %+v", res)
	}

	// Multipart upload, hex sysid.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "tgs.csv")
	fw.Write([]byte("tgid,alpha_tag\n10050,RFS CC Ops\n"))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/radio/talkgroups/import?sysid=0x2D1", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if rr := do(req); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"inserted":1`) {
		t.Errorf("multipart import = %d %s", rr.Code, rr.Body)
	}

	var tg struct {
		ID        int64  `db:"id"`
		AlphaTag  string `db:"alpha_tag"`
		Clear     int    `db:"calls_clear"`
		Encrypted int    `db:"calls_encrypted"`
	}
	if err := db.Get(&tg, `SELECT id, alpha_tag, calls_clear, calls_encrypted FROM radio_talkgroups WHERE tgid = 10047`); err != nil {
		t.Fatal(err)
	}
	if tg.AlphaTag != "RFS CC A" || tg.Clear != 12 || tg.Encrypted != 3 {
		t.Errorf("after import: %+v; counters must survive", tg)
	}

	put := func(path, body string) int {
		return do(httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))).Code
	}
	if code := put("/radio/talkgroups/"+strconv.FormatInt(tg.ID, 10), `{"hidden": true, "description": ""}`); code != http.StatusOK {
		t.Errorf("edit talkgroup = %d", code)
	}
	var hidden bool
	var desc *string
	if err := db.QueryRow(`SELECT hidden, description FROM radio_talkgroups WHERE id = $1`, tg.ID).Scan(&hidden, &desc); err != nil || !hidden || desc != nil {
		t.Errorf("after edit hidden=%v description=%v (%v); blank should clear", hidden, desc, err)
	}
	if code := put("/radio/talkgroups/"+strconv.FormatInt(tg.ID, 10), `{"calls_clear": 0}`); code != http.StatusUnprocessableEntity {
		t.Errorf("editing a counter = %d, want 422", code)
	}
	if code := put("/radio/sites/999", `{"name": "Nowhere"}`); code != http.StatusNotFound {
		t.Errorf("editing a missing site = %d, want 404", code)
	}
}
