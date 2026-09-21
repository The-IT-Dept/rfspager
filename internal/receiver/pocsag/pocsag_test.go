package pocsag

import (
	"strings"
	"testing"
)

const testAddr = 11416
const testMsg = "21 September 2026 19:24:16 LHPATER7 - 26-128802 - BBQ fire/Bonfire/Yard fire - FIRECALL - 12 SMITH ST,PATERSON,DUNGOG SHIRE (NSW),2421 - [151.60,-32.59]"

// withSilence appends enough zero bits after a transmission for the decoder
// to notice the sync word did not come back and finalise the last page.
func withSilence(bits []bool) []bool {
	for i := 0; i < 128; i++ {
		bits = append(bits, false)
	}
	return bits
}

// decode feeds bits through a fresh 512-baud decoder. If resyncAt >= 0 the
// decoder is told about a stream discontinuity just before that bit index —
// exactly what the receiver pipeline does when the SDR layer reports a
// dropped IQ buffer.
func decode(bits []bool, resyncAt int) []Page {
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
	return pages
}

func TestEncoderRoundTrip(t *testing.T) {
	for _, cw := range append([]uint32{AddressCodeword(testAddr, 0)}, AlphaCodewords(testMsg)...) {
		if _, errs, ok := correctBCH(cw); !ok || errs != 0 {
			t.Fatalf("encoder produced codeword %08x that does not BCH-verify (ok=%v errs=%d)", cw, ok, errs)
		}
	}
	pages := decode(withSilence(EncodeTransmission(testAddr, 0, testMsg)), -1)
	if len(pages) != 1 {
		t.Fatalf("want 1 page, got %d: %+v", len(pages), pages)
	}
	if pages[0].Address != "0011416" || pages[0].Alpha != testMsg || pages[0].BitRate != 512 {
		t.Fatalf("round trip mismatch: %+v", pages[0])
	}
}

// TestResyncAfterStreamGap models the SDR front end dropping IQ buffers
// mid-burst: a run of bits vanishes from the stream, shifting every later
// codeword boundary. The pipeline knows this happened and calls Resync()
// before feeding post-gap bits. The decoder must hand back the clean prefix
// of the real message and nothing else — no garbage tail, no phantom pages
// on other capcodes — regardless of whether the gap lands in the first
// batch (before the second sync confirmed the lock) or a later one.
func TestResyncAfterStreamGap(t *testing.T) {
	// 4 bits ≈ one 8.5 ms buffer at 512 baud; 35 ≈ eight buffers (~68 ms).
	for _, tc := range []struct{ gap, cwInto int }{{4, 6}, {5, 6}, {35, 6}, {4, 20}, {5, 20}, {35, 20}} {
		bits := withSilence(EncodeTransmission(testAddr, 0, testMsg))
		frame := int(testAddr & 7)
		cwIdx := 2*frame + 1 + tc.cwInto           // index within the codeword sequence
		cut := 576 + 32 + 32*cwIdx + 32*(cwIdx/16) // a sync word precedes every 16 codewords
		cutBits := append(append([]bool{}, bits[:cut]...), bits[cut+tc.gap:]...)
		pages := decode(cutBits, cut)

		t.Logf("gap=%d bits at message cw %d → %d page(s)", tc.gap, tc.cwInto, len(pages))
		for _, p := range pages {
			t.Logf("  addr=%s fn=%d alpha=%q", p.Address, p.Function, p.Alpha)
		}
		if len(pages) != 1 {
			t.Errorf("gap=%d@cw%d: want exactly 1 page (the real one, truncated), got %d", tc.gap, tc.cwInto, len(pages))
			continue
		}
		p := pages[0]
		if p.Address != "0011416" {
			t.Errorf("gap=%d@cw%d: page on wrong address %s", tc.gap, tc.cwInto, p.Address)
		}
		if !strings.HasPrefix(testMsg, p.Alpha) {
			t.Errorf("gap=%d@cw%d: alpha is not a clean prefix of the sent message:\n  got  %q", tc.gap, tc.cwInto, p.Alpha)
		}
		// Every codeword before the gap was intact, so we expect at least
		// the characters carried by those codewords (20 bits ≈ 2.85 chars each).
		if minChars := (tc.cwInto - 1) * 20 / 7; len(p.Alpha) < minChars {
			t.Errorf("gap=%d@cw%d: prefix too short: got %d chars, want ≥ %d", tc.gap, tc.cwInto, len(p.Alpha), minChars)
		}
	}
}

// TestResyncWithNothingPending must be a harmless no-op.
func TestResyncWithNothingPending(t *testing.T) {
	out := make(chan Page, 8)
	d := New(1200, out)
	d.Resync()
	for _, b := range withSilence(EncodeTransmission(1234567, 3, "HELLO WORLD")) {
		d.Feed(b)
	}
	d.Resync()
	close(out)
	var got []Page
	for p := range out {
		got = append(got, p)
	}
	if len(got) != 1 || got[0].Alpha != "HELLO WORLD" || got[0].Address != "1234567" || got[0].Function != 3 {
		t.Fatalf("unexpected pages: %+v", got)
	}
}
