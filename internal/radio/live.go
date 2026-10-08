package radio

import (
	"encoding/json"
	"sync"
)

// LiveBroker fans decoded voice out to listeners as it arrives from
// receivers (GET /radio/live). Unlike Broker it is fed straight from the
// ingest, not Postgres NOTIFY: audio is too high-rate for NOTIFY, and isn't
// stored per chunk. So a listener only hears receivers connected to the same
// server process. That is fine while there is one instance; with several,
// listeners would need routing to the receiving pod (or a shared bus).
//
// A listener that falls behind is dropped rather than slowing the ingest or
// being fed audio with holes in it; the client reconnects.
type LiveBroker struct {
	mu   sync.Mutex
	subs map[*LiveSub]struct{}
}

func NewLiveBroker() *LiveBroker {
	return &LiveBroker{subs: map[*LiveSub]struct{}{}}
}

// LiveEvent is one SSE event: Event is "audio" or "end", Data its JSON.
type LiveEvent struct {
	Event string
	Data  []byte
}

// LiveAudio is the data of an "audio" event: one chunk of a call's voice,
// 8 kHz mono signed 16-bit little-endian PCM, base64 in JSON.
type LiveAudio struct {
	CallUID string `json:"call_id"` // the receiver's call id
	CallID  int64  `json:"call"`    // radio_calls.id
	TGID    *int   `json:"tgid"`    // null for unit-to-unit calls
	Seq     int    `json:"seq"`
	PCM     []byte `json:"pcm"`
}

// LiveEnd is the data of an "end" event: no more audio for this call.
type LiveEnd struct {
	CallUID string `json:"call_id"`
	CallID  int64  `json:"call"`
	TGID    *int   `json:"tgid"`
}

// liveBuffer is how many events a listener may fall behind by. A chunk is
// typically a fraction of a second of audio, so this is several seconds even
// with a few calls at once.
const liveBuffer = 256

type LiveSub struct {
	ch   chan LiveEvent
	gone chan struct{}
	tgs  map[int]bool // empty: every talkgroup (and unit-to-unit calls)
}

// Events delivers the listener's events.
func (s *LiveSub) Events() <-chan LiveEvent { return s.ch }

// Gone is closed when the broker drops the listener for falling behind.
func (s *LiveSub) Gone() <-chan struct{} { return s.gone }

func (s *LiveSub) wants(tgid int) bool {
	return len(s.tgs) == 0 || (tgid != 0 && s.tgs[tgid])
}

// Subscribe adds a listener for the given talkgroups (none: all). The
// returned func removes it.
func (b *LiveBroker) Subscribe(tgs []int) (*LiveSub, func()) {
	s := &LiveSub{ch: make(chan LiveEvent, liveBuffer), gone: make(chan struct{}), tgs: map[int]bool{}}
	for _, tg := range tgs {
		s.tgs[tg] = true
	}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s, func() {
		b.mu.Lock()
		delete(b.subs, s)
		b.mu.Unlock()
	}
}

// PublishAudio forwards a chunk of a known call on a visible talkgroup.
// Audio for calls not yet tied to a row, or on hidden talkgroups, is never
// sent: we can't tell subscribers what it is, or they mustn't hear it.
func (b *LiveBroker) PublishAudio(meta CallMeta, uid string, seq int, pcm []byte) {
	if !meta.Known || meta.Silent() {
		return
	}
	b.publish(meta.TGID, "audio", func() any {
		return LiveAudio{CallUID: uid, CallID: meta.ID, TGID: tgidPtr(meta.TGID), Seq: seq, PCM: pcm}
	})
}

// PublishEnd tells listeners a call's audio is over.
func (b *LiveBroker) PublishEnd(meta CallMeta, uid string) {
	if !meta.Known || meta.Silent() {
		return
	}
	b.publish(meta.TGID, "end", func() any {
		return LiveEnd{CallUID: uid, CallID: meta.ID, TGID: tgidPtr(meta.TGID)}
	})
}

// publish marshals the event once, and only if someone wants it.
func (b *LiveBroker) publish(tgid int, event string, payload func() any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var ev LiveEvent
	for s := range b.subs {
		if !s.wants(tgid) {
			continue
		}
		if ev.Data == nil {
			data, err := json.Marshal(payload())
			if err != nil {
				return
			}
			ev = LiveEvent{Event: event, Data: data}
		}
		select {
		case s.ch <- ev:
		default:
			delete(b.subs, s)
			close(s.gone)
		}
	}
}

func tgidPtr(tgid int) *int {
	if tgid == 0 {
		return nil
	}
	return &tgid
}
