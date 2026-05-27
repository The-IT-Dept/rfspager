package recvhub

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/the-it-dept/rfspager/internal/db"
)

// HistorySize is how many frames we retain per system. At ~5 fps that's
// ~12 seconds of waterfall history, which is what a freshly-opened
// detail page sees before live frames take over.
const HistorySize = 60

// SnapshotInterval is how often the broker persists each system's ring
// buffer back to Postgres. Bounded write rate, restart-resilient.
const SnapshotInterval = 5 * time.Second

// SpectrumBroker fans out diagnostic spectrum frames from receiver agents
// to admin/mobile clients, and keeps a per-system ring buffer of recent
// frames that pre-populate any newly-opened detail page.
type SpectrumBroker struct {
	db *db.DB

	mu      sync.RWMutex
	clients map[int64]map[chan []byte]struct{}
	hist    map[int64]*history
}

// history is the in-memory ring buffer for one system.
type history struct {
	centreHz   uint32
	sampleRate uint32
	frames     [][]float32 // most recent at the end
	ts         []int64     // unix millis per frame
}

// HistoryFrame is what we publish on (re)subscribe — the same shape as a
// live SpectrumPayload plus a timestamp.
type HistoryFrame struct {
	Ts         int64     `json:"ts"`
	CentreHz   uint32    `json:"centre_hz"`
	SampleRate uint32    `json:"sample_rate"`
	Bins       []float32 `json:"bins"`
}

// HistoryBundle is the JSON shape we ship at the start of an SSE/WS
// connection so the client can render the last N frames instantly.
type HistoryBundle struct {
	Frames []HistoryFrame `json:"frames"`
}

func NewSpectrumBroker(database *db.DB) *SpectrumBroker {
	return &SpectrumBroker{
		db:      database,
		clients: map[int64]map[chan []byte]struct{}{},
		hist:    map[int64]*history{},
	}
}

// Start launches background workers: periodic Postgres snapshot + initial
// snapshot load so a restart doesn't wipe out everyone's history.
func (b *SpectrumBroker) Start(ctx context.Context) {
	go b.runSnapshotter(ctx)
}

// Subscribe returns a buffered chan that receives JSON-encoded spectrum
// frames for the given system, plus an unsubscribe func. The first
// message on the channel is a HistoryBundle (if any history exists);
// subsequent messages are individual live SpectrumPayload frames.
func (b *SpectrumBroker) Subscribe(systemID int64) (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	b.mu.Lock()
	subs := b.clients[systemID]
	if subs == nil {
		subs = map[chan []byte]struct{}{}
		b.clients[systemID] = subs
	}
	subs[ch] = struct{}{}
	h := b.hist[systemID]
	b.mu.Unlock()

	// Pre-pump history (non-blocking; on a 64-deep buffer we always fit).
	if h != nil && len(h.frames) > 0 {
		bundle := HistoryBundle{Frames: make([]HistoryFrame, 0, len(h.frames))}
		for i := range h.frames {
			bundle.Frames = append(bundle.Frames, HistoryFrame{
				Ts:         h.ts[i],
				CentreHz:   h.centreHz,
				SampleRate: h.sampleRate,
				Bins:       h.frames[i],
			})
		}
		raw, _ := json.Marshal(struct {
			Type string        `json:"type"`
			Data HistoryBundle `json:"data"`
		}{Type: "history", Data: bundle})
		select {
		case ch <- raw:
		default:
		}
	}

	return ch, func() {
		b.mu.Lock()
		if subs, ok := b.clients[systemID]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(b.clients, systemID)
			}
		}
		b.mu.Unlock()
		close(ch)
	}
}

// Publish fans a frame out to all subscribers for systemID and appends it
// to the in-memory ring buffer. The published payload is wrapped with a
// `{"type":"frame", "data": <SpectrumPayload>}` envelope so SSE clients
// can distinguish history bundles from live frames.
func (b *SpectrumBroker) Publish(systemID int64, centreHz, sampleRate uint32, bins []float32) {
	now := time.Now().UnixMilli()

	// Update ring buffer.
	b.mu.Lock()
	h := b.hist[systemID]
	if h == nil {
		h = &history{
			centreHz:   centreHz,
			sampleRate: sampleRate,
			frames:     make([][]float32, 0, HistorySize),
			ts:         make([]int64, 0, HistorySize),
		}
		b.hist[systemID] = h
	}
	h.centreHz = centreHz
	h.sampleRate = sampleRate
	if len(h.frames) >= HistorySize {
		h.frames = h.frames[1:]
		h.ts = h.ts[1:]
	}
	// Copy bins so caller can reuse its buffer.
	binsCopy := make([]float32, len(bins))
	copy(binsCopy, bins)
	h.frames = append(h.frames, binsCopy)
	h.ts = append(h.ts, now)
	subsCopy := make([]chan []byte, 0, len(b.clients[systemID]))
	for c := range b.clients[systemID] {
		subsCopy = append(subsCopy, c)
	}
	b.mu.Unlock()

	// Build the wire frame.
	raw, _ := json.Marshal(struct {
		Type string       `json:"type"`
		Data HistoryFrame `json:"data"`
	}{
		Type: "frame",
		Data: HistoryFrame{Ts: now, CentreHz: centreHz, SampleRate: sampleRate, Bins: binsCopy},
	})

	for _, ch := range subsCopy {
		select {
		case ch <- raw:
		default:
		}
	}
}

// HasSubscribers reports whether anyone is currently watching this system.
func (b *SpectrumBroker) HasSubscribers(systemID int64) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients[systemID]) > 0
}

// runSnapshotter periodically writes each system's ring buffer to Postgres.
// Single goroutine, bounded write rate.
func (b *SpectrumBroker) runSnapshotter(ctx context.Context) {
	t := time.NewTicker(SnapshotInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.flushSnapshots(ctx)
		}
	}
}

func (b *SpectrumBroker) flushSnapshots(ctx context.Context) {
	type pending struct {
		id     int64
		bundle HistoryBundle
	}
	var todo []pending

	b.mu.RLock()
	for id, h := range b.hist {
		if len(h.frames) == 0 {
			continue
		}
		bundle := HistoryBundle{Frames: make([]HistoryFrame, 0, len(h.frames))}
		for i := range h.frames {
			bundle.Frames = append(bundle.Frames, HistoryFrame{
				Ts:         h.ts[i],
				CentreHz:   h.centreHz,
				SampleRate: h.sampleRate,
				Bins:       h.frames[i],
			})
		}
		todo = append(todo, pending{id: id, bundle: bundle})
	}
	b.mu.RUnlock()

	for _, p := range todo {
		raw, err := json.Marshal(p.bundle)
		if err != nil {
			continue
		}
		_ = b.db.UpsertSpectrumSnapshot(ctx, p.id, raw)
	}
}

// LoadSnapshot rehydrates a system's in-memory ring buffer from the most
// recent Postgres snapshot. Called lazily on first publish/subscribe — we
// don't pre-load every system on startup.
func (b *SpectrumBroker) LoadSnapshot(ctx context.Context, systemID int64) {
	b.mu.RLock()
	_, exists := b.hist[systemID]
	b.mu.RUnlock()
	if exists {
		return
	}
	payload, _, err := b.db.GetSpectrumSnapshot(ctx, systemID)
	if err != nil || payload == nil {
		return
	}
	var bundle HistoryBundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return
	}
	h := &history{
		frames: make([][]float32, 0, len(bundle.Frames)),
		ts:     make([]int64, 0, len(bundle.Frames)),
	}
	for _, f := range bundle.Frames {
		h.centreHz = f.CentreHz
		h.sampleRate = f.SampleRate
		h.frames = append(h.frames, f.Bins)
		h.ts = append(h.ts, f.Ts)
	}
	b.mu.Lock()
	if _, exists := b.hist[systemID]; !exists {
		b.hist[systemID] = h
	}
	b.mu.Unlock()
}
