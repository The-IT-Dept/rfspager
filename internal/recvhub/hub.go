package recvhub

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/notify"
)

// PingInterval is how often we expect receivers to ping. We bump
// last_ping_at every time we see one; admin queries treat anything older
// than ~3× this as offline.
const PingInterval = 30 * time.Second

// PingTimeout closes the socket if no ping arrives within this window.
const PingTimeout = 90 * time.Second

// PodID is set once at startup by main; included in system_connections rows
// so each pod can identify its own entries.
var PodID string

// Hub owns the per-pod set of live receiver websocket connections plus the
// LISTEN goroutine that picks up cross-pod NOTIFY events.
type Hub struct {
	db       *db.DB
	listener *db.Listener
	notifier *notify.Notifier

	Logs *LogBroker

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
		conns:    make(map[int64]*Conn),
	}, nil
}

// Start launches background goroutines: cross-pod config NOTIFY listener +
// periodic stale-row sweeper. Stops when ctx is cancelled.
func (h *Hub) Start(ctx context.Context) error {
	configCh, err := h.listener.Listen("receiver_config", 16)
	if err != nil {
		return err
	}
	go h.runConfigListener(ctx, configCh)
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

func (h *Hub) runSweeper(ctx context.Context) {
	tick := time.NewTicker(PingTimeout)
	defer tick.Stop()
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
		}
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

// pushConfig pushes the latest system row to whichever local Conn (if any)
// holds the socket for system_id.
func (h *Hub) pushConfig(ctx context.Context, systemID int64) {
	h.mu.RLock()
	c := h.conns[systemID]
	h.mu.RUnlock()
	if c == nil {
		return
	}
	sys, err := h.db.GetSystemByID(ctx, systemID)
	if err != nil {
		slog.Warn("push config: load system", "system_id", systemID, "err", err)
		return
	}
	payload := ConfigPayload{System: systemToMap(sys)}
	c.send(Frame{Type: "config", Data: mustJSON(payload)})
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func systemToMap(s *models.System) map[string]any {
	m := map[string]any{
		"id":        s.ID,
		"system_id": s.SystemID,
		"name":      s.Name,
		"frequency": s.Frequency,
	}
	if s.Lat.Valid {
		m["lat"] = s.Lat.Float64
	}
	if s.Lng.Valid {
		m["lng"] = s.Lng.Float64
	}
	return m
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
