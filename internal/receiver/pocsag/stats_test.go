package pocsag

import (
	"testing"
)

// streamBitOffset returns the index in an EncodeTransmission bit stream of
// the MSB of codeword cwIdx (counting from the first codeword after the
// first sync word; a sync word precedes every 16 codewords).
func streamBitOffset(cwIdx int) int {
	return 576 + 32 + 32*cwIdx + 32*(cwIdx/16)
}

// flipCodewordBits flips the given codeword bit numbers (31 = first
// transmitted, 1 = last BCH-covered bit) of codeword cwIdx in bits.
func flipCodewordBits(bits []bool, cwIdx int, cwBits ...int) []bool {
	out := append([]bool{}, bits...)
	base := streamBitOffset(cwIdx)
	for _, b := range cwBits {
		out[base+(31-b)] = !out[base+(31-b)]
	}
	return out
}

func decodeWithStats(bits []bool, resyncAt int) ([]Page, Stats) {
	out := make(chan Page, 256)
	d := New(512, out)
	for i, b := range bits {
		if i == resyncAt {
			d.Resync()
		}
		d.Feed(b)
	}
	close(out)
	var pages []Page
	for p := range out {
		pages = append(pages, p)
	}
	return pages, d.Stats()
}

func TestStatsCleanTransmission(t *testing.T) {
	bits := withSilence(EncodeTransmission(testAddr, 0, testMsg))
	pages, st := decodeWithStats(bits, -1)
	if len(pages) != 1 || pages[0].Alpha != testMsg {
		t.Fatalf("decode failed: %+v", pages)
	}
	// Every codeword after the preamble is either an address, a message or
	// an idle codeword, and all of them are clean.
	cws := (len(EncodeTransmission(testAddr, 0, testMsg)) - 576) / 32
	syncs := cws / 17
	wantCW := uint64(cws - syncs)
	if st.CWClean != wantCW || st.CWCorrected != 0 || st.CWUncorrectable != 0 {
		t.Errorf("codewords: clean=%d corrected=%d uncorrectable=%d, want clean=%d and no errors", st.CWClean, st.CWCorrected, st.CWUncorrectable, wantCW)
	}
	if st.SyncLocks != 1 || st.SyncLosses != 1 {
		t.Errorf("sync: locks=%d losses=%d, want 1 and 1", st.SyncLocks, st.SyncLosses)
	}
	if st.Pages != 1 || st.PagesTruncated != 0 {
		t.Errorf("pages=%d truncated=%d, want 1 and 0", st.Pages, st.PagesTruncated)
	}
	if st.LastPageAt.IsZero() {
		t.Errorf("LastPageAt not set")
	}
}

func TestStatsCountCorrectedAndUncorrectableCodewords(t *testing.T) {
	clean := EncodeTransmission(testAddr, 0, testMsg)
	msgCW := 2*int(testAddr&7) + 1 + 4 // fifth message codeword, well inside the first batch

	// Find a 3-bit pattern BCH cannot repair; about half of them false-
	// correct, so pick one deterministically rather than assume.
	cw := codewordAt(clean, msgCW)
	var bad3 []int
	for a := 1; a < 31 && bad3 == nil; a++ {
		for b := a + 1; b < 31 && bad3 == nil; b++ {
			for c := b + 1; c < 31; c++ {
				if _, _, ok := correctBCH(cw ^ 1<<a ^ 1<<b ^ 1<<c); !ok {
					bad3 = []int{a, b, c}
					break
				}
			}
		}
	}
	if bad3 == nil {
		t.Fatal("no uncorrectable 3-bit pattern found")
	}

	cases := []struct {
		name                     string
		flips                    []int
		corrected, uncorrectable uint64
		wantText                 bool
	}{
		{"one bit", []int{17}, 1, 0, true},
		{"two bits", []int{5, 23}, 1, 0, true},
		{"three bits", bad3, 0, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bits := withSilence(flipCodewordBits(clean, msgCW, tc.flips...))
			pages, st := decodeWithStats(bits, -1)
			if st.CWCorrected != tc.corrected || st.CWUncorrectable != tc.uncorrectable {
				t.Errorf("corrected=%d uncorrectable=%d, want %d and %d", st.CWCorrected, st.CWUncorrectable, tc.corrected, tc.uncorrectable)
			}
			if len(pages) != 1 {
				t.Fatalf("want 1 page, got %d: %+v", len(pages), pages)
			}
			if tc.wantText && pages[0].Alpha != testMsg {
				t.Errorf("message not repaired: %q", pages[0].Alpha)
			}
			if !tc.wantText {
				// The damaged codeword's raw bits are kept so the page stays
				// aligned: same length, damage confined and flagged.
				if len(pages[0].Alpha) != len(testMsg) {
					t.Errorf("alignment lost: got %d chars want %d", len(pages[0].Alpha), len(testMsg))
				}
				if pages[0].Quality.Uncorrectable != 1 || len(pages[0].Quality.DamagedRanges) != 1 {
					t.Errorf("damage not reported: %+v", pages[0].Quality)
				}
			}
		})
	}
}

func TestStatsCountTruncatedPages(t *testing.T) {
	bits := withSilence(EncodeTransmission(testAddr, 0, testMsg))
	// Gap in the second batch, as TestResyncAfterStreamGap does.
	cwIdx := 2*int(testAddr&7) + 1 + 20
	cut := streamBitOffset(cwIdx)
	cutBits := append(append([]bool{}, bits[:cut]...), bits[cut+5:]...)
	pages, st := decodeWithStats(cutBits, cut)
	if len(pages) != 1 {
		t.Fatalf("want 1 page, got %d", len(pages))
	}
	if st.PagesTruncated != 1 {
		t.Errorf("truncated=%d, want 1", st.PagesTruncated)
	}
	if st.SyncLosses < 1 {
		t.Errorf("a resync while locked must count as a sync loss, got %d", st.SyncLosses)
	}
	if st.Pages != 1 {
		t.Errorf("pages=%d, want 1", st.Pages)
	}
}

func TestStatsSubAndAdd(t *testing.T) {
	a := Stats{CWClean: 10, Pages: 2}
	b := Stats{CWClean: 15, Pages: 3}
	d := b.Sub(a)
	if d.CWClean != 5 || d.Pages != 1 {
		t.Errorf("Sub: %+v", d)
	}
	s := a.Add(b)
	if s.CWClean != 25 || s.Pages != 5 {
		t.Errorf("Add: %+v", s)
	}
}

// codewordAt reassembles codeword cwIdx from the bit stream.
func codewordAt(bits []bool, cwIdx int) uint32 {
	base := streamBitOffset(cwIdx)
	var w uint32
	for i := 0; i < 32; i++ {
		w <<= 1
		if bits[base+i] {
			w |= 1
		}
	}
	return w
}
