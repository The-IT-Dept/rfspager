package ambe

import (
	"math"
	"os"
	"testing"
)

// loadVCW reads packed 72-bit codewords (9 bytes each, MSB first).
func loadVCW(t testing.TB, name string) [][72]byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	out := make([][72]byte, len(raw)/9)
	for i := range out {
		for b := range 72 {
			out[i][b] = raw[i*9+b/8] >> (7 - b%8) & 1
		}
	}
	return out
}

func rms(x []float64) float64 {
	var e float64
	for _, v := range x {
		e += v * v
	}
	return math.Sqrt(e / float64(len(x)))
}

// toneRatio is the fraction of x's power at frequency f (Hz), by
// single-bin DFT.
func toneRatio(x []float64, f float64) float64 {
	var re, im float64
	for n, v := range x {
		s, c := math.Sincos(2 * math.Pi * f * float64(n) / 8000)
		re += v * c
		im -= v * s
	}
	n := float64(len(x))
	return 2 * (re*re + im*im) / (n * n) / (rms(x) * rms(x))
}

func autocorr(x []float64, lag int) float64 {
	var num, e0, e1 float64
	for n := lag; n < len(x); n++ {
		num += x[n] * x[n-lag]
		e0 += x[n] * x[n]
		e1 += x[n-lag] * x[n-lag]
	}
	return num / math.Sqrt(e0*e1)
}

// op25enc_tones.vcw is OP25's AMBE encoder (ambe_encoder.cc, via
// testdata/harness/encoder_main.cc) run on testdata/harness/mkaudio.c's
// signal: 20 frames of silence, 50 of a 400 Hz sine, 15 of silence, then 50
// of a 150 Hz harmonic buzz.
func TestEncodedTones(t *testing.T) {
	d := NewDecoder()
	var pcm []float64
	for _, vcw := range loadVCW(t, "op25enc_tones.vcw") {
		out, errs := d.DecodeP25P2(vcw[:])
		if errs != 0 {
			t.Fatalf("clean encoder output decoded with %d errors", errs)
		}
		for _, v := range out {
			pcm = append(pcm, float64(v))
		}
	}
	frames := func(a, b int) []float64 { return pcm[a*160 : b*160] }

	for _, quiet := range [][2]int{{3, 20}, {76, 85}} {
		if r := rms(frames(quiet[0], quiet[1])); r > 200 {
			t.Errorf("silence frames %v: rms %.0f", quiet, r)
		}
	}

	// The encoder models the sine as harmonic 6 of a 68 Hz pitch, and
	// synthesis restarts phase every frame, so measure per frame.
	var at400, off float64
	for f := 30; f < 70; f++ {
		x := frames(f, f+1)
		at400 += toneRatio(x, 400) / 40
		off += max(toneRatio(x, 300), toneRatio(x, 500)) / 40
	}
	if r := rms(frames(30, 70)); r < 2000 {
		t.Errorf("tone rms %.0f, too quiet", r)
	}
	if at400 < 0.4 || off > at400/10 {
		t.Errorf("tone: %.2f of power at 400 Hz, %.2f at 300/500 Hz", at400, off)
	}

	// The encoder halves the buzz's pitch (74.5 Hz); either way the output
	// must repeat every 107 samples (two 150 Hz periods).
	if c := autocorr(frames(95, 135), 107); c < 0.8 {
		t.Errorf("buzz autocorrelation at 107 samples = %.2f", c)
	}
}

// voicedFrame is a fully voiced frame at a mid pitch and moderate gain.
var voicedFrame = FrameFromB([9]int{40, 0, 20, 100, 50, 10, 5, 5, 3})

func TestErasureRepeatsThenMutes(t *testing.T) {
	d := NewDecoder()
	for range 5 {
		d.DecodeFrame(voicedFrame, false)
	}
	want := d.Params()
	for i := 1; i <= maxRepeats; i++ {
		pcm := d.Erasure()
		p := d.Params()
		if p.Repeat != i || p.L != want.L || p.W0 != want.W0 {
			t.Fatalf("erasure %d: repeat %d L %d, want repeat %d L %d", i, p.Repeat, p.L, i, want.L)
		}
		if pcm == ([160]int16{}) {
			t.Fatalf("erasure %d: silent, want the previous frame repeated", i)
		}
	}
	if pcm := d.Erasure(); pcm != ([160]int16{}) {
		t.Fatal("4th erasure in a row should mute")
	}
	if p := d.Params(); p.Repeat != 0 || p.L != 30 {
		t.Fatalf("after mute: repeat %d L %d, want the reset state", p.Repeat, p.L)
	}
	// A good frame recovers.
	if pcm := d.DecodeFrame(voicedFrame, false); pcm == ([160]int16{}) || d.Params().Repeat != 0 {
		t.Fatal("good frame after mute did not decode")
	}
}

func TestUncorrectableFrameRepeats(t *testing.T) {
	d := NewDecoder()
	d.DecodeFrame(voicedFrame, false)
	vcw := EncodeP25P2(FrameFromB([9]int{90, 20, 5, 0, 0, 0, 0, 0, 0}))
	for _, pos := range onAirBits(0)[:4] {
		vcw[pos] ^= 1
	}
	if _, errs := d.DecodeP25P2(vcw[:]); errs != Uncorrectable {
		t.Fatalf("errs %d, want Uncorrectable", errs)
	}
	if p := d.Params(); p.Repeat != 1 || p.W0 != newDecoderAfter(voicedFrame).Params().W0 {
		t.Fatalf("uncorrectable frame was not replaced by a repeat: %+v", p)
	}
}

// newDecoderAfter returns a decoder that has decoded f once.
func newDecoderAfter(f Frame) *Decoder {
	d := NewDecoder()
	d.DecodeFrame(f, false)
	return d
}

func TestSpecialFramesMute(t *testing.T) {
	for _, b0 := range []int{120, 123, 126, 127} {
		d := newDecoderAfter(voicedFrame)
		if pcm := d.DecodeFrame(FrameFromB([9]int{b0}), false); pcm != ([160]int16{}) {
			t.Errorf("b0 %d: want silence", b0)
		}
	}
	// Silence frames are comfort noise: all unvoiced, still synthesised.
	d := newDecoderAfter(voicedFrame)
	d.DecodeFrame(FrameFromB([9]int{124, 0, 10}), false)
	if p := d.Params(); p.L != 14 || p.Voiced[0] {
		t.Errorf("silence frame: L %d voiced[0] %v", p.L, p.Voiced[0])
	}
}

func TestDibitsMatchesBits(t *testing.T) {
	frames := loadVCW(t, "op25enc_tones.vcw")
	a, b := NewDecoder(), NewDecoder()
	for i, vcw := range frames {
		var dibits [36]byte
		for k := range dibits {
			dibits[k] = vcw[2*k]<<1 | vcw[2*k+1]
		}
		pa, ea := a.DecodeP25P2(vcw[:])
		pb, eb := b.DecodeP25P2Dibits(dibits[:])
		if pa != pb || ea != eb {
			t.Fatalf("frame %d: dibit and bit forms decode differently", i)
		}
	}
}

func BenchmarkDecodeP25P2(b *testing.B) {
	frames := loadVCW(b, "op25enc_tones.vcw")
	for _, g := range loadGolden(b, "mbelib_f32.bin.gz") {
		if g.seq == 0 {
			frames = append(frames, g.vcw)
		}
	}
	d := NewDecoder()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		d.DecodeP25P2(frames[i%len(frames)][:])
	}
}

// BenchmarkDecodeWorstCase is the most expensive frame there is: 56
// harmonics, all unvoiced, all above the noise threshold's cut-in.
func BenchmarkDecodeWorstCase(b *testing.B) {
	vcw := EncodeP25P2(FrameFromB([9]int{119, 16, 20, 100, 50, 10, 5, 5, 3}))
	d := NewDecoder()
	for b.Loop() {
		d.DecodeP25P2(vcw[:])
	}
}

// TestAllPitchTransitions steps between every pair of harmonic counts (and
// the special b0 codes) in both directions, which exercises every path of
// the magnitude interpolation and the synthesis padding.
func TestAllPitchTransitions(t *testing.T) {
	var b0s []int
	for b0 := range 120 {
		if b0 == 0 || ambeL[b0] != ambeL[b0-1] {
			b0s = append(b0s, b0)
		}
	}
	b0s = append(b0s, 120, 124, 126)
	d := NewDecoder()
	for _, a := range b0s {
		for _, b := range b0s {
			for _, b0 := range []int{a, b} {
				d.DecodeFrame(FrameFromB([9]int{b0, b0 % 32, 18, 300, 70, 9, 4, 6, 2}), false)
				for l, m := range d.Params().Mag {
					if math.IsNaN(m) || math.IsInf(m, 0) {
						t.Fatalf("%d→%d: harmonic %d magnitude %v", a, b, l+1, m)
					}
				}
			}
		}
	}
}
