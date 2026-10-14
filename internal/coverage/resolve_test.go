package coverage

import "testing"

func TestPrefixMap(t *testing.T) {
	nb, hornsby, other := int64(1), int64(2), int64(3)
	var caps []Capcode
	add := func(addr string, agency *int64, ignore bool) {
		caps = append(caps, Capcode{ID: int64(len(caps) + 1), Address: addr, AgencyID: agency, Ignore: ignore})
	}
	// 1370: all Northern Beaches.
	add("1370001", &nb, false)
	add("1370002", &nb, false)
	add("1370003", &nb, false)
	// 1371: 4 of 5 Hornsby = 80%, enough.
	for i := range 4 {
		add("137110"+string(rune('0'+i)), &hornsby, false)
	}
	add("1371200", &other, false)
	// 1372: 3 of 5 = 60%, not enough.
	add("1372001", &hornsby, false)
	add("1372002", &hornsby, false)
	add("1372003", &hornsby, false)
	add("1372004", &other, false)
	add("1372005", &other, false)
	// 1373: one real capcode plus ignored noise that must not vote.
	add("1373001", &nb, false)
	add("1373002", &other, true)
	add("1373003", &other, true)
	// 1374: no agency; 13__ wildcard prefix ignored.
	add("1374001", nil, false)
	add("13__999", &other, false)

	m := buildPrefixMap(caps)
	want := map[string]int64{"1370": nb, "1371": hornsby, "1373": nb}
	if len(m) != len(want) {
		t.Errorf("prefix map = %v, want %v", m, want)
	}
	for p, a := range want {
		if m[p] != a {
			t.Errorf("prefix %s -> %d, want %d", p, m[p], a)
		}
	}
}

func TestResolver(t *testing.T) {
	nb, hunter, frnsw := int64(1), int64(2), int64(3)
	caps := []Capcode{
		{ID: 10, Address: "1370001", AgencyID: &nb},
		{ID: 11, Address: "1370002", AgencyID: &nb},
		{ID: 20, Address: "09500__", AgencyID: &hunter},
		{ID: 21, Address: "095004_", AgencyID: &hunter},
		{ID: 30, Address: "0107211", AgencyID: &frnsw},
		{ID: 40, Address: "0521839", AgencyID: nil},
	}
	r := NewResolver(caps)
	for _, tc := range []struct {
		address string
		stored  int64
		want    Resolution
	}{
		{"1370001", 0, Resolution{CapcodeID: 10, AgencyID: nb}},
		// Most specific wildcard wins, as in FindCapcodeByAddress.
		{"0950048", 0, Resolution{CapcodeID: 21, AgencyID: hunter}},
		{"0950071", 0, Resolution{CapcodeID: 20, AgencyID: hunter}},
		// Unknown address under a mapped prefix.
		{"1370999", 0, Resolution{AgencyID: nb, ByPrefix: true}},
		// The stored capcode is a fallback for an address that no longer
		// matches (capcode address edited) ...
		{"7777777", 30, Resolution{CapcodeID: 30, AgencyID: frnsw}},
		// ... but the current match wins when there is one.
		{"1370002", 30, Resolution{CapcodeID: 11, AgencyID: nb}},
		// A capcode with no agency resolves to it, agency 0.
		{"0521839", 0, Resolution{CapcodeID: 40}},
		{"5555555", 0, Resolution{}},
		{"12", 0, Resolution{}},
	} {
		if got := r.Resolve(tc.address, tc.stored); got != tc.want {
			t.Errorf("Resolve(%q, %d) = %+v, want %+v", tc.address, tc.stored, got, tc.want)
		}
	}
}

func TestLikeMatch(t *testing.T) {
	for _, tc := range []struct {
		s, p string
		want bool
	}{
		{"1370001", "1370001", true},
		{"1370001", "1370__1", true},
		{"1370001", "1370__2", false},
		{"1370001", "137%", true},
		{"1370001", "%001", true},
		{"137000", "1370__1", false},
		{"", "%", true},
		{"", "_", false},
	} {
		if got := likeMatch(tc.s, tc.p); got != tc.want {
			t.Errorf("likeMatch(%q, %q) = %v", tc.s, tc.p, got)
		}
	}
}
