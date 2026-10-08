package ambe

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"math"
	"os"
	"testing"
)

// Golden vectors from mbelib + OP25 (testdata/harness/harness.c). Each file
// is a run of fixed-size records, one per decoded frame, in four sequences
// that each start from a fresh decoder:
//
//	0  clean speech-like frames (random-walk pitch/gain), two silence frames
//	1  clean uniformly random frames: erasure/tone/silence b0, big jumps in L
//	2  speech-like frames with 0-9 bit errors, missing bursts (Erasure) and a
//	   long bad run that must end in muting
//	3  OP25's AMBE encoder on testdata/op25enc_tones.vcw's source signal
//
// mbelib_f32 is stock mbelib; mbelib_f64 is the same source compiled with
// float → double. Parameters are compared against the stock build. PCM is
// compared tightly against the double build (this package computes in
// float64) and loosely against the stock build, whose float32 phase
// accumulators drift a little over a call. Both builds link mbelib's rand()
// to the same xorshift generator as this package, so even unvoiced noise is
// sample-for-sample comparable.
//
// To regenerate: testdata/harness/build.sh <mbelib> <op25> <scratch-dir>,
// which reproduces the checked-in files bit for bit.

type goldenFrame struct {
	seq     int
	erasure bool // an Erasure() call rather than a codeword
	vcw     [72]byte
	u       Frame
	errs    int
	w0      float32
	L       int
	gamma   float32
	repeat  int
	voiced  [maxL]bool
	ml      [maxL]float32
	pcm     [160]int16
}

func loadGolden(t testing.TB, name string) []goldenFrame {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Seq, Kind uint8
		VCW       [9]uint8
		U         [4]uint16
		Errs      uint16
		W0        float32
		L         uint8
		Gamma     float32
		Repeat    uint8
		Vl        [maxL]uint8
		Ml        [maxL]float32
		PCM       [160]int16
	}
	size := binary.Size(rec)
	if len(data)%size != 0 {
		t.Fatalf("%s: %d bytes is not a whole number of %d-byte records", name, len(data), size)
	}
	r := bytes.NewReader(data)
	var out []goldenFrame
	for r.Len() > 0 {
		if err := binary.Read(r, binary.LittleEndian, &rec); err != nil {
			t.Fatal(err)
		}
		g := goldenFrame{
			seq: int(rec.Seq), erasure: rec.Kind == 1, u: rec.U, errs: int(rec.Errs),
			w0: rec.W0, L: int(rec.L), gamma: rec.Gamma, repeat: int(rec.Repeat),
			ml: rec.Ml, pcm: rec.PCM,
		}
		for i := range g.vcw {
			g.vcw[i] = rec.VCW[i/8] >> (7 - i%8) & 1
		}
		for i, v := range rec.Vl {
			g.voiced[i] = v != 0
		}
		out = append(out, g)
	}
	return out
}

// runGolden decodes the vectors' inputs and calls check with each frame's
// output and resulting parameters.
func runGolden(frames []goldenFrame, check func(i int, g *goldenFrame, pcm [160]int16, errs int, p Params)) {
	var d *Decoder
	for i := range frames {
		g := &frames[i]
		if i == 0 || g.seq != frames[i-1].seq {
			d = NewDecoder()
		}
		var pcm [160]int16
		errs := 0
		if g.erasure {
			pcm = d.Erasure()
		} else {
			pcm, errs = d.DecodeP25P2(g.vcw[:])
		}
		check(i, g, pcm, errs, d.Params())
	}
}

func TestGoldenFEC(t *testing.T) {
	frames := loadGolden(t, "mbelib_f32.bin.gz")
	if len(frames) < 400 {
		t.Fatalf("only %d frames", len(frames))
	}
	for i, g := range frames {
		if g.erasure {
			continue
		}
		f, errs := DecodeP25P2Frame(g.vcw[:])
		if f != g.u || errs != g.errs {
			t.Errorf("frame %d (seq %d): got %04x errs %d, want %04x errs %d", i, g.seq, f, errs, g.u, g.errs)
		}
	}
}

func TestGoldenParams(t *testing.T) {
	frames := loadGolden(t, "mbelib_f32.bin.gz")
	var worst float64
	runGolden(frames, func(i int, g *goldenFrame, _ [160]int16, errs int, p Params) {
		if errs != g.errs {
			t.Errorf("frame %d: errs %d, want %d", i, errs, g.errs)
		}
		if float32(p.W0) != g.w0 || p.L != g.L || p.Repeat != g.repeat {
			t.Fatalf("frame %d (seq %d): w0 %v L %d repeat %d, want %v %d %d",
				i, g.seq, float32(p.W0), p.L, p.Repeat, g.w0, g.L, g.repeat)
		}
		if d := relErr(p.Gamma, float64(g.gamma)); d > 1e-5 {
			t.Errorf("frame %d: gamma %v, want %v", i, p.Gamma, g.gamma)
		}
		for l := range p.L {
			if p.Voiced[l] != g.voiced[l] {
				t.Fatalf("frame %d: harmonic %d voiced %v, want %v", i, l+1, p.Voiced[l], g.voiced[l])
			}
			d := relErr(p.Mag[l], float64(g.ml[l]))
			worst = max(worst, d)
			if d > 1e-4 {
				t.Errorf("frame %d: harmonic %d magnitude %v, want %v", i, l+1, p.Mag[l], g.ml[l])
			}
		}
	})
	t.Logf("worst magnitude relative error vs mbelib: %.2g", worst)
}

func relErr(got, want float64) float64 {
	return math.Abs(got-want) / max(math.Abs(want), 1e-6)
}

// pcmStats compares PCM per frame: worst absolute sample difference and the
// overall SNR of ours against the reference.
type pcmStats struct {
	maxDiff    int
	sig, noise float64
	worstFrame int
}

func (s *pcmStats) add(i int, got, want [160]int16) {
	fd := 0
	for n := range got {
		d := int(got[n]) - int(want[n])
		fd = max(fd, abs(d))
		s.sig += float64(want[n]) * float64(want[n])
		s.noise += float64(d) * float64(d)
	}
	if fd > s.maxDiff {
		s.maxDiff, s.worstFrame = fd, i
	}
}

func (s *pcmStats) snr() float64 { return 10 * math.Log10(s.sig/max(s.noise, 1e-9)) }

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func TestGoldenPCM(t *testing.T) {
	for _, tc := range []struct {
		file string
		// Per-sequence floors. Against the double build the only
		// differences are rounding (≤ 1 LSB in practice). The stock build
		// differs from the double build by the same amounts as from us —
		// mbelib's own float32 noise, worst on sequence 3's long loud tone
		// where its unbounded phase accumulators lose precision.
		minSNR  [4]float64
		maxDiff int
	}{
		{"mbelib_f64.bin.gz", [4]float64{100, 100, 100, 100}, 2},
		{"mbelib_f32.bin.gz", [4]float64{55, 65, 65, 33}, 2500},
	} {
		t.Run(tc.file, func(t *testing.T) {
			frames := loadGolden(t, tc.file)
			var seqs [4]pcmStats
			runGolden(frames, func(i int, g *goldenFrame, pcm [160]int16, _ int, _ Params) {
				seqs[g.seq].add(i, pcm, g.pcm)
			})
			for s, st := range seqs {
				t.Logf("seq %d: SNR %.1f dB, max sample diff %d (frame %d)", s, st.snr(), st.maxDiff, st.worstFrame)
				if st.snr() < tc.minSNR[s] || st.maxDiff > tc.maxDiff {
					t.Errorf("seq %d: SNR %.1f dB (min %.0f), max sample diff %d (max %d) at frame %d",
						s, st.snr(), tc.minSNR[s], st.maxDiff, tc.maxDiff, st.worstFrame)
				}
			}
		})
	}
}
