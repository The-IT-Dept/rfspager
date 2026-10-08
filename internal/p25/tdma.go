package p25

import (
	"math"
	"math/bits"
)

// P25 Phase 2 TDMA outbound voice channels (TIA-102.BBAC), as the NSW PSN
// grants most voice onto. H-DQPSK at 6000 baud in 12.5 kHz carries two
// logical voice channels (slots 0 and 1) interleaved as 30 ms timeslots.
//
// Each timeslot is 180 dibits: a 20-dibit ISCH, then the burst. The ISCH is
// either the fixed S-ISCH sync word (timeslots 2, 3, 6, 7, 10, 11 of the
// 12-slot superframe) or an I-ISCH that says which timeslot this is. Voice
// bursts (4V: four 20 ms AMBE+2 codewords, 2V: two) are scrambled with a
// per-system LFSR sequence seeded from WACN, System ID and NAC.
//
// Layout and tables follow OP25 (boatbod/op25 p25p2_tdma.cc, p25p2_sync.cc,
// tdma/lfsr.py); offsets below are OP25's, which index the burst from dibit
// 10 of the timeslot.

const (
	p2SymbolRate  = 6_000
	p2SPS         = SampleRate / p2SymbolRate // 8 samples per symbol
	p2SlotDibits  = 180
	p2SuperSlots  = 12
	p2MaskDibits  = p2SuperSlots * p2SlotDibits
	p2SyncISCH    = uint64(0x575D57F7FF) // S-ISCH, 20 dibits of ±3
	p2ISCHMaxErrs = 7                    // (40,9,16) code
)

// Burst types from the DUID.
const (
	p2Burst4V        = 0
	p2BurstSACCH     = 3
	p2Burst2V        = 6
	p2BurstFACCH     = 9
	p2BurstSACCHText = 12
	p2BurstLCCH      = 13
	p2BurstFACCHText = 15
)

// p2WhichSlot maps a superframe timeslot (0–11) to its logical channel.
var p2WhichSlot = [p2SuperSlots]int{0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 1, 0}

// p2VoiceOffsets are the burst dibit offsets of each 36-dibit codeword.
var p2VoiceOffsets = [2][]int{{11, 48, 96, 133}, {11, 48}} // 4V, 2V

// p2XorMask builds the superframe's 2160-dibit scrambling sequence: a
// 44-bit register seeded with WACN·2^24 + SysID·2^12 + NAC, mixed once by
// a fixed matrix, then clocked as six coupled LFSRs (OP25 tdma/lfsr.py).
func p2XorMask(wacn uint32, sysid, nac uint16) []uint8 {
	seed := uint64(wacn)<<24 | uint64(sysid)<<12 | uint64(nac)
	// a[i] is register bit i counting from the MSB (bit 43).
	bit := func(r uint64, i int) uint64 { return r >> (43 - i) & 1 }
	var reg uint64
	for j := range 44 {
		var b uint64
		for _, d := range []int{0, 4, 9, 15, 20, 34} {
			if j-d >= 0 {
				b ^= bit(seed, j-d)
			}
		}
		reg |= b << (43 - j)
	}

	out := make([]uint8, p2MaskDibits)
	for i := range 2 * p2MaskDibits {
		out[i/2] = out[i/2]<<1 | uint8(reg>>43&1)
		reg = p2Clock(reg)
	}
	return out
}

// p2Clock advances the six sub-registers (4, 5, 6, 5, 14 and 10 bits wide,
// MSB first) by one step.
func p2Clock(r uint64) uint64 {
	s1, s2, s3 := r>>40&0xF, r>>35&0x1F, r>>29&0x3F
	s4, s5, s6 := r>>24&0x1F, r>>10&0x3FFF, r&0x3FF
	c := s1 >> 3 & 1
	s1 = (s1<<1 | c ^ s2>>4&1) & 0xF
	s2 = (s2<<1 | c ^ s3>>5&1) & 0x1F
	s3 = (s3<<1 | c ^ s4>>4&1) & 0x3F
	s4 = (s4<<1 | c ^ s5>>13&1) & 0x1F
	s5 = (s5<<1 | c ^ s6>>9&1) & 0x3FFF
	s6 = (s6<<1 | c) & 0x3FF
	return s1<<40 | s2<<35 | s3<<29 | s4<<24 | s5<<10 | s6
}

// p2ISCH decodes a 40-bit ISCH: the 7-bit I-ISCH value, −2 for the S-ISCH
// sync word, −1 if it's neither within the code's correction radius.
func p2ISCH(cw uint64) int {
	if bits.OnesCount64(cw^p2SyncISCH) <= p2ISCHMaxErrs {
		return -2
	}
	best, bestD := -1, p2ISCHMaxErrs+1
	for v, c := range ischCodewords {
		if d := bits.OnesCount64(cw ^ c); d < bestD {
			best, bestD = v, d
		}
	}
	return best
}

// TDMAStats counts what the TDMA decoder has seen.
type TDMAStats struct {
	Syncs       int         // S-ISCH sync words found
	Slots       int         // timeslots decoded while locked
	ISCHErrors  int         // ISCH that matched nothing
	BurstTypes  map[int]int // by DUID burst type (−1 = undecodable)
	VoiceFrames [2]int      // codewords emitted per logical slot
}

// TDMADecoder demodulates a Phase 2 voice channel and emits each AMBE+2
// codeword (36 descrambled dibits) with the logical slot it belongs to.
type TDMADecoder struct {
	demod  *Demod
	framer *p2Framer
}

// NewTDMADecoder decodes a voice channel on the system identified by wacn,
// sysid and the site's nac (all from the control channel). onVoice receives
// each 20 ms codeword in order; the slice is only valid during the call.
func NewTDMADecoder(wacn uint32, sysid, nac uint16, onVoice func(slot int, dibits []uint8)) *TDMADecoder {
	f := &p2Framer{
		mask:    p2XorMask(wacn, sysid, nac),
		onVoice: onVoice,
		peakAt:  -1,
		slotID:  -1,
		stats:   TDMAStats{BurstTypes: map[int]int{}},
	}
	d := newDemod(f, p2SPS)
	d.fll = false
	f.steer = d.steer
	return &TDMADecoder{demod: d, framer: f}
}

// SetFreqOffsetHz seeds the AFC. Sync-aided tracking only pulls in errors
// up to ±750 Hz (π/4 per symbol), so seed it from the control channel
// decoder's FreqOffsetHz on the same dongle.
func (t *TDMADecoder) SetFreqOffsetHz(hz float64) { t.demod.SetFreqOffsetHz(hz) }

func (t *TDMADecoder) FreqOffsetHz() float64 { return t.demod.FreqOffsetHz() }

// Process decodes a block of 48 kS/s baseband.
func (t *TDMADecoder) Process(baseband []complex128) { t.demod.Process(baseband) }

func (t *TDMADecoder) Stats() TDMAStats { return t.framer.stats }

// p2SyncSymbols is the S-ISCH as ±3 symbols.
var p2SyncSymbols = func() (s [20]float64) {
	for i := range 20 {
		if p2SyncISCH>>(38-2*i)&3 == 0b01 {
			s[i] = 3
		} else {
			s[i] = -3
		}
	}
	return s
}()

var p2SyncNorm = func() float64 {
	var n float64
	for _, v := range p2SyncSymbols {
		n += v * v
	}
	return math.Sqrt(n)
}()

const p2Ring = 4096 // samples; > one timeslot (1440) plus slack

// p2LockLoss: with six S-ISCH per 360 ms superframe, a second without one
// means the channel has gone (call over, or we've lost it).
const p2LockLoss = SampleRate

type p2Framer struct {
	mask    []uint8
	onVoice func(slot int, dibits []uint8)
	steer   func(radPerSample float64)
	stats   TDMAStats

	ring [p2Ring]float64
	n    int64

	peakScore float64
	peakAt    int64

	locked   bool
	lastSync int64   // sample index of the last S-ISCH end
	nextRead int64   // sample index of the next symbol
	gain     float64 // radians per symbol unit
	offset   float64
	slot     [p2SlotDibits]uint8
	pos      int // dibits filled in slot
	slotID   int // superframe timeslot of the slot being read; −1 unknown
}

func (f *p2Framer) at(i int64) float64 { return f.ring[i%p2Ring] }

func (f *p2Framer) push(ph float64) {
	i := f.n
	f.ring[i%p2Ring] = ph
	f.n++

	f.hunt(i)
	if f.locked && i-f.lastSync > p2LockLoss {
		f.locked, f.slotID = false, -1
	}
	for f.locked && f.nextRead <= i {
		f.readSymbol(f.at(f.nextRead))
		f.nextRead += p2SPS
	}
}

func (f *p2Framer) hunt(i int64) {
	const span = 19 * p2SPS
	if i < span {
		return
	}
	var c, e float64
	for k := range 20 {
		v := f.at(i - span + int64(k*p2SPS))
		c += p2SyncSymbols[k] * v
		e += v * v
	}
	score := 0.0
	if e > 0 {
		score = math.Abs(c) / (p2SyncNorm * math.Sqrt(e))
	}
	if f.peakAt >= 0 {
		if score > f.peakScore {
			f.peakScore, f.peakAt = score, i
		}
		if i-f.peakAt >= p2SPS/2 {
			f.sync(f.peakAt)
			f.peakAt = -1
		}
		return
	}
	if score >= syncThreshold {
		f.peakScore, f.peakAt = score, i
	}
}

// sync handles an S-ISCH whose correlation peaked at sample end (its last
// symbol). The correlation peak is a coarse timing estimate — it wanders by
// half a symbol from sync to sync — so once locked we keep our own tracked
// timing and only use the sync to refresh the slicer levels and AFC.
func (f *p2Framer) sync(end int64) {
	f.stats.Syncs++
	f.lastSync = end
	if f.locked {
		// Where our timing puts the end of the sync: dibit 19 of the slot.
		ours := f.nextRead - int64(f.pos-19)*p2SPS
		if abs64(ours-end) <= p2SPS/2+1 {
			f.adopt(f.fitSync(ours))
			return
		}
	}
	// (Re)acquire: pick the sampling offset around the peak where the sync
	// symbols fit their ideal values best.
	best, bestFit := end, syncFit{cost: math.Inf(1)}
	for d := int64(-p2SPS / 2); d <= p2SPS/2; d++ {
		if fit := f.fitSync(end + d); fit.cost < bestFit.cost {
			best, bestFit = end+d, fit
		}
	}
	if math.IsInf(bestFit.cost, 1) {
		return
	}
	f.adopt(bestFit)
	f.locked = true
	f.nextRead = best + p2SPS
	for k := range 20 {
		f.slot[k] = uint8(p2SyncISCH >> (38 - 2*k) & 3)
	}
	f.pos = 20
}

type syncFit struct{ gain, offset, cost float64 }

// fitSync least-squares fits phase = gain·symbol + offset over the sync
// word ending at end; cost is the normalised residual (lower = better timing).
func (f *p2Framer) fitSync(end int64) syncFit {
	const span = 19 * p2SPS
	var sx, sy, sxx, sxy float64
	for k := range 20 {
		x, y := p2SyncSymbols[k], f.at(end-span+int64(k*p2SPS))
		sx, sy, sxx, sxy = sx+x, sy+y, sxx+x*x, sxy+x*y
	}
	gain := (20*sxy - sx*sy) / (20*sxx - sx*sx)
	if gain < 1e-3 { // inverted or no signal: not a sync we can use
		return syncFit{cost: math.Inf(1)}
	}
	offset := (sy - gain*sx) / 20
	var res float64
	for k := range 20 {
		r := f.at(end-span+int64(k*p2SPS)) - gain*p2SyncSymbols[k] - offset
		res += r * r
	}
	return syncFit{gain, offset, res / (gain * gain)}
}

// adopt takes a sync fit's slicer levels and steers the AFC by a fraction
// of its offset — the residual carrier rotation per symbol.
func (f *p2Framer) adopt(fit syncFit) {
	f.gain, f.offset = fit.gain, fit.offset
	if f.steer != nil {
		f.steer(0.1 * fit.offset / p2SPS)
	}
}

// retime nudges symbol timing by up to a sample toward the widest eye,
// judged over the timeslot just read. Sample clocks differ by tens of ppm
// (~0.1 sample per timeslot), so this only ever needs to creep.
func (f *p2Framer) retime() {
	first := f.nextRead - p2SlotDibits*p2SPS
	cost := func(d int64) float64 {
		var c float64
		for k := range int64(p2SlotDibits - 2) {
			s := (f.at(first+k*p2SPS+d) - f.offset) / f.gain
			e := s - (2*math.Floor((s+3)/2) - 3 + 1) // distance to nearest odd level
			c += e * e
		}
		return c
	}
	c0, cm, cp := cost(0), cost(-1), cost(1)
	switch {
	case cm < c0 && cm <= cp:
		f.nextRead--
	case cp < c0:
		f.nextRead++
	}
}

func (f *p2Framer) readSymbol(ph float64) {
	s := (ph - f.offset) / f.gain
	var d uint8
	switch {
	case s > 2:
		d = 0b01
	case s > 0:
		d = 0b00
	case s > -2:
		d = 0b10
	default:
		d = 0b11
	}
	f.slot[f.pos] = d
	f.pos++
	if f.pos == p2SlotDibits {
		f.handleSlot()
		f.retime()
		f.pos = 0
	}
}

// handleSlot processes one complete 180-dibit timeslot (OP25 handle_packet).
func (f *p2Framer) handleSlot() {
	var cw uint64
	for _, d := range f.slot[:20] {
		cw = cw<<2 | uint64(d)
	}
	if f.slotID >= 0 {
		f.slotID = (f.slotID + 1) % p2SuperSlots
	}
	switch v := p2ISCH(cw); {
	case v >= 0:
		chn, loc := v>>5&3, v>>3&3
		if id := loc*4 + chn; id < p2SuperSlots {
			f.slotID = id
		}
	case v == -1:
		f.stats.ISCHErrors++
	}
	if f.slotID < 0 {
		return // timeslot position not yet known
	}
	f.stats.Slots++

	burst := f.slot[10:]
	bt := int(duidLookup[burst[10]<<6|burst[47]<<4|burst[132]<<2|burst[169]])
	f.stats.BurstTypes[bt]++
	if bt != p2Burst4V && bt != p2Burst2V {
		return
	}
	var x [p2SlotDibits - 10]uint8
	m := f.mask[f.slotID*p2SlotDibits:]
	for i := range x {
		x[i] = burst[i] ^ m[i]
	}
	offs := p2VoiceOffsets[0]
	if bt == p2Burst2V {
		offs = p2VoiceOffsets[1]
	}
	logical := p2WhichSlot[f.slotID]
	for _, o := range offs {
		f.stats.VoiceFrames[logical]++
		if f.onVoice != nil {
			f.onVoice(logical, x[o:o+36])
		}
	}
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
