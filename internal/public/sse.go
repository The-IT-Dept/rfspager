package public

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

// SSEBroker manages public SSE connections for the pager feed. It is driven
// by Postgres LISTEN/NOTIFY (channel rfs_message) — every pod's broker is
// woken on every new message and broadcasts to its own connected clients.
type SSEBroker struct {
	db      *sqlx.DB
	clients map[chan []byte]bool
	mu      sync.RWMutex
}

func NewSSEBroker(db *sqlx.DB) *SSEBroker {
	return &SSEBroker{
		db:      db,
		clients: make(map[chan []byte]bool),
	}
}

// Handler serves the SSE endpoint.
func (b *SSEBroker) Handler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 16)
	b.mu.Lock()
	b.clients[ch] = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
		close(ch)
	}()

	// Send initial keepalive
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-time.After(30 * time.Second):
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// Broadcast sends a message to all connected SSE clients.
func (b *SSEBroker) Broadcast(data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- data:
		default:
			// Client too slow, skip
		}
	}
}

// Run consumes message IDs from wakeCh (driven by db.Listener subscribed to
// rfs_message) and broadcasts each one to connected SSE clients. Blocks until
// ctx is cancelled.
func (b *SSEBroker) Run(ctx context.Context, wakeCh <-chan string) {
	slog.Info("public SSE broker started")
	defer slog.Info("public SSE broker stopped")
	for {
		select {
		case payload, ok := <-wakeCh:
			if !ok {
				return
			}
			id, err := strconv.ParseInt(payload, 10, 64)
			if err != nil {
				slog.Warn("public SSE: bad payload", "payload", payload, "error", err)
				continue
			}
			b.broadcastByID(ctx, id)
		case <-ctx.Done():
			return
		}
	}
}

// broadcastByID fetches a single message by id and broadcasts it. Filters to
// non-deleted messages with a known capcode (matching the previous poll's
// WHERE clause) — public feed only shows messages we can decorate.
func (b *SSEBroker) broadcastByID(ctx context.Context, id int64) {
	row := b.db.QueryRowxContext(ctx,
		`SELECT m.id, m.address, m.message, m.created_at,
		        c.alias  AS capcode_alias,
		        c.icon   AS capcode_icon,
		        c.colour AS capcode_colour,
		        a.agency AS agency_name,
		        s.system_id AS system_uuid,
		        s.name      AS system_name
		   FROM messages m
		   LEFT JOIN capcodes c ON c.id = m.capcode_id
		   LEFT JOIN agencies a ON a.id = c.agency_id
		   LEFT JOIN systems  s ON s.id = m.system_id AND s.deleted_at IS NULL
		  WHERE m.id = $1
		    AND m.deleted_at IS NULL
		    AND m.capcode_id IS NOT NULL`, id)

	result := make(map[string]any)
	if err := row.MapScan(result); err != nil {
		// Row was deleted between trigger fire and select, or capcode unknown.
		return
	}
	cleanMapScan(result)

	data, err := json.Marshal(result)
	if err != nil {
		slog.Error("public SSE: marshal failed", "id", id, "error", err)
		return
	}
	b.Broadcast(data)
}

