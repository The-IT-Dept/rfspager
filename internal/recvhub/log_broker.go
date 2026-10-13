package recvhub

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

// LogBroker fans out persisted log lines from a receiver to anyone
// subscribed to that system (owner mobile/web, admin). It writes every
// line to system_logs (capped at 7-day retention by rotateLogs).
type LogBroker struct {
	db *sqlx.DB

	mu          sync.RWMutex
	subscribers map[int64]map[chan []byte]struct{} // system_id → set of channels
}

func NewLogBroker(db *sqlx.DB) *LogBroker {
	return &LogBroker{
		db:          db,
		subscribers: make(map[int64]map[chan []byte]struct{}),
	}
}

// Publish persists the log line and fans it out. Called from the WS read
// loop on every `log` frame the receiver sends.
func (b *LogBroker) Publish(ctx context.Context, systemID int64, p LogPayload) {
	ts := time.UnixMilli(p.TS)
	if p.TS <= 0 {
		ts = time.Now().UTC()
	}
	level := p.Level
	if level == "" {
		level = "info"
	}
	rawJSON, _ := json.Marshal(p.Raw)

	if _, err := b.db.ExecContext(ctx, `
		INSERT INTO system_logs (system_id, ts, level, source, message, raw)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6)`,
		systemID, ts, level, p.Source, p.Message, rawJSON,
	); err != nil {
		slog.Warn("persist system log failed", "system_id", systemID, "err", err)
	}

	// Build the broadcast envelope. Match the shape we expose on the SSE
	// stream so subscribers can render directly without a second decode.
	envelope, _ := json.Marshal(map[string]any{
		"ts":      ts.UnixMilli(),
		"level":   level,
		"source":  p.Source,
		"message": p.Message,
		"raw":     p.Raw,
	})

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

// Subscribe returns a channel that receives every subsequent log envelope
// for systemID. The returned cleanup must be called by the subscriber.
func (b *LogBroker) Subscribe(systemID int64) (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
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

// RecentLogs returns the last `limit` log rows for backfilling new
// subscribers. Ordered oldest-first so the client can append in order.
func (b *LogBroker) RecentLogs(ctx context.Context, systemID int64, limit int) ([]map[string]any, error) {
	if limit < 1 || limit > 500 {
		limit = 200
	}
	rows, err := b.db.QueryxContext(ctx, `
		SELECT ts, level, source, message, raw
		  FROM system_logs
		 WHERE system_id = $1
		 ORDER BY ts DESC LIMIT $2`, systemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		row := make(map[string]any)
		if err := rows.MapScan(row); err != nil {
			continue
		}
		if b, ok := row["message"].([]byte); ok {
			row["message"] = string(b)
		}
		if b, ok := row["source"].([]byte); ok {
			row["source"] = string(b)
		}
		if b, ok := row["level"].([]byte); ok {
			row["level"] = string(b)
		}
		// Convert ts to unix-millis for the stream consistency.
		if ts, ok := row["ts"].(time.Time); ok {
			row["ts"] = ts.UnixMilli()
		}
		out = append(out, row)
	}
	// Reverse to oldest-first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// RotateOlderThan deletes log rows older than `cutoff`. Called daily by the
// hub's rotateLogs goroutine.
func (b *LogBroker) RotateOlderThan(ctx context.Context, cutoff time.Duration) (int64, error) {
	res, err := b.db.ExecContext(ctx,
		`DELETE FROM system_logs WHERE received_at < NOW() - $1::interval`,
		cutoff.String())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
