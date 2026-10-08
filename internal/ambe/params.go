package ambe

import "math"

// AMBE+2 half-rate parameter dequantisation, ported from mbelib's
// mbe_decodeAmbe2450Parms. Speech is modelled as a fundamental w0 with L
// harmonics, each voiced (a sinusoid) or unvoiced (band-limited noise) with a
// spectral magnitude Ml. The magnitudes are coded differentially against the
// previous frame's log2 magnitudes, so the decoder is stateful.
//
// Arithmetic is float64, except where mbelib's float32 rounding decides an
// integer (the voicing band index jl): that is computed in float32 so the
// decoded voicing matches mbelib exactly. w0 rounded to float32 is bit-equal
// to mbelib's.

// maxL is the most harmonics a frame can have; arrays are indexed 1..maxL as
// in mbelib, with index 0 used by the magnitude interpolation.
const maxL = 56

// mbeParams mirrors mbelib's mbe_parms. Fields beyond L keep whatever an
// earlier frame left there, as in mbelib; the interpolation and synthesis
// rely on that, so a frame must never be zeroed wholesale except by reset.
type mbeParams struct {
	w0     float64
	L      int
	voiced [maxL + 1]bool
	ml     [maxL + 1]float64 // spectral magnitudes
	// log2Ml has one spare slot: the interpolation reads index prev.L+1
	// (with zero weight) when prev.L is 56. In mbelib that read runs off
	// the end of the array into the next struct field.
	log2Ml [maxL + 2]float64
	phi    [maxL + 1]float64 // synthesis phase incl. random dither
	psi    [maxL + 1]float64 // phase accumulator, kept in [-π, π)
	gamma  float64           // gain
	repeat int               // consecutive repeated frames
}

// reset is mbe_initMbeParms for one parameter set.
func (p *mbeParams) reset() {
	*p = mbeParams{w0: 0.09378, L: 30}
	for l := range p.psi {
		p.psi[l] = math.Pi / 2
	}
}

// frameKind is what b0 says a frame is.
type frameKind int

const (
	kindVoice   frameKind = iota
	kindErasure           // b0 120..123: the encoder marked it bad
	kindTone              // b0 126..127: DTMF/call-progress tone (not synthesised)
)

// dctCos[n][k][j] is cos(π·k·(j+½)/n), the inverse-DCT kernel for a block of
// length n ≤ 17 (the PRBA uses n = 8).
var dctCos [18][17][17]float64

func init() {
	for n := 1; n < len(dctCos); n++ {
		for k := range n {
			for j := range n {
				dctCos[n][k][j] = math.Cos(math.Pi * float64(k) * (float64(j) + 0.5) / float64(n))
			}
		}
	}
}

// dequantize decodes b into cur, using prev for the differential terms. Like
// mbelib it also writes to prev: magnitudes past prev.L are extended so the
// interpolation can read them.
func dequantize(b *[9]int, cur, prev *mbeParams) frameKind {
	cur.repeat = prev.repeat

	b0 := b[0]
	var f0 float32
	silence := false
	switch {
	case b0 >= 120 && b0 <= 123:
		return kindErasure
	case b0 == 124 || b0 == 125:
		// Silence is coded as an all-unvoiced frame at a fixed pitch: the
		// rest of the frame still sets the comfort-noise level.
		silence = true
		cur.w0 = 2 * math.Pi / 32
		f0 = 1.0 / 32
		cur.L = 14
		for l := 1; l <= 14; l++ {
			cur.voiced[l] = false
		}
	case b0 >= 126:
		return kindTone
	default:
		f0 = ambeW0[b0]
		cur.w0 = float64(f0) * 2 * math.Pi
		cur.L = int(ambeL[b0])
	}
	L := cur.L

	// Unvoiced bands get their magnitude scaled down: noise spread across
	// a band carries more energy than a single harmonic of the same height.
	unvc := 0.2046 / math.Sqrt(cur.w0)

	if !silence {
		vuv := &ambeVUV[b[1]]
		for l := 1; l <= L; l++ {
			jl := int(float32(l) * 16 * f0) // float32, as mbelib: decides a band
			cur.voiced[l] = vuv[jl] != 0
		}
	}

	cur.gamma = float64(ambeDg[b[2]]) + 0.5*prev.gamma

	// PRBA vector G1..G8 → R1..R8 by inverse DCT.
	var gm [8]float64 // gm[m-1] = G_m; G1 is always 0
	for i, v := range ambePRBA24[b[3]] {
		gm[1+i] = float64(v)
	}
	for i, v := range ambePRBA58[b[4]] {
		gm[4+i] = float64(v)
	}
	var ri [8]float64
	for i := range 8 {
		var sum float64
		for m := range 8 {
			am := 2.0
			if m == 0 {
				am = 1
			}
			sum += am * gm[m] * dctCos[8][m][i]
		}
		ri[i] = sum
	}

	// Four residual blocks: the first two DCT coefficients of each come
	// from the PRBA pairs, the next up to four from the HOC codebooks.
	ji := &ambeLmprbl[L]
	hoc := [4]*[4]float32{&ambeHOCb5[b[5]], &ambeHOCb6[b[6]], &ambeHOCb7[b[7]], &ambeHOCb8[b[8]]}
	var tl [maxL + 1]float64
	l := 1
	for i := range 4 {
		var cik [17]float64 // cik[k-1] = C_i,k
		cik[0] = 0.5 * (ri[2*i] + ri[2*i+1])
		cik[1] = (ri[2*i] - ri[2*i+1]) / (2 * math.Sqrt2)
		n := int(ji[i])
		for k := 3; k <= n && k <= 6; k++ {
			cik[k-1] = float64(hoc[i][k-3])
		}
		for j := range n {
			var sum float64
			for k := range n {
				ak := 2.0
				if k == 0 {
					ak = 1
				}
				sum += ak * cik[k] * dctCos[n][k][j]
			}
			tl[l] = sum
			l++
		}
	}

	// Predict this frame's log magnitudes from the previous frame's,
	// resampled from prev.L harmonics to L (eqs. 40-43).
	if L > prev.L {
		for l := prev.L + 1; l <= L; l++ {
			prev.ml[l] = prev.ml[prev.L]
			prev.log2Ml[l] = prev.log2Ml[prev.L]
		}
	}
	prev.log2Ml[0] = prev.log2Ml[1]
	prev.ml[0] = prev.ml[1]

	var intkl [maxL + 1]int
	var deltal [maxL + 1]float64
	var sum43, sum42 float64
	ratio := float64(prev.L) / float64(L)
	for l := 1; l <= L; l++ {
		flok := ratio * float64(l)
		intkl[l] = int(flok)
		deltal[l] = flok - float64(intkl[l])
		sum43 += (1-deltal[l])*prev.log2Ml[intkl[l]] + deltal[l]*prev.log2Ml[intkl[l]+1]
		sum42 += tl[l]
	}
	sum43 *= 0.65 / float64(L)
	sum42 /= float64(L)
	bigGamma := cur.gamma - 0.5*math.Log2(float64(L)) - sum42

	for l := 1; l <= L; l++ {
		c1 := 0.65 * (1 - deltal[l]) * prev.log2Ml[intkl[l]]
		c2 := 0.65 * deltal[l] * prev.log2Ml[intkl[l]+1]
		cur.log2Ml[l] = tl[l] + c1 + c2 - sum43 + bigGamma
		// mbelib's exp(0.693·x) ≈ 2^x; kept for bit-compatibility.
		m := math.Exp(0.693 * cur.log2Ml[l])
		if !cur.voiced[l] {
			m *= unvc
		}
		cur.ml[l] = m
	}
	return kindVoice
}

// Params is a snapshot of one frame's decoded model parameters.
type Params struct {
	// W0 is the fundamental frequency in radians per sample (8 kHz).
	W0 float64
	// L is the number of harmonics.
	L int
	// Voiced[l-1] and Mag[l-1] are harmonic l's voicing decision and
	// spectral magnitude (before spectral enhancement).
	Voiced []bool
	Mag    []float64
	// Gamma is the frame gain.
	Gamma float64
	// Repeat counts consecutive frames repeated because of errors.
	Repeat int
}

func (p *mbeParams) snapshot() Params {
	out := Params{
		W0:     p.w0,
		L:      p.L,
		Voiced: make([]bool, p.L),
		Mag:    make([]float64, p.L),
		Gamma:  p.gamma,
		Repeat: p.repeat,
	}
	copy(out.Voiced, p.voiced[1:p.L+1])
	copy(out.Mag, p.ml[1:p.L+1])
	return out
}
