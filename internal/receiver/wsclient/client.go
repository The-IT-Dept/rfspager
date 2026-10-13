// Package wsclient is the receiver-agent side of the websocket transport.
//
// One Client per configured ws endpoint. It dials the server, sends a Hello
// with version + OS + dongle nameplate info, then runs forever:
//
//   - heartbeat ping every PingInterval (the pong round trip is the link RTT)
//   - reads server frames (welcome, pong, ack, config, command) on the read loop
//   - accepts decoded pages from the pipeline via SendPage and ships them as
//     page frames, expecting acks
//
// The receiver's first ws endpoint is the primary: only it speaks protocol
// version 2 (hello carries host facts and the running config; stats,
// config_result and command_result go up; config and command frames are
// honoured). Secondary endpoints get a version 1 hello and pages only, so a
// server still running older code never sees a frame it does not know.
//
// Reconnects with exponential backoff when the connection drops.
package wsclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"github.com/the-it-dept/rfspager/internal/receiver/buildinfo"
	"github.com/the-it-dept/rfspager/internal/receiver/sdr"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// PingInterval mirrors the server-side constant and is enforced by both ends.
const PingInterval = 30 * time.Second

// Client is the long-lived receiver→server transport.
type Client struct {
	serverURL string // http(s) base URL; we'll swap scheme for ws(s)
	apiKey    string

	// Primary marks the endpoint that owns the receiver: it gets the v2
	// hello and the v2 frames, and its config/command frames are acted on.
	// Set before Run.
	Primary bool

	// HostInfo and ConfigInfo supply the v2 hello fields. Either may be nil.
	HostInfo   func() rxproto.HostInfo
	ConfigInfo func() rxproto.HelloConfig

	// Callbacks, each invoked in its own goroutine so the read loop never
	// blocks on them. OnConfig also fires for the config carried by a
	// welcome, before OnWelcome and before Welcomed returns.
	OnConfig  func(rxproto.ConfigPayload)
	OnCommand func(rxproto.Command)
	OnWelcome func(rxproto.Welcome)

	// Tuner names come from the SDR layer at open time (the dongle list
	// from Enumerate doesn't include "Rafael Micro R828D" because that
	// only becomes known once we actually open the device).
	Tuners map[int]string

	mu       sync.Mutex
	conn     *websocket.Conn
	sendCh   chan rxproto.Frame
	welcomed chan rxproto.Welcome // holds the latest welcome

	// Link counters for stats.
	connects atomic.Int64
	dropped  atomic.Int64
	pending  atomic.Int64 // frames queued or being written; Drain waits for zero
	// connected is true between a welcome and the loss of that connection;
	// stats are only queued while it is set so an outage does not fill the
	// send buffer with stale health reports and crowd out pages.
	connected atomic.Bool
	rttMs     atomic.Int64
	pingMu    sync.Mutex
	pingID    string
	pingAt    time.Time
}

func New(serverURL, apiKey string) *Client {
	return &Client{
		serverURL: serverURL,
		apiKey:    apiKey,
		sendCh:    make(chan rxproto.Frame, 64),
		welcomed:  make(chan rxproto.Welcome, 1),
		Tuners:    map[int]string{},
	}
}

// ServerURL is the http(s) base URL this client dials.
func (c *Client) ServerURL() string { return c.serverURL }

// APIKey is the receiver key presented to this endpoint.
func (c *Client) APIKey() string { return c.apiKey }

// Run dials and runs the connection loop until ctx is cancelled. Reconnects
// with backoff on transient failures.
func (c *Client) Run(ctx context.Context) error {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		err := c.runOnce(ctx)
		c.connected.Store(false)
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return nil
		}
		slog.Warn("ws connection lost", "err", err, "retry_in", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (c *Client) runOnce(ctx context.Context) error {
	wsURL, err := c.buildWSURL()
	if err != nil {
		return err
	}
	headers := http.Header{}
	headers.Set("apikey", c.apiKey)

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "client closing")
	conn.SetReadLimit(1 << 20)
	c.connects.Add(1)

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Send hello first thing.
	if err := writeFrame(ctx, conn, rxproto.Frame{Type: rxproto.FrameHello, Data: mustJSON(c.hello())}); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	// Run write pump + heartbeat in goroutines; read loop in main.
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go c.writePump(loopCtx, conn)
	go c.heartbeat(loopCtx)

	return c.readLoop(loopCtx, conn)
}

// hello builds the hello payload: v2 with host and config on the primary,
// the plain v1 shape elsewhere.
func (c *Client) hello() rxproto.Hello {
	bi := buildinfo.Get()
	h := rxproto.Hello{
		Version:  bi.Version,
		OS:       bi.OS,
		Arch:     bi.Arch,
		Hostname: bi.Hostname,
		Dongles:  c.dongleList(),
	}
	if !c.Primary {
		return h
	}
	h.Proto = rxproto.ProtoVersion
	if c.HostInfo != nil {
		hi := c.HostInfo()
		h.Host = &hi
	}
	if c.ConfigInfo != nil {
		ci := c.ConfigInfo()
		h.Config = &ci
	}
	return h
}

func (c *Client) buildWSURL() (string, error) {
	u, err := url.Parse(c.serverURL)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/receiver/ws"
	return u.String(), nil
}

func (c *Client) dongleList() []rxproto.DongleInfo {
	enum := sdr.Enumerate()
	out := make([]rxproto.DongleInfo, 0, len(enum))
	for _, d := range enum {
		di := rxproto.DongleInfo{
			Index:        d.Index,
			Name:         d.Name,
			Manufacturer: d.Manufacturer,
			Product:      d.Product,
			Serial:       d.Serial,
		}
		if t, ok := c.Tuners[d.Index]; ok {
			di.Tuner = t
		}
		out = append(out, di)
	}
	return out
}

func (c *Client) writePump(ctx context.Context, conn *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-c.sendCh:
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := writeFrame(wctx, conn, f)
			cancel()
			c.pending.Add(-1)
			if err != nil {
				slog.Warn("ws write failed", "err", err)
				_ = conn.Close(websocket.StatusInternalError, "write failed")
				return
			}
		}
	}
}

func (c *Client) heartbeat(ctx context.Context) {
	t := time.NewTicker(PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			id := uuid.NewString()
			c.pingMu.Lock()
			c.pingID, c.pingAt = id, time.Now()
			c.pingMu.Unlock()
			c.enqueue(rxproto.Frame{Type: rxproto.FramePing, ID: id})
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		var f rxproto.Frame
		if err := wsjson.Read(ctx, conn, &f); err != nil {
			return err
		}
		switch f.Type {
		case rxproto.FrameWelcome:
			w, err := decodeWelcome(f.Data)
			if err != nil {
				slog.Warn("bad welcome from server", "err", err)
			}
			slog.Info("server welcomed",
				"system", w.Name, "system_id", w.SystemUUID, "ping_seconds", w.PingSeconds,
				"config_version", configVersion(w.Config), "latest_version", w.LatestVersion,
			)
			go c.handleWelcome(w)
		case rxproto.FramePong:
			c.notePong(f.ID)
		case rxproto.FrameConfig:
			if !c.Primary || c.OnConfig == nil {
				slog.Debug("config frame ignored on secondary endpoint", "server", c.serverURL)
				continue
			}
			p, err := decodeConfig(f.Data)
			if err != nil {
				slog.Warn("bad config frame from server", "err", err)
				continue
			}
			go c.OnConfig(p)
		case rxproto.FrameCommand:
			if !c.Primary || c.OnCommand == nil {
				slog.Debug("command frame ignored on secondary endpoint", "server", c.serverURL)
				continue
			}
			var cmd rxproto.Command
			if err := json.Unmarshal(f.Data, &cmd); err != nil || cmd.ID == "" || cmd.Type == "" {
				slog.Warn("bad command frame from server", "err", err, "data", string(f.Data))
				continue
			}
			go c.OnCommand(cmd)
		case rxproto.FrameAck:
			// Ack handling is best-effort for now; future work can correlate
			// against pending sends with a map keyed on f.ID.
		default:
			slog.Warn("unknown frame from server", "type", f.Type)
		}
	}
}

// handleWelcome applies the welcome's config, runs OnWelcome, then
// publishes the welcome to Welcomed, in that order, off the read loop.
func (c *Client) handleWelcome(w rxproto.Welcome) {
	c.connected.Store(true)
	if c.Primary && w.Config != nil && c.OnConfig != nil {
		c.OnConfig(*w.Config)
	}
	if c.OnWelcome != nil {
		c.OnWelcome(w)
	}
	// Replace whatever welcome is held so Welcomed sees the latest.
	select {
	case <-c.welcomed:
	default:
	}
	c.welcomed <- w
}

func (c *Client) notePong(id string) {
	c.pingMu.Lock()
	defer c.pingMu.Unlock()
	if id != "" && id == c.pingID {
		c.rttMs.Store(time.Since(c.pingAt).Milliseconds())
		c.pingID = ""
	}
}

// decodeWelcome parses a welcome; the embedded config, when present, is
// decoded over the defaults so keys missing from an older revision take
// their default values.
func decodeWelcome(data []byte) (rxproto.Welcome, error) {
	var w rxproto.Welcome
	if err := json.Unmarshal(data, &w); err != nil {
		return w, err
	}
	var probe struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return w, err
	}
	w.Config = nil
	if len(probe.Config) > 0 && string(probe.Config) != "null" {
		cp, err := decodeConfig(probe.Config)
		if err != nil {
			return w, err
		}
		w.Config = &cp
	}
	return w, nil
}

// decodeConfig parses a config payload with the config normalised over
// the defaults (the receiver-side equivalent of rxproto.Normalize).
func decodeConfig(data []byte) (rxproto.ConfigPayload, error) {
	p := rxproto.ConfigPayload{Config: rxproto.Defaults()}
	if err := json.Unmarshal(data, &p); err != nil {
		return rxproto.ConfigPayload{}, err
	}
	return p, nil
}

func configVersion(p *rxproto.ConfigPayload) int {
	if p == nil {
		return -1
	}
	return p.Version
}

// SendPage queues a decoded page for delivery to the server. Drops with a
// log if the buffer is full (server / network not keeping up).
func (c *Client) SendPage(addr, msg string, when time.Time, fn, baud int) {
	c.SendPageQ(addr, msg, when, fn, baud, nil)
}

// SendPageQ is SendPage with the decoder's quality assessment attached.
func (c *Client) SendPageQ(addr, msg string, when time.Time, fn, baud int, q *rxproto.PageQuality) {
	p := rxproto.Page{
		Address:  addr,
		Message:  msg,
		Datetime: when.Unix(),
		Function: fn,
		BitRate:  baud,
		Quality:  q,
	}
	c.enqueue(rxproto.Frame{Type: rxproto.FramePage, ID: uuid.NewString(), Data: mustJSON(p)})
}

// SendLog ships a single log line over the WS for persistence + live fan-out
// to owner + admin viewers. Best-effort: dropped if the send buffer is full
// — we never want logging to block the decode hot path.
func (c *Client) SendLog(level, source, message string, raw map[string]any) {
	c.enqueue(rxproto.Frame{Type: rxproto.FrameLog, Data: mustJSON(rxproto.Log{
		TS:      time.Now().UnixMilli(),
		Level:   level,
		Source:  source,
		Message: message,
		Raw:     raw,
	})})
}

// SendStats ships a periodic health report. Primary endpoint only.
func (c *Client) SendStats(s rxproto.Stats) {
	if !c.connected.Load() {
		return // nobody to report to; pages keep the buffer
	}
	c.sendV2(rxproto.Frame{Type: rxproto.FrameStats, Data: mustJSON(s)})
}

// SendConfigResult reports the outcome of a config revision. Primary only.
func (c *Client) SendConfigResult(r rxproto.ConfigResult) {
	c.sendV2(rxproto.Frame{Type: rxproto.FrameConfigResult, Data: mustJSON(r)})
}

// SendCommandResult reports command progress or completion. Primary only.
func (c *Client) SendCommandResult(r rxproto.CommandResult) {
	c.sendV2(rxproto.Frame{Type: rxproto.FrameCommandResult, ID: r.ID, Data: mustJSON(r)})
}

func (c *Client) sendV2(f rxproto.Frame) {
	if !c.Primary {
		slog.Debug("v2 frame not sent to secondary endpoint", "type", f.Type, "server", c.serverURL)
		return
	}
	c.enqueue(f)
}

// LinkStats reports the link counters for the stats frame.
func (c *Client) LinkStats() rxproto.LinkStats {
	return rxproto.LinkStats{
		ReconnectsTotal:    int(max(c.connects.Load()-1, 0)),
		QueueDepth:         len(c.sendCh),
		DroppedFramesTotal: int(c.dropped.Load()),
		RTTMs:              int(c.rttMs.Load()),
	}
}

// Drain waits until every queued frame has been written or ctx expires.
// Used before a deliberate exit so the last result frame gets out.
func (c *Client) Drain(ctx context.Context) {
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for {
		if c.pending.Load() == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Welcomed blocks until the server has sent a welcome frame (or ctx done).
func (c *Client) Welcomed(ctx context.Context) (rxproto.Welcome, error) {
	select {
	case w := <-c.welcomed:
		// Put it back so re-callers see the same value.
		select {
		case c.welcomed <- w:
		default:
		}
		return w, nil
	case <-ctx.Done():
		return rxproto.Welcome{}, ctx.Err()
	}
}

func (c *Client) enqueue(f rxproto.Frame) {
	select {
	case c.sendCh <- f:
		c.pending.Add(1)
	default:
		c.dropped.Add(1)
		slog.Warn("ws send buffer full, dropping", "type", f.Type)
	}
}

func writeFrame(ctx context.Context, conn *websocket.Conn, f rxproto.Frame) error {
	return wsjson.Write(ctx, conn, f)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
