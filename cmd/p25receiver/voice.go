package main

import (
	"encoding/binary"
	"log/slog"
	"time"

	"github.com/the-it-dept/rfspager/internal/ambe"
	"github.com/the-it-dept/rfspager/internal/p25"
)

// vocoder turns one Phase 2 voice codeword (36 descrambled dibits) into
// 20 ms of 8 kHz PCM. Implemented by internal/ambe.
type vocoder interface {
	DecodeP25P2Dibits(d []byte) (pcm [160]int16, errs int)
}

// newVocoder is nil until a vocoder is linked in; voice channels are then
// still followed and framed, but no audio is produced.
var newVocoder func() vocoder

// follower decodes the voice of every clear call whose channel lies inside
// the dongle's capture bandwidth, straight out of the same wideband IQ the
// control channel comes from: one channelizer + TDMA decoder per active
// voice frequency, one vocoder per call.
type follower struct {
	center uint32
	cc     *p25.Decoder // control channel decoder on the same dongle, for AFC
	ccHz   uint32
	site   p25.SitePayload
	send   func(frameType string, payload any)

	chans map[uint64]*voiceChan // by frequency
	calls map[string]*voiceCall // by call ID
	// afc remembers each voice frequency's converged tuning error: every
	// transmitter has its own small offset, and seeding from the last call
	// on the same channel skips the pull-in at the start of the next one.
	afc map[uint64]float64
}

type voiceChan struct {
	ch       *p25.Channelizer
	dec      *p25.TDMADecoder
	slots    [2]*voiceCall
	idleFrom time.Time
}

type voiceCall struct {
	id     string
	tg     uint16
	src    uint32
	start  int64 // unix ms
	voc    vocoder
	pcm    []byte
	seq    int
	frames int
	errs   int // Golay-corrected bits over decodable frames
	bad    int // uncorrectable frames
}

// chunkFrames: ship audio every 9 codewords (180 ms) — half a superframe,
// small enough for live listening, large enough not to flood the socket.
const chunkFrames = 9

// patchWindowMS: grants for the same radio this close together are one
// keyup on several talkgroups. Matches internal/radio's linkPatchSQL.
const patchWindowMS = 2_000

// chanLinger keeps an idle voice channel's decoder alive briefly: talkers
// key up again within the hang time on the same channel.
const chanLinger = 10 * time.Second

func newFollower(send func(string, any)) *follower {
	return &follower{send: send, chans: map[uint64]*voiceChan{}, calls: map[string]*voiceCall{}, afc: map[uint64]float64{}}
}

// retune drops every voice channel; the new centre applies from now on.
func (f *follower) retune(center, ccHz uint32, cc *p25.Decoder) {
	for id := range f.calls {
		f.finish(id)
	}
	clear(f.chans)
	f.center, f.ccHz, f.cc = center, ccHz, cc
}

// call reacts to call start/end payloads. It must run before call_end goes
// to the server so the last audio chunk precedes it.
func (f *follower) call(frameType string, c p25.CallPayload) {
	if frameType == p25.FrameCallEnd {
		f.finish(c.CallID)
		return
	}
	off := int64(c.FreqHz) - int64(f.center)
	if c.Encrypted || c.FreqHz == 0 || c.Slot > 1 || abs(off) > recordSpan || abs(off) < 20_000 ||
		f.cc == nil || f.site.SysID == 0 {
		return
	}
	// A patch grants one keyup on several talkgroups (often both slots of
	// the same channel). It's the same voice: decode and ship it once. The
	// server links the copies the same way.
	if c.Source != 0 {
		for _, other := range f.calls {
			if other.src == c.Source && abs(other.start-c.StartMS) < patchWindowMS {
				return
			}
		}
	}
	vc := f.chans[c.FreqHz]
	if vc == nil {
		vc = &voiceChan{ch: p25.NewChannelizer(float64(off), inputRate)}
		vc.dec = p25.NewTDMADecoder(f.site.WACN, f.site.SysID, f.site.NAC, func(slot int, d []uint8) {
			f.frame(vc, slot, d)
		})
		if hz, ok := f.afc[c.FreqHz]; ok {
			vc.dec.SetFreqOffsetHz(hz)
		} else {
			// Same crystal: scale the control channel's error to this frequency.
			vc.dec.SetFreqOffsetHz(f.cc.FreqOffsetHz() * float64(c.FreqHz) / float64(f.ccHz))
		}
		f.chans[c.FreqHz] = vc
	}
	if old := vc.slots[c.Slot]; old != nil {
		f.finish(old.id)
	}
	call := &voiceCall{id: c.CallID, tg: c.Group, src: c.Source, start: c.StartMS}
	if newVocoder != nil {
		call.voc = newVocoder()
	}
	vc.slots[c.Slot] = call
	f.calls[c.CallID] = call
}

func (f *follower) frame(vc *voiceChan, slot int, d []uint8) {
	call := vc.slots[slot]
	if call == nil {
		return
	}
	call.frames++
	if call.voc == nil {
		return
	}
	pcm, errs := call.voc.DecodeP25P2Dibits(d)
	if errs >= ambe.Uncorrectable {
		call.bad++
	} else {
		call.errs += errs
	}
	for _, s := range pcm {
		call.pcm = binary.LittleEndian.AppendUint16(call.pcm, uint16(s))
	}
	if len(call.pcm) >= chunkFrames*160*2 {
		f.flush(call)
	}
}

func (f *follower) flush(c *voiceCall) {
	if len(c.pcm) == 0 {
		return
	}
	f.send(p25.FrameAudio, p25.AudioPayload{CallID: c.id, Seq: c.seq, PCM: c.pcm})
	c.seq++
	c.pcm = nil
}

func (f *follower) finish(id string) {
	c := f.calls[id]
	if c == nil {
		return
	}
	delete(f.calls, id)
	f.flush(c)
	var st p25.TDMAStats
	afc := 0.0
	for hz, vc := range f.chans {
		for s := range vc.slots {
			if vc.slots[s] == c {
				vc.slots[s] = nil
				st = vc.dec.Stats()
				afc = vc.dec.FreqOffsetHz()
				f.afc[hz] = afc
			}
		}
	}
	slog.Info("voice", "tg", c.tg, "frames", c.frames, "chunks", c.seq,
		"avg_errs", float64(c.errs)/float64(max(c.frames-c.bad, 1)), "bad_frames", c.bad,
		"chan_syncs", st.Syncs, "chan_slots", st.Slots, "chan_isch_errs", st.ISCHErrors, "afc_hz", int(afc))
}

// process runs every active voice channel over a block of wideband IQ.
func (f *follower) process(wide []complex128, now time.Time) {
	for hz, vc := range f.chans {
		if vc.slots[0] == nil && vc.slots[1] == nil {
			if vc.idleFrom.IsZero() {
				vc.idleFrom = now
			} else if now.Sub(vc.idleFrom) > chanLinger {
				delete(f.chans, hz)
				continue
			}
		} else {
			vc.idleFrom = time.Time{}
		}
		vc.dec.Process(vc.ch.Process(wide))
	}
}
