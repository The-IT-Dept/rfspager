package radio

import (
	"sync"
	"time"

	"github.com/the-it-dept/rfspager/internal/p25"
)

// Assembler buffers each call's p25.audio chunks in memory until the call
// ends, then hands back the whole clip. It is bounded: per-call and total
// byte caps, a cap on calls tracked at once, and eviction of calls that stop
// sending without ever ending (receiver gone, call_end lost).
//
// Sequence handling. Chunks reach us in order (one websocket per receiver),
// so a seq lower than the next expected one is a duplicate or a straggler and
// is dropped. A seq higher than expected means the receiver dropped chunks;
// the hole is filled with silence the length of the missing chunks
// (estimated from the previous chunk's size, capped at MaxGapFill) so the
// clip keeps roughly the call's real timing. The first chunk we see starts
// the clip whatever its seq, with no leading silence: after a server restart
// we may join a call part way through.
//
// The state is per process. With more than one server instance a call's
// chunks must all reach the same one, which holds while each receiver keeps
// a single websocket.
type Assembler struct {
	cfg AssemblerConfig

	mu    sync.Mutex
	calls map[string]*pendingCall
	total int // bytes buffered across all calls
	// done remembers recently finished calls so stray chunks after the end
	// (or after eviction) don't start a second clip for the same call.
	done map[string]time.Time
}

type AssemblerConfig struct {
	MaxCallBytes  int           // per call; default 5 minutes of audio
	MaxTotalBytes int           // across all calls; default 64 MiB
	MaxCalls      int           // tracked at once; default 256
	MaxGapFill    int           // silence bytes per gap; default 1 second
	IdleTimeout   time.Duration // evict after no frames for this long; default 30s
	MaxAge        time.Duration // evict regardless after this long; default 10 minutes
}

func (c *AssemblerConfig) defaults() {
	const bytesPerSecond = p25.AudioSampleRate * 2
	if c.MaxCallBytes <= 0 {
		c.MaxCallBytes = 5 * 60 * bytesPerSecond
	}
	if c.MaxTotalBytes <= 0 {
		c.MaxTotalBytes = 64 << 20
	}
	if c.MaxCalls <= 0 {
		c.MaxCalls = 256
	}
	if c.MaxGapFill <= 0 {
		c.MaxGapFill = bytesPerSecond
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 30 * time.Second
	}
	if c.MaxAge <= 0 {
		c.MaxAge = 10 * time.Minute
	}
}

// doneTTL is how long a finished call's id is remembered.
const doneTTL = 10 * time.Minute

// CallMeta is what the ingest knows about a call from its radio_calls row.
// Known is false until that row has been looked up (and found).
type CallMeta struct {
	Known  bool
	ID     int64
	TGID   int // 0 for unit-to-unit calls
	Hidden bool
}

// Clip is a finished call's audio.
type Clip struct {
	UID       string
	Meta      CallMeta
	PCM       []byte
	StartedAt time.Time // when its first chunk arrived
	Chunks    int       // chunks received
	Missing   int       // chunks missing (filled with silence)
	Truncated bool      // hit a byte cap; later audio was dropped
	Evicted   bool      // never ended; flushed by Sweep
}

type pendingCall struct {
	clip      Clip
	createdAt time.Time
	lastAt    time.Time
	nextSeq   int // -1 until the first chunk
	lastLen   int
}

func NewAssembler(cfg AssemblerConfig) *Assembler {
	cfg.defaults()
	return &Assembler{cfg: cfg, calls: map[string]*pendingCall{}, done: map[string]time.Time{}}
}

// Begin starts tracking a call (from its call_start) so its audio chunks
// can be tied to its row. Returns false if the call is already over or the
// assembler is full.
func (a *Assembler) Begin(uid string, meta CallMeta, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.calls[uid]; e != nil {
		e.clip.Meta = meta
		return true
	}
	return a.create(uid, meta, now) != nil
}

// SetMeta records what's known about a call already being tracked.
func (a *Assembler) SetMeta(uid string, meta CallMeta) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.calls[uid]; e != nil {
		e.clip.Meta = meta
	}
}

func (a *Assembler) create(uid string, meta CallMeta, now time.Time) *pendingCall {
	if _, over := a.done[uid]; over || len(a.calls) >= a.cfg.MaxCalls {
		return nil
	}
	e := &pendingCall{clip: Clip{UID: uid, Meta: meta}, createdAt: now, lastAt: now, nextSeq: -1}
	a.calls[uid] = e
	return e
}

// AppendResult says what became of a chunk.
type AppendResult struct {
	Meta CallMeta
	// Accepted: the chunk is new audio for a live call (not a duplicate, not
	// after the call's end, not refused for lack of room). Accepted chunks
	// are worth forwarding live even if a byte cap kept them out of the clip.
	Accepted bool
	// New: this chunk started tracking the call (there was no Begin), so its
	// meta is unknown.
	New bool
}

// Append adds one chunk. pcm must be whole 16-bit samples.
func (a *Assembler) Append(uid string, seq int, pcm []byte, now time.Time) AppendResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	var res AppendResult
	e := a.calls[uid]
	if e == nil {
		if e = a.create(uid, CallMeta{}, now); e == nil {
			return res
		}
		res.New = true
	}
	res.Meta = e.clip.Meta
	if e.nextSeq >= 0 && seq < e.nextSeq {
		return res // duplicate or out of order
	}
	res.Accepted = true
	e.lastAt = now
	if e.clip.Chunks == 0 {
		e.clip.StartedAt = now
	}
	if e.nextSeq >= 0 && seq > e.nextSeq {
		missing := seq - e.nextSeq
		e.clip.Missing += missing
		fill := min(missing*e.lastLen, a.cfg.MaxGapFill) &^ 1
		a.appendLocked(e, make([]byte, fill))
	}
	a.appendLocked(e, pcm)
	e.clip.Chunks++
	e.nextSeq = seq + 1
	e.lastLen = len(pcm)
	return res
}

// appendLocked adds what fits under the per-call and total caps.
func (a *Assembler) appendLocked(e *pendingCall, b []byte) {
	room := min(a.cfg.MaxCallBytes-len(e.clip.PCM), a.cfg.MaxTotalBytes-a.total)
	if room < len(b) {
		e.clip.Truncated = true
		b = b[:max(room, 0)&^1]
	}
	e.clip.PCM = append(e.clip.PCM, b...)
	a.total += len(b)
}

// Finish stops tracking a call (its call_end arrived) and returns its clip,
// or nil if it had no audio.
func (a *Assembler) Finish(uid string, now time.Time) *Clip {
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.calls[uid]
	if e == nil {
		a.done[uid] = now
		return nil
	}
	return a.removeLocked(uid, e, now)
}

func (a *Assembler) removeLocked(uid string, e *pendingCall, now time.Time) *Clip {
	delete(a.calls, uid)
	a.done[uid] = now
	a.total -= len(e.clip.PCM)
	if len(e.clip.PCM) == 0 {
		return nil
	}
	clip := e.clip
	return &clip
}

// Sweep evicts calls idle for IdleTimeout or tracked for MaxAge and returns
// those that had audio, marked Evicted. It also forgets old finished calls.
func (a *Assembler) Sweep(now time.Time) []*Clip {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*Clip
	for uid, e := range a.calls {
		if now.Sub(e.lastAt) < a.cfg.IdleTimeout && now.Sub(e.createdAt) < a.cfg.MaxAge {
			continue
		}
		if clip := a.removeLocked(uid, e, now); clip != nil {
			clip.Evicted = true
			out = append(out, clip)
		}
	}
	for uid, at := range a.done {
		if now.Sub(at) > doneTTL {
			delete(a.done, uid)
		}
	}
	return out
}

// Stats reports calls tracked and bytes buffered.
func (a *Assembler) Stats() (calls, bytes int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls), a.total
}
