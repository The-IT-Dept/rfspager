package recvhub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/rxproto"
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

	// proto is the protocol version the receiver declared in hello: 1 for
	// receivers that predate rxproto (5.2.x/5.3.x), which ignore unknown
	// frames anyway. Config and command frames only go to proto >= 2.
	proto int

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
		podID:    PodID + ":" + uuid.NewString()[:8],
		sendCh:   make(chan Frame, 32),
		closed:   make(chan struct{}),
		proto:    1,
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

	// Protocol-2 extras: declared proto, host facts, the config revision the
	// receiver is running. Best-effort — the connection is already recorded.
	c.proto = helloProto(hello)
	var host []byte
	if hello.Host != nil {
		host, _ = json.Marshal(hello.Host)
	}
	helloVersion := 0
	if hello.Config != nil {
		helloVersion = hello.Config.Version
	}
	if err := database.UpdateSystemConnectionHello(ctx, c.SystemID, c.podID, c.proto, host, helloVersion); err != nil {
		slog.Warn("record receiver hello details", "system_id", c.SystemID, "err", err)
	}

	c.connectedAt = time.Now()

	// Now that we own the row, register locally + push welcome. A proto-2
	// receiver also gets the current config revision (seeded from what it
	// reports when the system has none) and the newest release tag.
	c.hub.Register(c)
	welcome := WelcomePayload{
		SystemID:    c.System.ID,
		SystemUUID:  c.System.SystemID,
		Name:        c.System.Name,
		PingSeconds: int(PingInterval / time.Second),
	}
	if c.proto >= 2 {
		welcome.Config = c.hub.welcomeConfig(ctx, c.SystemID, hello)
		welcome.LatestVersion = c.hub.latestVersion(ctx)
	}
	c.send(Frame{Type: rxproto.FrameWelcome, Data: mustJSON(welcome)})

	slog.Info("receiver connected",
		"system", c.System.Name,
		"system_id", c.System.SystemID,
		"version", hello.Version,
		"proto", c.proto,
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
			"version":        hello.Version,
			"proto":          c.proto,
			"os":             hello.OS,
			"arch":           hello.Arch,
			"hostname":       hello.Hostname,
			"dongles":        dongleNames,
			"config_version": helloVersion,
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

	// Commands queued while the receiver was away go out right behind the
	// welcome. Only proto-2 receivers understand them; older ones would log
	// an unknown frame per command and the rows would be marked sent for
	// nothing, so they stay queued until the receiver is updated or expire.
	if c.proto >= 2 {
		c.hub.deliverQueued(ctx, c)
	}

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

// helloProto clamps the declared protocol version: absent or junk means the
// pre-rxproto receiver (1); anything newer than this server speaks is
// treated as what we know, since frames are additive.
func helloProto(h *HelloPayload) int {
	switch {
	case h.Proto < 1:
		return 1
	case h.Proto > rxproto.ProtoVersion:
		return rxproto.ProtoVersion
	}
	return h.Proto
}

func (c *Conn) readHello(ctx context.Context) (*HelloPayload, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var f Frame
	if err := wsjson.Read(ctx, c.ws, &f); err != nil {
		return nil, err
	}
	if f.Type != rxproto.FrameHello {
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
	case rxproto.FramePing:
		c.send(Frame{Type: rxproto.FramePong, ID: f.ID})
	case rxproto.FramePage:
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
	case rxproto.FrameLog:
		// Persist + fan out to live subscribers (owner / admin).
		var lp LogPayload
		if err := json.Unmarshal(f.Data, &lp); err != nil {
			return
		}
		c.hub.Logs.Publish(ctx, c.SystemID, lp)
	case rxproto.FrameStats:
		if c.hub == nil {
			return
		}
		var st rxproto.Stats
		if err := json.Unmarshal(f.Data, &st); err != nil {
			slog.Warn("receiver sent malformed stats", "system_id", c.SystemID, "err", err)
			return
		}
		c.hub.Stats.Publish(ctx, c.SystemID, st)
	case rxproto.FrameConfigResult:
		if c.hub == nil {
			return
		}
		var res rxproto.ConfigResult
		if err := json.Unmarshal(f.Data, &res); err != nil {
			slog.Warn("receiver sent malformed config_result", "system_id", c.SystemID, "err", err)
			return
		}
		c.handleConfigResult(ctx, res)
	case rxproto.FrameCommandResult:
		if c.hub == nil {
			return
		}
		var res rxproto.CommandResult
		if err := json.Unmarshal(f.Data, &res); err != nil {
			slog.Warn("receiver sent malformed command_result", "system_id", c.SystemID, "err", err)
			return
		}
		c.handleCommandResult(ctx, res)
	default:
		slog.Warn("receiver sent unknown frame", "type", f.Type)
	}
}

// handleConfigResult records whether the receiver applied or rejected a
// config revision and writes the matching system event.
func (c *Conn) handleConfigResult(ctx context.Context, res rxproto.ConfigResult) {
	if res.Status != rxproto.ConfigApplied && res.Status != rxproto.ConfigRejected {
		slog.Warn("receiver sent unknown config_result status", "system_id", c.SystemID, "status", res.Status)
		return
	}
	if err := c.hub.db.UpdateSystemConnectionConfigStatus(ctx, c.SystemID, res.Version, res.Status); err != nil {
		slog.Warn("record config status", "system_id", c.SystemID, "err", err)
	}
	meta := map[string]any{"version": res.Version}
	if res.Status == rxproto.ConfigApplied {
		slog.Info("receiver applied config", "system_id", c.SystemID, "version", res.Version)
		EmitEvent(ctx, c.hub.db.Conn(), c.SystemID, EvtReceiverConfigApplied, SeverityInfo,
			fmt.Sprintf("Config revision %d applied", res.Version), meta)
		return
	}
	meta["errors"] = res.Errors
	slog.Warn("receiver rejected config", "system_id", c.SystemID, "version", res.Version, "errors", res.Errors)
	EmitEvent(ctx, c.hub.db.Conn(), c.SystemID, EvtReceiverConfigRejected, SeverityWarning,
		fmt.Sprintf("Config revision %d rejected%s", res.Version, summariseErrors(res.Errors)), meta)
}

// summariseErrors renders up to three "key: reason" pairs for an event
// message; the full map goes in the metadata.
func summariseErrors(errs map[string]string) string {
	if len(errs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(errs))
	for k := range errs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, 3)
	for _, k := range keys {
		if len(parts) == 3 {
			parts = append(parts, "…")
			break
		}
		parts = append(parts, k+": "+errs[k])
	}
	return ": " + strings.Join(parts, "; ")
}

// handleCommandResult updates the command row and, on completion, writes a
// system event. A failed capture command also fails its capture row so the
// admin UI stops waiting for an upload that will never come.
func (c *Conn) handleCommandResult(ctx context.Context, res rxproto.CommandResult) {
	switch res.Status {
	case rxproto.CmdProgress, rxproto.CmdOK, rxproto.CmdError:
	default:
		slog.Warn("receiver sent unknown command_result status", "system_id", c.SystemID, "status", res.Status)
		return
	}
	cmd, err := c.hub.db.GetSystemCommand(ctx, res.ID)
	if err != nil || cmd.SystemID != c.SystemID {
		// Unknown id, or a command that belongs to another system: a
		// receiver only gets to report on its own commands.
		slog.Warn("command_result for unknown command", "system_id", c.SystemID, "command_id", res.ID)
		return
	}
	if err := c.hub.db.UpdateSystemCommandStatus(ctx, res.ID, res.Status, res.Stage, res.Message); err != nil {
		slog.Warn("record command result", "system_id", c.SystemID, "command_id", res.ID, "err", err)
	}
	if res.Status == rxproto.CmdProgress {
		return
	}

	severity := SeverityInfo
	summary := fmt.Sprintf("Command %s succeeded", cmd.Type)
	if res.Status == rxproto.CmdError {
		severity = SeverityError
		summary = fmt.Sprintf("Command %s failed", cmd.Type)
		if res.Message != "" {
			summary += ": " + res.Message
		}
		if cmd.Type == rxproto.CmdCapture {
			reason := res.Message
			if reason == "" {
				reason = "capture failed on the receiver"
			}
			if err := c.hub.db.MarkSystemCaptureErrorByCommand(ctx, cmd.ID, reason); err != nil {
				slog.Warn("mark capture failed", "command_id", cmd.ID, "err", err)
			}
		}
	}
	slog.Info("receiver command finished", "system_id", c.SystemID, "command_id", cmd.ID, "type", cmd.Type, "status", res.Status)
	EmitEvent(ctx, c.hub.db.Conn(), c.SystemID, EvtReceiverCommand, severity, summary, map[string]any{
		"command_id": cmd.ID,
		"type":       cmd.Type,
		"params":     json.RawMessage(cmd.Params),
		"status":     res.Status,
		"stage":      res.Stage,
		"message":    res.Message,
	})
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
	c.trySend(f)
}

// trySend is send that reports whether the frame was queued, for callers
// that must not mark something delivered when it was dropped.
func (c *Conn) trySend(f Frame) bool {
	select {
	case c.sendCh <- f:
		return true
	default:
		slog.Warn("ws send buffer full, dropping", "system_id", c.SystemID, "type", f.Type)
		return false
	}
}

func (c *Conn) sendAck(id, status, errMsg string) {
	c.send(Frame{Type: rxproto.FrameAck, ID: id, Data: mustJSON(AckPayload{Status: status, Error: errMsg})})
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
