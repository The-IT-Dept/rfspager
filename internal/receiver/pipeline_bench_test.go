package receiver

import (
	"math"
	"math/rand"
	"testing"

	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
)

// makeIQ synthesises one RTL-SDR buffer of interleaved u8 IQ: two carriers
// at ±offsetHz (where the RFS / FRNSW channels sit) plus a little noise, so
// the mixer / FIR / demod / slicers all see realistic-looking input.
func makeIQ(n int, rate, offsetHz float64) []byte {
	buf := make([]byte, 2*n)
	r := rand.New(rand.NewSource(1))
	for i := 0; i < n; i++ {
		t := float64(i) / rate
		re := 0.3*math.Cos(2*math.Pi*offsetHz*t) + 0.3*math.Cos(-2*math.Pi*offsetHz*t) + 0.05*r.NormFloat64()
		im := 0.3*math.Sin(2*math.Pi*offsetHz*t) + 0.3*math.Sin(-2*math.Pi*offsetHz*t) + 0.05*r.NormFloat64()
		buf[2*i] = byte(math.Max(0, math.Min(255, 127.5+re*127.5)))
		buf[2*i+1] = byte(math.Max(0, math.Min(255, 127.5+im*127.5)))
	}
	return buf
}

// BenchmarkPipelineTwoChannels measures the cost of processing one 32 KiB
// RTL-SDR buffer (16384 complex samples, ~8.5 ms at 1.92 MS/s) through the
// exact per-buffer path cmd/receiver runs: IQToComplex + Process on both
// hardcoded channels. The "x-realtime" metric is CPU-time / wall-time of
// the IQ; anything ≥ 1.0 means the receiver cannot keep up and must drop.
func BenchmarkPipelineTwoChannels(b *testing.B) {
	const rate = 1_920_000
	const bufSamples = 16384
	iq := makeIQ(bufSamples, rate, 187_500)
	chans := []*Channel{
		NewChannel("RFS", 148_587_500, -187_500, rate, 38_400),
		NewChannel("FRNSW", 148_962_500, 187_500, rate, 38_400),
	}
	for _, c := range chans {
		go func(c *Channel) {
			for range c.Pages() {
			}
		}(c)
	}
	var work []complex128
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		work = dsp.IQToComplex(iq, work)
		for _, c := range chans {
			c.Process(work)
		}
	}
	b.StopTimer()
	perBuf := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
	realtime := float64(bufSamples) / rate * 1e9
	b.ReportMetric(perBuf/realtime, "x-realtime")
}
