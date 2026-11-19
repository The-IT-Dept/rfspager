package db

import (
	"strings"
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
	differentPages := func(a string, aQ *rxproto.PageQuality, b string, bQ *rxproto.PageQuality, near time.Time) bool {
		return differentPages(a, aQ, b, bQ, near, time.Second)
	}
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
	if !differentPagesApart(raymondClean, clean, "FRINC TYPE: AFA TURNOUT: 101 INC: 100009-17112026", clean, near, time.Minute) {
		t.Fatal("two FRNSW incidents to the same station a minute apart are different pages")
	}
	if differentPages("FRINC TYPE: q^C TURNOUT: 101 IC: 100001-17112P26", nil, raymondClean, clean, near) {
		t.Fatal("a damaged incident number says nothing")
	}
	miscorrected := "14 October 2026 07:46:03 LHRaYE - 26-131549 - Power pole alight - FIRECALL - 158F MASONITE RD,TOMAGO,PORT STEPHENS (NSW),2322 - [151.763336,-32.801304]"
	heavy := &rxproto.PageQuality{Codewords: 54, Corrected: 40}
	if differentPages(miscorrected, heavy, rayteClean, clean, near) {
		t.Fatal("a heavily corrected copy's incident number is not held against a copy of the same transmission")
	}
	if differentPages("FRINC TYPE: AFA TURNOUT: 101 INC: 100007-17112026", &rxproto.PageQuality{Codewords: 18, Corrected: 1},
		"FRINC TYPE: AFA TURNOUT: 101 INC: 100001-17112026", clean, near) {
		t.Fatal("copies of one transmission: a lightly repaired wrong digit doesn't make another page")
	}
	heavyTurnout := &rxproto.PageQuality{Codewords: 18, Corrected: 9}
	if !differentPagesApart("FRINC TYPE: AFA TURNOUT: 101 INC: 100001-17112026", heavyTurnout,
		"FRINC TYPE: AFA TURNOUT: 101 INC: 100002-17112026", clean, near, time.Minute) {
		t.Fatal("two turnouts to one station a minute apart are different calls, however the first decoded")
	}
}

var differentPagesApart = differentPages

// The shapes RFS districts page sibling units in (prod, 13-19 Nov 2026),
// with synthetic places; and decodes of one page that must still count as
// repairs.
func TestSiblingPages(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"appliance suffix", "19 November 2026 15:40:30 CCEXAMP - 26-100070 - MVA - OLD EXAMPLE HWY", "19 November 2026 15:40:30 CCEXAMPP - 26-100070 - MVA - OLD EXAMPLE HWY", true},
		{"appliance number", "18 November 2026 09:54:42 LHEXAMP1 - 26-100071 - MVA - EXAMPLE RD", "18 November 2026 09:54:42 LHEXAMP7 - 26-100071 - MVA - EXAMPLE RD", true},
		{"stop message", "13 November 2026 13:14:38 26-100072 CCEXAMP Stop Message // 1 EXAMPLE RD", "13 November 2026 13:14:38 26-100072 CCEXAMP1 Stop Message // 1 EXAMPLE RD", true},
		{"time-only header", "08:54:52 SNEXAMP - 26-100073 - AFA - INCIDENT CALL", "08:54:52 SNEXAMPP - 26-100073 - AFA - INCIDENT CALL", true},
		{"time-only, two seconds apart", "17:26:35 WSEXAMP - 26-100074 - Grass - FIRECALL", "17:26:37 WSEXAMPP - 26-100074 - Grass - FIRECALL", true},
		{"capital month", "18 NOVEMBER 2026 15:40:10 SHEXAMP - 26-100075 - MVA", "18 NOVEMBER 2026 15:40:10 SHEXAMP1 - 26-100075 - MVA", true},
		{"no header", "NBEXAMP - 26-100076 - MVA - EXAMPLE DR", "NBEXAMP1 - 26-100076 - MVA - EXAMPLE DR", true},
		{"a repaired letter in the address", "LHOPS18 - 26-100077 - CFR CALL - 2639 EXAMPLE RD,kXAMPLE TOWN", "LHOPS18 - 26-100077 - CFR CALL - 2639 EXAMPLE RD,EXAMPLE TOWN", false},
		{"a dropped letter", "FRINC TYPE: ASSIST AMBULANE TURNOUT: 78 INC: 100078-15112026", "FRINC TYPE: ASSIST AMBULANCE TURNOUT: 78 INC: 100078-15112026", false},
		{"a dropped digit", "13 November 2026 04:03:45 HKDO - 26-10079 - AFA", "13 November 2026 04:03:45 HKDO - 26-100079 - AFA", false},
		{"sibling and a repair", "08:54:52 SNEXAMP - 26-100073 - AFA - INCIDENT CALL - p1 EXAMPLE ST", "08:54:52 SNEXAMPP - 26-100073 - AFA - INCIDENT CALL - 09 EXAMPLE ST", false},
		{"an alarm number late in the page", "18 November 2026 09:54:42 LHDO - 26-100071 - AFA - AFA0350154,EXAMPLE", "18 November 2026 09:54:42 LHDO - 26-100071 - AFA - AFA0350155,EXAMPLE", false},
		{"identical", "08:54:52 SNEXAMP - x", "08:54:52 SNEXAMP - x", false},
		{"sibling cut a character shorter", "19 November 2026 10:00:00 CCEXAMP - 26-100081 - MVA - [151.370742797852,-33.1589", "19 November 2026 10:00:00 CCEXAMP1 - 26-100081 - MVA - [151.370742797852,-33.158", true},
		{"only the end differs", "19 November 2026 10:00:00 CCEXAMP - 26-100081 - MVA - [151.3707,-33.1589", "19 November 2026 10:00:00 CCEXAMP - 26-100081 - MVA - [151.3707,-33.15", false},
	}
	for _, c := range cases {
		if got := siblingPages(c.a, c.b); got != c.want {
			t.Errorf("%s: siblingPages = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRepairs(t *testing.T) {
	clean := &rxproto.PageQuality{Codewords: 40}
	lightly := &rxproto.PageQuality{Codewords: 40, Corrected: 2}
	full := "18 November 2026 14:41:11 HKEXAMP1 - 26-100080 - Car fire - VEHICLE FIRE - EXAMPLE RD,EXAMPLEVILLE (NSW),2076 - [151.1,-33.7]"
	if !repairs(strings.Replace(full, "EXAMPLE RD", "EXAMPLE R`", 1), lightly, full, clean) {
		t.Error("a clean copy of a page pushed with a wrong character repairs it")
	}
	if repairs(full, clean, strings.Replace(full, "Car", "Cxr", 1), clean) {
		t.Error("a clean copy replacing a clean one repairs nothing")
	}
	if repairs(full, lightly, full[:len(full)-20], clean) {
		t.Error("a copy that lost the end of the page repairs nothing")
	}
	if !repairs(full[:60], &rxproto.PageQuality{Codewords: 20, Truncated: true}, full, clean) {
		t.Error("a complete copy repairs one that was cut short")
	}
	if repairs(full, lightly, strings.Replace(full, "HKEXAMP1", "HKEXAMP7", 1), clean) {
		t.Error("a sibling unit's page repairs nothing")
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
