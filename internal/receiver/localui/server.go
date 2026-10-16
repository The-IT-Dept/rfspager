// Package localui serves the receiver's own web UI and JSON API on the
// host's network: live pages, stats, connections, logs, read-only settings
// and the host's webhooks. The UI is the Vite build of apps/receiver-ui,
// embedded from dist/. See docs/receiver-local-ui.md for the API.
package localui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/the-it-dept/rfspager/internal/receiver/buildinfo"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

//go:embed all:dist
var distFS embed.FS

// DefaultAddr is where the UI listens unless RECEIVER_UI_ADDR says otherwise.
const DefaultAddr = ":8148"

// System is the server-side identity from the latest welcome.
type System struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

// Endpoint is one upstream's state for the Connections tab.
type Endpoint struct {
	Name               string `json:"name"`
	Mode               string `json:"mode"`
	URL                string `json:"url"`
	Primary            bool   `json:"primary"`
	Connected          bool   `json:"connected"`
	ConnectedSince     *int64 `json:"connected_since"`
	RTTMs              int    `json:"rtt_ms"`
	ReconnectsTotal    int    `json:"reconnects_total"`
	QueueDepth         int    `json:"queue_depth"`
	DroppedFramesTotal int    `json:"dropped_frames_total"`
	PagesSent          int64  `json:"pages_sent"`
	PagesFailed        int64  `json:"pages_failed"`
	LastPageAt         *int64 `json:"last_page_at"`
	LastError          string `json:"last_error"`
	LastErrorAt        *int64 `json:"last_error_at"`
}

// Settings is the read-only Settings tab.
type Settings struct {
	Config        rxproto.RuntimeConfig `json:"config"`
	ConfigVersion int                   `json:"config_version"`
	ConfigSource  string                `json:"config_source"`
	Build         buildinfo.Info        `json:"build"`
	Host          rxproto.HostInfo      `json:"host"`
	Dongles       []rxproto.DongleInfo  `json:"dongles"`
	Endpoints     []EndpointInfo        `json:"endpoints"`
	StateDir      string                `json:"state_dir"`
	UIListen      string                `json:"ui_listen"`
}

// EndpointInfo is an endpoint without its key.
type EndpointInfo struct {
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	URL     string `json:"url"`
	Primary bool   `json:"primary"`
}

// Sources is how the server reads the rest of the receiver. Every func is
// optional.
type Sources struct {
	System     func() *System
	DongleOpen func() bool
	Endpoints  func() []Endpoint
	Settings   func() Settings
	Host       func() rxproto.HostStats
}

// Server is the local UI: stores, webhooks and the HTTP handler.
type Server struct {
	addr      string
	build     buildinfo.Info
	startedAt time.Time
	src       Sources

	Pages    *PageStore
	Logs     *LogBuffer
	Stats    *StatsHistory
	Webhooks *Webhooks

	hub *hub
}

// New builds the server. logs is created before slog is configured (see
// LogBuffer.Handler) and handed in here. ctx bounds the webhook workers.
func New(ctx context.Context, addr, stateDir string, build buildinfo.Info, logs *LogBuffer, src Sources) *Server {
	s := &Server{
		addr: addr, build: build, startedAt: time.Now(), src: src,
		Pages: NewPageStore(stateDir), Logs: logs, Stats: &StatsHistory{},
		hub: newHub(),
	}
	s.Webhooks = NewWebhooks(ctx, stateDir, s.identity)
	s.Webhooks.onChange = func() { s.hub.publish("status", s.status()) }
	logs.OnLog(func(e LogEntry) { s.hub.publish("log", e) })
	return s
}

func (s *Server) identity() Identity {
	id := Identity{Hostname: s.build.Hostname, Version: s.build.Version}
	if s.src.System != nil {
		if sys := s.src.System(); sys != nil {
			id.System = sys.Name
		}
	}
	return id
}

// AddPage records a decode, streams it, and hands posted pages to the
// webhooks. Cheap and non-blocking: called from the forwarder.
func (s *Server) AddPage(p Page) {
	p = s.Pages.Add(p)
	s.hub.publish("page", p)
	if p.Posted {
		s.Webhooks.Deliver(p)
	}
}

// AddStats records a stats frame and streams it.
func (s *Server) AddStats(st rxproto.Stats) {
	p := StatsPoint{TS: time.Now().UnixMilli(), Stats: st}
	s.Stats.Add(p)
	s.hub.publish("stats", p)
}

// Run serves until ctx is cancelled. A listen failure is logged, not fatal:
// the receiver's job is decoding, the UI is extra.
func (s *Server) Run(ctx context.Context) {
	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = srv.Shutdown(sctx)
				cancel()
				s.Pages.Close()
				return
			case <-t.C:
				if s.hub.count() > 0 {
					s.hub.publish("status", s.status())
				}
			}
		}
	}()
	slog.Info("local ui listening", "addr", s.addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("local ui stopped", "addr", s.addr, "err", err)
	}
}

// Handler is the API plus the embedded UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.status()) })
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/pages", s.handlePages)
	mux.HandleFunc("GET /api/logs", s.handleLogs)
	mux.HandleFunc("GET /api/connections", s.handleConnections)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/webhooks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"webhooks": s.Webhooks.List()})
	})
	mux.HandleFunc("POST /api/webhooks", s.handleWebhookCreate)
	mux.HandleFunc("PUT /api/webhooks/{id}", s.handleWebhookUpdate)
	mux.HandleFunc("DELETE /api/webhooks/{id}", s.handleWebhookDelete)
	mux.HandleFunc("POST /api/webhooks/{id}/test", s.handleWebhookTest)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
	})
	mux.Handle("/", s.uiHandler())
	return requireJSON(mux)
}

// requireJSON rejects state-changing requests that are not JSON. A
// cross-site page can only send JSON after a CORS preflight, which this
// server never answers, so this is the CSRF guard for a UI with no login.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
		default:
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mt != "application/json" {
				writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type upstreamStatus struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Connected bool   `json:"connected"`
	RTTMs     int    `json:"rtt_ms"`
}

type statusBody struct {
	Receiver struct {
		Hostname    string `json:"hostname"`
		Version     string `json:"version"`
		OS          string `json:"os"`
		Arch        string `json:"arch"`
		StartedAt   int64  `json:"started_at"`
		HostUptimeS int64  `json:"host_uptime_s"`
	} `json:"receiver"`
	System *System `json:"system"`
	Dongle struct {
		Open bool `json:"open"`
	} `json:"dongle"`
	Upstream *upstreamStatus `json:"upstream"`
	Webhooks struct {
		Total   int `json:"total"`
		Enabled int `json:"enabled"`
		Failing int `json:"failing"`
	} `json:"webhooks"`
	PagesToday       int     `json:"pages_today"`
	LastPageAt       *int64  `json:"last_page_at"`
	CPUTempC         float64 `json:"cpu_temp_c"`
	UndervoltageNow  bool    `json:"undervoltage_now"`
	UndervoltageEver bool    `json:"undervoltage_ever"`
}

func (s *Server) status() statusBody {
	var b statusBody
	b.Receiver.Hostname, b.Receiver.Version = s.build.Hostname, s.build.Version
	b.Receiver.OS, b.Receiver.Arch = s.build.OS, s.build.Arch
	b.Receiver.StartedAt = s.startedAt.UnixMilli()
	if s.src.System != nil {
		b.System = s.src.System()
	}
	if s.src.DongleOpen != nil {
		b.Dongle.Open = s.src.DongleOpen()
	}
	if s.src.Endpoints != nil {
		for _, e := range s.src.Endpoints() {
			if e.Primary {
				b.Upstream = &upstreamStatus{e.Name, e.URL, e.Connected, e.RTTMs}
				break
			}
		}
	}
	b.Webhooks.Total, b.Webhooks.Enabled, b.Webhooks.Failing = s.Webhooks.Summary()
	count, last := s.Pages.Today()
	b.PagesToday = count
	if last != 0 {
		b.LastPageAt = &last
	}
	if s.src.Host != nil {
		h := s.src.Host()
		b.Receiver.HostUptimeS = h.UptimeS
		b.CPUTempC, b.UndervoltageNow, b.UndervoltageEver = h.CPUTempC, h.UndervoltageNow, h.UndervoltageEver
	}
	return b
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	window := time.Hour
	switch r.URL.Query().Get("window") {
	case "6h":
		window = 6 * time.Hour
	case "24h":
		window = 24 * time.Hour
	}
	pts, interval := s.Stats.Window(time.Now(), window)
	if pts == nil {
		pts = []StatsPoint{}
	}
	writeJSON(w, 200, map[string]any{"interval_s": interval, "latest": s.Stats.Latest(), "points": pts})
}

func (s *Server) handlePages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pq := PageQuery{Limit: 100, Filtered: q.Get("filtered") == "1" || q.Get("filtered") == "true", Q: q.Get("q")}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		pq.Limit = min(n, 500)
	}
	if n, err := strconv.ParseInt(q.Get("before"), 10, 64); err == nil && n > 0 {
		pq.Before = n
	}
	pages, more := s.Pages.Query(pq)
	writeJSON(w, 200, map[string]any{"pages": pages, "more": more})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	limit := 500
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = min(n, 2000)
	}
	writeJSON(w, 200, map[string]any{"logs": s.Logs.Recent(limit, parseLevel(r.URL.Query().Get("level")))})
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	eps := []Endpoint{}
	if s.src.Endpoints != nil {
		eps = s.src.Endpoints()
	}
	writeJSON(w, 200, map[string]any{"endpoints": eps, "webhooks": s.Webhooks.List()})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	var st Settings
	if s.src.Settings != nil {
		st = s.src.Settings()
	}
	st.Build = s.build
	st.UIListen = s.addr
	if st.Dongles == nil {
		st.Dongles = []rxproto.DongleInfo{}
	}
	if st.Endpoints == nil {
		st.Endpoints = []EndpointInfo{}
	}
	writeJSON(w, 200, st)
}

func (s *Server) handleWebhookCreate(w http.ResponseWriter, r *http.Request) {
	var in WebhookInput
	if !readJSON(w, r, &in) {
		return
	}
	v, err := s.Webhooks.Create(in)
	if err != nil {
		writeWebhookErr(w, err)
		return
	}
	s.hub.publish("status", s.status())
	writeJSON(w, 201, v)
}

func (s *Server) handleWebhookUpdate(w http.ResponseWriter, r *http.Request) {
	var in WebhookInput
	if !readJSON(w, r, &in) {
		return
	}
	v, err := s.Webhooks.Update(r.PathValue("id"), in)
	if err != nil {
		writeWebhookErr(w, err)
		return
	}
	s.hub.publish("status", s.status())
	writeJSON(w, 200, v)
}

func (s *Server) handleWebhookDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.Webhooks.Delete(r.PathValue("id")); err != nil {
		writeWebhookErr(w, err)
		return
	}
	s.hub.publish("status", s.status())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWebhookTest(w http.ResponseWriter, r *http.Request) {
	sample := Page{
		ID: 0, TS: time.Now().UnixMilli(), Channel: "TEST", Baud: 512, Function: 3,
		Address: "0000000", Message: "RFSPAGER TEST PAGE - PLEASE DISREGARD", Posted: true,
	}
	if s.src.Settings != nil {
		if chs := s.src.Settings().Config.EnabledChannels(); len(chs) > 0 {
			sample.Channel, sample.FreqHz = chs[0].Label, chs[0].FreqHz
		}
	}
	res, err := s.Webhooks.Test(r.Context(), r.PathValue("id"), sample)
	if err != nil {
		writeWebhookErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func writeWebhookErr(w http.ResponseWriter, err error) {
	var ve ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, 422, map[string]any{"errors": ve})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, 404, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, 500, map[string]string{"error": err.Error()})
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(v); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleEvents streams page, stats, log and status events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	sub := s.hub.subscribe()
	defer s.hub.unsubscribe(sub)

	write := func(ev event) bool {
		if _, err := io.WriteString(w, "event: "+ev.name+"\ndata: "); err != nil {
			return false
		}
		if _, err := w.Write(ev.data); err != nil {
			return false
		}
		if _, err := io.WriteString(w, "\n\n"); err != nil {
			return false
		}
		return true
	}
	b, _ := json.Marshal(s.status())
	if !write(event{"status", b}) {
		return
	}
	fl.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.gone:
			return // too slow; the browser reconnects
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		case ev := <-sub.ch:
			if !write(ev) {
				return
			}
			fl.Flush()
		}
	}
}

// uiHandler serves the embedded build; unknown paths get index.html.
func (s *Server) uiHandler() http.Handler {
	sub, _ := fs.Sub(distFS, "dist")
	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			p = "index.html"
		}
		if p == "index.html" {
			b, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, "This receiver was built without its web UI (run npm run build in apps/receiver-ui). The API is at /api/status.\n")
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(b)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

type event struct {
	name string
	data []byte
}

type subscriber struct {
	ch   chan event
	gone chan struct{}
	once sync.Once
}

// hub fans events out to SSE clients. A client whose buffer is full is
// dropped rather than slowing the publisher.
type hub struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

func newHub() *hub { return &hub{subs: map[*subscriber]struct{}{}} }

func (h *hub) subscribe() *subscriber {
	s := &subscriber{ch: make(chan event, 256), gone: make(chan struct{})}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (h *hub) unsubscribe(s *subscriber) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
	s.once.Do(func() { close(s.gone) })
}

func (h *hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

func (h *hub) publish(name string, v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) == 0 {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	for s := range h.subs {
		select {
		case s.ch <- event{name, b}:
		default:
			delete(h.subs, s)
			s.once.Do(func() { close(s.gone) })
		}
	}
}
