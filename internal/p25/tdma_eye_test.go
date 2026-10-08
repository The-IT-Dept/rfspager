package p25

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
)

// Eye diagram and per-position error profile of a voice channel in a
// wideband u8 capture: P25_WIDE=file P25_CENTER=Hz P25_CHAN=Hz P25_AFC=Hz.
func TestDebugTDMAEye(t *testing.T) {
	path := os.Getenv("P25_WIDE")
	if path == "" {
		t.Skip()
	}
	num := func(k string) float64 { v, _ := strconv.ParseFloat(os.Getenv(k), 64); return v }
	center, chanHz, afc := num("P25_CENTER"), num("P25_CHAN"), num("P25_AFC")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ch := NewChannelizer(chanHz-center, 2_400_000)
	d := NewTDMADecoder(0xBEE00, 0x2D1, 0x2D2, nil)
	d.SetFreqOffsetHz(afc)
	var hist [25]int
	var posErr [p2SlotDibits]float64
	var posN [p2SlotDibits]int
	d.framer.debugSym = func(pos int, s float64) {
		b := int(math.Round(s*2)) + 12
		if b >= 0 && b < len(hist) {
			hist[b]++
		}
		e := s - nearestLevel(s)
		posErr[pos] += math.Abs(e)
		posN[pos]++
	}
	buf := make([]byte, 1<<18)
	var work []complex128
	for {
		n, err := io.ReadFull(f, buf)
		if n == 0 {
			break
		}
		work = dsp.IQToComplex(buf[:n], work)
		d.Process(ch.Process(work))
		if err != nil {
			break
		}
	}
	var sb strings.Builder
	for i, c := range hist {
		fmt.Fprintf(&sb, "%+.1f:%d ", float64(i-12)/2, c)
	}
	t.Logf("eye (symbol value: count): %s", sb.String())
	sb.Reset()
	for p := 0; p < p2SlotDibits; p += 10 {
		var e float64
		var n int
		for q := p; q < p+10; q++ {
			e += posErr[q]
			n += posN[q]
		}
		fmt.Fprintf(&sb, "%d:%.2f ", p, e/float64(max(n, 1)))
	}
	t.Logf("mean |distance to nearest level| by slot position (per 10 dibits): %s", sb.String())
	t.Logf("stats %+v afc %.0f", d.Stats(), d.FreqOffsetHz())
}
