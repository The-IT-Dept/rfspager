package ambe

import "math/bits"

// Golay codes protecting the two most significant AMBE+2 words.
//
// The (23,12) code is systematic with the 12 data bits in bits 22..11 and the
// 11 check bits in 10..0, generator x^11+x^10+x^6+x^5+x^4+x^2+1 (0xC75) —
// the same layout OP25 (golay_23_encode) and mbelib (golayGenerator) use.
// The (24,12) code used for c0 is that codeword shifted up one bit with an
// even-parity bit appended in bit 0.
//
// (23,12) is a perfect code: every 23-bit word lies within distance 3 of
// exactly one codeword, so the 2048 syndromes map one-to-one onto the 2048
// error patterns of weight ≤ 3 and the decoder is a single table lookup. It
// can never report "uncorrectable" — four errors silently decode to the wrong
// codeword. The extended (24,12) code's parity bit is what lets c0 detect
// that case.

const golayPoly = 0xC75

// golaySyndromeToPattern maps a (23,12) syndrome to its weight ≤ 3 error
// pattern.
var golaySyndromeToPattern [2048]uint32

func init() {
	golaySyndromeToPattern[0] = 0
	for i := range 23 {
		a := uint32(1) << i
		golaySyndromeToPattern[golaySyndrome(a)] = a
		for j := i + 1; j < 23; j++ {
			b := a | uint32(1)<<j
			golaySyndromeToPattern[golaySyndrome(b)] = b
			for k := j + 1; k < 23; k++ {
				c := b | uint32(1)<<k
				golaySyndromeToPattern[golaySyndrome(c)] = c
			}
		}
	}
}

// golaySyndrome is the remainder of a 23-bit word divided by the generator;
// zero for a valid codeword.
func golaySyndrome(cw uint32) uint32 {
	for i := 22; i >= 11; i-- {
		if cw&(1<<i) != 0 {
			cw ^= golayPoly << (i - 11)
		}
	}
	return cw
}

// golay23Encode returns the 23-bit codeword for 12 data bits.
func golay23Encode(data uint32) uint32 {
	cw := (data & 0xfff) << 11
	return cw | golaySyndrome(cw)
}

// golay24Encode returns the 24-bit extended codeword for 12 data bits.
func golay24Encode(data uint32) uint32 {
	cw := golay23Encode(data)
	return cw<<1 | uint32(bits.OnesCount32(cw)&1)
}

// golay23Decode corrects up to three errors in a 23-bit codeword. errs is the
// number of bits flipped; dataErrs counts only those among the 12 data bits
// (mbelib's error count, which drives its frame-repeat rule).
func golay23Decode(cw uint32) (data uint32, errs, dataErrs int) {
	cw &= 0x7fffff
	pat := golaySyndromeToPattern[golaySyndrome(cw)]
	return (cw ^ pat) >> 11, bits.OnesCount32(pat), bits.OnesCount32(pat >> 11)
}

// golay24Decode corrects up to three errors in a 24-bit extended codeword
// and detects four. When ok is false the data is still the (23,12) decoder's
// best guess — exactly what mbelib, which ignores the parity bit, would use.
func golay24Decode(cw uint32) (data uint32, errs, dataErrs int, ok bool) {
	data, errs, dataErrs = golay23Decode(cw >> 1)
	if (bits.OnesCount32(golay23Encode(data))+int(cw&1))&1 != 0 {
		errs++ // the parity bit itself, or a fourth error
	}
	return data, errs, dataErrs, errs <= 3
}
