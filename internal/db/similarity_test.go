package db

import (
	"testing"

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// The 26-131349 pages of 2026-10-14 07:46 exactly as the receivers logged
// them. Newcastle decoded the three unit pages cleanly. Budgewoi's copies of
// the weak third transmission were "repaired" into near-identical garbage,
// and one copy of the first transmission into a phantom capcode.
const (
	rayteClean  = "14 October 2026 07:46:03 LHRAYTE - 26-131349 - Power pole alight - FIRECALL - 158F MASONITE RD,TOMAGO,PORT STEPHENS (NSW),2322 - [151.763336,-32.801304]"
	rayte7Clean = "14 October 2026 07:46:04 LHRAYTE7 - 26-131349 - Power pole alight - FIRECALL - 158F MASONITE RD,TOMAGO,PORT STEPHENS (NSW),2322 - [151.763336,-32.801304]"
	rayte1Clean = "14 October 2026 07:46:04 LHRAYTE1 - 26-131349 - Power pole alight - FIRECALL - 158F MASONITE RD,TOMAGO,PORT STEPHENS (NSW),2322 - [151.763336,-32.801304]"
	// 40 of 54 codewords corrected, none unrepairable, and two characters wrong.
	rayteGarbled = "14 October 2026 07:46:03 LHRaYE - 26-131349 - Power pole alight - FIRECALL - 158F MASONITE RD,TOMAGO,PORT STEPHENS (NSW),2322 - [151.763336,-32.801304]"
	// 34 corrected, 1 unrepairable: the incident number itself is wrong.
	rayte1Garbled = "14 October 2026 07:46:04 LHRAYTE1 - 26-1149 - Poer polG `light - FIRECALL - 158F MASONITE RD,TOMAEg<PORT STEPHENS (NSW),2322 - [151.763336,-32.801304]"
	// 20 corrected, 10 unrepairable, truncated, and decoded to capcode 1041008.
	raytePhantom = "14 October r06 4966:04 ,PRAYTE1 % \n3'031349 , Zower pne)(`ligj4#, FIGCALL - 1581mASG"
	// A different dispatch to the same brigade a minute later.
	rayteOtherJob = "14 October 2026 07:47:30 LHRAYTE - 26-131350 - Grass fire - FIRECALL - 12 ADELAIDE ST,RAYMOND TERRACE,PORT STEPHENS (NSW),2324 - [151.744,-32.761]"
	rayteStop     = "14 October 2026 07:58:12 26-131349 LHRAYTE Stop Message // 158F MASONITE RD TOMAGO"
)

func TestTextSimilarityOnRealPages(t *testing.T) {
	cases := []struct {
		name            string
		sample, against string
		min, max        float64
	}{
		{"identical", rayteClean, rayteClean, 1, 1},
		{"two wrong characters in the unit code", rayteGarbled, rayteClean, 0.95, 1},
		{"wrong incident number and a few letters", rayte1Garbled, rayte7Clean, 0.9, 1},
		{"phantom-capcode garbage against the real page", raytePhantom, rayte7Clean, 0.6, 0.8},
		{"phantom-capcode garbage against another job", raytePhantom, rayteOtherJob, 0, 0.45},
		{"another job to the same brigade", rayteOtherJob, rayteClean, 0, 0.75},
		{"the stop message", rayteStop, rayteClean, 0, 0.5},
		{"a copy cut short", rayteClean[:70], rayteClean, 0.95, 1},
	}
	for _, c := range cases {
		got := textSimilarity(c.sample, c.against)
		if got < c.min || got > c.max {
			t.Errorf("%s: similarity %.3f, want within [%.2f, %.2f]", c.name, got, c.min, c.max)
		}
	}
}

func TestSuspectCopy(t *testing.T) {
	cases := []struct {
		q    *rxproto.PageQuality
		want bool
	}{
		{nil, false},
		{&rxproto.PageQuality{Codewords: 54, Corrected: 9}, false},
		{&rxproto.PageQuality{Codewords: 54, Corrected: 26}, false},
		{&rxproto.PageQuality{Codewords: 54, Corrected: 27}, true},
		{&rxproto.PageQuality{Codewords: 54, Corrected: 2, Uncorrectable: 1}, true},
		{&rxproto.PageQuality{Codewords: 20, Truncated: true}, true},
	}
	for _, c := range cases {
		if got := suspectCopy(c.q); got != c.want {
			t.Errorf("suspectCopy(%+v) = %v, want %v", c.q, got, c.want)
		}
	}
}

// Fewer corrections rank higher once nothing is unrepairable, but one
// unrepairable codeword outranks any number of successful corrections, and
// identical text never replaces itself.
func TestDamageScoreOrdering(t *testing.T) {
	clean := &rxproto.PageQuality{Codewords: 54, Corrected: 9}
	heavy := &rxproto.PageQuality{Codewords: 54, Corrected: 40}
	oneBad := &rxproto.PageQuality{Codewords: 54, Corrected: 2, Uncorrectable: 1}
	if !betterCopy(rayteClean, clean, rayteGarbled, heavy) {
		t.Fatal("a lightly corrected copy should replace a heavily corrected one")
	}
	if betterCopy(rayteGarbled, heavy, rayteClean, clean) {
		t.Fatal("a heavily corrected copy must not replace a lightly corrected one")
	}
	if betterCopy(rayte1Garbled, oneBad, rayteGarbled, heavy) {
		t.Fatal("an unrepairable codeword outranks forty corrections")
	}
	if betterCopy(rayteClean, &rxproto.PageQuality{Codewords: 54}, rayteClean, clean) {
		t.Fatal("identical text has nothing to replace")
	}
}
