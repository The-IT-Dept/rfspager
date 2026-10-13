package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRelease serves /api/binaries/receiver/<platform>[.sha256]?version=tag
// the way the server's proxy does.
type fakeRelease struct {
	t       *testing.T
	srv     *httptest.Server
	body    []byte
	sum     string // served checksum; defaults to the real one
	mu      sync.Mutex
	queries []string
}

func newFakeRelease(t *testing.T, body []byte) *fakeRelease {
	h := sha256.Sum256(body)
	r := &fakeRelease{t: t, body: body, sum: hex.EncodeToString(h[:])}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("apikey") != "KEY" {
			http.Error(w, "unauthorised", http.StatusUnauthorized)
			return
		}
		r.mu.Lock()
		r.queries = append(r.queries, req.URL.Path+"?"+req.URL.RawQuery)
		r.mu.Unlock()
		if req.URL.Query().Get("version") != "v5.5.0" {
			http.Error(w, "no such release", http.StatusNotFound)
			return
		}
		switch req.URL.Path {
		case "/api/binaries/receiver/linux-arm64":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(r.body)
		case "/api/binaries/receiver/linux-arm64.sha256":
			_, _ = w.Write([]byte(r.sum + "  rfspager-receiver-linux-arm64\n"))
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

type binDir struct {
	dir, exe, prev, next string
}

func newBinDir(t *testing.T) binDir {
	dir := t.TempDir()
	b := binDir{dir: dir, exe: filepath.Join(dir, "receiver"), prev: filepath.Join(dir, "receiver.prev"), next: filepath.Join(dir, "receiver.new")}
	if err := os.WriteFile(b.exe, []byte("OLD BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	return b
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

func TestUpdateSwapsBinaryAndWritesMarker(t *testing.T) {
	rel := newFakeRelease(t, []byte("NEW BINARY"))
	bins := newBinDir(t)
	store := NewStore(t.TempDir())
	u := &Updater{ServerURL: rel.srv.URL + "/", APIKey: "KEY", Platform: "linux-arm64", Executable: bins.exe, Store: store, Version: "5.4.0-abc1234"}
	var stages []string
	err := u.Run(context.Background(), "cmd-1", "v5.5.0", func(stage, msg string) { stages = append(stages, stage) })
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, bins.exe); got != "NEW BINARY" {
		t.Errorf("exe content %q", got)
	}
	if got := readFile(t, bins.prev); got != "OLD BINARY" {
		t.Errorf("prev content %q", got)
	}
	if fi, err := os.Stat(bins.exe); err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("exe mode: %v %v", fi.Mode(), err)
	}
	if _, err := os.Stat(bins.next); !os.IsNotExist(err) {
		t.Errorf("receiver.new left behind")
	}
	if strings.Join(stages, ",") != "downloading,verifying,swapping,restarting" {
		t.Errorf("stages %v", stages)
	}
	m, err := store.LoadMarker()
	if err != nil || m == nil {
		t.Fatalf("marker: %v %v", m, err)
	}
	if m.CommandID != "cmd-1" || m.From != "5.4.0-abc1234" || m.To != "v5.5.0" || m.Boots != 0 || time.Since(m.StartedAt) > time.Minute {
		t.Errorf("marker %+v", m)
	}
	raw, _ := os.ReadFile(filepath.Join(store.Dir(), markerFile))
	var asJSON map[string]any
	if err := json.Unmarshal(raw, &asJSON); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"command_id", "from", "to", "started_at", "boots"} {
		if _, ok := asJSON[k]; !ok {
			t.Errorf("marker file missing %q: %s", k, raw)
		}
	}
	for _, q := range rel.queries {
		if !strings.Contains(q, "version=v5.5.0") {
			t.Errorf("request without version: %s", q)
		}
	}
}

func TestUpdateRefusesBadChecksum(t *testing.T) {
	rel := newFakeRelease(t, []byte("NEW BINARY"))
	rel.sum = strings.Repeat("0", 64)
	bins := newBinDir(t)
	store := NewStore(t.TempDir())
	u := &Updater{ServerURL: rel.srv.URL, APIKey: "KEY", Platform: "linux-arm64", Executable: bins.exe, Store: store}
	err := u.Run(context.Background(), "cmd-2", "v5.5.0", func(string, string) {})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected a checksum error, got %v", err)
	}
	if readFile(t, bins.exe) != "OLD BINARY" {
		t.Error("binary replaced despite a bad checksum")
	}
	if _, err := os.Stat(bins.next); !os.IsNotExist(err) {
		t.Error("receiver.new not cleaned up")
	}
	if m, _ := store.LoadMarker(); m != nil {
		t.Error("marker written for a failed update")
	}
}

func TestUpdateUnknownVersionFails(t *testing.T) {
	rel := newFakeRelease(t, []byte("NEW BINARY"))
	bins := newBinDir(t)
	u := &Updater{ServerURL: rel.srv.URL, APIKey: "KEY", Platform: "linux-arm64", Executable: bins.exe, Store: NewStore(t.TempDir())}
	if err := u.Run(context.Background(), "c", "v0.0.1", func(string, string) {}); err == nil {
		t.Fatal("expected an error for an unknown release")
	}
	if readFile(t, bins.exe) != "OLD BINARY" {
		t.Error("binary replaced")
	}
}

func TestUpdateNeedsStateDir(t *testing.T) {
	u := &Updater{ServerURL: "http://127.0.0.1:1", APIKey: "KEY", Store: NewStore("")}
	if err := u.Run(context.Background(), "c", "v5.5.0", func(string, string) {}); err == nil {
		t.Fatal("update without a state dir must be refused")
	}
}

func TestBootCheckConfirmsAndRollsBack(t *testing.T) {
	bins := newBinDir(t)
	store := NewStore(t.TempDir())

	// No marker: nothing happens.
	if m, rb, err := BootCheck(store, bins.exe, "5.5.0-abc"); m != nil || rb || err != nil {
		t.Fatalf("no marker: %v %v %v", m, rb, err)
	}

	// Stale marker (another version running): removed.
	_ = store.SaveMarker(&UpdateMarker{CommandID: "c", To: "v5.5.0", StartedAt: time.Now()})
	if m, rb, err := BootCheck(store, bins.exe, "5.4.0-abc"); m != nil || rb || err != nil {
		t.Fatalf("stale marker: %v %v %v", m, rb, err)
	}
	if m, _ := store.LoadMarker(); m != nil {
		t.Fatal("stale marker not removed")
	}

	// Matching version: boots count up; the marker is returned so the
	// caller confirms after welcome.
	_ = os.WriteFile(bins.prev, []byte("OLD BINARY"), 0o755)
	_ = os.WriteFile(bins.exe, []byte("NEW BINARY"), 0o755)
	_ = store.SaveMarker(&UpdateMarker{CommandID: "c", From: "5.4.0", To: "v5.5.0", StartedAt: time.Now()})
	for boot := 1; boot <= MaxBootsBeforeRollback; boot++ {
		m, rb, err := BootCheck(store, bins.exe, "5.5.0-abc1234")
		if err != nil || rb || m == nil || m.Boots != boot {
			t.Fatalf("boot %d: m=%+v rb=%v err=%v", boot, m, rb, err)
		}
	}
	// One more inside the window: rollback.
	m, rb, err := BootCheck(store, bins.exe, "5.5.0-abc1234")
	if err != nil || !rb || m != nil {
		t.Fatalf("rollback boot: m=%+v rb=%v err=%v", m, rb, err)
	}
	if readFile(t, bins.exe) != "OLD BINARY" {
		t.Errorf("exe not rolled back: %q", readFile(t, bins.exe))
	}
	if _, err := os.Stat(bins.prev); !os.IsNotExist(err) {
		t.Error("receiver.prev should have been renamed over the exe")
	}
	if m, _ := store.LoadMarker(); m != nil {
		t.Error("marker not removed after rollback")
	}

	// Many boots but outside the window: no rollback (a slow crash loop is
	// not an install problem).
	_ = os.WriteFile(bins.prev, []byte("OLD BINARY"), 0o755)
	_ = os.WriteFile(bins.exe, []byte("NEW BINARY"), 0o755)
	_ = store.SaveMarker(&UpdateMarker{CommandID: "c", To: "v5.5.0", StartedAt: time.Now().Add(-RollbackWindow - time.Minute), Boots: 10})
	if m, rb, err := BootCheck(store, bins.exe, "v5.5.0"); err != nil || rb || m == nil || m.Boots != 11 {
		t.Fatalf("outside window: m=%+v rb=%v err=%v", m, rb, err)
	}
	if readFile(t, bins.exe) != "NEW BINARY" {
		t.Error("rolled back outside the window")
	}

	// Crash loop but nothing to roll back to: marker cleared, keep running.
	os.Remove(bins.prev)
	_ = store.SaveMarker(&UpdateMarker{CommandID: "c", To: "v5.5.0", StartedAt: time.Now(), Boots: 5})
	if m, rb, err := BootCheck(store, bins.exe, "5.5.0"); err != nil || rb || m != nil {
		t.Fatalf("no prev: m=%+v rb=%v err=%v", m, rb, err)
	}
}

func TestBaseVersion(t *testing.T) {
	for in, want := range map[string]string{
		"v5.5.0":                     "5.5.0",
		"5.5.0-abc1234":              "5.5.0",
		"v5.4.0-3-gabc1234-dirty-ab": "5.4.0",
		" 5.5.0 ":                    "5.5.0",
		"abc1234":                    "abc1234",
		"":                           "",
	} {
		if got := BaseVersion(in); got != want {
			t.Errorf("BaseVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetectPlatform(t *testing.T) {
	p := DetectPlatform()
	if !strings.Contains(p, "-") || strings.Contains(p, "arm-") {
		t.Errorf("platform %q", p)
	}
}

func TestBootCheckNeverRollsBackAStableUpdate(t *testing.T) {
	bins := newBinDir(t)
	store := NewStore(t.TempDir())
	_ = os.WriteFile(bins.prev, []byte("OLD BINARY"), 0o755)
	_ = os.WriteFile(bins.exe, []byte("NEW BINARY"), 0o755)

	// The binary ran long enough once (server outage restarts afterwards
	// must not count), so even many boots inside the window keep it.
	m := &UpdateMarker{CommandID: "c", From: "5.4.0", To: "v5.5.0", StartedAt: time.Now(), Boots: 1}
	_ = store.SaveMarker(m)
	if err := store.MarkStable(m); err != nil {
		t.Fatal(err)
	}
	for range MaxBootsBeforeRollback + 3 {
		got, rb, err := BootCheck(store, bins.exe, "5.5.0-abc1234")
		if err != nil || rb || got == nil || got.StableAt.IsZero() {
			t.Fatalf("stable marker: m=%+v rb=%v err=%v", got, rb, err)
		}
	}
	if readFile(t, bins.exe) != "NEW BINARY" {
		t.Error("a stable update was rolled back")
	}

	// MarkStable is a no-op for a marker that no longer exists or belongs to
	// another update, so a late timer cannot resurrect a confirmed marker.
	_ = store.DeleteMarker()
	if err := store.MarkStable(m); err != nil {
		t.Fatalf("MarkStable after delete: %v", err)
	}
	if cur, _ := store.LoadMarker(); cur != nil {
		t.Error("MarkStable recreated a deleted marker")
	}
	_ = store.SaveMarker(&UpdateMarker{CommandID: "other", To: "v5.6.0", StartedAt: time.Now()})
	_ = store.MarkStable(m)
	if cur, _ := store.LoadMarker(); cur == nil || !cur.StableAt.IsZero() {
		t.Error("MarkStable touched another update's marker")
	}
}
