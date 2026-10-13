package recvhub

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/notify"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// PingInterval is how often we expect receivers to ping. We bump
// last_ping_at every time we see one; admin queries treat anything older
// than ~3× this as offline.
const PingInterval = 30 * time.Second

// PingTimeout closes the socket if no ping arrives within this window.
const PingTimeout = 90 * time.Second

// Retention and expiry policy applied by the sweeper.
const (
	// statsRetention is how long stats reports are kept. Charts sample at
	// read time, so the raw rows are the only history.
	statsRetention = 30 * 24 * time.Hour
	// commandExpiry bounds how long a command waits for its receiver. A
	// restart somebody asked for this morning must not fire next week.
	commandExpiry = time.Hour
	// maintenanceInterval is how often the retention and expiry passes run.
	maintenanceInterval = 10 * time.Minute
	// latestVersionTimeout bounds the release lookup that decorates the
	// welcome frame, so a slow GitHub cannot stall receiver connects.
	latestVersionTimeout = 3 * time.Second
)

// PodID is set once at startup by main; included in system_connections rows
// so each pod can identify its own entries.
var PodID string

// Hub owns the per-pod set of live receiver websocket connections plus the
// LISTEN goroutines that pick up cross-pod NOTIFY events.
type Hub struct {
	db       *db.DB
	listener *db.Listener
	notifier *notify.Notifier

	Logs  *LogBroker
	Stats *StatsBroker

	// LatestVersion, when set, returns the newest published receiver release
	// tag (e.g. "v5.5.0") for the welcome frame, or "" when unknown. Wired
	// from cmd/server to the install package's release cache.
	LatestVersion func(ctx context.Context) string

	mu    sync.RWMutex
	conns map[int64]*Conn // system_id → live conn on this pod
}

// SetNotifier wires a push notifier in. Optional — when nil the watchdog
// still records online/offline events but skips the push.
func (h *Hub) SetNotifier(n *notify.Notifier) { h.notifier = n }

func New(database *db.DB, listener *db.Listener) (*Hub, error) {
	return &Hub{
		db:       database,
		listener: listener,
		Logs:     NewLogBroker(database.Conn()),
		Stats:    NewStatsBroker(database),
		conns:    make(map[int64]*Conn),
	}, nil
}

// Start launches background goroutines: cross-pod config and command NOTIFY
// listeners, the periodic sweeper, watchdog and log rotation. Stops when
// ctx is cancelled.
func (h *Hub) Start(ctx context.Context) error {
	configCh, err := h.listener.Listen("receiver_config", 16)
	if err != nil {
		return err
	}
	commandCh, err := h.listener.Listen("receiver_command", 16)
	if err != nil {
		return err
	}
	go h.runConfigListener(ctx, configCh)
	go h.runCommandListener(ctx, commandCh)
	go h.runSweeper(ctx)
	go h.runWatchdog(ctx)
	go h.runLogRotation(ctx)
	return nil
}

func (h *Hub) runConfigListener(ctx context.Context, ch <-chan string) {
	for {
		select {
		case <-ctx.Done():
			return
		case payload := <-ch:
			systemID, err := strconv.ParseInt(payload, 10, 64)
			if err != nil {
				slog.Warn("receiver_config: bad payload", "payload", payload)
				continue
			}
			h.pushConfig(ctx, systemID)
		}
	}
}

// runCommandListener reacts to the system_commands insert trigger. The
// payload is the command id; whichever pod holds the receiver's socket
// delivers it, every other pod finds no local conn and does nothing.
func (h *Hub) runCommandListener(ctx context.Context, ch <-chan string) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-ch:
			h.DeliverCommand(ctx, id)
		}
	}
}

func (h *Hub) runSweeper(ctx context.Context) {
	tick := time.NewTicker(PingTimeout)
	defer tick.Stop()
	maintain := time.NewTicker(maintenanceInterval)
	defer maintain.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			n, err := h.db.PruneStaleSystemConnections(ctx, PingTimeout)
			if err != nil {
				slog.Error("prune stale connections", "err", err)
				continue
			}
			if n > 0 {
				slog.Info("pruned stale connections", "count", n)
			}
		case <-maintain.C:
			h.maintain(ctx)
		}
	}
}

// maintain applies the retention and expiry policy.
func (h *Hub) maintain(ctx context.Context) {
	if n, err := h.db.PruneSystemStats(ctx, statsRetention); err != nil {
		slog.Error("prune system stats", "err", err)
	} else if n > 0 {
		slog.Info("pruned old system stats", "deleted", n)
	}
	if n, err := h.db.ExpireSystemCommands(ctx, commandExpiry); err != nil {
		slog.Error("expire system commands", "err", err)
	} else if n > 0 {
		slog.Info("expired undelivered commands", "count", n)
	}
}

// Register puts a Conn in the hub. If another conn for the same system was
// already there (same pod reconnecting), it's closed first.
func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	prev := h.conns[c.SystemID]
	h.conns[c.SystemID] = c
	h.mu.Unlock()
	if prev != nil {
		prev.closeWithReason("replaced by new connection")
	}
}

// Unregister removes a Conn iff it's still the one we registered. (A later
// connection may have replaced it.)
func (h *Hub) Unregister(c *Conn) {
	h.mu.Lock()
	if cur, ok := h.conns[c.SystemID]; ok && cur == c {
		delete(h.conns, c.SystemID)
	}
	h.mu.Unlock()
}

// conn returns the live local connection for a system, or nil.
func (h *Hub) conn(systemID int64) *Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.conns[systemID]
}

// currentConfig loads the newest stored revision as a ConfigPayload. found
// is false when the system has no revisions (the receiver runs defaults).
func (h *Hub) currentConfig(ctx context.Context, systemID int64) (payload ConfigPayload, found bool, err error) {
	version, raw, found, err := h.db.CurrentReceiverConfig(ctx, systemID)
	if err != nil || !found {
		return ConfigPayload{}, false, err
	}
	cfg, err := rxproto.Normalize(raw)
	if err != nil {
		return ConfigPayload{}, false, err
	}
	return ConfigPayload{Version: version, Config: cfg}, true, nil
}

// pushConfig sends the newest revision to whichever local Conn (if any)
// holds the socket for system_id. Fired by the receiver_config NOTIFY,
// which both the revisions insert trigger and the older systems-row update
// trigger raise. Only proto-2 receivers understand the typed payload.
func (h *Hub) pushConfig(ctx context.Context, systemID int64) {
	c := h.conn(systemID)
	if c == nil || c.proto < 2 {
		return
	}
	payload, found, err := h.currentConfig(ctx, systemID)
	if err != nil {
		slog.Warn("push config: load revision", "system_id", systemID, "err", err)
		return
	}
	if !found {
		return
	}
	c.send(Frame{Type: rxproto.FrameConfig, Data: mustJSON(payload)})
	slog.Info("config pushed to receiver", "system_id", systemID, "version", payload.Version)
}

// welcomeConfig returns the config a connecting proto-2 receiver should run.
// When the system has no revision yet, revision 1 is seeded from the config
// the receiver reported in hello (its env bootstrap / defaults), so a
// receiver tuned by hand before remote management keeps that tuning.
func (h *Hub) welcomeConfig(ctx context.Context, systemID int64, hello *HelloPayload) *ConfigPayload {
	payload, found, err := h.currentConfig(ctx, systemID)
	if err != nil {
		slog.Warn("welcome: load config revision", "system_id", systemID, "err", err)
		return nil
	}
	if found {
		return &payload
	}

	receiverVersion := hello.Version
	if receiverVersion == "" {
		receiverVersion = "unknown version"
	}
	cfg := rxproto.Defaults()
	note := "seeded from receiver " + receiverVersion
	switch {
	case hello.Config == nil:
		note += " (no config reported; defaults)"
	case hello.Config.Effective.Validate() != nil:
		slog.Warn("receiver reported an invalid config, seeding defaults",
			"system_id", systemID, "errors", hello.Config.Effective.Validate())
		note += " (reported config invalid; defaults)"
	default:
		cfg = hello.Config.Effective
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	rev, seeded, err := h.db.SeedReceiverConfigRevision(ctx, systemID, raw, "seeded-from-receiver", note)
	if err != nil {
		slog.Warn("seed receiver config", "system_id", systemID, "err", err)
		return nil
	}
	if seeded {
		slog.Info("seeded receiver config revision 1", "system_id", systemID, "receiver_version", hello.Version)
	}
	normalized, err := rxproto.Normalize(rev.Config)
	if err != nil {
		slog.Warn("seeded config does not decode", "system_id", systemID, "err", err)
		return nil
	}
	return &ConfigPayload{Version: rev.Version, Config: normalized}
}

// latestVersion asks the configured release lookup, bounded so a slow
// upstream cannot hold up the welcome.
func (h *Hub) latestVersion(ctx context.Context) string {
	if h.LatestVersion == nil {
		return ""
	}
	lctx, cancel := context.WithTimeout(ctx, latestVersionTimeout)
	defer cancel()
	return h.LatestVersion(lctx)
}

// deliverQueued ships every still-queued command for the conn's system,
// oldest first. Called right after welcome on a proto-2 connect.
// unansweredCommandGrace is how long a sent command may go without a result
// before a reconnecting receiver causes it to be expired. Long enough for a
// restart or update to come back and confirm, short enough that the UI
// does not poll a dead command all day.
const unansweredCommandGrace = 2 * time.Minute

func (h *Hub) deliverQueued(ctx context.Context, c *Conn) {
	if n, err := h.db.ExpireUnansweredSystemCommands(ctx, c.SystemID, unansweredCommandGrace); err != nil {
		slog.Warn("expire unanswered commands", "system_id", c.SystemID, "err", err)
	} else if n > 0 {
		slog.Info("expired unanswered commands", "system_id", c.SystemID, "count", n)
	}
	cmds, err := h.db.QueuedSystemCommands(ctx, c.SystemID)
	if err != nil {
		slog.Warn("load queued commands", "system_id", c.SystemID, "err", err)
		return
	}
	for i := range cmds {
		h.sendCommand(ctx, c, &cmds[i])
	}
}

// sendCommand claims a queued command and ships it on c. Claiming first
// (queued → sent, conditional) means the NOTIFY path and the connect-time
// path can both try the same command and only one sends it. A frame that
// does not fit the send buffer is put back to queued for the next connect.
func (h *Hub) sendCommand(ctx context.Context, c *Conn, cmd *db.SystemCommand) bool {
	if c.proto < 2 || cmd.Status != db.CommandQueued {
		return false
	}
	won, err := h.db.ClaimSystemCommand(ctx, cmd.ID)
	if err != nil {
		slog.Warn("claim command", "command_id", cmd.ID, "err", err)
		return false
	}
	if !won {
		return false
	}
	frame := Frame{
		Type: rxproto.FrameCommand,
		ID:   cmd.ID,
		Data: mustJSON(rxproto.Command{ID: cmd.ID, Type: cmd.Type, Params: cmd.Params}),
	}
	if !c.trySend(frame) {
		if err := h.db.UpdateSystemCommandStatus(ctx, cmd.ID, db.CommandQueued, "", ""); err != nil {
			slog.Warn("requeue command", "command_id", cmd.ID, "err", err)
		}
		return false
	}
	cmd.Status = db.CommandSent
	slog.Info("command sent to receiver", "system_id", c.SystemID, "command_id", cmd.ID, "type", cmd.Type)
	return true
}

// DeliverCommand sends the command with this id to its receiver if that
// receiver is connected to this pod with proto >= 2 and the command is
// still queued; otherwise the row stays queued for delivery on connect.
// Reports whether this pod handed it over. The receiver_command NOTIFY
// listener calls it; the admin API only inserts the row and relies on the
// insert trigger.
func (h *Hub) DeliverCommand(ctx context.Context, id string) bool {
	cmd, err := h.db.GetSystemCommand(ctx, id)
	if err != nil {
		if !errors.Is(err, db.ErrNotFound) {
			slog.Warn("receiver_command: load command", "command_id", id, "err", err)
		}
		return false
	}
	c := h.conn(cmd.SystemID)
	if c == nil {
		return false
	}
	return h.sendCommand(ctx, c, cmd)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// Close gracefully closes every active conn — called on server shutdown.
func (h *Hub) Close() {
	h.mu.Lock()
	conns := make([]*Conn, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	h.conns = map[int64]*Conn{}
	h.mu.Unlock()
	for _, c := range conns {
		c.closeWithReason("server shutdown")
	}
}

// silence unused-import linters when websocket isn't referenced elsewhere.
var _ = websocket.StatusNormalClosure
