// Package dsp implements the small DSP primitives the receiver needs:
//
//	IQ bytes → mixer (NCO) → FIR lowpass + decimate → FM demod → bits
//
// Everything is plain float64 / complex128. The only stages that run at the
// full ADC rate are IQToComplex, the mixer and the FIR's input side, and
// those are written block-at-a-time: on a Raspberry Pi 4 the previous
// per-sample design (a cos+sin per sample per channel, a ring-buffer push
// per sample) cost ~1.25× realtime for two channels at 1.92 MS/s, so the
// receiver could never keep up and permanently shed ~18% of IQ buffers.
package dsp

import (
	"math"
	"math/cmplx"
)

// u8ToF64 maps an RTL-SDR sample byte to [-1, 1] with 127.5 as zero.
var u8ToF64 [256]float64

func init() {
	for i := range u8ToF64 {
		u8ToF64[i] = (float64(i) - 127.5) / 127.5
	}
}

// IQToComplex unpacks one async RTL-SDR buffer (interleaved u8 I, Q, I, Q…)
// into complex128 samples centred on 0+0i. Each byte is in the range [0, 255]
// where 127.5 is zero.
func IQToComplex(buf []byte, out []complex128) []complex128 {
	n := len(buf) / 2
	if cap(out) < n {
		out = make([]complex128, n)
	}
	out = out[:n]
	for i := range n {
		out[i] = complex(u8ToF64[buf[2*i]], u8ToF64[buf[2*i+1]])
	}
	return out
}

// Mixer is a numerically-controlled oscillator. Multiplying input IQ samples
// by the mixer's phasor shifts the spectrum by -freqHz (so we shift the
// channel of interest down to baseband).
//
// Rather than evaluating cos/sin per sample, it keeps a unit phasor and
// rotates it by a fixed complex multiply each sample; the magnitude is
// renormalised once per block so rounding drift never accumulates.
type Mixer struct {
	ph  complex128 // current phasor, exp(j·phase)
	rot complex128 // per-sample rotation, exp(j·phaseInc)
}

func NewMixer(freqHz, sampleRateHz float64) *Mixer {
	inc := -2 * math.Pi * freqHz / sampleRateHz
	return &Mixer{ph: 1, rot: complex(math.Cos(inc), math.Sin(inc))}
}

// Mix returns sample·exp(j·phase) and advances the NCO by one step.
func (m *Mixer) Mix(sample complex128) complex128 {
	y := sample * m.ph
	m.ph *= m.rot
	return y
}

// MixBlock mixes every sample in `in`, writing into out (reallocated if too
// small) and returning it. Phase carries across calls.
func (m *Mixer) MixBlock(in, out []complex128) []complex128 {
	if cap(out) < len(in) {
		out = make([]complex128, len(in))
	}
	out = out[:len(in)]
	ph, rot := m.ph, m.rot
	for i, s := range in {
		out[i] = s * ph
		ph *= rot
	}
	m.ph = ph / complex(cmplx.Abs(ph), 0)
	return out
}

// FIRDecimator is a real-coefficient FIR with integer decimation applied to
// a complex sample stream. Only the decimated outputs are ever computed:
// out[m] = Σ taps[i]·x[(m+1)·D − 1 − i], with x[<0] = 0, where D is the
// decimation factor. Input may be delivered in blocks of any size; the
// output is identical regardless of how the stream is chunked.
type FIRDecimator struct {
	taps  []float64
	deci  int
	hist  []complex128 // last len(taps)-1 input samples, oldest first
	ext   []complex128 // scratch: hist ++ current block
	phase int          // input samples consumed since the last output
}

func NewFIRDecimator(taps []float64, decimation int) *FIRDecimator {
	return &FIRDecimator{
		taps: taps,
		deci: decimation,
		hist: make([]complex128, len(taps)-1),
	}
}

// ProcessBlock filters and decimates `in`, appending outputs to out[:0]
// (reallocated as needed) and returning it.
func (f *FIRDecimator) ProcessBlock(in, out []complex128) []complex128 {
	h := len(f.hist)
	need := h + len(in)
	if cap(f.ext) < need {
		f.ext = make([]complex128, need)
	}
	ext := f.ext[:need]
	copy(ext, f.hist)
	copy(ext[h:], in)

	out = out[:0]
	taps := f.taps
	for k := f.deci - 1 - f.phase; k < len(in); k += f.deci {
		p := h + k // newest sample contributing to this output
		var re, im float64
		for i, tp := range taps {
			x := ext[p-i]
			re += real(x) * tp
			im += imag(x) * tp
		}
		out = append(out, complex(re, im))
	}
	f.phase = (f.phase + len(in)) % f.deci
	copy(f.hist, ext[need-h:])
	return out
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
