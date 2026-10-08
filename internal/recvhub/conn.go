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
	"github.com/google/uuid"
	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/p25"
)

// Conn wraps one accepted websocket connection from a receiver agent. Each
// Conn runs its own read loop (consuming pings + pages) and write pump
// (serving frames from a buffered chan so the read goroutine never blocks).
type Conn struct {
	SystemID int64
	System   *models.System

	ws  *websocket.Conn
	hub *Hub

	sendCh    chan Frame
	closeOnce sync.Once
	closed    chan struct{}

	// connectedAt is when we received the hello; used to compute downtime
	// on disconnect.
	connectedAt time.Time

	// podID is this connection's identity in system_connections. It is derived
	// from PodID but is per-socket, because two sockets for one system can be
	// live on the same process at once and whichever tears down first must not
	// delete the row the other still owns. Using the process-wide recvhub.PodID
	// here is what made a takeover delete the successor's own row.
	podID string

	// emittedConnect tracks whether we wrote a `receiver_connected` event
	// for this socket. Determines whether the deferred disconnect emitter
	// fires a paired event.
	emittedConnect bool

	// OnPage is called for every well-formed page frame the client sends.
	// The handler returns the ack status to ship back.
	OnPage func(ctx context.Context, sys *models.System, p PagePayload) (status string, err error)

	// OnRadio is called for every p25.* frame (see internal/p25/wire.go)
	// with the frame's raw payload. Radio frames are high-rate and
	// fire-and-forget: they are never acked, and an error is only logged.
	OnRadio func(ctx context.Context, sys *models.System, frameType string, data json.RawMessage) error

	// radioAuthAt is when a radio frame last passed stillAuthorized; see
	// radioAuthorized. Only touched from the read loop.
	radioAuthAt time.Time
}

// radioAuthTTL is how long a radio frame's authorization check is reused.
// Pages are checked one by one, but a busy P25 site sends several frames a
// second, and a DB round trip for each would buy nothing: revocation still
// closes the socket within this window (and the heartbeat checks too).
const radioAuthTTL = 30 * time.Second

// radioFrameTimeout bounds one radio frame's database work, so a stuck
// database stalls the read loop for at most this long per frame.
const radioFrameTimeout = 10 * time.Second

// newConn is called from the HTTP handler after a successful upgrade + auth.
func newConn(hub *Hub, ws *websocket.Conn, sys *models.System) *Conn {
	return &Conn{
		SystemID: sys.ID,
		System:   sys,
		ws:       ws,
		hub:      hub,
		podID:    PodID + ":" + uuid.NewString()[:8],
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
		_ = database.DeleteSystemConnection(context.Background(), c.SystemID, c.podID)
	}()
	// Defer the receiver_disconnected event last so it lands even if other
	// cleanup paths panic. Uses background context because r.Context() is
	// already cancelled by the time defers run.
	defer func() {
		if !c.emittedConnect {
			return
		}
		bg := context.Background()
		uptime := time.Since(c.connectedAt).Round(time.Second)
		EmitEvent(bg, database.Conn(), c.SystemID, EvtReceiverDisconnected, SeverityInfo,
			"Receiver disconnected",
			map[string]any{
				"connected_at":   c.connectedAt,
				"uptime_seconds": int(uptime.Seconds()),
			})
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
		PodID:            c.podID,
		ReceiverVersion:  nullableString(hello.Version),
		ReceiverOS:       nullableString(hello.OS),
		ReceiverArch:     nullableString(hello.Arch),
		ReceiverHostname: nullableString(hello.Hostname),
		Dongles:          dongles,
	}); err != nil {
		slog.Error("upsert system connection", "err", err)
		return
	}

	c.connectedAt = time.Now()

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

	// Event log: receiver_connected with the hardware fingerprint. The
	// watchdog will subsequently emit receiver_online if this connect
	// resolves a prior offline event.
	dongleNames := make([]string, 0, len(hello.Dongles))
	for _, d := range hello.Dongles {
		dongleNames = append(dongleNames, d.Name)
	}
	EmitEvent(ctx, database.Conn(), c.SystemID, EvtReceiverConnected, SeverityInfo,
		"Receiver connected",
		map[string]any{
			"version":  hello.Version,
			"os":       hello.OS,
			"arch":     hello.Arch,
			"hostname": hello.Hostname,
			"dongles":  dongleNames,
		})
	// Track that this socket emitted the connect event so the deferred
	// disconnect emitter knows to fire its pair.
	c.emittedConnect = true

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
				if !c.stillAuthorized(ctx, database) {
					c.closeWithReason("receiver authorization revoked")
					return
				}
				ok, err := database.TouchSystemConnection(context.Background(), c.SystemID, c.podID)
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

type systemKeyLookup interface {
	GetSystemByAPIKey(context.Context, string) (*models.System, error)
}

func (c *Conn) stillAuthorized(ctx context.Context, database systemKeyLookup) bool {
	if c.System == nil || !c.System.APIKey.Valid || c.System.APIKey.String == "" {
		return false
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := database.GetSystemByAPIKey(checkCtx, c.System.APIKey.String)
	return err == nil && current.ID == c.SystemID
}

func (c *Conn) handleFrame(ctx context.Context, database systemKeyLookup, f Frame) {
	switch f.Type {
	case "ping":
		c.send(Frame{Type: "pong", ID: f.ID})
	case "page":
		// Approval, ownership and key rotation apply to already-open sockets too.
		if !c.stillAuthorized(ctx, database) {
			c.closeWithReason("receiver authorization revoked")
			return
		}
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
		// The waterfall was removed. Receivers built before that still send
		// these frames, so swallow them here rather than let them fall through
		// to the unknown-frame warning below and spam the log once per frame.
		// Safe to delete once every receiver in the field has been updated.
	case p25.FrameSite, p25.FrameCallStart, p25.FrameCallEnd, p25.FrameUnit, p25.FrameAudio:
		c.handleRadio(ctx, database, f)
	case "log":
		// Persist + fan out to live subscribers (owner / admin).
		var lp LogPayload
		if err := json.Unmarshal(f.Data, &lp); err != nil {
			return
		}
		c.hub.Logs.Publish(ctx, c.SystemID, lp)
	default:
		slog.Warn("receiver sent unknown frame", "type", f.Type)
	}
}

// handleRadio stores one P25 frame. Failures are logged and the socket
// stays up: one bad frame shouldn't cost the receiver its connection.
func (c *Conn) handleRadio(ctx context.Context, database systemKeyLookup, f Frame) {
	if !c.radioAuthorized(ctx, database) {
		c.closeWithReason("receiver authorization revoked")
		return
	}
	if c.OnRadio == nil {
		return
	}
	fctx, cancel := context.WithTimeout(ctx, radioFrameTimeout)
	defer cancel()
	if err := c.OnRadio(fctx, c.System, f.Type, f.Data); err != nil {
		slog.Warn("receiver radio frame rejected", "system_id", c.SystemID, "type", f.Type, "err", err)
	}
}

// radioAuthorized is stillAuthorized, reused for radioAuthTTL after a pass.
func (c *Conn) radioAuthorized(ctx context.Context, database systemKeyLookup) bool {
	if !c.radioAuthAt.IsZero() && time.Since(c.radioAuthAt) < radioAuthTTL {
		return true
	}
	if !c.stillAuthorized(ctx, database) {
		return false
	}
	c.radioAuthAt = time.Now()
	return true
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
		if c.ws != nil {
			_ = c.ws.Close(websocket.StatusNormalClosure, reason)
		}
	})
}

func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
