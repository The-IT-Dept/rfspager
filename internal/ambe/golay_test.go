package ambe

import (
	"math/bits"
	"math/rand/v2"
	"testing"
)

// OP25 op25_golay.h golay_24_encode's generator rows: the codeword for each
// single data bit, MSB first.
var op25Golay24Rows = [12]uint32{
	040006165, 020003073, 010007550, 04003664, 02001732, 01006631,
	0403315, 0201547, 0106706, 045227, 024476, 014353,
}

func TestGolay24MatchesOP25(t *testing.T) {
	for i, want := range op25Golay24Rows {
		if got := golay24Encode(1 << (11 - i)); got != want {
			t.Errorf("data bit %d: %#o, want %#o", 11-i, got, want)
		}
	}
}

func TestGolayCodewords(t *testing.T) {
	for d := range uint32(4096) {
		cw := golay24Encode(d)
		if golaySyndrome(cw>>1) != 0 || bits.OnesCount32(cw)&1 != 0 {
			t.Fatalf("data %03x: %06x is not a valid codeword", d, cw)
		}
		if got, errs, _, ok := golay24Decode(cw); got != d || errs != 0 || !ok {
			t.Fatalf("data %03x: clean decode gave %03x errs %d ok %v", d, got, errs, ok)
		}
	}
}

// randPattern returns a random n-bit word with exactly w bits set.
func randPattern(r *rand.Rand, n, w int) uint32 {
	var p uint32
	for bits.OnesCount32(p) < w {
		p |= 1 << r.IntN(n)
	}
	return p
}

func TestGolay23CorrectsUpToThree(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 20000 {
		d := r.Uint32() & 0xfff
		w := r.IntN(4)
		pat := randPattern(r, 23, w)
		got, errs, dataErrs := golay23Decode(golay23Encode(d) ^ pat)
		if got != d || errs != w || dataErrs != bits.OnesCount32(pat>>11) {
			t.Fatalf("data %03x pattern %06x: got %03x errs %d/%d", d, pat, got, errs, dataErrs)
		}
	}
}

func TestGolay24(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for range 20000 {
		d := r.Uint32() & 0xfff
		w := r.IntN(5)
		pat := randPattern(r, 24, w)
		got, errs, _, ok := golay24Decode(golay24Encode(d) ^ pat)
		if w <= 3 {
			if got != d || errs != w || !ok {
				t.Fatalf("data %03x, %d errors (%06x): got %03x errs %d ok %v", d, w, pat, got, errs, ok)
			}
		} else if ok {
			t.Fatalf("data %03x, 4 errors (%06x): not detected (got %03x errs %d)", d, pat, got, errs)
		}
	}
}
