package hostcomp

import (
	"strings"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// 10:02:30, so the window ends at the 10:00 bucket boundary.
var testNow = time.Date(2026, 10, 14, 10, 2, 30, 0, time.UTC)

func TestBucketCoversTheLongestStatsInterval(t *testing.T) {
	if Bucket < time.Duration(rxproto.MaxStatsIntervalS)*time.Second {
		t.Fatalf("bucket %v is shorter than the longest stats interval %ds", Bucket, rxproto.MaxStatsIntervalS)
	}
	if Bucket != 5*time.Minute {
		t.Fatalf("bucket = %v, want 5m", Bucket)
	}
}

func TestReceiverWindow(t *testing.T) {
	end := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		firstStats time.Time
		ok         bool
		start      time.Time
		buckets    int64
	}{
		{"brand new", testNow.Add(-time.Hour), false, time.Time{}, 0},
		{"23h of history", end.Add(-23 * time.Hour), false, time.Time{}, 0},
		{"exactly 24h of complete buckets", end.Add(-24 * time.Hour), true, end.Add(-24 * time.Hour), 288},
		{"first row mid-bucket counts its bucket", end.Add(-24*time.Hour + 4*time.Minute), true, end.Add(-24 * time.Hour), 288},
		{"one bucket short of 24h", end.Add(-24*time.Hour + 5*time.Minute), false, time.Time{}, 0},
		{"30h old: window is its history", end.Add(-30 * time.Hour), true, end.Add(-30 * time.Hour), 360},
		{"exactly 72h", end.Add(-72 * time.Hour), true, end.Add(-72 * time.Hour), 864},
		{"10 days old: capped at 72h", end.Add(-240 * time.Hour), true, end.Add(-72 * time.Hour), 864},
		{"first row in the future (clock skew)", testNow.Add(time.Hour), false, time.Time{}, 0},
	} {
		w, ok := receiverWindow(testNow, tc.firstStats)
		if ok != tc.ok {
			t.Errorf("%s: ok = %v, want %v", tc.name, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if w.end != end.Unix() || w.start != tc.start.Unix() || w.buckets() != tc.buckets {
			t.Errorf("%s: window %s..%s (%d buckets), want %s..%s (%d)", tc.name,
				time.Unix(w.start, 0).UTC(), time.Unix(w.end, 0).UTC(), w.buckets(), tc.start, end, tc.buckets)
		}
	}
}

func TestMeetsUptime(t *testing.T) {
	for _, tc := range []struct {
		connected, total int64
		want             bool
	}{
		{324, 360, true},  // exactly 90%
		{323, 360, false}, // just under
		{2592, 2880, true},
		{778, 864, true},  // 90.05%
		{777, 864, false}, // 89.93%
		{864, 864, true},
		{0, 288, false},
		{0, 0, false},
	} {
		if got := meetsUptime(tc.connected, tc.total); got != tc.want {
			t.Errorf("meetsUptime(%d, %d) = %v, want %v", tc.connected, tc.total, got, tc.want)
		}
	}
}

// measured builds a receiver as evaluate leaves it: window from its first
// stats row, then the connected bucket count and decode flag.
func measured(firstStatsAgo time.Duration, uptime float64, decoded bool) *receiver {
	r := &receiver{SystemID: 1, UserID: 1}
	w, ok := receiverWindow(testNow, testNow.Add(-firstStatsAgo))
	if ok {
		r.window = w
		r.connected = int64(uptime * float64(w.buckets()))
	}
	r.decoded = decoded
	return r
}

func TestReceiverQualifies(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    *receiver
		want bool
	}{
		{"72h+ at 100% with pages", measured(10*24*time.Hour, 1, true), true},
		{"exactly 90% over 30h", measured(30*time.Hour, 0.9, true), true},
		{"30h old at 95%", measured(30*time.Hour, 0.95, true), true},
		{"30h old at 89%", measured(30*time.Hour, 0.89, true), false},
		{"new receiver (<24h) at 100%", measured(20*time.Hour, 1, true), false},
		{"no pages decoded", measured(10*24*time.Hour, 1, false), false},
		{"no stats at all", &receiver{decoded: true}, false},
	} {
		if got := tc.r.qualifies(); got != tc.want {
			t.Errorf("%s: qualifies = %v, want %v (connected %d of %d)", tc.name, got, tc.want, tc.r.connected, tc.r.window.buckets())
		}
	}
	// 30h at 95% would fail if it were judged on a full 72h window.
	r := measured(30*time.Hour, 0.95, true)
	if r.window.buckets() != 360 || meetsUptime(r.connected, 864) {
		t.Fatalf("30h receiver window = %d buckets", r.window.buckets())
	}
}

func TestQualifyingReceivers(t *testing.T) {
	approved := testNow.Add(-30 * 24 * time.Hour)
	mk := func(systemID, userID int64, ok bool, conversation string, approvedAt time.Time) *receiver {
		r := measured(10*24*time.Hour, 0.5, true)
		if ok {
			r = measured(10*24*time.Hour, 1, true)
		}
		r.SystemID, r.UserID, r.Conversation, r.ApprovedAt = systemID, userID, conversation, approvedAt
		return r
	}
	got := qualifyingReceivers([]*receiver{
		// user 1: two receivers, only the second qualifies
		mk(10, 1, false, "conv-10", approved.Add(time.Hour)),
		mk(11, 1, true, "", approved),
		// user 2: both qualify; the one with a conversation wins over the
		// newer one without
		mk(20, 2, true, "", approved.Add(time.Hour)),
		mk(21, 2, true, "conv-21", approved),
		// user 3: both qualify, no conversations: most recently approved
		mk(30, 3, true, "", approved),
		mk(31, 3, true, "", approved.Add(time.Hour)),
		// user 4: nothing qualifies
		mk(40, 4, false, "", approved),
	})
	want := map[int64]int64{1: 11, 2: 21, 3: 31}
	if len(got) != len(want) {
		t.Fatalf("qualifying users = %v", got)
	}
	for user, system := range want {
		if got[user] == nil || got[user].SystemID != system {
			t.Errorf("user %d: receiver %+v, want system %d", user, got[user], system)
		}
	}
}

func TestAccessSQL(t *testing.T) {
	if got := Access("u"); got != "(COALESCE(u.subscription_valid, false) OR COALESCE(u.host_comp_until > NOW(), false))" {
		t.Fatalf("Access(u) = %s", got)
	}
	if got := Access(""); strings.Contains(got, ".") || !strings.Contains(got, "host_comp_until > NOW()") {
		t.Fatalf("Access() = %s", got)
	}
	if got := CompOnly("u"); got != "(NOT COALESCE(u.subscription_valid, false) AND COALESCE(u.host_comp_until > NOW(), false))" {
		t.Fatalf("CompOnly(u) = %s", got)
	}
}
