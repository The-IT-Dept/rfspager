package ambe

import (
	"math/rand/v2"
	"testing"
)

func TestInterleaveIsPermutation(t *testing.T) {
	widths := [4]int{24, 23, 11, 14}
	var seen [4][24]bool
	for i, e := range p25p2Interleave {
		blk, bit := e>>5, int(e&31)
		if bit >= widths[blk] || seen[blk][bit] {
			t.Fatalf("entry %d: block %d bit %d out of range or repeated", i, blk, bit)
		}
		seen[blk][bit] = true
	}
}

// OP25's pr_n[] table holds the c1 mask (shifted left one bit) for each u0.
func TestMaskMatchesOP25(t *testing.T) {
	for _, tc := range []struct {
		u0  int
		prN uint32
	}{
		{0, 4377671}, {1, 1693438}, {2, 3163945}, {3, 7023824}, {8, 8494239},
	} {
		if got := p25p2Mask[tc.u0]; got != tc.prN>>1 {
			t.Errorf("u0 %d: mask %06x, want %06x", tc.u0, got, tc.prN>>1)
		}
	}
}

func randFrame(r *rand.Rand) Frame {
	return Frame{uint16(r.IntN(1 << 12)), uint16(r.IntN(1 << 12)), uint16(r.IntN(1 << 11)), uint16(r.IntN(1 << 14))}
}

func TestP25P2RoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for range 5000 {
		f := randFrame(r)
		vcw := EncodeP25P2(f)
		if got, errs := DecodeP25P2Frame(vcw[:]); got != f || errs != 0 {
			t.Fatalf("%04x: decoded %04x errs %d", f, got, errs)
		}
		if FrameFromB(f.B()) != f {
			t.Fatalf("%04x: B/FrameFromB not inverse (%v)", f, f.B())
		}
	}
}

// onAirBits lists the codeword positions carrying block blk.
func onAirBits(blk uint8) []int {
	var out []int
	for i, e := range p25p2Interleave {
		if e>>5 == blk {
			out = append(out, i)
		}
	}
	return out
}

func TestP25P2Errors(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	c0, c1, c3 := onAirBits(0), onAirBits(1), onAirBits(3)
	flip := func(vcw *[72]byte, pos []int, n int) {
		for _, k := range r.Perm(len(pos))[:n] {
			vcw[pos[k]] ^= 1
		}
	}
	for range 2000 {
		f := randFrame(r)
		e0, e1 := r.IntN(4), r.IntN(4)
		vcw := EncodeP25P2(f)
		flip(&vcw, c0, e0)
		flip(&vcw, c1, e1)
		got, errs := DecodeP25P2Frame(vcw[:])
		if got != f || errs != e0+e1 {
			t.Fatalf("%d+%d errors: decoded %04x errs %d, want %04x", e0, e1, got, errs, f)
		}

		// Unprotected bits are delivered as received and not counted.
		vcw = EncodeP25P2(f)
		flip(&vcw, c3, 1)
		if got, errs := DecodeP25P2Frame(vcw[:]); got[3] == f[3] || errs != 0 {
			t.Fatalf("c3 error: got %04x errs %d", got, errs)
		}

		vcw = EncodeP25P2(f)
		flip(&vcw, c0, 4)
		if _, errs := DecodeP25P2Frame(vcw[:]); errs != Uncorrectable {
			t.Fatalf("4 errors in c0: errs %d, want Uncorrectable", errs)
		}
	}
}
