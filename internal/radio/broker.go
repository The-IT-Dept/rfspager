package radio

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
)

// Broker fans call starts and ends out to SSE subscribers. It is driven by
// Postgres LISTEN on radio_call (see migration 032), so every pod sees calls
// ingested by any pod, the same way the mobile message SSE works.
type Broker struct {
	store   *Store
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func NewBroker(store *Store) *Broker {
	return &Broker{store: store, clients: map[chan []byte]struct{}{}}
}

// Run consumes call ids from wakeCh and broadcasts each call as JSON. Blocks
// until ctx is cancelled. Calls on hidden talkgroups are not broadcast.
func (b *Broker) Run(ctx context.Context, wakeCh <-chan string) {
	slog.Info("radio SSE broker started")
	defer slog.Info("radio SSE broker stopped")
	for {
		select {
		case payload, ok := <-wakeCh:
			if !ok {
				return
			}
			id, err := strconv.ParseInt(payload, 10, 64)
			if err != nil {
				slog.Warn("radio SSE: bad payload", "payload", payload, "error", err)
				continue
			}
			if !b.hasClients() {
				continue
			}
			call, err := b.store.GetCall(ctx, id)
			if err != nil {
				if !IsNotFound(err) {
					slog.Warn("radio SSE: load call", "id", id, "error", err)
				}
				continue
			}
			data, err := json.Marshal(call)
			if err != nil {
				continue
			}
			b.Broadcast(data)
		case <-ctx.Done():
			return
		}
	}
}

// Subscribe registers a client. The returned func unregisters it; the
// channel is never closed, so readers should stop on their own context.
func (b *Broker) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
	}
}

// Broadcast sends data to every client, skipping any that are too slow.
func (b *Broker) Broadcast(data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- data:
		default:
		}
	}
}

func (b *Broker) hasClients() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients) > 0
}
