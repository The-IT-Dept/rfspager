package p25

// ½-rate trellis code used for TSBKs and confirmed/unconfirmed data blocks
// (TIA-102.BAAA §7). 96 data bits = 48 dibits, plus a flush dibit of 0,
// drive a 4-state encoder whose state is the previous input dibit; each step
// emits one 4-bit constellation point, so 49 points = 196 bits = 98 dibits
// on air, which are then block-interleaved.

// trellisOut[state][input] is the constellation point emitted.
var trellisOut = [4][4]uint8{
	{0, 15, 12, 3},
	{4, 11, 8, 7},
	{13, 2, 1, 14},
	{9, 6, 5, 10},
}

// pointDibits maps a constellation point to the two on-air dibits it is sent
// as, packed hi<<2|lo. Verified against live NSW PSN control channels: with
// this table every TSBK in the fixtures passes its CRC.
var pointDibits = [16]uint8{
	0b0010, 0b1010, 0b0111, 0b1111, 0b1110, 0b0110, 0b1011, 0b0011,
	0b1101, 0b0101, 0b1000, 0b0000, 0b0001, 0b1001, 0b0100, 0b1100,
}

// blockInterleave[i] is the de-interleaved position of on-air dibit i for a
// 98-dibit (196-bit) block.
var blockInterleave = [98]uint8{
	0, 1, 8, 9, 16, 17, 24, 25, 32, 33, 40, 41, 48, 49, 56, 57, 64, 65, 72, 73, 80, 81, 88, 89, 96, 97,
	2, 3, 10, 11, 18, 19, 26, 27, 34, 35, 42, 43, 50, 51, 58, 59, 66, 67, 74, 75, 82, 83, 90, 91,
	4, 5, 12, 13, 20, 21, 28, 29, 36, 37, 44, 45, 52, 53, 60, 61, 68, 69, 76, 77, 84, 85, 92, 93,
	6, 7, 14, 15, 22, 23, 30, 31, 38, 39, 46, 47, 54, 55, 62, 63, 70, 71, 78, 79, 86, 87, 94, 95,
}

// trellisEncode is the transmit side, used by tests to build known-good
// blocks: 12 data bytes → 98 interleaved on-air dibits.
func trellisEncode(data [12]byte) [98]uint8 {
	var tx [98]uint8 // de-interleaved order
	state := uint8(0)
	for k := range 49 {
		var in uint8
		if k < 48 {
			in = (data[k/4] >> (6 - 2*(k%4))) & 3
		}
		p := pointDibits[trellisOut[state][in]]
		tx[2*k], tx[2*k+1] = p>>2, p&3
		state = in
	}
	var out [98]uint8
	for i, pos := range blockInterleave {
		out[i] = tx[pos]
	}
	return out
}

// trellisDecode de-interleaves 98 on-air dibits and Viterbi-decodes them back
// to 12 bytes. The metric is Hamming distance between received and expected
// dibit pairs; errs is the path metric of the survivor (0 = clean block).
func trellisDecode(rx []uint8) (data [12]byte, errs int) {
	var di [98]uint8
	for i, pos := range blockInterleave {
		di[pos] = rx[i]
	}

	const inf = 1 << 30
	var metric [4]int
	metric[1], metric[2], metric[3] = inf, inf, inf // encoder starts in state 0
	var from [49][4]uint8                           // survivor predecessor per step/state

	for k := range 49 {
		got := di[2*k]<<2 | di[2*k+1]
		next := [4]int{inf, inf, inf, inf}
		for s := range 4 {
			if metric[s] == inf {
				continue
			}
			for in := range 4 {
				m := metric[s] + popcount4(pointDibits[trellisOut[s][in]]^got)
				if m < next[in] {
					next[in] = m
					from[k][in] = uint8(s)
				}
			}
		}
		metric = next
	}

	// The flush dibit is 0, so the path must end in state 0.
	errs = metric[0]
	state := uint8(0)
	var inputs [49]uint8
	for k := 48; k >= 0; k-- {
		inputs[k] = state
		state = from[k][state]
	}
	for k := range 48 {
		data[k/4] |= inputs[k] << (6 - 2*(k%4))
	}
	return data, errs
}

func popcount4(x uint8) int {
	return int((x & 1) + (x >> 1 & 1) + (x >> 2 & 1) + (x >> 3 & 1))
}
