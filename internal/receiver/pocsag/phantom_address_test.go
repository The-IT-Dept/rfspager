package pocsag

import "testing"

// softConf is a per-stream-bit confidence vector: every bit certain except
// the listed positions, which are doubtful.
func softConf(n int, doubtful ...int) []float64 {
	conf := make([]float64, n)
	for i := range conf {
		conf[i] = 1.0
	}
	for _, i := range doubtful {
		conf[i] = 0.1
	}
	return conf
}

// decodeSoft feeds bits with their confidences through a fresh 512-baud
// decoder, the way the receiver pipeline does.
func decodeSoft(bits []bool, conf []float64) []Page {
	out := make(chan Page, 256)
	d := New(512, out)
	for i, b := range bits {
		d.FeedSoft(b, conf[i])
	}
	close(out)
	var pages []Page
	for p := range out {
		pages = append(pages, p)
	}
	return pages
}

// threeErrorsBeyondBCH flips three bits of codeword n and returns the
// damaged stream and the flipped positions, choosing a pattern the
// algebraic decoder cannot repair so that only a soft-decision repair
// could.
func threeErrorsBeyondBCH(t *testing.T, bits []bool, n int) ([]bool, []int) {
	t.Helper()
	s := codewordStart(n)
	for _, pattern := range [][]int{{3, 9, 15}, {2, 11, 20}, {5, 13, 27}, {1, 17, 25}} {
		pos := []int{s + pattern[0], s + pattern[1], s + pattern[2]}
		bad := flip(bits, pos...)
		if _, _, ok := correctWithParity(codewordAt(bad, n)); !ok {
			return bad, pos
		}
	}
	t.Fatalf("no three-error pattern beyond BCH found for codeword %d", n)
	return nil, nil
}

// A soft-decision repair of the address codeword is a guess at who the page
// is for. On 2026-10-14 Budgewoi filed a Raymond Terrace page under the
// phantom capcode 1041008 that way. A chased address must not start a page.
func TestChasedAddressDoesNotStartPage(t *testing.T) {
	bits := withSilence(EncodeTransmission(testAddr, 0, testMsg))
	bad, pos := threeErrorsBeyondBCH(t, bits, 0)
	conf := softConf(len(bad), pos...)
	// Sanity: with those bits doubtful the chase decoder would repair the
	// word, so before the fix this page decoded.
	if _, _, ok := chase(codewordAt(bad, 0), streamConf(conf, 0)); !ok {
		t.Fatal("test setup: chase should be able to repair the address codeword")
	}
	if pages := decodeSoft(bad, conf); len(pages) != 0 {
		t.Fatalf("a chased address started a page: %+v", pages)
	}
}

// The same three doubtful errors in a message codeword are still repaired
// by the chase decoder: the restriction is on addresses only.
func TestChasedMessageCodewordStillRepaired(t *testing.T) {
	bits := withSilence(EncodeTransmission(testAddr, 0, testMsg))
	bad, pos := threeErrorsBeyondBCH(t, bits, 5)
	pages := decodeSoft(bad, softConf(len(bad), pos...))
	if len(pages) != 1 {
		t.Fatalf("want 1 page, got %d", len(pages))
	}
	if pages[0].Alpha != testMsg {
		t.Fatalf("chase did not repair the message codeword:\n got: %q\nwant: %q", pages[0].Alpha, testMsg)
	}
	if pages[0].Quality.Uncorrectable != 0 || pages[0].Quality.Corrected != 1 {
		t.Fatalf("quality = %+v, want one corrected codeword", pages[0].Quality)
	}
}

// streamConf extracts codeword n's 32 confidences from a stream vector in
// the decoder's bit order (index 31 is the first-received bit).
func streamConf(conf []float64, n int) *[32]float64 {
	var out [32]float64
	s := codewordStart(n)
	for i := 0; i < 32; i++ {
		out[31-i] = conf[s+i]
	}
	return &out
}
