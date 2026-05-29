// Package wsclient is the receiver-agent side of the websocket transport.
//
// One Client per receiver process. It dials the server, sends a Hello with
// version + OS + dongle nameplate info, then runs forever:
//
//   - heartbeat ping every PingInterval
//   - reads server frames (pong, welcome, config, ack) on the read loop
//   - accepts decoded pages from the pipeline via Send and ships them as
//     {type:"page", ...} frames, expecting acks
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
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"github.com/the-it-dept/rfspager/internal/receiver/buildinfo"
	"github.com/the-it-dept/rfspager/internal/receiver/sdr"
)

// PingInterval mirrors the server-side constant and is enforced by both ends.
const PingInterval = 30 * time.Second

// Frame is the shared envelope. Kept independent of the server-side recvhub
// package so the receiver binary doesn't import server code.
type Frame struct {
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

type HelloPayload struct {
	Version  string       `json:"version"`
	OS       string       `json:"os"`
	Arch     string       `json:"arch"`
	Hostname string       `json:"hostname"`
	Dongles  []DongleInfo `json:"dongles"`
}

type DongleInfo struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Product      string `json:"product,omitempty"`
	Serial       string `json:"serial,omitempty"`
	Tuner        string `json:"tuner,omitempty"`
}

// PageFrame is the schema for the page frame.Data; matches the server.
type PageFrame struct {
	Address  string `json:"address"`
	Message  string `json:"message"`
	Datetime int64  `json:"datetime"`
	Function int    `json:"fn"`
	BitRate  int    `json:"baud"`
}

// Welcome carries the server's view of who we are.
type Welcome struct {
	SystemID    int64  `json:"id"`
	SystemUUID  string `json:"system_id"`
	Name        string `json:"name"`
	PingSeconds int    `json:"ping_seconds"`
}

// Client is the long-lived receiver→server transport.
type Client struct {
	serverURL string // http(s) base URL; we'll swap scheme for ws(s)
	apiKey    string

	// Tuner names come from the SDR layer at open time (the dongle list
	// from Enumerate doesn't include "Rafael Micro R828D" because that
	// only becomes known once we actually open the device).
	Tuners map[int]string

	mu       sync.Mutex
	conn     *websocket.Conn
	sendCh   chan Frame
	welcomed chan Welcome // closed when we receive welcome
}

func New(serverURL, apiKey string) *Client {
	return &Client{
		serverURL: serverURL,
		apiKey:    apiKey,
		sendCh:    make(chan Frame, 64),
		welcomed:  make(chan Welcome, 1),
		Tuners:    map[int]string{},
	}
}

// Run dials and runs the connection loop until ctx is cancelled. Reconnects
// with backoff on transient failures.
func (c *Client) Run(ctx context.Context) error {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		err := c.runOnce(ctx)
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

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Send hello first thing.
	bi := buildinfo.Get()
	hello := HelloPayload{
		Version:  bi.Version,
		OS:       bi.OS,
		Arch:     bi.Arch,
		Hostname: bi.Hostname,
		Dongles:  c.dongleList(),
	}
	if err := writeFrame(ctx, conn, Frame{Type: "hello", Data: mustJSON(hello)}); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	// Run write pump + heartbeat in goroutines; read loop in main.
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go c.writePump(loopCtx, conn)
	go c.heartbeat(loopCtx)

	return c.readLoop(loopCtx, conn)
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

func (c *Client) dongleList() []DongleInfo {
	enum := sdr.Enumerate()
	out := make([]DongleInfo, 0, len(enum))
	for _, d := range enum {
		di := DongleInfo{
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
			c.enqueue(Frame{Type: "ping", ID: uuid.NewString()})
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		var f Frame
		if err := wsjson.Read(ctx, conn, &f); err != nil {
			return err
		}
		switch f.Type {
		case "welcome":
			var w Welcome
			_ = json.Unmarshal(f.Data, &w)
			slog.Info("server welcomed", "system", w.Name, "system_id", w.SystemUUID, "ping_seconds", w.PingSeconds)
			select {
			case c.welcomed <- w:
			default:
			}
		case "pong":
			// nothing to do.
		case "config":
			slog.Info("config push received", "data", string(f.Data))
		case "ack":
			// Ack handling is best-effort for now; future work can correlate
			// against pending sends with a map keyed on f.ID.
		default:
			slog.Warn("unknown frame from server", "type", f.Type)
		}
	}
}

// SendPage queues a decoded page for delivery to the server. Drops with a
// log if the buffer is full (server / network not keeping up).
func (c *Client) SendPage(addr, msg string, when time.Time, fn, baud int) {
	p := PageFrame{
		Address:  addr,
		Message:  msg,
		Datetime: when.Unix(),
		Function: fn,
		BitRate:  baud,
	}
	c.enqueue(Frame{Type: "page", ID: uuid.NewString(), Data: mustJSON(p)})
}

// SpectrumFrame matches the server's SpectrumPayload schema.
type SpectrumFrame struct {
	CentreHz   uint32    `json:"centre_hz"`
	SampleRate uint32    `json:"sample_rate"`
	Bins       []float32 `json:"bins"`
}

// SendSpectrum queues a waterfall slice for diagnostic display in the admin
// portal. Best-effort: dropped silently if the network's slow.
func (c *Client) SendSpectrum(centreHz, sampleRate uint32, bins []float32) {
	c.enqueue(Frame{Type: "spectrum", Data: mustJSON(SpectrumFrame{
		CentreHz:   centreHz,
		SampleRate: sampleRate,
		Bins:       bins,
	})})
}

// LogFrame matches the server's LogPayload schema. The receiver assigns ts
// (its own clock, unix milliseconds); the server records received_at itself.
type LogFrame struct {
	TS      int64          `json:"ts"`
	Level   string         `json:"level,omitempty"`
	Source  string         `json:"source,omitempty"`
	Message string         `json:"message"`
	Raw     map[string]any `json:"raw,omitempty"`
}

// SendLog ships a single log line over the WS for persistence + live fan-out
// to owner + admin viewers. Best-effort: dropped if the send buffer is full
// — we never want logging to block the decode hot path.
func (c *Client) SendLog(level, source, message string, raw map[string]any) {
	c.enqueue(Frame{Type: "log", Data: mustJSON(LogFrame{
		TS:      time.Now().UnixMilli(),
		Level:   level,
		Source:  source,
		Message: message,
		Raw:     raw,
	})})
}

// Welcomed blocks until the server has sent a welcome frame (or ctx done).
func (c *Client) Welcomed(ctx context.Context) (Welcome, error) {
	select {
	case w := <-c.welcomed:
		// Put it back so re-callers see the same value.
		select {
		case c.welcomed <- w:
		default:
		}
		return w, nil
	case <-ctx.Done():
		return Welcome{}, ctx.Err()
	}
}

func (c *Client) enqueue(f Frame) {
	select {
	case c.sendCh <- f:
	default:
		slog.Warn("ws send buffer full, dropping", "type", f.Type)
	}
}

func writeFrame(ctx context.Context, conn *websocket.Conn, f Frame) error {
	return wsjson.Write(ctx, conn, f)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
