package p25

import (
	"encoding/binary"
	"math"
	"os"
	"strings"
	"testing"
)

func TestP2XorMaskMatchesOP25(t *testing.T) {
	// Reference: OP25 tdma/lfsr.py evaluated with its literal 44×44 matrix
	// for the NSW PSN (WACN BEE00, SysID 2D1, NAC 2D2).
	const want = "2332320000023101023102202321331002121121102202123223330112003333"
	m := p2XorMask(0xBEE00, 0x2D1, 0x2D2)
	var b strings.Builder
	sum := 0
	for i, d := range m {
		if i < len(want) {
			b.WriteByte('0' + d)
		}
		sum += int(d)
	}
	if b.String() != want || sum != 3304 {
		t.Fatalf("mask %s… sum %d", b.String(), sum)
	}
}

// readCF32 loads a 48 kS/s complex float32 recording (p25receiver -record).
func readCF32(t *testing.T, path string) []complex128 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]complex128, len(raw)/8)
	for i := range out {
		re := math.Float32frombits(binary.LittleEndian.Uint32(raw[8*i:]))
		im := math.Float32frombits(binary.LittleEndian.Uint32(raw[8*i+4:]))
		out[i] = complex(float64(re), float64(im))
	}
	return out
}

func TestDebugTDMARecording(t *testing.T) {
	path := os.Getenv("P25_CF32")
	if path == "" {
		t.Skip("set P25_CF32 to a p25receiver -record file")
	}
	d := NewTDMADecoder(0xBEE00, 0x2D1, 0x2D2, nil)
	d.SetFreqOffsetHz(-1700) // the control channel AFC on that dongle
	d.Process(readCF32(t, path))
	t.Logf("%+v afc=%.0fHz", d.Stats(), d.demod.FreqOffsetHz())
}

func TestNearestLevel(t *testing.T) {
	for s, want := range map[float64]float64{3: 3, 2.1: 3, 1.9: 1, 0.2: 1, -0.2: -1, -1.7: -1, -2.3: -3, -5: -3, 5: 3} {
		if got := nearestLevel(s); got != want {
			t.Errorf("nearestLevel(%v) = %v, want %v", s, got, want)
		}
	}
}
