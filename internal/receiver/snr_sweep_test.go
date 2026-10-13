package receiver

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
	"github.com/the-it-dept/rfspager/internal/receiver/pocsag"
)

// TestSNRSweep is the measuring stick for decoder work. It pushes the same
// RFS-style page through the production pipeline at a range of noise levels
// and reports, per level, how many trials decoded the page exactly, how many
// produced the page with a few damaged characters, and how many produced
// nothing or garbage. It only runs when RFS_SNR_SWEEP is set, because it
// takes a while:
//
//	RFS_SNR_SWEEP=1 go test ./internal/receiver -run TestSNRSweep -v
//
// Optional: RFS_SNR_TRIALS (default 12), RFS_SNR_BAUD (default 512).
func TestSNRSweep(t *testing.T) {
	if os.Getenv("RFS_SNR_SWEEP") == "" {
		t.Skip("set RFS_SNR_SWEEP=1 to run the sweep")
	}
	trials := 12
	if v, err := strconv.Atoi(os.Getenv("RFS_SNR_TRIALS")); err == nil && v > 0 {
		trials = v
	}
	baud := 512
	if v, err := strconv.Atoi(os.Getenv("RFS_SNR_BAUD")); err == nil && v > 0 {
		baud = v
	}
	bits := pocsag.EncodeTransmission(11416, 0, testMsg)
	// Carrier amplitude is swept at a fixed noise level so the ADC never
	// clips. SNR is quoted in the 9.5 kHz Carson bandwidth of a 512-baud
	// POCSAG signal, which is what matters for FM threshold.
	const noise = 0.3
	levels := []float64{0.12, 0.09, 0.07, 0.06, 0.05, 0.045, 0.04, 0.035, 0.03, 0.025}
	fmt.Printf("%-9s %-9s %6s %6s %7s %8s %8s  (trials=%d baud=%d)\n", "amp", "snr_dB", "exact", "near", "none", "garbage", "phantom", trials, baud)
	for _, amp := range levels {
		var exact, near, none, garbage, phantom int
		for trial := range trials {
			ch := NewChannel("RFS", 148_587_500, -testOffset, testRate, testAudio)
			stop := collect(ch)
			src := newFSK(bits, baud, -testOffset)
			src.amp, src.noise = amp, noise
			src.r = rand.New(rand.NewSource(int64(1000 + trial)))
			var work []complex128
			for i := 0; i < src.totalBuffers(); i++ {
				work = dsp.IQToComplex(src.next(), work)
				ch.Process(work)
			}
			pages := stop()
			var best *pocsag.Page
			for i := range pages {
				if pages[i].Address != "0011416" {
					phantom++
					if os.Getenv("RFS_SNR_DEBUG") != "" {
						fmt.Printf("    amp=%.3f trial=%d phantom addr=%s fn=%d q=%+v alpha=%q\n", amp, trial, pages[i].Address, pages[i].Function, pages[i].Quality, truncate(pages[i].Alpha, 60))
					}
					continue
				}
				if best == nil || len(pages[i].Alpha) > len(best.Alpha) {
					best = &pages[i]
				}
			}
			if os.Getenv("RFS_SNR_DEBUG") != "" && best != nil && best.Alpha != testMsg {
				fmt.Printf("    amp=%.3f trial=%d real q=%+v alpha=%q\n", amp, trial, best.Quality, truncate(best.Alpha, 70))
			}
			switch {
			case best == nil:
				none++
			case best.Alpha == testMsg:
				exact++
			case charErrors(best.Alpha, testMsg) <= 6 && len(best.Alpha) >= len(testMsg)-6:
				near++
			default:
				garbage++
			}
		}
		fmt.Printf("%-9.3f %-9.1f %6d %6d %7d %8d %8d\n", amp, snrDB(amp, noise, baud), exact, near, none, garbage, phantom)
	}
}

// snrDB is the carrier-to-noise ratio inside the signal's Carson bandwidth
// for the synthetic source: complex noise power per sample is 2*noise^2 over
// the full testRate, of which the fraction B/testRate lands in-band.
func snrDB(amp, noise float64, baud int) float64 {
	carson := 2 * (4500 + float64(baud)/2)
	noiseInBand := 2 * noise * noise * carson / testRate
	return 10 * math.Log10(amp*amp/noiseInBand)
}

// charErrors counts mismatching characters position by position plus the
// length difference, which is what a reader experiences as "damage".
func charErrors(got, want string) int {
	n := 0
	for i := 0; i < len(got) && i < len(want); i++ {
		if got[i] != want[i] {
			n++
		}
	}
	return n + int(math.Abs(float64(len(got)-len(want))))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var _ = strings.TrimSpace
