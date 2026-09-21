package dsp

import (
	"math/rand"
	"testing"
)

// TestBitSlicerKeepsRunningOnNoise: between bursts the demodulator output
// is noise, with sign flips almost every sample. The slicer must keep
// emitting (garbage) bits at roughly the baud rate — the decoder relies on
// seeing bits after a burst to notice the sync word did not come back and
// release the last page. A timing loop that is pulled back toward the bit
// boundary on every crossing stalls here and emits nothing.
func TestBitSlicerKeepsRunningOnNoise(t *testing.T) {
	const fs = 38_400.0
	r := rand.New(rand.NewSource(3))
	for _, baud := range []float64{512, 1200, 2400} {
		s := NewBitSlicer(fs, baud)
		n := 0
		for i := 0; i < int(fs)*2; i++ { // two seconds of noise
			if _, ok := s.Push(r.Float64()*2 - 1); ok {
				n++
			}
		}
		want := int(2 * baud)
		if n < want*8/10 || n > want*12/10 {
			t.Errorf("baud=%v: emitted %d bits in 2s of noise, want ≈ %d", baud, n, want)
		}
	}
}

// TestBitSlicerRecoversNoisyBits: a rectangular ±0.7 bit stream with a DC
// offset and heavy per-sample noise (σ = 0.5, i.e. ~3 dB SNR per sample).
// At 75 samples/bit there is ample energy per symbol; a slicer that
// integrates across the bit must recover it error-free once locked, while
// a single-sample slicer would see ~8% bit errors.
func TestBitSlicerRecoversNoisyBits(t *testing.T) {
	const fs = 38_400.0
	for _, baud := range []float64{512, 1200} {
		r := rand.New(rand.NewSource(int64(baud)))
		spb := fs / baud
		const nBits = 1500
		tx := make([]bool, nBits)
		for i := range tx {
			tx[i] = r.Intn(2) == 1
		}
		// Preamble-ish start so the DC tracker and timing settle quickly.
		for i := 0; i < 64; i++ {
			tx[i] = i%2 == 0
		}
		s := NewBitSlicer(fs, baud)
		var rx []bool
		total := int(float64(nBits) * spb)
		for i := 0; i < total; i++ {
			bit := tx[int(float64(i)/spb)]
			x := -0.7
			if bit {
				x = 0.7
			}
			x += 0.1 + 0.5*r.NormFloat64() // DC offset + noise
			if b, ok := s.Push(x); ok {
				rx = append(rx, b)
			}
		}
		if len(rx) < nBits-5 || len(rx) > nBits+5 {
			t.Fatalf("baud=%v: got %d bits, want ≈ %d", baud, len(rx), nBits)
		}
		// Align on the tail (timing may have slipped a bit or two while
		// locking) and count errors over the last 1200 bits.
		best, bestErrs := 0, 1<<30
		for off := -4; off <= 4; off++ {
			errs := 0
			for i := nBits - 1200; i < nBits; i++ {
				j := i + off
				if j < 0 || j >= len(rx) {
					errs++
					continue
				}
				if rx[j] != tx[i] {
					errs++
				}
			}
			if errs < bestErrs {
				best, bestErrs = off, errs
			}
		}
		if bestErrs != 0 {
			t.Errorf("baud=%v: %d bit errors in last 1200 bits (best alignment offset %d)", baud, bestErrs, best)
		}
	}
}
