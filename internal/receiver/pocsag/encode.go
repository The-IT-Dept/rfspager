package pocsag

// POCSAG encoder. Used by tests and tooling to synthesise known-good
// transmissions; the receiver itself never transmits.

// BCHEncode takes the 21 payload bits of a codeword (type bit in bit 20,
// then 20 data bits) and returns the full 32-bit codeword: BCH(31,21)
// check bits appended, even parity over bits 31..1 placed in bit 0.
func BCHEncode(data21 uint32) uint32 {
	code := (data21 & 0x1FFFFF) << 10
	code |= bchSyndromeOf(code)
	parity := uint32(popcount32(code) & 1)
	return code<<1 | parity
}

// AddressCodeword builds the address codeword for a 21-bit capcode and
// function (0–3). The low 3 bits of the capcode are implied by the frame
// position in the batch, so only the upper 18 are encoded here.
func AddressCodeword(addr uint32, fn int) uint32 {
	high := (addr >> 3) & 0x3FFFF
	return BCHEncode(high<<2 | uint32(fn&3))
}

// AlphaCodewords packs msg as 7-bit ASCII, each character LSB-first, into
// 20-bit message codewords (type bit set). Trailing bits are zero-padded.
func AlphaCodewords(msg string) []uint32 {
	var bits []bool
	for _, c := range []byte(msg) {
		for i := 0; i < 7; i++ {
			bits = append(bits, c&(1<<i) != 0)
		}
	}
	for len(bits)%20 != 0 {
		bits = append(bits, false)
	}
	var cws []uint32
	for i := 0; i < len(bits); i += 20 {
		var d uint32
		for j := 0; j < 20; j++ {
			d <<= 1
			if bits[i+j] {
				d |= 1
			}
		}
		cws = append(cws, BCHEncode(1<<20|d))
	}
	return cws
}

// EncodeTransmission assembles a complete over-the-air bit sequence for one
// alphanumeric page: 576-bit preamble, then sync + 16 codewords per batch
// with the address in its frame slot and the message following, idle fill
// to the batch boundary, and one trailing all-idle batch (as real
// transmitters commonly send). Bits are MSB-first within each codeword.
func EncodeTransmission(addr uint32, fn int, msg string) []bool {
	var out []bool
	push := func(w uint32) {
		for i := 31; i >= 0; i-- {
			out = append(out, w&(1<<uint(i)) != 0)
		}
	}
	for i := 0; i < 576; i++ {
		out = append(out, i%2 == 0)
	}
	var cws []uint32
	for i := 0; i < 2*int(addr&7); i++ {
		cws = append(cws, IdleCodeword)
	}
	cws = append(cws, AddressCodeword(addr, fn))
	cws = append(cws, AlphaCodewords(msg)...)
	for len(cws)%16 != 0 {
		cws = append(cws, IdleCodeword)
	}
	for i := 0; i < 16; i++ {
		cws = append(cws, IdleCodeword)
	}
	for i := 0; i < len(cws); i += 16 {
		push(SyncWord)
		for _, c := range cws[i : i+16] {
			push(c)
		}
	}
	return out
}
