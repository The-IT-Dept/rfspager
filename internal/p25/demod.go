package p25

import (
	"math"
	"math/cmplx"
)

// Demod turns 48 kS/s complex baseband into a stream of soft symbol-rate
// phase values, one per input sample, and hands them to a Framer.
//
// Each output is arg(x[n]·conj(x[n−10])): the carrier phase advance across one
// symbol period, in radians. P25 symbols are ±π/4 (dibits 00/10) and ±3π/4
// (01/11), for C4FM and CQPSK alike. The Framer picks which of the ten
// samples per symbol to use by locking onto the frame sync.
//
// A frequency-locked loop removes tuning error first. Without it a 2 ppm
// dongle at 420 MHz (~840 Hz) adds 63° per symbol — enough to wrap ±3π/4
// symbols round to the opposite sign.
type Demod struct {
	framer *Framer

	hist [samplesPerSymbol]complex128 // last 10 AFC-corrected samples
	hpos int
	prev complex128 // previous sample for the instantaneous-frequency estimate

	ncoPhase float64 // AFC rotation applied to the input, radians
	ncoFreq  float64 // AFC frequency, radians/sample
}

// afcGain is the FLL loop gain per sample. ~0.3 s time constant at 48 kS/s:
// slow enough not to track modulation, fast enough to settle in a frame or
// two after tuning.
const afcGain = 1.0 / 15_000

// maxAFC clamps the loop to ±3 kHz so a burst of noise can't run it away.
const maxAFC = 2 * math.Pi * 3_000 / SampleRate

func NewDemod(f *Framer) *Demod { return &Demod{framer: f} }

// FreqOffsetHz is the AFC's current estimate of the tuning error.
func (d *Demod) FreqOffsetHz() float64 { return d.ncoFreq * SampleRate / (2 * math.Pi) }

// Process demodulates a block of baseband samples.
func (d *Demod) Process(in []complex128) {
	for _, x := range in {
		x *= cmplx.Rect(1, -d.ncoPhase)
		d.ncoPhase += d.ncoFreq
		if d.ncoPhase > math.Pi {
			d.ncoPhase -= 2 * math.Pi
		} else if d.ncoPhase < -math.Pi {
			d.ncoPhase += 2 * math.Pi
		}

		// FLL: integrate the residual instantaneous frequency. P25 data is
		// close enough to balanced that the modulation averages out.
		if inst := cmplx.Phase(x * cmplx.Conj(d.prev)); cmplx.Abs(x) > 0 {
			d.ncoFreq = max(-maxAFC, min(maxAFC, d.ncoFreq+afcGain*inst))
		}
		d.prev = x

		old := d.hist[d.hpos]
		d.hist[d.hpos] = x
		d.hpos = (d.hpos + 1) % samplesPerSymbol
		d.framer.push(cmplx.Phase(x * cmplx.Conj(old)))
	}
}

// ProcessCS8 demodulates interleaved signed 8-bit IQ (the fixture format).
func (d *Demod) ProcessCS8(iq []byte) {
	buf := make([]complex128, len(iq)/2)
	for i := range buf {
		buf[i] = complex(float64(int8(iq[2*i])), float64(int8(iq[2*i+1])))
	}
	d.Process(buf)
}
