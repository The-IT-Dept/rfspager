package p25

import "github.com/the-it-dept/rfspager/internal/receiver/dsp"

// Channelizer shifts one 12.5 kHz channel out of a dongle's wideband IQ and
// decimates it to SampleRate in two stages: a cheap wide filter down to
// ~240 kS/s, then a sharp one at the low rate. A single-stage filter at
// 1.92 MS/s would need thousands of taps to reject a stronger neighbour
// 12.5 kHz away; the second stage does it with ~240.
type Channelizer struct {
	mixer  *dsp.Mixer
	stage1 *dsp.FIRDecimator
	stage2 *dsp.FIRDecimator

	mixed, mid, out []complex128
}

// NewChannelizer extracts the channel offsetHz from the dongle centre.
// inputRate must be a multiple of 240 kHz times 5 of SampleRate (e.g. 1.92
// or 2.4 MS/s).
func NewChannelizer(offsetHz float64, inputRate int) *Channelizer {
	const midRate = 5 * SampleRate // 240 kS/s
	d1 := inputRate / midRate
	return &Channelizer{
		mixer:  dsp.NewMixer(offsetHz, float64(inputRate)),
		stage1: dsp.NewFIRDecimator(dsp.LowpassTaps(40_000, float64(inputRate), 8*d1+1), d1),
		stage2: dsp.NewFIRDecimator(dsp.LowpassTaps(7_000, midRate, 241), midRate/SampleRate),
	}
}

// Process returns the 48 kS/s baseband for a block of wideband IQ. The
// returned slice is reused on the next call.
func (c *Channelizer) Process(in []complex128) []complex128 {
	c.mixed = c.mixer.MixBlock(in, c.mixed)
	c.mid = c.stage1.ProcessBlock(c.mixed, c.mid)
	c.out = c.stage2.ProcessBlock(c.mid, c.out)
	return c.out
}
