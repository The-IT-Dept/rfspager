package receiver

import (
	"github.com/the-it-dept/rfspager/internal/rxproto"
	"log/slog"
	"time"
)

// MultiSender delivers every page to all configured upstreams.
//
// One dongle, several backends: during the cutover a receiver feeds the legacy
// Laravel ingress and the Go backend at the same time, so both see identical
// traffic from identical hardware and can be compared directly. Without it,
// proving the new stack means a second Pi on the same antenna.
//
// Delivery is independent per upstream. Each underlying sender is already
// fire-and-forget — wsclient queues and drops when its buffer is full, legacy
// posts in its own goroutine — so one slow or dead backend cannot stall the
// decode path or starve the others.
type MultiSender struct {
	senders []NamedSender
}

// NamedSender pairs a transport with the endpoint name, so a failure can be
// attributed to a specific upstream in the logs.
type NamedSender struct {
	Name   string
	Sender PageSender
}

func NewMultiSender(senders []NamedSender) *MultiSender {
	return &MultiSender{senders: senders}
}

// Len reports how many upstreams are attached.
func (m *MultiSender) Len() int { return len(m.senders) }

// Names lists the attached upstreams, for startup logging.
func (m *MultiSender) Names() []string {
	out := make([]string, 0, len(m.senders))
	for _, s := range m.senders {
		out = append(out, s.Name)
	}
	return out
}

func (m *MultiSender) SendPage(addr, msg string, when time.Time, fn, baud int) {
	m.SendPageQ(addr, msg, when, fn, baud, nil)
}

// SendPageQ fans the page out with its decode quality to transports that
// carry it, and without to those that do not.
func (m *MultiSender) SendPageQ(addr, msg string, when time.Time, fn, baud int, q *rxproto.PageQuality) {
	for _, s := range m.senders {
		// Guard each upstream: a panic in one transport must not lose the page
		// for the others, and must not take the receiver down.
		func(s NamedSender) {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("page send panicked", "endpoint", s.Name, "panic", r)
				}
			}()
			if qs, ok := s.Sender.(QualitySender); ok {
				qs.SendPageQ(addr, msg, when, fn, baud, q)
			} else {
				s.Sender.SendPage(addr, msg, when, fn, baud)
			}
		}(s)
	}
}
