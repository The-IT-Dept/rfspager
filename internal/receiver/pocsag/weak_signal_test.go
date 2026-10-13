package pocsag

import (
	"strings"
	"testing"
)

// Bit positions in an EncodeTransmission stream: 576 preamble bits, then a
// 32-bit sync, then 16 codewords of 32 bits per batch, then sync again.
const (
	preambleBits = 576
	syncBits     = 32
)

// codewordStart returns the first bit index of codeword n (0-based, counting
// across batches, address codeword = 0).
func codewordStart(n int) int {
	batch := n / codewordsPerBatch
	within := n % codewordsPerBatch
	return preambleBits + syncBits + batch*(syncBits+codewordsPerBatch*32) + within*32
}

func syncStart(batch int) int {
	return preambleBits + batch*(syncBits+codewordsPerBatch*32)
}

func flip(bits []bool, idx ...int) []bool {
	out := append([]bool(nil), bits...)
	for _, i := range idx {
		out[i] = !out[i]
	}
	return out
}

// An unrepairable codeword in the middle of a page must not shift every
// later character. Before the fix the decoder dropped the codeword's 20
// bits, so the rest of the page decoded 6 bits out of alignment as garbage.
func TestUncorrectableCodewordKeepsAlignment(t *testing.T) {
	bits := EncodeTransmission(testAddr, 0, testMsg)
	// Codeword 5 (a message codeword) gets 3 bit errors: beyond BCH.
	s := codewordStart(5)
	bad := flip(bits, s+3, s+9, s+15)
	pages := decode(withSilence(bad), -1)
	if len(pages) != 1 {
		t.Fatalf("want 1 page, got %d: %+v", len(pages), pages)
	}
	got := pages[0].Alpha
	if len(got) != len(testMsg) {
		t.Fatalf("length changed: got %d want %d\n got: %q", len(got), len(testMsg), got)
	}
	// The damaged codeword covers at most 4 characters; everything after
	// them must be intact.
	mismatch := 0
	firstBad, lastBad := -1, -1
	for i := range testMsg {
		if got[i] != testMsg[i] {
			mismatch++
			if firstBad < 0 {
				firstBad = i
			}
			lastBad = i
		}
	}
	if mismatch > 4 || (lastBad-firstBad) > 4 {
		t.Fatalf("damage should be confined to the bad codeword, got %d mismatches between %d and %d\n got: %q\nwant: %q", mismatch, firstBad, lastBad, got, testMsg)
	}
	if pages[0].Quality.Uncorrectable != 1 {
		t.Fatalf("page should report one uncorrectable codeword, got %+v", pages[0].Quality)
	}
	// Characters from the damaged codeword are flagged for the server/UI.
	if len(pages[0].Quality.DamagedRanges) == 0 {
		t.Fatalf("expected a damaged range to be reported, got %+v", pages[0].Quality)
	}
}

// Two different unrepairable codewords in one page still leave the text
// aligned and the page intact around them.
func TestTwoUncorrectableCodewordsStillAligned(t *testing.T) {
	bits := EncodeTransmission(testAddr, 0, testMsg)
	a, b := codewordStart(3), codewordStart(20)
	bad := flip(bits, a+1, a+12, a+20, b+2, b+8, b+27)
	pages := decode(withSilence(bad), -1)
	if len(pages) != 1 || len(pages[0].Alpha) != len(testMsg) {
		t.Fatalf("want one full-length page, got %+v", pages)
	}
	tail := testMsg[len(testMsg)-30:]
	if !strings.HasSuffix(pages[0].Alpha, tail) {
		t.Fatalf("tail should be intact, got %q", pages[0].Alpha[len(pages[0].Alpha)-30:])
	}
}

// A correction that flips bits to a codeword whose even parity does not
// hold is a mis-correction, not a repair: the raw bits are kept and the
// codeword counted as uncorrectable rather than inventing text.
func TestParityRejectsMiscorrection(t *testing.T) {
	cws := append([]uint32{AddressCodeword(testAddr, 0)}, AlphaCodewords(testMsg)...)
	for _, cw := range cws {
		if !evenParity(cw) {
			t.Fatalf("encoder produced a codeword with odd parity: %08x", cw)
		}
	}
	// Three errors on a message codeword: BCH will "correct" to some other
	// valid codeword about half the time. Find one such case and check
	// that parity catches it when it would otherwise lie.
	target := cws[6]
	found := false
	for a := 1; a < 31 && !found; a++ {
		for b := a + 1; b < 31 && !found; b++ {
			for c := b + 1; c < 31 && !found; c++ {
				bad := target ^ (1 << a) ^ (1 << b) ^ (1 << c)
				fixed, errs, ok := correctBCH(bad)
				if ok && errs > 0 && fixed != target && !evenParity(fixed) {
					found = true
					if _, _, ok2 := correctWithParity(bad); ok2 {
						t.Fatalf("mis-correction %08x -> %08x with bad parity was accepted", bad, fixed)
					}
				}
			}
		}
	}
	if !found {
		t.Skip("no parity-detectable mis-correction found for this codeword")
	}
}

// While locked, a sync word with up to 3 bit errors at the expected
// position keeps the page going instead of truncating it.
func TestNoisySyncKeepsLock(t *testing.T) {
	bits := EncodeTransmission(testAddr, 0, testMsg)
	s := syncStart(2) // the sync before the third batch, mid-message
	bad := flip(bits, s+4, s+17, s+29)
	pages := decode(withSilence(bad), -1)
	if len(pages) != 1 || pages[0].Alpha != testMsg {
		t.Fatalf("a 3-error sync mid-page should not truncate: got %+v", pages)
	}
}

// A one-bit timing slip before a sync (one bit dropped or repeated) must
// be absorbed by realigning on the sync word, not by losing the rest of
// the page.
func TestBitSlipBeforeSyncRealigns(t *testing.T) {
	bits := EncodeTransmission(testAddr, 0, testMsg)
	s := syncStart(2)
	// Drop one bit just before the sync: everything after arrives one bit early.
	dropped := append(append([]bool(nil), bits[:s-5]...), bits[s-4:]...)
	pages := decode(withSilence(dropped), -1)
	if len(pages) != 1 || pages[0].Alpha != testMsg {
		t.Fatalf("dropped bit before sync should realign: got %+v", pages)
	}
	// Repeat one bit just before the sync: everything after arrives one bit late.
	repeated := append(append([]bool(nil), bits[:s-5]...), bits[s-6:]...)
	pages = decode(withSilence(repeated), -1)
	if len(pages) != 1 || pages[0].Alpha != testMsg {
		t.Fatalf("repeated bit before sync should realign: got %+v", pages)
	}
}

// The end of a transmission is still detected: after the last batch the
// decoder must release the page once the sync fails to return, without
// appending noise as message text.
func TestEndOfTransmissionStillReleases(t *testing.T) {
	bits := EncodeTransmission(testAddr, 0, testMsg)
	pages := decode(withSilence(bits), -1)
	if len(pages) != 1 || pages[0].Alpha != testMsg {
		t.Fatalf("got %+v", pages)
	}
}
