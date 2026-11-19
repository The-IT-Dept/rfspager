package pagetext

import (
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestDispatchTime(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		near    string
		want    string // RFC3339 UTC, "" = not parsed
		wantLen int
	}{
		{"RFS page in daylight time", "19 November 2026 08:48:12 HVDO - 26-100009 - Alarm Operating", "2026-11-18T21:49:00Z", "2026-11-18T21:48:12Z", 25},
		{"single-digit day", "5 October 2026 17:20:57 MCEXAMP - 26-100003", "2026-10-05T06:21:00Z", "2026-10-05T06:20:57Z", 23},
		{"zero-padded day", "07 October 2026 14:46:06 MCEXAMP - 26-100011", "2026-10-07T03:47:00Z", "2026-10-07T03:46:06Z", 24},
		{"standard time", "20 June 2026 09:00:00 SHDO - 26-100000", "2026-06-19T23:00:30Z", "2026-06-19T23:00:00Z", 21},
		{"SES page with two spaces", "19 November 2026 11:14:22  NO PERSONS TRAPPED", "2026-11-19T00:15:00Z", "2026-11-19T00:14:22Z", 25},
		{"header is the whole page", "18 November 2026 08:51:11", "2026-11-17T21:52:00Z", "2026-11-17T21:51:11Z", 25},
		{"ISO header", "2026-11-19 12:42:53 PAGING TEST MESSAGE ONLY", "2026-11-19T01:43:00Z", "2026-11-19T01:42:53Z", 19},
		{"repeated hour read as daylight time", "5 April 2026 02:30:00 LHDO - x", "2026-04-04T15:31:00Z", "2026-04-04T15:30:00Z", 21},
		{"repeated hour read as standard time", "5 April 2026 02:30:00 LHDO - x", "2026-04-04T16:31:00Z", "2026-04-04T16:30:00Z", 21},
		{"skipped hour does not exist", "4 October 2026 02:30:00 LHDO - x", "2026-10-03T16:31:00Z", "", 0},
		{"31 November", "31 November 2026 10:00:00 LHDO - x", "2026-11-30T23:00:00Z", "", 0},
		{"garbled year", "18 November 206f 22:14:13 LHEXAMP", "2026-11-19T02:02:19Z", "", 0},
		{"garbled month", "18 Novembar 2026 22:14:13 LHEXAMP", "2026-11-19T02:02:19Z", "", 0},
		{"seconds run into the text", "14 November 2026 09:30:260Pager Test", "2026-11-13T22:30:11Z", "", 0},
		{"hour out of range", "14 November 2026 29:30:26 LHDO", "2026-11-13T22:30:11Z", "", 0},
		{"FRNSW page has no header", "FRINC TYPE: AFA TURNOUT: 101 INC: 100001-17112026", "2026-11-16T21:24:44Z", "", 0},
		{"leading garbage", "N.N?LRu(@L-+l-HP v0y6P", "2026-11-19T02:03:37Z", "", 0},
	}
	for _, c := range cases {
		got, n, ok := DispatchTime(c.text, utc(c.near))
		if c.want == "" {
			if ok {
				t.Errorf("%s: parsed %v, want no header", c.name, got)
			}
			continue
		}
		if !ok || !got.Equal(utc(c.want)) || n != c.wantLen {
			t.Errorf("%s: got %v len %d ok %v, want %s len %d", c.name, got.UTC(), n, ok, c.want, c.wantLen)
		}
	}
}

func TestDamaged(t *testing.T) {
	cases := []struct {
		name string
		text string
		q    *rxproto.PageQuality
		want bool
	}{
		{"clean", "FRINC TYPE: AFA TURNOUT: 101 INC: 100001-17112026", &rxproto.PageQuality{Codewords: 18}, false},
		{"a few corrections", "x", &rxproto.PageQuality{Codewords: 54, Corrected: 9}, false},
		{"one unrepairable", "x", &rxproto.PageQuality{Codewords: 54, Corrected: 2, Uncorrectable: 1}, true},
		{"truncated", "LZmNC TYPE: AFA TURNOUT: ", &rxproto.PageQuality{Codewords: 9, Corrected: 4, Uncorrectable: 1, Truncated: true}, true},
		{"half corrected", "x", &rxproto.PageQuality{Codewords: 54, Corrected: 27}, true},
		{"no quality, clean text", "18 November 2026 08:51:11 LHEXAMP1 - 26-100007 - MVA", nil, false},
		{"no quality, garbled text", "FRINC TYPE: q^C TURNOUT: 102 IC: 100002-17112P26", nil, true},
	}
	for _, c := range cases {
		if got := Damaged(c.text, c.q); got != c.want {
			t.Errorf("%s: Damaged = %v, want %v", c.name, got, c.want)
		}
	}
}

// Modelled on every page in 45 days of prod data (5 Oct – 19 Nov 2026)
// whose header parsed as more than 15 minutes old outside a replay burst,
// plus pages from the 19 Nov replay: same header shapes, timings and damage,
// synthetic place names. The live ones are all damaged decodes whose header
// must not be believed; the replayed ones are clean and stale.
func TestStaleHeaderOnProdPages(t *testing.T) {
	cases := []struct {
		name  string
		id    int64
		text  string
		heard string
		q     *rxproto.PageQuality
		want  bool
	}{
		// Live pages with a damaged header.
		{"minute digit in an unrepairable range", 2490900,
			"31 October 2026 09:04:51 HVEXAM - 26-100001 - Grass - FIRECALL - EXAMPLE RD", "2026-10-30T22:59:09Z",
			&rxproto.PageQuality{Codewords: 52, Corrected: 24, Uncorrectable: 2, Damaged: [][2]int{{11, 14}, {17, 20}}}, false},
		{"hour digit in an unrepairable range, heavily corrected", 2485980,
			"19 October 2026 01:01:02 bVK[E HIlL0ILY TEST 1730<S", "2026-10-19T09:01:14Z",
			&rxproto.PageQuality{Codewords: 20, Corrected: 15, Uncorrectable: 4, Damaged: [][2]int{{14, 17}, {25, 28}, {28, 30}, {49, 51}}}, false},
		{"leading digit lost", 2488503,
			"6 October 2026 12:37:19 26-100002 MCEXAM7 Stop M!ssage // 12A EXAMPLE STREET", "2026-10-26T01:38:14Z",
			&rxproto.PageQuality{Codewords: 42, Corrected: 32, Uncorrectable: 2, Damaged: [][2]int{{0, 1}, {101, 104}}}, false},
		{"digits lost, damage elsewhere but heavily corrected", 2486613,
			"5 October 2026 16:07:29 THEXAMP9m - 26-03!891 - Smwig4ij!vicinity - niRECIL", "2026-10-21T05:09:08Z",
			&rxproto.PageQuality{Codewords: 31, Corrected: 25, Uncorrectable: 3, Damaged: [][2]int{{39, 41}, {67, 70}, {80, 83}}}, false},
		{"no quality record, garbled", 2482346,
			"5 October 2026 17:20:57 MCEXAMP - 26-100003 - Assist Ambulance - ASSIS'AZ@d", "2026-10-07T06:21:10Z", nil, false},
		{"header ten hours ahead", 2493613,
			"06 November 2026 19:00:05 SHEXAMP ):26-100004 -!OVA - EXAMPLEGEN,1234 EXAMPL", "2026-11-05T22:00:17Z",
			&rxproto.PageQuality{Codewords: 51, Corrected: 36, Uncorrectable: 9, Damaged: [][2]int{{14, 17}}}, false},
		{"paging clock a couple of minutes ahead", 2501671,
			"18 November 2026 08:58:14 FNDO - 26-100005 - Spillage - INCIDENT CALL - EXA", "2026-11-17T21:55:42Z",
			&rxproto.PageQuality{Codewords: 49}, false},
		{"busy network, nine minutes late", 2502153,
			"19 November 2026 03:12:46 FNDO - 26-100006 - x", "2026-11-18T16:21:16Z",
			&rxproto.PageQuality{Codewords: 50, Corrected: 3}, false},
		// The 19 Nov replay.
		{"replayed a day later", 2502464,
			"18 November 2026 08:51:11 LHEXAMP1 - 26-100007 - MVA - 1 EXAMPLE RD,EXAMPLEVILLE", "2026-11-19T02:00:01Z",
			&rxproto.PageQuality{Codewords: 50, Corrected: 2}, true},
		{"replayed fifteen hours later", 2502485,
			"18 November 2026 22:14:14 LHDO - 26-100008 - Car fire - VEHICLE FIRE - EXAMPLE DR", "2026-11-19T02:02:25Z",
			&rxproto.PageQuality{Codewords: 48}, true},
		{"replayed four hours later", 2502500,
			"19 November 2026 08:50:34 HVEXAMP1 - 26-100009 - Alarm Operating - INCIDENT CALL", "2026-11-19T02:03:57Z",
			&rxproto.PageQuality{Codewords: 46, Corrected: 1}, true},
		{"replayed SES page", 2502501,
			"19 November 2026 11:03:45  NHZEXA RCR at  EXAMPLE ROAD, EXAMPLEVILLE NSW 2000.", "2026-11-19T02:04:06Z", nil, true},
		{"replay with damage after the header", 0,
			"18 November 2026 19:56:02 LHEXAMP7 - 26-100010 - Structure/b`ilding fire", "2026-11-19T02:02:03Z",
			&rxproto.PageQuality{Codewords: 50, Corrected: 4, Uncorrectable: 1, Damaged: [][2]int{{60, 63}}}, true},
		{"replay with a damaged header", 2502483,
			"18 November 206f 22:14:13 LHEXAMP - 2>m1000X80- Car firE", "2026-11-19T02:02:19Z",
			&rxproto.PageQuality{Codewords: 50, Corrected: 20, Uncorrectable: 4, Damaged: [][2]int{{14, 17}}}, false},
	}
	for _, c := range cases {
		lag, _, got := StaleHeader(c.text, c.q, utc(c.heard))
		if got != c.want {
			t.Errorf("%s (%d): StaleHeader = %v (lag %v), want %v", c.name, c.id, got, lag, c.want)
		}
	}
}

func TestStaleHeaderThresholds(t *testing.T) {
	header := "19 November 2026 08:00:00 LHDO - 26-100020 - Grass - FIRECALL"
	dispatched := utc("2026-11-18T21:00:00Z")
	clean := &rxproto.PageQuality{Codewords: 40}
	cases := []struct {
		lag  time.Duration
		want bool
	}{
		{-2 * time.Minute, false}, // paging clock ahead of ours
		{0, false},
		{10 * time.Minute, false},
		{30 * time.Minute, false},
		{31 * time.Minute, true},
		{28 * time.Hour, true},
		{6*24*time.Hour + 23*time.Hour, true},
		{7 * 24 * time.Hour, false}, // a header that old is not believed
	}
	for _, c := range cases {
		if _, _, got := StaleHeader(header, clean, dispatched.Add(c.lag)); got != c.want {
			t.Errorf("lag %v: stale = %v, want %v", c.lag, got, c.want)
		}
	}
}

// page builds a clean RFS page dispatched at the given Sydney wall time.
func page(sydneyWall string) string {
	t, err := time.ParseInLocation("2 January 2006 15:04:05", sydneyWall, sydney)
	if err != nil {
		panic(err)
	}
	return t.Format("2 January 2006 15:04:05") + " LHDO - 26-100030 - Grass - FIRECALL - EXAMPLE RD"
}

func TestReplayWatchNeedsABurstOfDifferentAges(t *testing.T) {
	clean := &rxproto.PageQuality{Codewords: 40}
	now := utc("2026-11-19T02:00:00Z") // 13:00 AEDT

	// The 19 Nov replay: a day of backlog in a few minutes.
	w := NewReplayWatch()
	backlog := []string{"18 November 2026 08:51:11", "18 November 2026 09:54:42", "18 November 2026 17:22:44", "19 November 2026 00:17:00"}
	for i, b := range backlog[:2] {
		at := now.Add(time.Duration(i) * 10 * time.Second)
		w.Observe(page(b), clean, at, at)
		w.Observe(page(b), clean, at, at) // a second receiver's copy is the same page
	}
	if w.Burst(now.Add(20 * time.Second)) {
		t.Fatal("two stale pages are not yet a burst")
	}
	w.Observe(page(backlog[2]), clean, now.Add(25*time.Second), now.Add(25*time.Second))
	if !w.Burst(now.Add(26 * time.Second)) {
		t.Fatal("three stale pages hours apart in age are a replay")
	}
	if w.Burst(now.Add(26*time.Second + burstWindow)) {
		t.Fatal("the burst is forgotten once its pages are older than the window")
	}

	// A paging clock 40 minutes slow: every live page looks 40 minutes old.
	w = NewReplayWatch()
	for i := 0; i < 6; i++ {
		heard := now.Add(time.Duration(i) * time.Minute)
		dispatched := heard.Add(-40*time.Minute - time.Duration(i*7)*time.Second)
		w.Observe(page(dispatched.In(sydney).Format("2 January 2006 15:04:05")), clean, heard, heard)
	}
	if w.Burst(now.Add(6 * time.Minute)) {
		t.Fatal("a constant offset is a clock fault, not a replay")
	}

	// Stale-looking pages that are not trusted are not counted.
	w = NewReplayWatch()
	damaged := &rxproto.PageQuality{Codewords: 40, Uncorrectable: 1, Damaged: [][2]int{{12, 15}}}
	for _, b := range backlog {
		w.Observe(page(b), damaged, now, now)
	}
	if w.Burst(now) {
		t.Fatal("copies with a damaged header must not make a burst")
	}
}

func TestIncidentID(t *testing.T) {
	cases := []struct {
		text string
		q    *rxproto.PageQuality
		want string
	}{
		{"18 November 2026 08:51:11 LHEXAMP1 - 26-100007 - MVA", nil, "26-100007"},
		{"18 November 2026 08:51:11 26-100007 LHDO Stop Message // EXAMPLE ST", nil, "26-100007"},
		{"FRINC TYPE: AFA TURNOUT: 101 INC: 100001-17112026", &rxproto.PageQuality{Codewords: 18}, "INC 100001-17112026"},
		{"FRINC TYPE: q^C TURNOUT: 102 IC: 100002-17112P26", nil, ""},
		{"14 October 2026 07:46:04 LHRAYTE1 - 26-1149 - Poer polG", nil, ""},
		{"18 November 2026 08:51:11 LHEXAMP1 - 26-100007 - MVA", &rxproto.PageQuality{Codewords: 30, Uncorrectable: 1, Damaged: [][2]int{{38, 41}}}, ""},
		{"18 November 2026 08:51:11 LHEXAMP1 - 26-100007 - MVA", &rxproto.PageQuality{Codewords: 30, Uncorrectable: 1, Damaged: [][2]int{{50, 52}}}, "26-100007"},
		{"COFFS HARBOUR DAILY TEST 12:30", nil, ""},
		{"FRINC TYPE: AFA TURNOUT: 101 INC: 1234-05012027", nil, "INC 1234-05012027"},
		{"14 October 2026 07:46:03 LHRaYE - 26-131549 - Power pole alight", &rxproto.PageQuality{Codewords: 54, Corrected: 40}, ""},
	}
	for _, c := range cases {
		got, ok := IncidentID(c.text, c.q)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("IncidentID(%q) = %q %v, want %q", c.text, got, ok, c.want)
		}
	}
}

func TestHeaderTrusted(t *testing.T) {
	text := "18 November 2026 08:51:11 LHEXAMP1 - 26-100007"
	cases := []struct {
		name string
		q    *rxproto.PageQuality
		want bool
	}{
		{"clean", &rxproto.PageQuality{Codewords: 30}, true},
		{"truncated after the header", &rxproto.PageQuality{Codewords: 12, Truncated: true}, true},
		{"unrepairable after the header", &rxproto.PageQuality{Codewords: 30, Uncorrectable: 1, Damaged: [][2]int{{30, 33}}}, true},
		{"unrepairable range touching the header", &rxproto.PageQuality{Codewords: 30, Uncorrectable: 1, Damaged: [][2]int{{24, 27}}}, false},
		{"unrepairable but no ranges", &rxproto.PageQuality{Codewords: 30, Uncorrectable: 1}, false},
		{"heavily corrected", &rxproto.PageQuality{Codewords: 30, Corrected: 15}, false},
		{"no quality, clean", nil, true},
	}
	for _, c := range cases {
		if got := HeaderTrusted(text, c.q, 25); got != c.want {
			t.Errorf("%s: HeaderTrusted = %v, want %v", c.name, got, c.want)
		}
	}
	if HeaderTrusted("18 November 2026 08:51:11 LHEXAMP1 - 26-1000@7", nil, 25) {
		t.Error("no quality and a garble character anywhere: not trusted")
	}
}
