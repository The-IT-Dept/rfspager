package mobile

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

// SSEBroker manages Server-Sent Events connections for real-time message
// streaming to the mobile app. Driven by Postgres LISTEN/NOTIFY (channel
// rfs_message) — see Run.
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

// Run consumes message IDs from wakeCh (driven by db.Listener subscribed to
// rfs_message) and broadcasts each one to connected SSE clients. Blocks
// until ctx is cancelled.
func (b *SSEBroker) Run(ctx context.Context, wakeCh <-chan string) {
	slog.Info("mobile SSE broker started")
	defer slog.Info("mobile SSE broker stopped")
	for {
		select {
		case payload, ok := <-wakeCh:
			if !ok {
				return
			}
			id, err := strconv.ParseInt(payload, 10, 64)
			if err != nil {
				slog.Warn("mobile SSE: bad payload", "payload", payload, "error", err)
				continue
			}
			b.BroadcastNewMessage(id)
		case <-ctx.Done():
			return
		}
	}
}

// Broadcast sends a message event to all connected SSE clients.
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

// BroadcastNewMessage sends a new message notification to all SSE clients.
func (b *SSEBroker) BroadcastNewMessage(messageID int64) {
	row := b.db.QueryRowx(
		`SELECT m.id, m.address, m.message, m.lat, m.lng, m.created_at,
		 c.alias as capcode_alias, c.icon as capcode_icon, c.colour as capcode_colour,
		 a.agency as agency_name
		 FROM messages m
		 LEFT JOIN capcodes c ON c.id = m.capcode_id
		 LEFT JOIN agencies a ON a.id = c.agency_id
		 WHERE m.id = $1`, messageID)

	result := make(map[string]any)
	if err := row.MapScan(result); err != nil {
		return
	}
	cleanMapScan(result)

	data, err := json.Marshal(result)
	if err != nil {
		return
	}
	b.Broadcast(data)
}

// Handler is the HTTP handler for SSE connections.
func (b *SSEBroker) Handler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	// Push headers + an initial keepalive so HTTP clients unblock from
	// http.Client.Do() and EventSource starts dispatching readyState=OPEN.
	w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	ch := make(chan []byte, 64)
	b.mu.Lock()
	b.clients[ch] = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
		close(ch)
	}()

	// Send keepalive comment every 30s
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	slog.Info("SSE client connected", "remote", r.RemoteAddr)

	for {
		select {
		case data := <-ch:
			w.Write([]byte("event: message\ndata: "))
			w.Write(data)
			w.Write([]byte("\n\n"))
			flusher.Flush()
		case <-ticker.C:
			w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		case <-r.Context().Done():
			slog.Info("SSE client disconnected", "remote", r.RemoteAddr)
			return
		}
	}
}
