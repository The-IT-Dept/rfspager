package ambe

// P25 Phase 2 vocoder codeword (TIA-102.BBAC) → AMBE+2 frame, ported from
// OP25's p25p2_vf.cc (extract_vcw / process_vcw / encode_vcw).
//
// A 72-bit voice codeword carries four FEC blocks:
//
//	c0  24 bits  Golay(24,12) over u0
//	c1  23 bits  Golay(23,12) over u1, XORed with a PRNG mask seeded from u0
//	c2  11 bits  u2, unprotected
//	c3  14 bits  u3, unprotected
//
// The u1 mask means a corrupted u0 scrambles u1 too, so the decoder can't
// trust u1 unless u0 decoded cleanly — which is why the error rules weight
// c0 so heavily.

// Frame is the 49 information bits of one AMBE+2 half-rate frame as OP25's
// u[] words: u0 (12 bits), u1 (12), u2 (11), u3 (14). Read MSB first,
// u0‖u1‖u2‖u3 is mbelib's ambe_d[0..48].
type Frame [4]uint16

// p25p2Interleave[i] is the (block, bit) that on-air codeword bit i carries,
// block<<5 | bit. Bit numbers count from the block's LSB, so {0, 23} is the
// MSB of c0. Transcribed from OP25 extract_vcw.
var p25p2Interleave = [72]uint8{
	0<<5 | 23, 0<<5 | 5, 1<<5 | 10, 2<<5 | 3, 0<<5 | 22, 0<<5 | 4, 1<<5 | 9, 2<<5 | 2,
	0<<5 | 21, 0<<5 | 3, 1<<5 | 8, 2<<5 | 1, 0<<5 | 20, 0<<5 | 2, 1<<5 | 7, 2<<5 | 0,
	0<<5 | 19, 0<<5 | 1, 1<<5 | 6, 3<<5 | 13, 0<<5 | 18, 0<<5 | 0, 1<<5 | 5, 3<<5 | 12,
	0<<5 | 17, 1<<5 | 22, 1<<5 | 4, 3<<5 | 11, 0<<5 | 16, 1<<5 | 21, 1<<5 | 3, 3<<5 | 10,
	0<<5 | 15, 1<<5 | 20, 1<<5 | 2, 3<<5 | 9, 0<<5 | 14, 1<<5 | 19, 1<<5 | 1, 3<<5 | 8,
	0<<5 | 13, 1<<5 | 18, 1<<5 | 0, 3<<5 | 7, 0<<5 | 12, 1<<5 | 17, 2<<5 | 10, 3<<5 | 6,
	0<<5 | 11, 1<<5 | 16, 2<<5 | 9, 3<<5 | 5, 0<<5 | 10, 1<<5 | 15, 2<<5 | 8, 3<<5 | 4,
	0<<5 | 9, 1<<5 | 14, 2<<5 | 7, 3<<5 | 3, 0<<5 | 8, 1<<5 | 13, 2<<5 | 6, 3<<5 | 2,
	0<<5 | 7, 1<<5 | 12, 2<<5 | 5, 3<<5 | 1, 0<<5 | 6, 1<<5 | 11, 2<<5 | 4, 3<<5 | 0,
}

// p25p2Mask[u0] is the 23-bit mask XORed onto c1. It is the top bit of
// successive outputs of the 16-bit LCG x' = 173x + 13849 seeded with 16·u0
// (OP25's pr_n[u0] >> 1; mbelib's mbe_demodulateAmbe3600x2450Data).
var p25p2Mask [4096]uint32

func init() {
	for u0 := range uint32(4096) {
		pr := 16 * u0
		var m uint32
		for range 23 {
			pr = (173*pr + 13849) & 0xffff
			m = m<<1 | pr>>15
		}
		p25p2Mask[u0] = m
	}
}

// deinterleaveP25P2 splits 72 codeword bits (one per byte, on-air order) into
// the raw blocks c0..c3.
func deinterleaveP25P2(vcw []byte) (c [4]uint32) {
	_ = vcw[71]
	for i, t := range p25p2Interleave {
		c[t>>5] |= uint32(vcw[i]&1) << (t & 31)
	}
	return c
}

// fecResult is a decoded codeword plus the error figures the frame-repeat
// logic needs.
type fecResult struct {
	frame Frame
	// errs is the total number of bits corrected in c0 and c1, or
	// Uncorrectable.
	errs int
	// mbeErrs is mbelib's errs2: data bits corrected in c0 and c1, forced
	// above the repeat threshold when c0 is uncorrectable.
	mbeErrs int
}

// Uncorrectable is the error count DecodeP25P2 reports when c0 has more
// errors than Golay(24,12) can correct.
const Uncorrectable = 99

func decodeP25P2FEC(c [4]uint32) fecResult {
	u0, e0, d0, ok := golay24Decode(c[0])
	u1, e1, d1 := golay23Decode(c[1] ^ p25p2Mask[u0])
	r := fecResult{
		frame:   Frame{uint16(u0), uint16(u1), uint16(c[2] & 0x7ff), uint16(c[3] & 0x3fff)},
		errs:    e0 + e1,
		mbeErrs: d0 + d1,
	}
	if !ok {
		r.errs, r.mbeErrs = Uncorrectable, Uncorrectable
	}
	return r
}

// DecodeP25P2Frame de-interleaves, Golay-corrects and de-scrambles one 72-bit
// voice codeword (see Decoder.DecodeP25P2 for the bit order). errs is the
// number of bits corrected, or Uncorrectable.
func DecodeP25P2Frame(vcw []byte) (f Frame, errs int) {
	r := decodeP25P2FEC(deinterleaveP25P2(vcw))
	return r.frame, r.errs
}

// EncodeP25P2 is the inverse of DecodeP25P2Frame: it builds the 72 on-air
// codeword bits for a frame. Used to synthesise test traffic.
func EncodeP25P2(f Frame) (vcw [72]byte) {
	u0 := uint32(f[0] & 0xfff)
	c := [4]uint32{
		golay24Encode(u0),
		golay23Encode(uint32(f[1]&0xfff)) ^ p25p2Mask[u0],
		uint32(f[2] & 0x7ff),
		uint32(f[3] & 0x3fff),
	}
	for i, t := range p25p2Interleave {
		vcw[i] = byte(c[t>>5]>>(t&31)) & 1
	}
	return vcw
}

// B returns the nine quantiser indices b0..b8 the frame's bits are
// scattered into (OP25 p25p2_vf::unpack_b): b0 pitch (7 bits), b1 voicing
// (5), b2 gain (5), b3/b4 PRBA vectors (9, 7) and b5..b8 higher-order
// coefficients (5, 4, 4, 3).
func (f Frame) B() (b [9]int) {
	u0, u1, u2, u3 := int(f[0]), int(f[1]), int(f[2]), int(f[3])
	b[0] = (u0>>5)&0x78 | (u3>>9)&0x7
	b[1] = (u0>>3)&0x1e | (u3>>13)&0x1
	b[2] = (u0<<1)&0x1e | (u3>>12)&0x1
	b[3] = (u1>>3)&0x1fe | (u3>>8)&0x1
	b[4] = (u1<<3)&0x78 | (u3>>5)&0x7
	b[5] = (u2>>6)&0x1e | (u3>>4)&0x1
	b[6] = (u2>>3)&0x0e | (u3>>3)&0x1
	b[7] = u2&0x0e | (u3>>2)&0x1
	b[8] = (u2<<2)&0x04 | u3&0x3
	return b
}

// FrameFromB is the inverse of Frame.B (OP25 encode_vcw's packing).
func FrameFromB(b [9]int) Frame {
	return Frame{
		uint16((b[0]&0x78)<<5 | (b[1]&0x1e)<<3 | (b[2]&0x1e)>>1),
		uint16((b[3]&0x1fe)<<3 | (b[4]&0x78)>>3),
		uint16((b[5]&0x1e)<<6 | (b[6]&0xe)<<3 | b[7]&0xe | (b[8]&0x4)>>2),
		uint16((b[1]&0x1)<<13 | (b[2]&0x1)<<12 | (b[0]&0x7)<<9 | (b[3]&0x1)<<8 |
			(b[4]&0x7)<<5 | (b[5]&0x1)<<4 | (b[6]&0x1)<<3 | (b[7]&0x1)<<2 | b[8]&0x3),
	}
}
