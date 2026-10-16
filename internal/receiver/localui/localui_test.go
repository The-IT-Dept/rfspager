package localui

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/receiver/buildinfo"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

func TestPageStorePersistsAndQueries(t *testing.T) {
	dir := t.TempDir()
	s := NewPageStore(dir)
	now := time.Now().UnixMilli()
	s.Add(Page{TS: now, Address: "0521839", Message: "RFS: GRASS FIRE BUDGEWOI", Posted: true})
	s.Add(Page{TS: now, Address: "0180111", Message: "0180111", Posted: false})
	s.Add(Page{TS: now, Address: "0412003", Message: "FRNSW: AFA LAKE HAVEN", Posted: true})
	s.Close()

	s = NewPageStore(dir) // reload from pages.jsonl
	defer s.Close()
	got, more := s.Query(PageQuery{Limit: 10})
	if len(got) != 2 || more || got[0].Address != "0412003" || got[1].Address != "0521839" {
		t.Fatalf("posted pages newest first, got %+v more=%v", got, more)
	}
	if all, _ := s.Query(PageQuery{Limit: 10, Filtered: true}); len(all) != 3 {
		t.Fatalf("filtered=1 should include the dropped decode, got %d", len(all))
	}
	if hit, _ := s.Query(PageQuery{Limit: 10, Q: "lake haven"}); len(hit) != 1 || hit[0].Address != "0412003" {
		t.Fatalf("search by message, got %+v", hit)
	}
	if hit, _ := s.Query(PageQuery{Limit: 10, Q: "05218"}); len(hit) != 1 {
		t.Fatalf("search by capcode, got %+v", hit)
	}
	page1, more := s.Query(PageQuery{Limit: 1})
	if !more {
		t.Fatal("expected more after the first page")
	}
	page2, _ := s.Query(PageQuery{Limit: 1, Before: page1[0].ID})
	if len(page2) != 1 || page2[0].Address != "0521839" {
		t.Fatalf("before= paging, got %+v", page2)
	}
	if p := s.Add(Page{TS: now, Posted: true}); p.ID != 4 {
		t.Fatalf("ids continue after reload, got %d", p.ID)
	}
	if n, last := s.Today(); n != 3 || last != now {
		t.Fatalf("today = %d last %d, want 3 posted", n, last)
	}
}

func TestPageStoreCompacts(t *testing.T) {
	dir := t.TempDir()
	s := NewPageStore(dir)
	for i := 0; i < pagesCompactAt+10; i++ {
		s.Add(Page{TS: int64(i), Posted: true})
	}
	s.Close()
	f, err := os.Open(filepath.Join(dir, pagesFile))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lines := 0
	for sc := bufio.NewScanner(f); sc.Scan(); {
		lines++
	}
	if lines != pagesKeep+10 {
		t.Fatalf("file has %d lines, want %d after one compaction", lines, pagesKeep+10)
	}
}

func TestStatsWindowMergesBuckets(t *testing.T) {
	var h StatsHistory
	base := time.Now().Add(-6 * time.Hour)
	n := maxStatsPoints * 2
	for i := 0; i < n; i++ {
		h.Add(StatsPoint{TS: base.Add(time.Duration(i) * 15 * time.Second).UnixMilli(), Stats: rxproto.Stats{
			IntervalS: 15,
			Dongle:    rxproto.DongleStats{DSPLoad: float64(i % 2)},
			Channels:  []rxproto.ChannelStats{{Label: "RFS", SNRDB: float64(10 + 10*(i%2)), Pages: 1}},
		}})
	}
	pts, interval := h.Window(time.Now(), 24*time.Hour)
	if len(pts) != maxStatsPoints || interval != 30 {
		t.Fatalf("got %d points at %d s, want %d at 30 s", len(pts), interval, maxStatsPoints)
	}
	c := pts[0].Channels[0]
	if c.Pages != 2 || c.SNRDB != 15 || pts[0].Dongle.DSPLoad != 0.5 {
		t.Fatalf("counters summed and gauges averaged, got pages=%d snr=%v load=%v", c.Pages, c.SNRDB, pts[0].Dongle.DSPLoad)
	}
	if few, _ := h.Window(time.Now(), time.Hour); len(few) < 239 || len(few) > 240 {
		t.Fatalf("1h window should be raw, got %d", len(few))
	}
}

func TestLogBufferCapturesAttrs(t *testing.T) {
	b := NewLogBuffer(3)
	var seen []LogEntry
	b.OnLog(func(e LogEntry) { seen = append(seen, e) })
	log := slog.New(b.Handler(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log = log.With("component", "dsp")
	log.Debug("one")
	log.Info("two", "err", io.EOF)
	log.WithGroup("g").Warn("three", "k", 1)
	log.Error("four")
	got := b.Recent(10, slog.LevelDebug)
	if len(got) != 3 || got[0].Message != "four" || got[2].Message != "two" {
		t.Fatalf("ring of 3, newest first: %+v", got)
	}
	if got[2].Attrs["err"] != "EOF" || got[2].Attrs["component"] != "dsp" {
		t.Fatalf("attrs: %+v", got[2].Attrs)
	}
	if got[1].Attrs["g.k"] != int64(1) {
		t.Fatalf("grouped attr: %+v", got[1].Attrs)
	}
	if warn := b.Recent(10, slog.LevelWarn); len(warn) != 2 {
		t.Fatalf("level filter: %+v", warn)
	}
	if len(seen) != 4 {
		t.Fatalf("OnLog saw %d records", len(seen))
	}
}

type sink struct {
	mu     sync.Mutex
	bodies [][]byte
	sigs   []string
	events []string
	fail   int // fail this many requests first
}

func (s *sink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail > 0 {
		s.fail--
		http.Error(w, "down", 503)
		return
	}
	s.bodies = append(s.bodies, b)
	s.sigs = append(s.sigs, r.Header.Get("X-RFSPager-Signature"))
	s.events = append(s.events, r.Header.Get("X-RFSPager-Event"))
}

func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := New(ctx, ":0", t.TempDir(), buildinfo.Info{Version: "5.9.0-test", Hostname: "rx-test"}, NewLogBuffer(100), Sources{
		System: func() *System { return &System{Name: "Budgewoi"} },
	})
	s.Webhooks.retries = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func doJSON(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, url, rd)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestWebhookDeliveryAndSignature(t *testing.T) {
	snk := &sink{fail: 2} // two failures, then the third attempt lands
	hook := httptest.NewServer(snk)
	defer hook.Close()
	s, ts := newTestServer(t)

	code, created := doJSON(t, "POST", ts.URL+"/api/webhooks", map[string]any{"name": "nr", "url": hook.URL, "secret": "s3cret"})
	if code != 201 || created["has_secret"] != true || created["secret"] != nil {
		t.Fatalf("create: %d %+v", code, created)
	}
	s.AddPage(Page{TS: time.Now().UnixMilli(), Address: "0180111", Posted: false}) // filtered: not delivered
	s.AddPage(Page{TS: time.Now().UnixMilli(), Address: "0521839", Message: "RFS: TEST", Channel: "RFS", Posted: true})
	waitFor(t, "delivery", func() bool { return snk.count() == 1 })

	snk.mu.Lock()
	body, sig := snk.bodies[0], snk.sigs[0]
	snk.mu.Unlock()
	if sig != Sign("s3cret", body) {
		t.Fatalf("signature %q does not match body", sig)
	}
	var p struct {
		Event    string   `json:"event"`
		Receiver Identity `json:"receiver"`
		Page     struct {
			Address string `json:"address"`
			Message string `json:"message"`
		} `json:"page"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	if p.Event != "page" || p.Page.Address != "0521839" || p.Receiver.System != "Budgewoi" {
		t.Fatalf("payload: %s", body)
	}
	v := s.Webhooks.List()[0]
	if v.State != "ok" || v.Sent != 1 || v.Failed != 0 {
		t.Fatalf("after retries succeeded: %+v", v)
	}
}

func TestWebhookFailsAfterRetries(t *testing.T) {
	snk := &sink{fail: 100}
	hook := httptest.NewServer(snk)
	defer hook.Close()
	s, ts := newTestServer(t)
	doJSON(t, "POST", ts.URL+"/api/webhooks", map[string]any{"url": hook.URL})
	s.AddPage(Page{TS: time.Now().UnixMilli(), Address: "0521839", Posted: true})
	waitFor(t, "failure", func() bool { return s.Webhooks.List()[0].Failed == 1 })
	v := s.Webhooks.List()[0]
	if v.State != "failing" || v.LastStatus != 503 || v.LastError != "HTTP 503" {
		t.Fatalf("failing webhook: %+v", v)
	}
	if _, _, failing := s.Webhooks.Summary(); failing != 1 {
		t.Fatal("summary should count the failing webhook")
	}
}

func TestWebhookAPI(t *testing.T) {
	snk := &sink{}
	hook := httptest.NewServer(snk)
	defer hook.Close()
	s, ts := newTestServer(t)

	// Not JSON: refused, so a cross-site form cannot add a webhook.
	resp, err := http.Post(ts.URL+"/api/webhooks", "text/plain", strings.NewReader(`{"url":"http://x"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 415 {
		t.Fatalf("text/plain POST: %d", resp.StatusCode)
	}

	code, body := doJSON(t, "POST", ts.URL+"/api/webhooks", map[string]any{"url": "ftp://nope"})
	if code != 422 || body["errors"].(map[string]any)["url"] == nil {
		t.Fatalf("bad url: %d %+v", code, body)
	}

	_, created := doJSON(t, "POST", ts.URL+"/api/webhooks", map[string]any{"url": hook.URL, "secret": "a"})
	id := created["id"].(string)

	// Update without secret keeps it; "" clears it.
	code, upd := doJSON(t, "PUT", ts.URL+"/api/webhooks/"+id, map[string]any{"name": "renamed", "url": hook.URL, "enabled": true})
	if code != 200 || upd["has_secret"] != true || upd["name"] != "renamed" {
		t.Fatalf("update keeping secret: %d %+v", code, upd)
	}
	_, upd = doJSON(t, "PUT", ts.URL+"/api/webhooks/"+id, map[string]any{"url": hook.URL, "secret": ""})
	if upd["has_secret"] != false {
		t.Fatalf("secret should be cleared: %+v", upd)
	}

	code, res := doJSON(t, "POST", ts.URL+"/api/webhooks/"+id+"/test", map[string]any{})
	if code != 200 || res["ok"] != true || res["status"] != float64(200) {
		t.Fatalf("test: %d %+v", code, res)
	}
	snk.mu.Lock()
	if snk.events[0] != "test" {
		t.Fatalf("test event header: %v", snk.events)
	}
	snk.mu.Unlock()

	// Persisted with the secret, 0600.
	fi, err := os.Stat(s.Webhooks.path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("webhooks.json: %v %v", fi, err)
	}

	if code, _ := doJSON(t, "DELETE", ts.URL+"/api/webhooks/"+id, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := doJSON(t, "DELETE", ts.URL+"/api/webhooks/"+id, nil); code != 404 {
		t.Fatalf("second delete: %d", code)
	}
}

func TestWebhooksReloadFromDisk(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := func() Identity { return Identity{} }
	w := NewWebhooks(ctx, dir, id)
	f := false
	if _, err := w.Create(WebhookInput{Name: "a", URL: "http://example.invalid/x", Enabled: &f}); err != nil {
		t.Fatal(err)
	}
	w2 := NewWebhooks(ctx, dir, id)
	l := w2.List()
	if len(l) != 1 || l[0].Name != "a" || l[0].Enabled || l[0].State != "disabled" {
		t.Fatalf("reloaded: %+v", l)
	}
}

func TestEventsStream(t *testing.T) {
	s, ts := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	rd := bufio.NewReader(resp.Body)
	want := []string{"event: status", "event: page"}
	go func() {
		waitFor(t, "subscriber", func() bool { return s.hub.count() == 1 })
		s.AddPage(Page{TS: 1, Address: "0521839", Posted: true})
	}()
	for _, w := range want {
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatalf("waiting for %q: %v", w, err)
			}
			if strings.TrimSpace(line) == w {
				break
			}
		}
	}
}

func TestUIFallsBackToIndex(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/some/client/route")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// 200 with a built UI, 503 with only the placeholder; never a 404.
	if resp.StatusCode != 200 && resp.StatusCode != 503 {
		t.Fatalf("client route: %d", resp.StatusCode)
	}
	resp, err = http.Get(ts.URL + "/api/nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("unknown api path: %d", resp.StatusCode)
	}
}
