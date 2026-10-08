package p25

import (
	"math"
	"os"
	"sort"
	"testing"
)

func TestDebugTDMASyncSpacing(t *testing.T) {
	path := os.Getenv("P25_CF32")
	if path == "" {
		t.Skip()
	}
	x := readCF32(t, path)
	// Run the demod only, capture the phase stream, then correlate offline.
	var ph []float64
	d := newDemod(sinkFunc(func(p float64) { ph = append(ph, p) }), p2SPS)
	d.fll = false
	d.SetFreqOffsetHz(-2150)
	d.Process(x)
	const span = 19 * p2SPS
	var peaks []int
	var scores []float64
	for i := span; i < len(ph); i++ {
		var c, e float64
		for k := range 20 {
			v := ph[i-span+k*p2SPS]
			c += p2SyncSymbols[k] * v
			e += v * v
		}
		sc := c / (p2SyncNorm * math.Sqrt(e))
		if sc > 0.8 && (len(peaks) == 0 || i-peaks[len(peaks)-1] > 4) {
			peaks = append(peaks, i)
			scores = append(scores, sc)
		} else if sc > 0.8 && sc > scores[len(scores)-1] {
			peaks[len(peaks)-1], scores[len(scores)-1] = i, sc
		}
	}
	gaps := map[int]int{}
	for i := 1; i < len(peaks); i++ {
		g := peaks[i] - peaks[i-1]
		gaps[(g+720)/1440]++ // in timeslots
	}
	t.Logf("%d sync peaks; gap histogram in timeslots: %v", len(peaks), gaps)
	var dev []int
	for i := 1; i < len(peaks); i++ {
		g := peaks[i] - peaks[i-1]
		if n := (g + 720) / 1440; n == 1 || n == 3 {
			dev = append(dev, g-n*1440)
		}
	}
	t.Logf("deviation from whole timeslots (samples), first 40: %v", dev[:min(40, len(dev))])
	// Chain slot indices through the gaps and fit position = a + L·index.
	var idx []float64
	var pos []float64
	cur := 0
	for i, p := range peaks {
		if i > 0 {
			cur += (p - peaks[i-1] + 720) / 1440
		}
		idx = append(idx, float64(cur))
		pos = append(pos, float64(p))
	}
	var sx, sy, sxx, sxy float64
	n := float64(len(idx))
	for i := range idx {
		sx, sy, sxx, sxy = sx+idx[i], sy+pos[i], sxx+idx[i]*idx[i], sxy+idx[i]*pos[i]
	}
	L := (n*sxy - sx*sy) / (n*sxx - sx*sx)
	t.Logf("fitted timeslot length %.3f samples (%.0f ppm), span %d slots", L, (L/1440-1)*1e6, cur)
	// Fractional timing: residue of peak positions mod 1440 relative to the first.
	res := map[int]int{}
	for _, p := range peaks {
		res[((p-peaks[0])%1440+1440)%1440/8]++
	}
	type kv struct{ k, v int }
	var top []kv
	for k, v := range res {
		top = append(top, kv{k, v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v })
	t.Logf("residue (symbols) top: %v", top[:min(8, len(top))])
	// Eye: symbol values at the sync-derived phase for 200 symbols after a peak.
	var hist [13]int
	for _, p := range peaks[:min(50, len(peaks))] {
		for k := 1; k <= 160; k++ {
			i := p + k*p2SPS
			if i >= len(ph) {
				break
			}
			s := ph[i] / (math.Pi / 4)
			b := int(math.Round(s*2)) + 6
			if b >= 0 && b < 13 {
				hist[b]++
			}
		}
	}
	t.Logf("eye histogram (s in half-units -3..+3): %v", hist)
}

type sinkFunc func(float64)

func (f sinkFunc) push(p float64) { f(p) }

func TestDebugTDMADescramble(t *testing.T) {
	path := os.Getenv("P25_CF32")
	if path == "" {
		t.Skip()
	}
	x := readCF32(t, path)
	for _, nac := range []uint16{0x2D2, 0x2D1, 0x2DF} {
		counts := map[string]int{}
		n := 0
		d := NewTDMADecoder(0xBEE00, 0x2D1, nac, func(slot int, dib []uint8) {
			counts[string(dib)]++
			n++
		})
		d.SetFreqOffsetHz(-1700)
		d.Process(x)
		best := 0
		for _, c := range counts {
			best = max(best, c)
		}
		t.Logf("NAC %03X: %d codewords, %d distinct, most common repeats %d times", nac, n, len(counts), best)
	}
}
