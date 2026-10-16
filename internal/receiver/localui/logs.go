package localui

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// LogEntry is one log record as the local UI shows it.
type LogEntry struct {
	ID      int64          `json:"id"`
	TS      int64          `json:"ts"` // unix ms
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// LogBuffer keeps the newest log records in memory for the Logs tab.
type LogBuffer struct {
	mu     sync.Mutex
	ring   []LogEntry
	size   int
	nextID int64
	notify atomic.Pointer[func(LogEntry)]
}

// NewLogBuffer keeps the newest size records.
func NewLogBuffer(size int) *LogBuffer {
	return &LogBuffer{size: size, nextID: 1}
}

func (b *LogBuffer) add(e LogEntry) {
	b.mu.Lock()
	e.ID = b.nextID
	b.nextID++
	b.ring = append(b.ring, e)
	if len(b.ring) > b.size {
		b.ring = append(b.ring[:0:0], b.ring[len(b.ring)-b.size:]...)
	}
	b.mu.Unlock()
	if fn := b.notify.Load(); fn != nil {
		(*fn)(e)
	}
}

// Recent returns up to limit records at or above minLevel, newest first.
func (b *LogBuffer) Recent(limit int, minLevel slog.Level) []LogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]LogEntry, 0, min(limit, len(b.ring)))
	for i := len(b.ring) - 1; i >= 0 && len(out) < limit; i-- {
		if parseLevel(b.ring[i].Level) >= minLevel {
			out = append(out, b.ring[i])
		}
	}
	return out
}

// OnLog sets the function called (synchronously, so keep it cheap) for
// every new record.
func (b *LogBuffer) OnLog(fn func(LogEntry)) { b.notify.Store(&fn) }

// Handler returns an slog.Handler that records into b and passes every
// record on to next.
func (b *LogBuffer) Handler(next slog.Handler) slog.Handler {
	return &logHandler{buf: b, next: next}
}

type logHandler struct {
	buf    *LogBuffer
	next   slog.Handler
	attrs  []slog.Attr
	groups string // "a.b." prefix for attrs added after WithGroup
}

func (h *logHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *logHandler) Handle(ctx context.Context, r slog.Record) error {
	attrs := make(map[string]any, len(h.attrs)+r.NumAttrs())
	for _, a := range h.attrs {
		attrs[a.Key] = attrValue(a.Value)
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs[h.groups+a.Key] = attrValue(a.Value)
		return true
	})
	if len(attrs) == 0 {
		attrs = nil
	}
	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	h.buf.add(LogEntry{TS: ts.UnixMilli(), Level: levelName(r.Level), Message: r.Message, Attrs: attrs})
	return h.next.Handle(ctx, r)
}

func (h *logHandler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append([]slog.Attr{}, h.attrs...)
	for _, a := range as {
		c.attrs = append(c.attrs, slog.Attr{Key: h.groups + a.Key, Value: a.Value})
	}
	c.next = h.next.WithAttrs(as)
	return &c
}

func (h *logHandler) WithGroup(name string) slog.Handler {
	c := *h
	c.groups = h.groups + name + "."
	c.next = h.next.WithGroup(name)
	return &c
}

// attrValue keeps JSON-friendly scalars and stringifies everything else
// (an error marshals as {} otherwise).
func attrValue(v slog.Value) any {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString, slog.KindInt64, slog.KindUint64, slog.KindFloat64, slog.KindBool:
		return v.Any()
	default:
		return v.String()
	}
}

func levelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn"
	case l >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

func parseLevel(s string) slog.Level {
	switch s {
	case "error":
		return slog.LevelError
	case "warn":
		return slog.LevelWarn
	case "info":
		return slog.LevelInfo
	default:
		return slog.LevelDebug
	}
}
