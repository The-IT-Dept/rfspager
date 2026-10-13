package recvhub

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// statsStore is the slice of the database the broker needs. *db.DB
// satisfies it; tests substitute a fake.
type statsStore interface {
	InsertSystemStats(ctx context.Context, systemID int64, ts time.Time, stats []byte) error
	UpdateSystemConnectionStats(ctx context.Context, systemID int64, stats []byte, at time.Time) error
	RecentSystemStats(ctx context.Context, systemID int64, limit int) ([]db.SystemStatsRow, error)
}

// StatsBroker persists every stats report a receiver sends and fans it out
// to subscribers of that system (admin health tiles and charts). Shaped
// like LogBroker: a Publish from the websocket read loop, buffered
// per-subscriber channels, slow consumers dropped rather than blocking.
type StatsBroker struct {
	store statsStore

	mu          sync.RWMutex
	subscribers map[int64]map[chan []byte]struct{} // system_id → set of channels
}

func NewStatsBroker(store statsStore) *StatsBroker {
	return &StatsBroker{
		store:       store,
		subscribers: make(map[int64]map[chan []byte]struct{}),
	}
}

// StatsEnvelope is the one shape every stats consumer sees, on the REST
// history endpoint and the SSE stream alike:
//
//	{"ts": <unix milliseconds, server receive time>, "stats": <rxproto.Stats>}
func StatsEnvelope(ts time.Time, stats []byte) []byte {
	if len(stats) == 0 {
		stats = []byte("null")
	}
	b, _ := json.Marshal(map[string]any{
		"ts":    ts.UnixMilli(),
		"stats": json.RawMessage(stats),
	})
	return b
}

// Publish stores the report against the server's receive time, caches it on
// the connection row for list views, and broadcasts the envelope.
func (b *StatsBroker) Publish(ctx context.Context, systemID int64, stats rxproto.Stats) {
	ts := time.Now().UTC()
	raw, err := json.Marshal(stats)
	if err != nil {
		slog.Warn("marshal receiver stats failed", "system_id", systemID, "err", err)
		return
	}
	if err := b.store.InsertSystemStats(ctx, systemID, ts, raw); err != nil {
		slog.Warn("persist system stats failed", "system_id", systemID, "err", err)
	}
	if err := b.store.UpdateSystemConnectionStats(ctx, systemID, raw, ts); err != nil {
		slog.Warn("cache system stats on connection failed", "system_id", systemID, "err", err)
	}

	envelope := StatsEnvelope(ts, raw)
	// Hold the read lock across the sends: cancel() deletes and closes the
	// channel under the write lock, so no send can race a close or a map
	// write. The sends are non-blocking, so the lock is held only briefly.
	b.mu.RLock()
	for ch := range b.subscribers[systemID] {
		select {
		case ch <- envelope:
		default:
			// Slow consumer — drop rather than block the WS hot path.
		}
	}
	b.mu.RUnlock()
}

// Subscribe returns a channel that receives every subsequent stats envelope
// for systemID. The returned cleanup must be called by the subscriber.
func (b *StatsBroker) Subscribe(systemID int64) (<-chan []byte, func()) {
	ch := make(chan []byte, 16)
	b.mu.Lock()
	if _, ok := b.subscribers[systemID]; !ok {
		b.subscribers[systemID] = make(map[chan []byte]struct{})
	}
	b.subscribers[systemID][ch] = struct{}{}
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if subs, ok := b.subscribers[systemID]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(b.subscribers, systemID)
			}
		}
		b.mu.Unlock()
		close(ch)
	}
}

// Recent returns the newest `limit` envelopes, oldest first, for backfilling
// a new stream subscriber.
func (b *StatsBroker) Recent(ctx context.Context, systemID int64, limit int) ([][]byte, error) {
	rows, err := b.store.RecentSystemStats(ctx, systemID, limit)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0, len(rows))
	for _, row := range rows {
		out = append(out, StatsEnvelope(row.TS, row.Stats))
	}
	return out, nil
}
