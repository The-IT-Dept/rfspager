// Package spectrum produces low-rate dB power-spectrum frames from a
// stream of IQ samples. Used by the receiver agent to ship a waterfall
// view back to the server for diagnostic display in the admin portal.
package spectrum

import (
	"math"

	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
)

// Default FFT and output bin sizes. The IQ rate is ~1.92 MS/s; an FFT of
// 1024 gives ~1875 Hz/bin, downsampled to 256 display bins by averaging
// groups of four — plenty of resolution to see POCSAG bursts in the band.
const (
	FFTSize    = 1024
	OutputBins = 256
)

// Compute applies a Hann window + FFT to up to FFTSize samples, takes
// magnitude in dB, fftshifts so DC is in the middle, then averages groups
// of (FFTSize/OutputBins) bins to produce a compact spectrum slice.
//
// Returns OutputBins float32 values; lowest frequency at index 0, highest
// at index OutputBins-1. dB values are negative; 0 dB ≈ full-scale.
func Compute(iq []complex128) []float32 {
	if len(iq) < FFTSize {
		return nil
	}
	// Window into a working buffer so we don't mutate the caller's slice.
	work := make([]complex128, FFTSize)
	src := iq[len(iq)-FFTSize:]
	w := dsp.HannWindow(FFTSize)
	for i := range FFTSize {
		work[i] = complex(real(src[i])*w[i], imag(src[i])*w[i])
	}
	dsp.FFT(work)

	// Compute power in dBFS-ish (relative to FFT-size normalised).
	mag := make([]float64, FFTSize)
	scale := 1.0 / float64(FFTSize)
	for i, v := range work {
		p := (real(v)*real(v) + imag(v)*imag(v)) * scale * scale
		if p < 1e-12 {
			p = 1e-12
		}
		mag[i] = 10 * math.Log10(p)
	}

	// fftshift: rotate so DC (bin 0) ends up in the middle.
	half := FFTSize / 2
	shifted := make([]float64, FFTSize)
	copy(shifted[:half], mag[half:])
	copy(shifted[half:], mag[:half])

	// Decimate to OutputBins by averaging consecutive groups.
	groupSize := FFTSize / OutputBins
	out := make([]float32, OutputBins)
	for i := range OutputBins {
		var sum float64
		base := i * groupSize
		for k := range groupSize {
			sum += shifted[base+k]
		}
		out[i] = float32(sum / float64(groupSize))
	}
	return out
}
