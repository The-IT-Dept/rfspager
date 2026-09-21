package dsp

import (
	"math"
	"math/cmplx"
	"math/rand"
	"testing"
)

func randIQ(n int, seed int64) []complex128 {
	r := rand.New(rand.NewSource(seed))
	out := make([]complex128, n)
	for i := range out {
		out[i] = complex(r.Float64()*2-1, r.Float64()*2-1)
	}
	return out
}

// TestIQToComplexMatchesFormula pins the u8 → float mapping: 0 → -1, 255 → +1,
// 127.5 → 0 (there is no exact zero byte; 127 and 128 straddle it).
func TestIQToComplexMatchesFormula(t *testing.T) {
	buf := make([]byte, 512)
	for i := range 256 {
		buf[2*i] = byte(i)
		buf[2*i+1] = byte(255 - i)
	}
	got := IQToComplex(buf, nil)
	if len(got) != 256 {
		t.Fatalf("len=%d", len(got))
	}
	for i, v := range got {
		wantRe := (float64(i) - 127.5) / 127.5
		wantIm := (float64(255-i) - 127.5) / 127.5
		if math.Abs(real(v)-wantRe) > 1e-15 || math.Abs(imag(v)-wantIm) > 1e-15 {
			t.Fatalf("byte %d: got %v want (%v,%v)", i, v, wantRe, wantIm)
		}
	}
}

// TestMixBlockMatchesReference checks the block mixer against a literal
// cos/sin-per-sample NCO, fed in irregular chunk sizes so the phase carried
// between calls is exercised.
func TestMixBlockMatchesReference(t *testing.T) {
	const fs = 1_920_000.0
	for _, f := range []float64{-187_500, 187_500, 12_345.678} {
		in := randIQ(200_000, 1)
		want := make([]complex128, len(in))
		phase, inc := 0.0, -2*math.Pi*f/fs
		for i, s := range in {
			want[i] = s * complex(math.Cos(phase), math.Sin(phase))
			phase += inc
			if phase > math.Pi { // wrap like the original per-sample NCO did
				phase -= 2 * math.Pi
			} else if phase < -math.Pi {
				phase += 2 * math.Pi
			}
		}
		m := NewMixer(f, fs)
		var got []complex128
		for pos := 0; pos < len(in); {
			n := 1 + (pos*7919)%5000
			if pos+n > len(in) {
				n = len(in) - pos
			}
			got = append(got, m.MixBlock(in[pos:pos+n], nil)...)
			pos += n
		}
		if len(got) != len(want) {
			t.Fatalf("f=%v: got %d samples want %d", f, len(got), len(want))
		}
		maxErr := 0.0
		for i := range want {
			if e := cmplx.Abs(got[i] - want[i]); e > maxErr {
				maxErr = e
			}
		}
		if maxErr > 1e-9 {
			t.Errorf("f=%v: max abs error vs reference NCO = %g", f, maxErr)
		}
	}
}

// TestFIRDecimatorBlockMatchesReference checks ProcessBlock against the
// textbook definition — out[m] = Σ taps[i]·x[(m+1)·D − 1 − i], x[<0] = 0 —
// and that the output is identical no matter how the input is chunked.
func TestFIRDecimatorBlockMatchesReference(t *testing.T) {
	taps := LowpassTaps(12_000, 1_920_000, 96)
	const decim = 50
	in := randIQ(300_000, 2)

	var want []complex128
	for n := decim - 1; n < len(in); n += decim {
		var acc complex128
		for i, tp := range taps {
			j := n - i
			if j < 0 {
				break
			}
			acc += complex(real(in[j])*tp, imag(in[j])*tp)
		}
		want = append(want, acc)
	}

	for _, chunk := range []int{1, 7, 49, 50, 51, 97, 16384, len(in)} {
		f := NewFIRDecimator(taps, decim)
		var got []complex128
		for pos := 0; pos < len(in); pos += chunk {
			end := min(pos+chunk, len(in))
			got = append(got, f.ProcessBlock(in[pos:end], nil)...)
		}
		if len(got) != len(want) {
			t.Fatalf("chunk=%d: got %d outputs, want %d", chunk, len(got), len(want))
		}
		for i := range want {
			if cmplx.Abs(got[i]-want[i]) > 1e-12 {
				t.Fatalf("chunk=%d: output %d differs: got %v want %v", chunk, i, got[i], want[i])
			}
		}
	}
}
