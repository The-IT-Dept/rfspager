package wsclient

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// fakeHub accepts one receiver connection, records what it receives and
// lets the test push frames down.
type fakeHub struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	frames []rxproto.Frame
	connCh chan *websocket.Conn
	ready  chan rxproto.Frame // hello
}

func newFakeHub(t *testing.T) *fakeHub {
	h := &fakeHub{t: t, connCh: make(chan *websocket.Conn, 4), ready: make(chan rxproto.Frame, 4)}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/receiver/ws" || r.Header.Get("apikey") != "KEY" {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var hello rxproto.Frame
		if err := wsjson.Read(r.Context(), conn, &hello); err != nil {
			return
		}
		h.ready <- hello
		h.connCh <- conn
		for {
			var f rxproto.Frame
			if err := wsjson.Read(r.Context(), conn, &f); err != nil {
				return
			}
			h.mu.Lock()
			h.frames = append(h.frames, f)
			h.mu.Unlock()
			if f.Type == rxproto.FramePing {
				_ = wsjson.Write(r.Context(), conn, rxproto.Frame{Type: rxproto.FramePong, ID: f.ID})
			}
		}
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *fakeHub) received(typ string) []rxproto.Frame {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []rxproto.Frame
	for _, f := range h.frames {
		if f.Type == typ {
			out = append(out, f)
		}
	}
	return out
}

func (h *fakeHub) waitFor(typ string, n int) []rxproto.Frame {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := h.received(typ); len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %d %s frame(s)", n, typ)
	return nil
}

func TestPrimarySpeaksV2(t *testing.T) {
	hub := newFakeHub(t)
	c := New(hub.srv.URL, "KEY")
	c.Primary = true
	c.HostInfo = func() rxproto.HostInfo { return rxproto.HostInfo{Model: "Pi"} }
	c.ConfigInfo = func() rxproto.HelloConfig { return rxproto.HelloConfig{Version: 3, Effective: rxproto.Defaults()} }

	var gotConfig []rxproto.ConfigPayload
	var gotCmd []rxproto.Command
	var mu sync.Mutex
	c.OnConfig = func(p rxproto.ConfigPayload) { mu.Lock(); gotConfig = append(gotConfig, p); mu.Unlock() }
	c.OnCommand = func(cmd rxproto.Command) { mu.Lock(); gotCmd = append(gotCmd, cmd); mu.Unlock() }
	welcomes := 0
	c.OnWelcome = func(rxproto.Welcome) { mu.Lock(); welcomes++; mu.Unlock() }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	helloFrame := <-hub.ready
	var hello rxproto.Hello
	if err := json.Unmarshal(helloFrame.Data, &hello); err != nil {
		t.Fatal(err)
	}
	if hello.Proto != rxproto.ProtoVersion || hello.Host == nil || hello.Host.Model != "Pi" || hello.Config == nil || hello.Config.Version != 3 {
		t.Fatalf("hello not v2: %s", helloFrame.Data)
	}

	conn := <-hub.connCh
	// Welcome with a config that omits most keys: they must normalise to
	// defaults, with the explicit gain kept.
	welcome := `{"id":1,"system_id":"u","name":"Bud","ping_seconds":30,"latest_version":"v9.9.9","config":{"version":4,"config":{"tuner_gain_tenths_db":496}}}`
	if err := wsjson.Write(ctx, conn, rxproto.Frame{Type: rxproto.FrameWelcome, Data: json.RawMessage(welcome)}); err != nil {
		t.Fatal(err)
	}
	wctx, wcancel := context.WithTimeout(ctx, 3*time.Second)
	defer wcancel()
	w, err := c.Welcomed(wctx)
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "Bud" || w.LatestVersion != "v9.9.9" || w.Config == nil || w.Config.Version != 4 {
		t.Fatalf("welcome parsed wrong: %+v", w)
	}
	if w.Config.Config.TunerGainTenthsDB != 496 || w.Config.Config.SampleRate != 1_920_000 || len(w.Config.Config.Channels) != 2 {
		t.Fatalf("welcome config not normalised over defaults: %+v", w.Config.Config)
	}
	mu.Lock()
	if len(gotConfig) != 1 || gotConfig[0].Version != 4 || welcomes != 1 {
		t.Fatalf("OnConfig/OnWelcome: configs=%d welcomes=%d", len(gotConfig), welcomes)
	}
	mu.Unlock()

	// A config frame and a command frame.
	_ = wsjson.Write(ctx, conn, rxproto.Frame{Type: rxproto.FrameConfig, Data: json.RawMessage(`{"version":5,"config":{"ppm":7}}`)})
	_ = wsjson.Write(ctx, conn, rxproto.Frame{Type: rxproto.FrameCommand, Data: json.RawMessage(`{"id":"c1","type":"restart"}`)})
	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		ok := len(gotConfig) == 2 && len(gotCmd) == 1
		mu.Unlock()
		if ok || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	if len(gotConfig) != 2 || gotConfig[1].Version != 5 || gotConfig[1].Config.PPM != 7 || gotConfig[1].Config.SampleRate != 1_920_000 {
		t.Fatalf("config frame: %+v", gotConfig)
	}
	if len(gotCmd) != 1 || gotCmd[0].ID != "c1" || gotCmd[0].Type != rxproto.CmdRestart {
		t.Fatalf("command frame: %+v", gotCmd)
	}
	mu.Unlock()

	// Up-frames.
	c.SendStats(rxproto.Stats{IntervalS: 15})
	c.SendConfigResult(rxproto.ConfigResult{Version: 5, Status: rxproto.ConfigApplied})
	c.SendCommandResult(rxproto.CommandResult{ID: "c1", Type: rxproto.CmdRestart, Status: rxproto.CmdOK})
	c.SendPage("0011416", "hello", time.Now(), 0, 512)
	hub.waitFor(rxproto.FrameStats, 1)
	hub.waitFor(rxproto.FrameConfigResult, 1)
	if got := hub.waitFor(rxproto.FrameCommandResult, 1); got[0].ID != "c1" {
		t.Fatalf("command_result id %q", got[0].ID)
	}
	hub.waitFor(rxproto.FramePage, 1)

	dctx, dcancel := context.WithTimeout(ctx, time.Second)
	defer dcancel()
	c.Drain(dctx)
	ls := c.LinkStats()
	if ls.ReconnectsTotal != 0 || ls.QueueDepth != 0 || ls.DroppedFramesTotal != 0 {
		t.Fatalf("link stats: %+v", ls)
	}
}

func TestSecondaryStaysV1(t *testing.T) {
	hub := newFakeHub(t)
	c := New(hub.srv.URL, "KEY")
	c.HostInfo = func() rxproto.HostInfo { return rxproto.HostInfo{Model: "Pi"} }
	c.ConfigInfo = func() rxproto.HelloConfig { return rxproto.HelloConfig{Version: 3} }
	called := make(chan string, 4)
	c.OnConfig = func(rxproto.ConfigPayload) { called <- "config" }
	c.OnCommand = func(rxproto.Command) { called <- "command" }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	helloFrame := <-hub.ready
	if s := string(helloFrame.Data); strings.Contains(s, `"proto"`) || strings.Contains(s, `"host"`) || strings.Contains(s, `"config"`) {
		t.Fatalf("secondary hello carries v2 fields: %s", s)
	}
	conn := <-hub.connCh
	_ = wsjson.Write(ctx, conn, rxproto.Frame{Type: rxproto.FrameWelcome, Data: json.RawMessage(`{"id":1,"system_id":"u","name":"Dev","ping_seconds":30,"config":{"version":9,"config":{}}}`)})
	_ = wsjson.Write(ctx, conn, rxproto.Frame{Type: rxproto.FrameConfig, Data: json.RawMessage(`{"version":5,"config":{}}`)})
	_ = wsjson.Write(ctx, conn, rxproto.Frame{Type: rxproto.FrameCommand, Data: json.RawMessage(`{"id":"c1","type":"restart"}`)})
	wctx, wcancel := context.WithTimeout(ctx, 3*time.Second)
	defer wcancel()
	if _, err := c.Welcomed(wctx); err != nil {
		t.Fatal(err)
	}
	c.SendStats(rxproto.Stats{})
	c.SendConfigResult(rxproto.ConfigResult{})
	c.SendCommandResult(rxproto.CommandResult{ID: "x"})
	c.SendPage("0011416", "hello", time.Now(), 0, 512)
	hub.waitFor(rxproto.FramePage, 1)
	select {
	case what := <-called:
		t.Fatalf("secondary honoured a %s frame", what)
	case <-time.After(200 * time.Millisecond):
	}
	for _, typ := range []string{rxproto.FrameStats, rxproto.FrameConfigResult, rxproto.FrameCommandResult} {
		if got := hub.received(typ); len(got) != 0 {
			t.Fatalf("secondary sent a %s frame", typ)
		}
	}
}

func TestDroppedFramesAreCounted(t *testing.T) {
	c := New("http://127.0.0.1:1", "KEY")
	c.Primary = true
	for i := 0; i < 70; i++ {
		c.SendPage("1", "m", time.Now(), 0, 512)
	}
	ls := c.LinkStats()
	if ls.QueueDepth != 64 || ls.DroppedFramesTotal != 6 {
		t.Fatalf("link stats: %+v", ls)
	}
}

// A full send buffer must never take the process down. The drop warning goes
// through the LogShippingHandler into SendLog; if SendLog logged its own
// drop, the two would recurse until the stack overflowed. That crashed
// Figtree every twelve minutes of its 2026-10-14 internet outage, because
// log frames were also queued while offline and filled the buffer.
func TestLogShippingNeverRecursesOnFullBuffer(t *testing.T) {
	c := New("http://127.0.0.1:1", "KEY")
	prev := slog.Default()
	slog.SetDefault(slog.New(NewLogShippingHandler(slog.NewTextHandler(io.Discard, nil), c)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Offline: a log line is not queued at all, so an outage cannot fill the
	// buffer with log frames and crowd out pages.
	c.SendLog("info", "", "offline line", nil)
	if n := len(c.sendCh); n != 0 {
		t.Fatalf("queued %d log frames while disconnected", n)
	}

	c.connected.Store(true)
	for range cap(c.sendCh) {
		c.SendLog("info", "", "fill", nil)
	}
	if n := len(c.sendCh); n != cap(c.sendCh) {
		t.Fatalf("buffer holds %d frames, want %d", n, cap(c.sendCh))
	}

	// Buffer full: the page drop logs a warning, the warning is shipped, the
	// shipped frame is dropped silently. Before the fix this never returned.
	c.SendPage("0950336", "page", time.Now(), 0, 512)
	c.SendLog("warn", "", "one more", nil)
	if d := c.dropped.Load(); d != 3 {
		t.Fatalf("dropped = %d, want 3 (page, its warning, the extra log line)", d)
	}
	if n := len(c.sendCh); n != cap(c.sendCh) {
		t.Fatalf("buffer holds %d frames after drops, want %d", n, cap(c.sendCh))
	}
}
