package recvhub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/models"
)

// Conn wraps one accepted websocket connection from a receiver agent. Each
// Conn runs its own read loop (consuming pings + pages) and write pump
// (serving frames from a buffered chan so the read goroutine never blocks).
type Conn struct {
	SystemID int64
	System   *models.System

	ws  *websocket.Conn
	hub *Hub

	sendCh   chan Frame
	closeOnce sync.Once
	closed    chan struct{}

	// OnPage is called for every well-formed page frame the client sends.
	// The handler returns the ack status to ship back.
	OnPage func(ctx context.Context, sys *models.System, p PagePayload) (status string, err error)
}

// newConn is called from the HTTP handler after a successful upgrade + auth.
func newConn(hub *Hub, ws *websocket.Conn, sys *models.System) *Conn {
	return &Conn{
		SystemID: sys.ID,
		System:   sys,
		ws:       ws,
		hub:      hub,
		sendCh:   make(chan Frame, 32),
		closed:   make(chan struct{}),
	}
}

// run is the per-connection event loop. It blocks until the client disconnects
// or the context is cancelled; it cleans up its own DB row and hub registration
// on the way out.
func (c *Conn) run(ctx context.Context, database *db.DB) {
	defer c.closeWithReason("loop exit")
	defer c.hub.Unregister(c)
	defer func() {
		_ = database.DeleteSystemConnection(context.Background(), c.SystemID, PodID)
	}()

	// Read the first frame — must be hello.
	hello, err := c.readHello(ctx)
	if err != nil {
		slog.Warn("receiver hello failed", "system_id", c.SystemID, "err", err)
		return
	}

	dongles, _ := json.Marshal(hello.Dongles)
	if err := database.UpsertSystemConnection(ctx, &db.SystemConnection{
		SystemID:         c.SystemID,
		PodID:            PodID,
		ReceiverVersion:  nullableString(hello.Version),
		ReceiverOS:       nullableString(hello.OS),
		ReceiverArch:     nullableString(hello.Arch),
		ReceiverHostname: nullableString(hello.Hostname),
		Dongles:          dongles,
	}); err != nil {
		slog.Error("upsert system connection", "err", err)
		return
	}

	// Now that we own the row, register locally + push welcome.
	c.hub.Register(c)
	c.send(Frame{Type: "welcome", Data: mustJSON(WelcomePayload{
		SystemID:    c.System.ID,
		SystemUUID:  c.System.SystemID,
		Name:        c.System.Name,
		PingSeconds: int(PingInterval / time.Second),
	})})

	slog.Info("receiver connected",
		"system", c.System.Name,
		"system_id", c.System.SystemID,
		"version", hello.Version,
		"os", hello.OS,
		"arch", hello.Arch,
		"hostname", hello.Hostname,
		"dongles", len(hello.Dongles),
	)

	// Start the write pump.
	writePump := make(chan struct{})
	go func() {
		defer close(writePump)
		c.runWritePump(ctx)
	}()

	// Heartbeat: bump last_ping_at periodically. If our row's been taken
	// by another pod, drop out.
	heartbeat := time.NewTicker(PingInterval)
	defer heartbeat.Stop()
	go func() {
		for {
			select {
			case <-c.closed:
				return
			case <-heartbeat.C:
				ok, err := database.TouchSystemConnection(context.Background(), c.SystemID, PodID)
				if err != nil {
					slog.Warn("touch connection failed", "err", err)
					continue
				}
				if !ok {
					slog.Info("connection taken over by another pod", "system_id", c.SystemID)
					c.closeWithReason("taken over")
					return
				}
			}
		}
	}()

	// Read loop.
	for {
		var f Frame
		if err := wsjson.Read(ctx, c.ws, &f); err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Info("receiver read loop ended", "system_id", c.SystemID, "err", err)
			}
			return
		}
		c.handleFrame(ctx, database, f)
	}
}

func (c *Conn) readHello(ctx context.Context) (*HelloPayload, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var f Frame
	if err := wsjson.Read(ctx, c.ws, &f); err != nil {
		return nil, err
	}
	if f.Type != "hello" {
		return nil, errors.New("first frame was not 'hello'")
	}
	var h HelloPayload
	if err := json.Unmarshal(f.Data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

func (c *Conn) handleFrame(ctx context.Context, database *db.DB, f Frame) {
	switch f.Type {
	case "ping":
		c.send(Frame{Type: "pong", ID: f.ID})
	case "page":
		var p PagePayload
		if err := json.Unmarshal(f.Data, &p); err != nil {
			c.sendAck(f.ID, "err", err.Error())
			return
		}
		status := "ok"
		var pageErr error
		if c.OnPage != nil {
			status, pageErr = c.OnPage(ctx, c.System, p)
		}
		if pageErr != nil {
			c.sendAck(f.ID, "err", pageErr.Error())
			return
		}
		c.sendAck(f.ID, status, "")
		_ = database // reserved for future direct DB use
	case "spectrum":
		// Decode + republish via the broker, which also appends to the
		// ring buffer + persists snapshots.
		var sp SpectrumPayload
		if err := json.Unmarshal(f.Data, &sp); err != nil {
			return
		}
		c.hub.Spectrum.Publish(c.SystemID, sp.CentreHz, sp.SampleRate, sp.Bins)
	default:
		slog.Warn("receiver sent unknown frame", "type", f.Type)
	}
}

func (c *Conn) runWritePump(ctx context.Context) {
	for {
		select {
		case <-c.closed:
			return
		case <-ctx.Done():
			return
		case f := <-c.sendCh:
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := wsjson.Write(wctx, c.ws, f)
			cancel()
			if err != nil {
				slog.Warn("ws write failed", "system_id", c.SystemID, "err", err)
				c.closeWithReason("write failed")
				return
			}
		}
	}
}

// send queues a frame for the write pump. Drops if the buffer is full —
// receiver is slow and we don't want to back up the hub.
func (c *Conn) send(f Frame) {
	select {
	case c.sendCh <- f:
	default:
		slog.Warn("ws send buffer full, dropping", "system_id", c.SystemID, "type", f.Type)
	}
}

func (c *Conn) sendAck(id, status, errMsg string) {
	c.send(Frame{Type: "ack", ID: id, Data: mustJSON(AckPayload{Status: status, Error: errMsg})})
}

func (c *Conn) closeWithReason(reason string) {
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.ws.Close(websocket.StatusNormalClosure, reason)
	})
}

func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
