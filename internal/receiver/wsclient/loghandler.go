package wsclient

import (
	"context"
	"log/slog"
	"strings"
)

// LogShippingHandler is an slog.Handler that mirrors every record to the
// upstream WebSocket via Client.SendLog while preserving the original
// stdout/stderr handler the receiver was using. Best-effort: dropped
// silently if the send buffer is full.
//
// Use it like:
//
//	base := slog.NewTextHandler(os.Stdout, ...)
//	slog.SetDefault(slog.New(wsclient.NewLogShippingHandler(base, ws)))
type LogShippingHandler struct {
	base slog.Handler
	send LogSender
}

// LogSender is the subset of *Client this handler depends on. Decoupled so
// the receiver's main package can swap in a no-op for testing.
type LogSender interface {
	SendLog(level, source, message string, raw map[string]any)
}

func NewLogShippingHandler(base slog.Handler, send LogSender) *LogShippingHandler {
	return &LogShippingHandler{base: base, send: send}
}

func (h *LogShippingHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.base.Enabled(ctx, lvl)
}

func (h *LogShippingHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.send != nil {
		// Skip our own internal "ws write failed" logs — emitting those over
		// the same WS that just failed would loop.
		if !strings.Contains(r.Message, "ws write failed") {
			level := levelToString(r.Level)
			raw := map[string]any{}
			var source string
			r.Attrs(func(a slog.Attr) bool {
				if a.Key == "source" {
					source = a.Value.String()
					return true
				}
				raw[a.Key] = a.Value.Any()
				return true
			})
			h.send.SendLog(level, source, r.Message, raw)
		}
	}
	return h.base.Handle(ctx, r)
}

func (h *LogShippingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogShippingHandler{base: h.base.WithAttrs(attrs), send: h.send}
}

func (h *LogShippingHandler) WithGroup(name string) slog.Handler {
	return &LogShippingHandler{base: h.base.WithGroup(name), send: h.send}
}

func levelToString(l slog.Level) string {
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
