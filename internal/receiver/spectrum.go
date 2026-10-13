package receiver

import (
	"math"

	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
)

// Spectrum accumulates an averaged power spectrum of the raw dongle IQ: the
// dongle loop feeds it whole buffers until it has seen the requested number
// of samples, then Done is closed and BinsDB holds one value per bin, in dB
// relative to a full-scale tone, ordered from centre - fs/2 to centre +
// fs/2 (fftshifted). Each FFT frame is Hann-windowed and |X|^2 is averaged
// across frames; the result is normalised by the window's coherent gain so
// a full-scale CW carrier reads 0 dB.
type Spectrum struct {
	bins   int
	need   int // samples to consume in total
	seen   int
	frames int

	win     []float64
	acc     []float64
	scratch []complex128
	result  []float64
	done    chan struct{}
}

// NewSpectrum builds an accumulator for bins (a power of two) over samples
// raw IQ samples.
func NewSpectrum(bins, samples int) *Spectrum {
	return &Spectrum{
		bins:    bins,
		need:    samples,
		win:     dsp.HannWindow(bins),
		acc:     make([]float64, bins),
		scratch: make([]complex128, bins),
		done:    make(chan struct{}),
	}
}

// Feed consumes one IQ block. It returns true once the capture is complete;
// the caller must stop feeding after that.
func (s *Spectrum) Feed(block []complex128) bool {
	if s.seen >= s.need {
		return true
	}
	for i := 0; i+s.bins <= len(block) && s.seen < s.need; i += s.bins {
		for k := range s.bins {
			s.scratch[k] = block[i+k] * complex(s.win[k], 0)
		}
		dsp.FFT(s.scratch)
		for k, x := range s.scratch {
			s.acc[k] += real(x)*real(x) + imag(x)*imag(x)
		}
		s.frames++
		s.seen += s.bins
	}
	if s.seen < s.need {
		return false
	}
	s.finish()
	return true
}

func (s *Spectrum) finish() {
	var gain float64
	for _, w := range s.win {
		gain += w
	}
	norm := 1 / (gain * gain * float64(max(s.frames, 1)))
	s.result = make([]float64, s.bins)
	half := s.bins / 2
	for j := range s.bins {
		p := s.acc[(j+half)%s.bins] * norm
		if p < 1e-30 {
			p = 1e-30
		}
		s.result[j] = 10 * math.Log10(p)
	}
	close(s.done)
}

// Done is closed when the spectrum is complete.
func (s *Spectrum) Done() <-chan struct{} { return s.done }

// BinsDB returns the averaged spectrum. Only valid after Done is closed.
func (s *Spectrum) BinsDB() []float64 { return s.result }
