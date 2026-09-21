package dsp

// BitSlicer recovers POCSAG bits from an FM-demodulated baseband signal.
//
// Three stages per input sample:
//
//  1. Matched filter. The demod output is averaged over the most recent
//     half bit-period. POCSAG symbols are rectangular, so this integrates
//     most of the symbol energy while rejecting noise well outside the baud
//     bandwidth (the channel FIR is 12 kHz wide to accommodate 2400 baud;
//     a 512-baud symbol only needs ~1 kHz, so this is worth ~15 dB there).
//     The averaged signal's zero crossings land a quarter bit after the
//     true transition, and the point where the window sits wholly inside
//     one symbol is half a bit after that — which is exactly the
//     "crossings at phase 0, sample at phase spb/2" model used below.
//
//  2. DC tracking. A slow single-pole filter estimates the demod DC offset
//     (tuner frequency error) so the slicing threshold stays centred. It is
//     deliberately slow (~10 Hz) so runs of identical bits and random data
//     do not pull the threshold around; the 576-bit preamble gives it ample
//     time to settle, and the offset it tracks is essentially static.
//
//  3. Timing recovery. phase ∈ [0, spb) advances by one per sample and the
//     bit is sampled when it passes spb/2. The first zero crossing in each
//     bit period nudges phase 10% toward the nearest boundary. Limiting
//     corrections to one per period matters: between bursts the input is
//     noise, with a crossing on almost every sample, and correcting on each
//     one would pin the phase near zero so no bits are ever emitted. The
//     decoder relies on a steady stream of (garbage) bits after a burst to
//     see the sync word fail to reappear and release the final page.
type BitSlicer struct {
	samplesPerBit float64
	halfBit       float64
	phase         float64
	corrected     bool // a timing correction was applied in this bit period

	dc      float64
	dcAlpha float64

	win  int // matched-filter window, ~half a bit
	ring []float64
	rpos int
	sum  float64

	prev float64 // previous filtered, DC-removed sample
}

// NewBitSlicer constructs a slicer for the given audio sample rate and baud.
func NewBitSlicer(sampleRateHz, baud float64) *BitSlicer {
	spb := sampleRateHz / baud
	win := max(int(spb/2), 1)
	return &BitSlicer{
		samplesPerBit: spb,
		halfBit:       spb / 2,
		dcAlpha:       1.0 / (sampleRateHz / 10.0),
		win:           win,
		ring:          make([]float64, win),
	}
}

// Push feeds one demodulated sample. Returns (bit, true) when this sample
// is the mid-bit instant for a new symbol; otherwise (false, false).
func (s *BitSlicer) Push(x float64) (bool, bool) {
	s.dc += s.dcAlpha * (x - s.dc)

	s.sum += x - s.ring[s.rpos]
	s.ring[s.rpos] = x
	s.rpos++
	if s.rpos == s.win {
		s.rpos = 0
	}
	y := s.sum/float64(s.win) - s.dc

	if !s.corrected && ((s.prev < 0 && y >= 0) || (s.prev > 0 && y <= 0)) {
		// Signed distance from the nearest bit boundary; pull toward it.
		e := s.phase
		if e > s.halfBit {
			e -= s.samplesPerBit
		}
		s.phase -= 0.1 * e
		s.corrected = true
	}
	s.prev = y

	old := s.phase
	s.phase++
	emit := old < s.halfBit && s.phase >= s.halfBit
	bit := y > 0
	if s.phase >= s.samplesPerBit {
		s.phase -= s.samplesPerBit
		s.corrected = false
	}
	return bit, emit
}
