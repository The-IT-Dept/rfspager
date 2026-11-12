package importer

import (
	"testing"
	"time"
)

// The feed's pubDate is UTC: the same incident's description reads
// "UPDATED: 12 Nov 2026 12:03" (AEDT, UTC+11) for pubDate "12/11/2026 1:03:00 AM".
func TestParsePubDateIsUTC(t *testing.T) {
	cases := map[string]time.Time{
		"12/11/2026 1:03:00 AM":  time.Date(2026, 11, 12, 1, 3, 0, 0, time.UTC),
		"12/11/2026 2:29:00 PM":  time.Date(2026, 11, 12, 14, 29, 0, 0, time.UTC),
		"02/07/2026 11:15:30 AM": time.Date(2026, 7, 2, 11, 15, 30, 0, time.UTC),
	}
	for in, want := range cases {
		got := parsePubDate(in)
		if got == nil || !got.Equal(want) {
			t.Errorf("parsePubDate(%q) = %v, want %v", in, got, want)
		}
	}
	syd, _ := time.LoadLocation("Australia/Sydney")
	if got := parsePubDate("12/11/2026 1:03:00 AM").In(syd).Format("2 Jan 2006 15:04"); got != "12 Nov 2026 12:03" {
		t.Errorf("in Sydney = %s, want the description's 12 Nov 2026 12:03", got)
	}
	if parsePubDate("") != nil || parsePubDate("not a date") != nil {
		t.Error("bad input should give nil")
	}
}
