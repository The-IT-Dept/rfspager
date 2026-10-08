package p25

// crcCCITT computes the TSBK/PDU-header CRC: CCITT polynomial
// x^16+x^12+x^5+1, register initialised to 0, result inverted.
func crcCCITT(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return ^crc
}

// tsbkCRCOK checks the CRC in the last two bytes of a 12-byte TSBK.
func tsbkCRCOK(b [12]byte) bool {
	return crcCCITT(b[:10]) == uint16(b[10])<<8|uint16(b[11])
}
