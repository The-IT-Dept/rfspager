package p25

import "math/bits"

// The Network ID that follows every frame sync is a BCH(63,16,23) codeword
// plus one parity bit: 12-bit NAC + 4-bit DUID, protected by 47 check bits.
// d=23 corrects up to 11 bit errors.
//
// 2^16 codewords is small enough to decode by exhaustive nearest-codeword
// search: 65,536 XOR+popcount per frame at ~15 frames/s is nothing, and it is
// maximum-likelihood rather than bounded-distance, so it never does worse
// than a Berlekamp–Massey decoder would.

// nidGen is the BCH(63,16) generator polynomial, 6331 1413 6723 5453 (octal).
const nidGen uint64 = 0o6331141367235453

// nidCodewords[d] is the 63-bit codeword for data d (data in the top 16
// bits) shifted up one, with the always-zero 64th bit as bit 0.
var nidCodewords [1 << 16]uint64

func init() {
	for d := range uint64(1 << 16) {
		nidCodewords[d] = encodeNID(uint16(d))
	}
}

// encodeNID returns the 64-bit on-air NID for a 16-bit NAC<<4|DUID.
func encodeNID(data uint16) uint64 {
	reg := uint64(data) << 47
	for i := 62; i >= 47; i-- {
		if reg&(1<<i) != 0 {
			reg ^= nidGen << (i - 47)
		}
	}
	cw := uint64(data)<<47 | reg // 63 bits
	// The 64th bit is sent as 0 (not parity, as it's easy to assume: on the
	// NSW PSN every NID ends in 0, and an even-parity rule miscounts one bit
	// error in most frames).
	return cw << 1
}

// maxNIDErrors bounds what we accept. Beyond 11 errors the nearest codeword
// is no longer guaranteed to be the transmitted one; a little slack below
// that keeps false NIDs from noise out.
const maxNIDErrors = 9

// decodeNID returns NAC, DUID and the number of bit errors corrected, or
// ok=false when the received word is too far from every codeword.
func decodeNID(rx uint64) (nac uint16, duid uint8, errs int, ok bool) {
	best, bestD := 0, 65
	for d, cw := range nidCodewords {
		if dist := bits.OnesCount64(cw ^ rx); dist < bestD {
			best, bestD = d, dist
			if dist == 0 {
				break
			}
		}
	}
	if bestD > maxNIDErrors {
		return 0, 0, bestD, false
	}
	return uint16(best >> 4), uint8(best & 0xF), bestD, true
}

// Data Unit IDs carried in the NID.
const (
	DUIDHDU   = 0x0 // header data unit (voice)
	DUIDTDU   = 0x3 // terminator without link control
	DUIDLDU1  = 0x5 // logical link data unit 1 (voice)
	DUIDTSBK  = 0x7 // trunking signalling block(s)
	DUIDLDU2  = 0xA // logical link data unit 2 (voice)
	DUIDPDU   = 0xC // packet data unit (incl. multi-block trunking)
	DUIDTDULC = 0xF // terminator with link control
)
