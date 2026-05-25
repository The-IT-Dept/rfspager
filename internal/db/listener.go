package db

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/lib/pq"
)

// Listener wraps pq.Listener and demuxes NOTIFY events from one persistent
// connection to per-channel Go chans. pq handles reconnect under the hood;
// when it reconnects, our LISTEN statements are re-issued automatically.
type Listener struct {
	inner *pq.Listener
	mu    sync.RWMutex
	subs  map[string][]chan<- string
	done  chan struct{}
}

// NewListener opens a dedicated PG connection for LISTEN/NOTIFY.
func NewListener(databaseURL string) (*Listener, error) {
	l := &Listener{
		subs: map[string][]chan<- string{},
		done: make(chan struct{}),
	}
	report := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			slog.Error("pg listener event", "event", ev, "error", err)
			return
		}
		slog.Info("pg listener event", "event", ev)
	}
	// pq.Listener establishes its connection asynchronously on a goroutine;
	// it will fire a ConnectionEvent when it's up (or surface failures via the
	// reporter callback). Don't Ping here — it always returns "no connection"
	// before the first event arrives.
	l.inner = pq.NewListener(databaseURL, 10*time.Second, time.Minute, report)
	go l.demux()
	return l, nil
}

// Listen subscribes to a NOTIFY channel and returns a Go chan that receives
// each notification's Extra payload. Multiple subscribers may register for
// the same channel — every NOTIFY fans out to all of them. Buffered;
// channel sends are non-blocking, slow consumers see their notifications
// dropped (workers should keep a reconciliation tick as backstop).
func (l *Listener) Listen(channel string, buf int) (<-chan string, error) {
	ch := make(chan string, buf)
	l.mu.Lock()
	firstSubscriber := len(l.subs[channel]) == 0
	l.subs[channel] = append(l.subs[channel], ch)
	l.mu.Unlock()
	if firstSubscriber {
		if err := l.inner.Listen(channel); err != nil {
			// Roll back the subscription registration.
			l.mu.Lock()
			l.subs[channel] = l.subs[channel][:len(l.subs[channel])-1]
			l.mu.Unlock()
			return nil, err
		}
	}
	return ch, nil
}

func (l *Listener) demux() {
	for {
		select {
		case n := <-l.inner.NotificationChannel():
			if n == nil {
				// nil means the connection was lost and pq is reconnecting.
				continue
			}
			l.mu.RLock()
			subs := l.subs[n.Channel]
			l.mu.RUnlock()
			for _, ch := range subs {
				select {
				case ch <- n.Extra:
				default:
					slog.Warn("pg listener: dropped notify (consumer slow)", "channel", n.Channel)
				}
			}
		case <-time.After(90 * time.Second):
			// Periodic ping keeps the connection alive and surfaces failures.
			if err := l.inner.Ping(); err != nil {
				slog.Error("pg listener ping failed", "error", err)
			}
		case <-l.done:
			return
		}
	}
}

// Close stops demuxing and closes the underlying connection.
func (l *Listener) Close(ctx context.Context) error {
	select {
	case <-l.done:
		return nil
	default:
		close(l.done)
	}
	return l.inner.Close()
}
