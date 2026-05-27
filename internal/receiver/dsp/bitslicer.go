package dsp

// BitSlicer recovers POCSAG bits from an FM-demodulated baseband signal.
//
// Phase model: phase ∈ [0, samplesPerBit). It increments by 1 each input
// sample. Bit boundaries occur when phase wraps from samplesPerBit back to
// 0 (this is where we expect zero crossings of the signal to land). The
// bit *value* is sampled at mid-bit — phase = samplesPerBit/2 — which is
// the centre of the symbol where intersymbol interference is minimal.
//
// The slicer runs a slow DC-tracking filter so the slicing threshold stays
// near 0, then nudges its phase toward observed zero crossings so symbol
// timing locks onto the incoming bit clock.
type BitSlicer struct {
	samplesPerBit float64
	halfBit       float64
	phase         float64
	dc            float64
	dcAlpha       float64
	prev          float64
}

// NewBitSlicer constructs a slicer for the given audio sample rate and baud.
func NewBitSlicer(sampleRateHz, baud float64) *BitSlicer {
	spb := sampleRateHz / baud
	return &BitSlicer{
		samplesPerBit: spb,
		halfBit:       spb / 2,
		// ~50 Hz cutoff DC tracker — fast enough to follow slow drift,
		// slow enough not to chase the modulation itself.
		dcAlpha: 1.0 / (sampleRateHz / 50.0),
	}
}

// Push feeds one demodulated sample. Returns (bit, true) when this sample
// is the mid-bit instant for a new symbol; otherwise (false, false).
func (s *BitSlicer) Push(x float64) (bool, bool) {
	s.dc += s.dcAlpha * (x - s.dc)
	y := x - s.dc

	// Zero-crossing detector aligns symbol timing. We expect crossings to
	// happen at bit boundaries (phase ≈ 0 / samplesPerBit), so nudge phase
	// toward whichever boundary is closer.
	if (s.prev < 0 && y >= 0) || (s.prev > 0 && y <= 0) {
		if s.phase > s.halfBit {
			s.phase += 0.1 * (s.samplesPerBit - s.phase)
		} else {
			s.phase -= 0.1 * s.phase
		}
	}
	s.prev = y

	// Increment phase and detect:
	//   - mid-bit crossing (sample now)
	//   - end-of-bit wrap   (reset phase)
	old := s.phase
	s.phase += 1
	emit := old < s.halfBit && s.phase >= s.halfBit
	bit := y > 0
	if s.phase >= s.samplesPerBit {
		s.phase -= s.samplesPerBit
	}
	return bit, emit
}
