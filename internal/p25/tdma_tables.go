// Code generated from OP25 (boatbod/op25 gr-op25_repeater/lib/p25p2_isch.cc,
// p25p2_duid.cc; GPL-3.0). DO NOT EDIT.

package p25

// ischCodewords[v] is the 40-bit I-ISCH codeword carrying the 7-bit value v.
var ischCodewords = [128]uint64{
	0x184229d461, 0x18761451f6, 0x181ae27e2f, 0x182edffbb8,
	0x18df8a7510, 0x18ebb7f087, 0x188741df5e, 0x18b37c5ac9,
	0x1146a44f13, 0x117299ca84, 0x111e6fe55d, 0x112a5260ca,
	0x11db07ee62, 0x11ef3a6bf5, 0x1183cc442c, 0x11b7f1c1bb,
	0x1a4a2e239e, 0x1a7e13a609, 0x1a12e589d0, 0x1a26d80c47,
	0x1ad78d82ef, 0x1ae3b00778, 0x1a8f4628a1, 0x1abb7bad36,
	0x134ea3b8ec, 0x137a9e3d7b, 0x13166812a2, 0x1322559735,
	0x13d300199d, 0x13e73d9c0a, 0x138bcbb3d3, 0x13bff63644,
	0x1442f705ef, 0x1476ca8078, 0x141a3cafa1, 0x142e012a36,
	0x14df54a49e, 0x14eb692109, 0x14879f0ed0, 0x14b3a28b47,
	0x1d467a9e9d, 0x1d72471b0a, 0x1d1eb134d3, 0x1d2a8cb144,
	0x1ddbd93fec, 0x1defe4ba7b, 0x1d831295a2, 0x1db72f1035,
	0x164af0f210, 0x167ecd7787, 0x16123b585e, 0x162606ddc9,
	0x16d7535361, 0x16e36ed6f6, 0x168f98f92f, 0x16bba57cb8,
	0x1f4e7d6962, 0x1f7a40ecf5, 0x1f16b6c32c, 0x1f228b46bb,
	0x1fd3dec813, 0x1fe7e34d84, 0x1f8b15625d, 0x1fbf28e7ca,
	0x084d62c339, 0x08795f46ae, 0x0815a96977, 0x082194ece0,
	0x08d0c16248, 0x08e4fce7df, 0x08880ac806, 0x08bc374d91,
	0x0149ef584b, 0x017dd2dddc, 0x011124f205, 0x0125197792,
	0x01d44cf93a, 0x01e0717cad, 0x018c875374, 0x01b8bad6e3,
	0x0a456534c6, 0x0a7158b151, 0x0a1dae9e88, 0x0a29931b1f,
	0x0ad8c695b7, 0x0aecfb1020, 0x0a800d3ff9, 0x0ab430ba6e,
	0x0341e8afb4, 0x0375d52a23, 0x03192305fa, 0x032d1e806d,
	0x03dc4b0ec5, 0x03e8768b52, 0x038480a48b, 0x03b0bd211c,
	0x044dbc12b7, 0x0479819720, 0x041577b8f9, 0x04214a3d6e,
	0x04d01fb3c6, 0x04e4223651, 0x0488d41988, 0x04bce99c1f,
	0x0d493189c5, 0x0d7d0c0c52, 0x0d11fa238b, 0x0d25c7a61c,
	0x0dd49228b4, 0x0de0afad23, 0x0d8c5982fa, 0x0db864076d,
	0x0645bbe548, 0x06718660df, 0x061d704f06, 0x06294dca91,
	0x06d8184439, 0x06ec25c1ae, 0x0680d3ee77, 0x06b4ee6be0,
	0x0f41367e3a, 0x0f750bfbad, 0x0f19fdd474, 0x0f2dc051e3,
	0x0fdc95df4b, 0x0fe8a85adc, 0x0f845e7505, 0x0fb063f092,
}

// duidLookup maps the 8 DUID bits of a Phase 2 burst (4 dibits) to the burst
// type (0 4V, 3 SACCH, 6 2V, 9 FACCH, 12/13/15 unscrambled ACCH), or -1.
var duidLookup = [256]int8{
	0, 0, 0, -1, 0, -1, -1, 1, 0, -1, -1, 4, -1, 8, 2, -1,
	0, -1, -1, 1, -1, 1, 1, 1, -1, 3, 9, -1, 5, -1, -1, 1,
	0, -1, -1, 10, -1, 6, 2, -1, -1, 3, 2, -1, 2, -1, 2, 2,
	-1, 3, 7, -1, 11, -1, -1, 1, 3, 3, -1, 3, -1, 3, 2, -1,
	0, -1, -1, 4, -1, 6, 12, -1, -1, 4, 4, 4, 5, -1, -1, 4,
	-1, 13, 7, -1, 5, -1, -1, 1, 5, -1, -1, 4, 5, 5, 5, -1,
	-1, 6, 7, -1, 6, 6, -1, 6, 14, -1, -1, 4, -1, 6, 2, -1,
	7, -1, 7, 7, -1, 6, 7, -1, -1, 3, 7, -1, 5, -1, -1, 15,
	0, -1, -1, 10, -1, 8, 12, -1, -1, 8, 9, -1, 8, 8, -1, 8,
	-1, 13, 9, -1, 11, -1, -1, 1, 9, -1, 9, 9, -1, 8, 9, -1,
	-1, 10, 10, 10, 11, -1, -1, 10, 14, -1, -1, 10, -1, 8, 2, -1,
	11, -1, -1, 10, 11, 11, 11, -1, -1, 3, 9, -1, 11, -1, -1, 15,
	-1, 13, 12, -1, 12, -1, 12, 12, 14, -1, -1, 4, -1, 8, 12, -1,
	13, 13, -1, 13, -1, 13, 12, -1, -1, 13, 9, -1, 5, -1, -1, 15,
	14, -1, -1, 10, -1, 6, 12, -1, 14, 14, 14, -1, 14, -1, -1, 15,
	-1, 13, 7, -1, 11, -1, -1, 15, 14, -1, -1, 15, -1, 15, 15, 15,
}
