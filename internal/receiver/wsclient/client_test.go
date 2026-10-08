package wsclient

import (
	"io"
	"log/slog"
	"testing"
)

// A full send buffer with log shipping on used to recurse forever: the drop
// warning was shipped as a log frame, which was dropped, which warned...
func TestEnqueueFullBufferWithLogShippingDoesNotRecurse(t *testing.T) {
	c := New("https://example.invalid", "key")
	for range cap(c.sendCh) {
		c.sendCh <- Frame{Type: "page"}
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(NewLogShippingHandler(slog.NewTextHandler(io.Discard, nil), c)))
	defer slog.SetDefault(prev)

	for range 100 {
		c.Send("p25.audio", map[string]int{"seq": 1})
	}
	if got := c.dropped.Load(); got < 100 {
		t.Fatalf("dropped = %d, want ≥ 100", got)
	}
}
