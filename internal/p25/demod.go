package p25

import (
	"math"
	"math/cmplx"
)

// Demod turns 48 kS/s complex baseband into a stream of soft symbol-rate
// phase values, one per input sample, and hands them to a Framer.
//
// Each output is arg(x[n]·conj(x[n−sps])): the carrier phase advance across
// one symbol period, in radians. P25 symbols are ±π/4 (dibits 00/10) and ±3π/4
// (01/11), for C4FM and CQPSK alike. The Framer picks which of the ten
// samples per symbol to use by locking onto the frame sync.
//
// A frequency-locked loop removes tuning error first. Without it a 2 ppm
// dongle at 420 MHz (~840 Hz) adds 63° per symbol — enough to wrap ±3π/4
// symbols round to the opposite sign.
type Demod struct {
	sink symbolSink

	hist []complex128 // last one-symbol's worth of AFC-corrected samples
	hpos int
	prev complex128 // previous sample for the instantaneous-frequency estimate

	ncoPhase float64 // AFC rotation applied to the input, radians
	ncoFreq  float64 // AFC frequency, radians/sample

	// fll enables the data-driven loop. Phase 2 voice channels turn it off:
	// their idle and unscrambled bursts aren't balanced, which biases it,
	// so the TDMA framer steers the NCO from each sync word instead.
	fll bool
}

// afcGain is the FLL loop gain per sample. ~0.3 s time constant at 48 kS/s:
// slow enough not to track modulation, fast enough to settle in a frame or
// two after tuning.
const afcGain = 1.0 / 15_000

// maxAFC clamps the loop to ±3 kHz so a burst of noise can't run it away.
const maxAFC = 2 * math.Pi * 3_000 / SampleRate

// symbolSink consumes the per-sample differential phase stream.
type symbolSink interface{ push(ph float64) }

// NewDemod feeds a Phase 1 Framer (4800 baud, 10 samples/symbol).
func NewDemod(f *Framer) *Demod { return newDemod(f, samplesPerSymbol) }

func newDemod(sink symbolSink, sps int) *Demod {
	return &Demod{sink: sink, hist: make([]complex128, sps), fll: true}
}

// SetFreqOffsetHz presets the AFC, e.g. from the control channel decoder
// running on the same dongle.
func (d *Demod) SetFreqOffsetHz(hz float64) {
	d.ncoFreq = max(-maxAFC, min(maxAFC, 2*math.Pi*hz/SampleRate))
}

// steer nudges the AFC by delta radians/sample.
func (d *Demod) steer(delta float64) {
	d.ncoFreq = max(-maxAFC, min(maxAFC, d.ncoFreq+delta))
}

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
		if inst := cmplx.Phase(x * cmplx.Conj(d.prev)); d.fll && cmplx.Abs(x) > 0 {
			d.ncoFreq = max(-maxAFC, min(maxAFC, d.ncoFreq+afcGain*inst))
		}
		d.prev = x

		old := d.hist[d.hpos]
		d.hist[d.hpos] = x
		d.hpos = (d.hpos + 1) % len(d.hist)
		d.sink.push(cmplx.Phase(x * cmplx.Conj(old)))
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
