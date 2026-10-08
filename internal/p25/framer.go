package p25

import "math"

// frameSync is the 48-bit P25 Phase 1 frame sync, 24 dibits of ±3 only.
const frameSync uint64 = 0x5575F5FF77FF

// syncSymbols is frameSync as symbol values (+3 for dibit 01, −3 for 11).
var syncSymbols [24]float64

// syncNorm is ‖syncSymbols‖, for the normalised correlation.
var syncNorm float64

func init() {
	for i := range 24 {
		if (frameSync>>(46-2*i))&3 == 0b01 {
			syncSymbols[i] = 3
		} else {
			syncSymbols[i] = -3
		}
		syncNorm += syncSymbols[i] * syncSymbols[i]
	}
	syncNorm = math.Sqrt(syncNorm)
}

// syncThreshold is the normalised correlation (1.0 = perfect) above which we
// take a frame sync. Clean signals sit at 0.95+; random data rarely tops 0.6.
const syncThreshold = 0.8

// Block is one ½-rate trellis-coded 12-byte block (a TSBK or PDU header).
type Block struct {
	Data  [12]byte
	Raw   [98]uint8 // on-air dibits as received (interleaved)
	Errs  int       // Viterbi path metric; 0 = received clean
	CRCOK bool
}

// Frame is one decoded P25 data unit.
type Frame struct {
	Sample  int64 // input sample index at the end of the frame sync
	NAC     uint16
	DUID    uint8
	NIDErrs int
	Blocks  []Block // TSBKs (DUID 7) or the PDU header (DUID 0xC)
}

const ringSize = 1024 // samples; must exceed the sync span + peak hold-off

// Framer finds frame syncs in the Demod's phase stream, samples symbols at
// the sync-recovered timing, and assembles frames.
type Framer struct {
	emit func(Frame)

	ring [ringSize]float64
	n    int64 // index of the next sample to be pushed

	// Sync peak tracking.
	peakScore float64
	peakAt    int64 // −1 when not tracking a candidate

	// Active frame. Timing comes from the sync and is held for the frame:
	// even a 50 ppm clock error drifts < 0.3 samples over a 3-TSBK frame.
	active   bool
	nextRead int64   // sample index of the next symbol
	dibitIdx int     // frame dibit index of that symbol (sync = 0..23)
	gain     float64 // radians per symbol unit, from the sync fit
	offset   float64 // residual phase bias, radians
	data     []uint8 // data dibits since the NID, status symbols removed
	frame    Frame
}

func NewFramer(emit func(Frame)) *Framer {
	return &Framer{emit: emit, peakAt: -1}
}

func (f *Framer) at(i int64) float64 { return f.ring[i%ringSize] }

func (f *Framer) push(ph float64) {
	i := f.n
	f.ring[i%ringSize] = ph
	f.n++

	f.hunt(i)
	for f.active && f.nextRead <= i {
		f.readSymbol(f.at(f.nextRead))
		f.nextRead += samplesPerSymbol
	}
}

// hunt runs the sync correlator for a sync ending at sample i.
func (f *Framer) hunt(i int64) {
	const span = 23 * samplesPerSymbol
	if i < span {
		return
	}
	var c, e float64
	for k := range 24 {
		v := f.at(i - int64(span) + int64(k*samplesPerSymbol))
		c += syncSymbols[k] * v
		e += v * v
	}
	score := 0.0
	if e > 0 {
		score = math.Abs(c) / (syncNorm * math.Sqrt(e))
	}

	if f.peakAt >= 0 {
		if score > f.peakScore {
			f.peakScore, f.peakAt = score, i
		}
		if i-f.peakAt >= samplesPerSymbol/2 {
			f.startFrame(f.peakAt)
			f.peakAt = -1
		}
		return
	}
	if score >= syncThreshold {
		f.peakScore, f.peakAt = score, i
	}
}

// startFrame locks symbol timing to a sync whose last symbol is at sample
// end, abandoning any frame in progress (a fresh sync always wins).
func (f *Framer) startFrame(end int64) {
	// Least-squares fit phase = gain·symbol + offset over the sync symbols.
	const span = 23 * samplesPerSymbol
	var sx, sy, sxx, sxy float64
	for k := range 24 {
		x, y := syncSymbols[k], f.at(end-span+int64(k*samplesPerSymbol))
		sx, sy, sxx, sxy = sx+x, sy+y, sxx+x*x, sxy+x*y
	}
	den := 24*sxx - sx*sx
	f.gain = (24*sxy - sx*sy) / den
	f.offset = (sy - f.gain*sx) / 24
	if math.Abs(f.gain) < 1e-6 {
		return
	}

	f.active = true
	f.nextRead = end + samplesPerSymbol
	f.dibitIdx = 24
	f.data = f.data[:0]
	f.frame = Frame{Sample: end}
}

// readSymbol slices one symbol into a dibit and advances the frame.
func (f *Framer) readSymbol(ph float64) {
	idx := f.dibitIdx
	f.dibitIdx++
	if (idx+1)%36 == 0 {
		return // status symbol
	}
	s := (ph - f.offset) / f.gain
	var dibit uint8
	switch {
	case s > 2:
		dibit = 0b01
	case s > 0:
		dibit = 0b00
	case s > -2:
		dibit = 0b10
	default:
		dibit = 0b11
	}
	f.data = append(f.data, dibit)
	f.advance()
}

// advance acts on the data dibits collected so far.
func (f *Framer) advance() {
	switch {
	case len(f.data) == 32: // NID complete
		var nid uint64
		for _, d := range f.data {
			nid = nid<<2 | uint64(d)
		}
		nac, duid, errs, ok := decodeNID(nid)
		if !ok {
			f.active = false
			return
		}
		f.frame.NAC, f.frame.DUID, f.frame.NIDErrs = nac, duid, errs
		if duid != DUIDTSBK && duid != DUIDPDU {
			f.finish()
		}
	case len(f.data) > 32 && (len(f.data)-32)%98 == 0: // a block complete
		blk := f.data[len(f.data)-98:]
		data, errs := trellisDecode(blk)
		b := Block{Data: data, Errs: errs, CRCOK: tsbkCRCOK(data)}
		copy(b.Raw[:], blk)
		f.frame.Blocks = append(f.frame.Blocks, b)
		lastBlock := b.CRCOK && data[0]&0x80 != 0
		if f.frame.DUID == DUIDPDU || lastBlock || len(f.frame.Blocks) == 3 {
			f.finish()
		}
	}
}

func (f *Framer) finish() {
	f.active = false
	f.emit(f.frame)
}
