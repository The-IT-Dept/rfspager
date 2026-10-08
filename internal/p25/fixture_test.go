package p25

import (
	"fmt"
	"os"
	"testing"
)

// decodeFixture runs a cs8 capture through the full demod + framer.
func decodeFixture(t *testing.T, name string) []Frame {
	t.Helper()
	iq, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var frames []Frame
	d := NewDemod(NewFramer(func(f Frame) { frames = append(frames, f) }))
	d.ProcessCS8(iq)
	return frames
}

func TestFixtureDecodeStats(t *testing.T) {
	for _, name := range []string{"cc_420.2983.cs8", "cc_419.1125.cs8", "cc_419.0250.cs8"} {
		frames := decodeFixture(t, name)
		nacs := map[string]int{}
		duids := map[uint8]int{}
		var blocks, crcOK int
		for _, f := range frames {
			nacs[fmt.Sprintf("%03X", f.NAC)]++
			duids[f.DUID]++
			for _, b := range f.Blocks {
				blocks++
				if b.CRCOK {
					crcOK++
				}
			}
		}
		t.Logf("%s: %d frames, NACs %v, DUIDs %v, blocks %d, CRC ok %d", name, len(frames), nacs, duids, blocks, crcOK)
	}
}
