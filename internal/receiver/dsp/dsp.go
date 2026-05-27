// Package dsp implements the small DSP primitives the receiver needs:
//
//	IQ bytes → mixer (NCO) → FIR lowpass + decimate → FM demod → bits
//
// Everything is plain float64 / complex128 — POCSAG's audio bandwidth is
// tiny so the per-channel rate is well under 50 kHz after decimation, and
// the only thing that runs at the full ADC rate is the mixer + the first
// FIR stage. We keep things straightforward rather than vectorised.
package dsp

import "math"

// IQToComplex unpacks one async RTL-SDR buffer (interleaved u8 I, Q, I, Q…)
// into complex128 samples centred on 0+0i. Each byte is in the range [0, 255]
// where 128 is zero.
func IQToComplex(buf []byte, out []complex128) []complex128 {
	n := len(buf) / 2
	if cap(out) < n {
		out = make([]complex128, n)
	}
	out = out[:n]
	for i := range n {
		i8 := (float64(buf[2*i]) - 127.5) / 127.5
		q8 := (float64(buf[2*i+1]) - 127.5) / 127.5
		out[i] = complex(i8, q8)
	}
	return out
}

// Mixer is a numerically-controlled oscillator. Multiplying input IQ samples
// by Mixer.Step() shifts the spectrum by -freqHz (so we shift the channel of
// interest down to baseband).
type Mixer struct {
	phase    float64
	phaseInc float64
}

func NewMixer(freqHz, sampleRateHz float64) *Mixer {
	return &Mixer{phaseInc: -2 * math.Pi * freqHz / sampleRateHz}
}

// Mix returns sample*exp(j*phase) and advances the NCO by one step.
func (m *Mixer) Mix(sample complex128) complex128 {
	c := complex(math.Cos(m.phase), math.Sin(m.phase))
	m.phase += m.phaseInc
	if m.phase > math.Pi {
		m.phase -= 2 * math.Pi
	} else if m.phase < -math.Pi {
		m.phase += 2 * math.Pi
	}
	return sample * c
}

// FIRDecimator is a real-coefficient FIR with integer decimation. Coefficients
// are applied to a complex sample stream — we only compute output samples
// (every Nth call to Push produces one Out value).
type FIRDecimator struct {
	taps []float64
	hist []complex128
	pos  int // ring buffer write position
	deci int
	skip int // counts down to next output
}

func NewFIRDecimator(taps []float64, decimation int) *FIRDecimator {
	return &FIRDecimator{
		taps: taps,
		hist: make([]complex128, len(taps)),
		deci: decimation,
	}
}

// Push appends one input sample. If the decimator is ready to emit, returns
// (out, true); otherwise (0, false).
func (f *FIRDecimator) Push(x complex128) (complex128, bool) {
	f.hist[f.pos] = x
	f.pos = (f.pos + 1) % len(f.hist)

	f.skip++
	if f.skip < f.deci {
		return 0, false
	}
	f.skip = 0

	// Convolve taps with history (newest-to-oldest: tap[0] is the most recent sample).
	var acc complex128
	for i := range f.taps {
		j := f.pos - 1 - i
		if j < 0 {
			j += len(f.hist)
		}
		acc += f.hist[j] * complex(f.taps[i], 0)
	}
	return acc, true
}

// LowpassTaps designs a windowed-sinc lowpass with the given cutoff (Hz),
// sample rate, and number of taps. cutoffHz is the -6 dB point.
func LowpassTaps(cutoffHz, sampleRateHz float64, n int) []float64 {
	if n%2 == 0 {
		n++ // force odd for symmetry
	}
	taps := make([]float64, n)
	fc := cutoffHz / sampleRateHz // normalised
	mid := float64(n-1) / 2
	var sum float64
	for i := range n {
		k := float64(i) - mid
		var s float64
		if k == 0 {
			s = 2 * fc
		} else {
			s = math.Sin(2*math.Pi*fc*k) / (math.Pi * k)
		}
		// Hamming window
		w := 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(n-1))
		taps[i] = s * w
		sum += taps[i]
	}
	// Normalise DC gain to 1.
	for i := range taps {
		taps[i] /= sum
	}
	return taps
}

// FMDemod is a classic quadrature FM demodulator: y[n] = arg(x[n] * conj(x[n-1])).
// The output is the instantaneous frequency in radians/sample, which is what
// downstream bit slicers want.
type FMDemod struct {
	prev complex128
}

func (f *FMDemod) Demod(x complex128) float64 {
	d := x * complex(real(f.prev), -imag(f.prev))
	y := math.Atan2(imag(d), real(d))
	f.prev = x
	return y
}
