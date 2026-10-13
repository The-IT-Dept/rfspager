package recvhub

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

func captureRouter(database *db.DB, dir string) http.Handler {
	r := chi.NewRouter()
	r.Put("/api/receiver/captures/{id}", CaptureUploadHandler(database, dir))
	return r
}

func putCapture(router http.Handler, id, apiKey string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/receiver/captures/"+id, bytes.NewReader(body))
	if apiKey != "" {
		req.Header.Set("apikey", apiKey)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func TestCaptureUploadStoresFileAndMarksReady(t *testing.T) {
	_, database, sys := testHub(t)
	ctx := context.Background()
	dir := t.TempDir()
	router := captureRouter(database, dir)

	channel := "RFS"
	capture := &db.SystemCapture{ID: uuid.NewString(), SystemID: sys.ID, Kind: rxproto.CaptureAudio, Channel: &channel, RequestedBy: "admin:x"}
	if err := database.InsertSystemCapture(ctx, capture); err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte{1, 2, 3, 4}, 256)
	rr := putCapture(router, capture.ID, "key-budgewoi", body, map[string]string{
		"X-Capture-Kind": "audio", "X-Capture-Rate": "38400", "X-Capture-Channel": "RFS",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	got, err := database.GetSystemCapture(ctx, capture.ID)
	if err != nil || got.Status != db.CaptureReady || got.SizeBytes == nil || *got.SizeBytes != int64(len(body)) ||
		got.SampleRate == nil || *got.SampleRate != 38400 || got.ReadyAt == nil || got.Path == nil {
		t.Fatalf("capture row = %+v err=%v", got, err)
	}
	wantPath := filepath.Join(dir, strconv.FormatInt(sys.ID, 10), capture.ID+".bin")
	if abs, _ := filepath.Abs(wantPath); *got.Path != abs && *got.Path != wantPath {
		t.Fatalf("path = %s, want %s", *got.Path, wantPath)
	}
	stored, err := os.ReadFile(*got.Path)
	if err != nil || !bytes.Equal(stored, body) {
		t.Fatalf("stored file: %v (%d bytes)", err, len(stored))
	}
	if entries, _ := os.ReadDir(filepath.Dir(*got.Path)); len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
	if n, msg := countEvents(t, database, sys.ID, EvtReceiverCaptureReady); n != 1 || !strings.Contains(msg, "audio") {
		t.Fatalf("capture events = %d %q", n, msg)
	}

	// Uploading again is refused; the file is untouched.
	if rr := putCapture(router, capture.ID, "key-budgewoi", []byte("x"), nil); rr.Code != http.StatusConflict {
		t.Fatalf("second upload status=%d", rr.Code)
	}

	// Spectrum captures land as .json and need no channel.
	spectrum := &db.SystemCapture{ID: uuid.NewString(), SystemID: sys.ID, Kind: rxproto.CaptureSpectrum, RequestedBy: "admin:x"}
	if err := database.InsertSystemCapture(ctx, spectrum); err != nil {
		t.Fatal(err)
	}
	rr = putCapture(router, spectrum.ID, "key-budgewoi", []byte(`{"centre_hz":148775000,"sample_rate":1920000,"bins_db":[-80]}`), map[string]string{"X-Capture-Rate": "1920000"})
	if rr.Code != http.StatusOK {
		t.Fatalf("spectrum status=%d body=%s", rr.Code, rr.Body.String())
	}
	got, _ = database.GetSystemCapture(ctx, spectrum.ID)
	if got.Path == nil || !strings.HasSuffix(*got.Path, spectrum.ID+".json") || got.Channel != nil || *got.SampleRate != 1_920_000 {
		t.Fatalf("spectrum row = %+v", got)
	}
}

func TestCaptureUploadRejectsBadRequests(t *testing.T) {
	_, database, sys := testHub(t)
	ctx := context.Background()
	router := captureRouter(database, t.TempDir())

	capture := &db.SystemCapture{ID: uuid.NewString(), SystemID: sys.ID, Kind: rxproto.CaptureIQ, RequestedBy: "admin:x"}
	if err := database.InsertSystemCapture(ctx, capture); err != nil {
		t.Fatal(err)
	}
	other := seedHubSystem(t, database, "figtree")
	foreign := &db.SystemCapture{ID: uuid.NewString(), SystemID: other.ID, Kind: rxproto.CaptureIQ, RequestedBy: "admin:x"}
	if err := database.InsertSystemCapture(ctx, foreign); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		id, key string
		body    []byte
		headers map[string]string
		status  int
	}{
		{"no key", capture.ID, "", []byte("x"), nil, http.StatusUnauthorized},
		{"bad key", capture.ID, "nope", []byte("x"), nil, http.StatusUnauthorized},
		{"malformed id", "not-a-uuid", "key-budgewoi", []byte("x"), nil, http.StatusBadRequest},
		{"unknown id", uuid.NewString(), "key-budgewoi", []byte("x"), nil, http.StatusNotFound},
		{"another system's capture", foreign.ID, "key-budgewoi", []byte("x"), nil, http.StatusNotFound},
		{"kind mismatch", capture.ID, "key-budgewoi", []byte("x"), map[string]string{"X-Capture-Kind": "audio"}, http.StatusBadRequest},
		{"empty body", capture.ID, "key-budgewoi", nil, nil, http.StatusBadRequest},
	}
	for _, tc := range cases {
		rr := putCapture(router, tc.id, tc.key, tc.body, tc.headers)
		if rr.Code != tc.status {
			t.Errorf("%s: status=%d want %d (%s)", tc.name, rr.Code, tc.status, rr.Body.String())
		}
	}
	if got, _ := database.GetSystemCapture(ctx, capture.ID); got.Status != db.CaptureRequested {
		t.Fatalf("rejected uploads changed the row: %+v", got)
	}
	if got, _ := database.GetSystemCapture(ctx, foreign.ID); got.Status != db.CaptureRequested {
		t.Fatalf("another system's key touched the row: %+v", got)
	}
}

func TestCaptureUploadEnforcesTheSizeCap(t *testing.T) {
	_, database, sys := testHub(t)
	ctx := context.Background()
	dir := t.TempDir()
	router := captureRouter(database, dir)

	captureCap = 16
	t.Cleanup(func() { captureCap = MaxCaptureBytes })

	capture := &db.SystemCapture{ID: uuid.NewString(), SystemID: sys.ID, Kind: rxproto.CaptureAudio, RequestedBy: "admin:x"}
	if err := database.InsertSystemCapture(ctx, capture); err != nil {
		t.Fatal(err)
	}
	rr := putCapture(router, capture.ID, "key-budgewoi", bytes.Repeat([]byte("a"), 17), nil)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got, _ := database.GetSystemCapture(ctx, capture.ID)
	if got.Status != db.CaptureError || got.Error == nil || !strings.Contains(*got.Error, "cap") {
		t.Fatalf("row after oversize upload = %+v", got)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, strconv.FormatInt(sys.ID, 10))); len(entries) != 0 {
		t.Fatalf("oversize upload left files: %v", entries)
	}

	// Exactly at the cap is fine.
	exact := &db.SystemCapture{ID: uuid.NewString(), SystemID: sys.ID, Kind: rxproto.CaptureAudio, RequestedBy: "admin:x"}
	if err := database.InsertSystemCapture(ctx, exact); err != nil {
		t.Fatal(err)
	}
	if rr := putCapture(router, exact.ID, "key-budgewoi", bytes.Repeat([]byte("a"), 16), nil); rr.Code != http.StatusOK {
		t.Fatalf("at-cap status=%d body=%s", rr.Code, rr.Body.String())
	}
}
