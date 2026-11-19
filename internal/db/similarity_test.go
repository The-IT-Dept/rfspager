package db

import (
	"testing"
	"time"

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

	raymondClean    = "FRINC TYPE: AFA TURNOUT: 101 INC: 100001-17112026"
	raymondFragment = "LZmNC TYPE: AFA TURNOUT: "
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
		// The plain score is one-way: a complete copy scores low against a
		// fragment stored first. fragmentOfSameTransmission covers that case.
		{"a clean copy against a damaged fragment stored first", raymondClean, raymondFragment, 0, 0.5},
	}
	for _, c := range cases {
		got := textSimilarity(c.sample, c.against)
		if got < c.min || got > c.max {
			t.Errorf("%s: similarity %.3f, want within [%.2f, %.2f]", c.name, got, c.min, c.max)
		}
	}
}

// An FRNSW turnout, 17 Nov 2026: one receiver's truncated, repaired copy was
// stored a second before four clean ones; the plain score put the clean copy
// at 0.43 against it and the page was stored and pushed twice.
func TestFragmentOfSameTransmission(t *testing.T) {
	at := time.Date(2026, 11, 16, 21, 24, 43, 0, time.UTC)
	stored := duplicateRow{Message: raymondFragment, CreatedAt: at}
	if !fragmentOfSameTransmission(stored, raymondClean, at.Add(time.Second)) {
		t.Fatal("a damaged fragment stored a second earlier is the same transmission")
	}
	if fragmentOfSameTransmission(stored, raymondClean, at.Add(time.Minute)) {
		t.Fatal("a fragment's opening matches any page of the same format; only within the simulcast window")
	}
	if !fragmentOfSameTransmission(duplicateRow{Message: raymondClean, CreatedAt: at}, raymondFragment, at.Add(time.Second)) {
		t.Fatal("the fragment may also arrive after the complete copy")
	}
	if fragmentOfSameTransmission(duplicateRow{Message: "FRINC TYPE", CreatedAt: at}, raymondClean, at) {
		t.Fatal("too short to say which page it came from")
	}
	if fragmentOfSameTransmission(duplicateRow{Message: rayteClean[:60], CreatedAt: at}, rayteOtherJob, at) {
		t.Fatal("a fragment of a different job to the same brigade")
	}
	if fragmentOfSameTransmission(duplicateRow{Message: raymondClean, CreatedAt: at}, raymondClean, at) {
		t.Fatal("equal texts are not fragments of each other")
	}
}

func TestDifferentPages(t *testing.T) {
	near := time.Date(2026, 10, 13, 20, 50, 0, 0, time.UTC)
	clean := &rxproto.PageQuality{Codewords: 50}
	if !differentPages(rayteClean, clean, rayteOtherJob, clean, near) {
		t.Fatal("another job to the same brigade a minute later is a different page")
	}
	if differentPages(rayteGarbled, &rxproto.PageQuality{Codewords: 54, Corrected: 40}, rayteClean, clean, near) {
		t.Fatal("a garbled re-send of the same page is the same page")
	}
	if differentPages(rayte1Garbled, &rxproto.PageQuality{Codewords: 54, Corrected: 34, Uncorrectable: 1}, rayte7Clean, clean, near) {
		t.Fatal("a re-send whose incident number was damaged is still the same page")
	}
	if !differentPages(raymondClean, clean, "FRINC TYPE: AFA TURNOUT: 101 INC: 100009-17112026", clean, near) {
		t.Fatal("two FRNSW incidents to the same station are different pages")
	}
	if differentPages("FRINC TYPE: q^C TURNOUT: 101 IC: 100001-17112P26", nil, raymondClean, clean, near) {
		t.Fatal("a damaged incident number says nothing")
	}
	miscorrected := "14 October 2026 07:46:03 LHRaYE - 26-131549 - Power pole alight - FIRECALL - 158F MASONITE RD,TOMAGO,PORT STEPHENS (NSW),2322 - [151.763336,-32.801304]"
	if differentPages(miscorrected, &rxproto.PageQuality{Codewords: 54, Corrected: 40}, rayteClean, clean, near) {
		t.Fatal("an incident number from a heavily corrected copy says nothing")
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
