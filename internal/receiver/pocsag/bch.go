package pocsag

// BCH(31,21) error correction for POCSAG codewords.
//
// Each codeword is 32 bits. Bits 31..1 carry the BCH(31,21) codeword
// (type bit + 20 data + 10 check bits); bit 0 is even parity over bits 31..1
// — we ignore parity since the BCH is more powerful (corrects up to 2 errors).
//
// Generator polynomial: x^10 + x^9 + x^8 + x^6 + x^5 + x^3 + 1 = 0x769
//
// We precompute a syndrome → error-pattern table for all single- and double-
// bit errors (31 + C(31,2) = 496 patterns out of 1024 possible syndromes).
// Anything outside this table is treated as uncorrectable.

const bchGen = uint32(0x769)

var bchSyndromeToPattern [1024]uint32 // 31-bit error pattern in bits 30..0
var bchSyndromeValid [1024]bool

func init() {
	bchSyndromeValid[0] = true // syndrome 0 = no errors

	for i := range 31 {
		pat := uint32(1) << i
		s := bchSyndromeOf(pat)
		if !bchSyndromeValid[s] {
			bchSyndromeValid[s] = true
			bchSyndromeToPattern[s] = pat
		}
	}
	for i := range 31 {
		for j := i + 1; j < 31; j++ {
			pat := uint32(1)<<i | uint32(1)<<j
			s := bchSyndromeOf(pat)
			if !bchSyndromeValid[s] {
				bchSyndromeValid[s] = true
				bchSyndromeToPattern[s] = pat
			}
		}
	}
}

// bchSyndromeOf computes the BCH syndrome of a 31-bit codeword sitting in
// bits 30..0.
func bchSyndromeOf(code uint32) uint32 {
	poly := bchGen << 20
	mask := uint32(1 << 30)
	for mask >= (1 << 10) {
		if code&mask != 0 {
			code ^= poly
		}
		mask >>= 1
		poly >>= 1
	}
	return code & 0x3FF
}

// correctBCH takes a 32-bit POCSAG codeword (with parity in bit 0) and
// attempts to correct up to 2 bit errors. It returns the corrected codeword
// (parity bit preserved as-is), the number of bit errors that had to be
// corrected (0 = clean, 1–2 = corrected, -1 = uncorrectable), and a bool
// indicating whether correction was possible at all.
func correctBCH(cw uint32) (uint32, int, bool) {
	code := (cw >> 1) & 0x7FFFFFFF
	s := bchSyndromeOf(code)
	if s == 0 {
		return cw, 0, true
	}
	if !bchSyndromeValid[s] {
		return cw, -1, false
	}
	pat := bchSyndromeToPattern[s]
	errors := popcount32(pat)
	fixed := code ^ pat
	return (fixed << 1) | (cw & 1), errors, true
}

func popcount32(x uint32) int {
	c := 0
	for x != 0 {
		c++
		x &= x - 1
	}
	return c
}
