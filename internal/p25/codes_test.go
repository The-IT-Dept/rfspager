package p25

import (
	"math/rand/v2"
	"testing"
)

func TestNIDRoundTripWithErrors(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		data := uint16(rng.Uint32())
		rx := encodeNID(data)
		for range 8 {
			rx ^= 1 << rng.IntN(64)
		}
		nac, duid, _, ok := decodeNID(rx)
		if !ok || nac != data>>4 || duid != uint8(data&0xF) {
			t.Fatalf("data %04x: got nac %03x duid %x ok %v", data, nac, duid, ok)
		}
	}
}

func TestTrellisRoundTripWithErrors(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for range 200 {
		var data [12]byte
		for i := range data {
			data[i] = byte(rng.Uint32())
		}
		tx := trellisEncode(data)
		got, errs := trellisDecode(tx[:])
		if got != data || errs != 0 {
			t.Fatalf("clean: got %x errs %d, want %x", got, errs, data)
		}
		// The ½-rate code is weak (it leans on the CRC), but two scattered
		// single-bit errors are within its reach.
		for _, p := range []int{5, 77} {
			tx[p] ^= uint8(1 << rng.IntN(2))
		}
		if got, _ := trellisDecode(tx[:]); got != data {
			t.Fatalf("2 errors: got %x want %x", got, data)
		}
	}
}

func TestCRCCCITTKnownVector(t *testing.T) {
	// CRC-16/GENIBUS (CCITT poly, init 0xFFFF, inverted) differs from ours;
	// ours is init 0, inverted: "123456789" → ^0x31C3 = 0xCE3C.
	if got := crcCCITT([]byte("123456789")); got != 0xCE3C {
		t.Fatalf("crc = %04x", got)
	}
}
