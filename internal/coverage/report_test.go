package coverage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParamsNormalize(t *testing.T) {
	for _, tc := range []struct {
		in   Params
		want Params
		ok   bool
	}{
		{Params{}, Params{Days: 30, Channel: "RFS"}, true},
		{Params{Days: 120, Channel: "frnsw", SystemID: 2}, Params{Days: 120, Channel: "FRNSW", SystemID: 2}, true},
		{Params{Days: 7, Channel: "ALL"}, Params{Days: 7, Channel: "all"}, true},
		{Params{Days: 400}, Params{Days: 365, Channel: "RFS"}, false},
		{Params{Days: -1}, Params{Days: 1, Channel: "RFS"}, false},
		{Params{Days: 30, Channel: "VHF"}, Params{Days: 30, Channel: "RFS"}, false},
	} {
		got, ok := tc.in.Normalize()
		if got != tc.want || ok != tc.ok {
			t.Errorf("Normalize(%+v) = %+v, %v; want %+v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// fixture: two located receivers (one moved recently), one unlocated
// receiver, three RFS districts plus FRNSW and SES, and transmitters in
// every status.
func reportFixture() reportInputs {
	const (
		gosford = 1
		hornsby = 2
		frnsw   = 3
		ses     = 4
		beaches = 5
	)
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	moved := now.Add(-48 * time.Hour)
	at := func(lat, lng float64) (float64, float64) { return lat, lng }
	tx := func(txID int64, channel string, agency *int64, lat, lng float64) Transmitter {
		return Transmitter{ID: txID, Channel: channel, Name: "T", Licensee: "NSW RURAL FIRE SERVICE",
			Lat: lat, Lng: lng, EIRPW: f(331), HeightM: f(15), ElevationM: f(300), AgencyID: agency, DistrictSource: "auto"}
	}
	r1lat, r1lng := at(-33.23, 151.56)
	return reportInputs{
		params: Params{Days: 30, Channel: ChannelRFS},
		since:  now.Add(-30 * 24 * time.Hour),
		until:  now,
		agencies: []Agency{
			{ID: gosford, Name: "Gosford", CoverageDistrict: true},
			{ID: hornsby, Name: "Hornsby", CoverageDistrict: true},
			{ID: frnsw, Name: "FRNSW"},
			{ID: ses, Name: "SES"},
			{ID: beaches, Name: "Northern Beaches", CoverageDistrict: true},
		},
		capcodes: []Capcode{
			{ID: 10, Address: "1001001", AgencyID: id(gosford)},
			{ID: 11, Address: "1001002", AgencyID: id(gosford)},
			{ID: 12, Address: "1001003", AgencyID: id(gosford)},
			{ID: 13, Address: "1001004", AgencyID: id(gosford), Ignore: true},
			{ID: 20, Address: "2002001", AgencyID: id(hornsby)},
			{ID: 30, Address: "0107211", AgencyID: id(frnsw)},
			{ID: 40, Address: "4004001", AgencyID: id(ses)},
			{ID: 50, Address: "1370001", AgencyID: id(beaches)},
		},
		receivers: []ReceiverSite{
			{ID: 1, Name: "Budgewoi", Lat: r1lat, Lng: r1lng},
			{ID: 2, Name: "Newcastle", Lat: -32.93, Lng: 151.75, LocatedSince: &moved},
		},
		transmitters: []Transmitter{
			tx(101, "RFS", id(gosford), r1lat+0.15, r1lng), // ~17 km: probable
			tx(102, "RFS", id(gosford), r1lat+0.20, r1lng), // ~22 km: within 6 dB
			tx(103, "RFS", id(hornsby), -33.70, 151.10),    // Hornsby heard, not linked
			tx(104, "RFS", id(beaches), -33.65, 151.30),    // silent district
			tx(105, "RFS", nil, -31.0, 150.0),              // unmapped
			tx(106, "FRNSW", id(frnsw), -32.92, 151.78),    // other channel
			tx(107, "RFS", id(gosford), r1lat+1.5, r1lng),  // ~167 km: too weak to be possible
		},
		systemPages: []systemAddressPages{
			{SystemID: 1, Address: "1001001", Attributable: true, Pages: 3, LastHeard: now.Add(-time.Hour)},
			{SystemID: 1, Address: "1001002", CapcodeID: id(11), Attributable: true, Pages: 1, LastHeard: now.Add(-2 * time.Hour)},
			{SystemID: 1, Address: "2002001", Attributable: true, Pages: 1, LastHeard: now.Add(-3 * time.Hour)},
			// Newcastle heard Gosford, but only before it moved here.
			{SystemID: 2, Address: "1001001", Attributable: false, Pages: 5, LastHeard: now.Add(-72 * time.Hour)},
			// An unlocated receiver: totals only.
			{SystemID: 9, Address: "2002001", Attributable: true, Pages: 4, LastHeard: now},
			// FRNSW is off-channel for an RFS report.
			{SystemID: 1, Address: "0107211", Attributable: true, Pages: 7, LastHeard: now},
		},
		addressPages: []addressPages{
			{Address: "1001001", Pages: 6}, // 3 + 5 heard by two receivers, 2 in common minutes
			{Address: "1001002", Pages: 1},
			{Address: "2002001", Pages: 4},
			{Address: "0107211", Pages: 7},
			{Address: "4004001", Pages: 2},
			{Address: "9999123", Pages: 3},
			{Address: "9999456", Pages: 1},
			{Address: "1370555", Pages: 0},
		},
		heat:      [][3]float64{{151.56, -33.1, 2}},
		lastHeard: map[int64]time.Time{beaches: time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)},
	}
}

func TestBuildRFS(t *testing.T) {
	in := reportFixture()
	r := build(in, in.until)

	// Districts: RFS districts only, sorted by pages, silent ones included.
	var names []string
	for _, d := range r.Districts {
		names = append(names, d.Agency)
	}
	if strings.Join(names, ",") != "Gosford,Hornsby,Northern Beaches" {
		t.Fatalf("districts = %v", names)
	}
	g, h, nb := r.Districts[0], r.Districts[1], r.Districts[2]
	if g.Pages != 7 || g.CapcodesHeard != 2 || g.CapcodesKnown != 3 || g.Transmitters != 3 || g.Channel != "RFS" {
		t.Errorf("Gosford = %+v", g)
	}
	if len(g.Receivers) != 1 || g.Receivers[0] != (DistrictHeard{SystemID: 1, Pages: 4}) {
		t.Errorf("Gosford receivers = %+v (Newcastle's pre-move pages must not count)", g.Receivers)
	}
	if h.Pages != 4 || len(h.Receivers) != 1 || h.Receivers[0].Pages != 1 {
		t.Errorf("Hornsby = %+v", h)
	}
	if nb.Pages != 0 || nb.LastHeardAt == nil || !nb.LastHeardAt.Equal(time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("Northern Beaches = %+v", nb)
	}

	// Receivers.
	if len(r.Receivers) != 2 {
		t.Fatalf("receivers = %+v", r.Receivers)
	}
	bud, newc := r.Receivers[0], r.Receivers[1]
	if bud.Pages != 5 || bud.DistrictsHeard != 2 || bud.LastHeardAt == nil || !bud.LastHeardAt.Equal(in.until.Add(-time.Hour)) {
		t.Errorf("Budgewoi = %+v", bud)
	}
	if bud.ReachKm < 16 || bud.ReachKm > 17.5 {
		t.Errorf("Budgewoi reach = %v, want ~16.7 (its probable link)", bud.ReachKm)
	}
	if newc.Pages != 0 || newc.DistrictsHeard != 0 || newc.LastHeardAt == nil {
		t.Errorf("Newcastle = %+v (pre-move pages: last heard only)", newc)
	}

	// Links: Budgewoi heard 4 Gosford pages (>= 3) but only 1 Hornsby page.
	if len(r.Links) != 2 {
		t.Fatalf("links = %+v", r.Links)
	}
	if l := r.Links[0]; l.Kind != StatusProbable || l.TransmitterID != 101 || l.SystemID != 1 || l.Pages != 4 || l.AgencyID != 1 {
		t.Errorf("probable link = %+v", l)
	}
	if l := r.Links[1]; l.Kind != StatusPossible || l.TransmitterID != 102 {
		t.Errorf("possible link = %+v", l)
	}

	status := map[int64]string{}
	for _, tx := range r.Transmitters {
		status[tx.ID] = tx.Status
		if tx.ID == 101 && (len(tx.HeardBy) != 1 || tx.HeardBy[0] != 1 || tx.Pages != 4 || tx.Agency == nil || *tx.Agency != "Gosford") {
			t.Errorf("transmitter 101 = %+v", tx)
		}
	}
	want := map[int64]string{101: "probable", 102: "possible", 103: "district_heard", 104: "not_heard", 105: "unmapped", 107: "district_heard"}
	if len(status) != len(want) {
		t.Errorf("transmitters = %v, want %v (FRNSW excluded)", status, want)
	}
	for txID, s := range want {
		if status[txID] != s {
			t.Errorf("transmitter %d status = %q, want %q", txID, status[txID], s)
		}
	}

	// Unknown addresses with no prefix match; 1370555 maps by prefix.
	if len(r.UnmappedPrefixes) != 1 || r.UnmappedPrefixes[0] != (PrefixPages{Prefix: "9999", Pages: 4}) {
		t.Errorf("unmapped prefixes = %+v", r.UnmappedPrefixes)
	}
}

// Garbled decodes land on random addresses, some under a real district's
// prefix (Budgewoi "hearing" Monaro at 380 km). Pages to unknown capcodes
// count separately and never link a receiver to a district.
func TestBuildPrefixOnlyPagesDoNotLink(t *testing.T) {
	in := reportFixture()
	in.systemPages = append(in.systemPages,
		systemAddressPages{SystemID: 1, Address: "1370999", Attributable: true, Pages: 5, LastHeard: in.until})
	in.addressPages = append(in.addressPages, addressPages{Address: "1370999", Pages: 5})
	r := build(in, in.until)

	for _, l := range r.Links {
		if l.AgencyID == 5 {
			t.Errorf("prefix-only pages linked Northern Beaches: %+v", l)
		}
	}
	for _, tx := range r.Transmitters {
		if tx.ID == 104 && tx.Status != StatusNotHeard {
			t.Errorf("Northern Beaches transmitter status = %q, want not_heard", tx.Status)
		}
	}
	for _, d := range r.Districts {
		if d.Agency == "Northern Beaches" && (d.Pages != 0 || d.UnknownCapcodePages != 5 || len(d.Receivers) != 0) {
			t.Errorf("Northern Beaches = %+v, want 0 pages and 5 unknown-capcode pages", d)
		}
	}
	if r.Receivers[0].DistrictsHeard != 2 {
		t.Errorf("Budgewoi districts heard = %d, want 2 (prefix-only district excluded)", r.Receivers[0].DistrictsHeard)
	}
	// 6+1+4 known RFS, 2 SES, 4 unmapped, 5 prefix-only; FRNSW off-channel.
	if r.TotalPages != 22 {
		t.Errorf("total pages = %d, want 22", r.TotalPages)
	}
}

func TestBuildFRNSW(t *testing.T) {
	in := reportFixture()
	in.params.Channel = ChannelFRNSW
	r := build(in, in.until)
	if len(r.Districts) != 1 || r.Districts[0].Agency != "FRNSW" || r.Districts[0].Pages != 7 || r.Districts[0].Channel != "FRNSW" {
		t.Fatalf("districts = %+v", r.Districts)
	}
	if len(r.Transmitters) != 1 || r.Transmitters[0].ID != 106 || r.Transmitters[0].Status != StatusProbable {
		t.Errorf("transmitters = %+v", r.Transmitters)
	}
	if r.Receivers[0].Pages != 7 || r.Receivers[0].DistrictsHeard != 1 {
		t.Errorf("Budgewoi on FRNSW = %+v", r.Receivers[0])
	}
	if len(r.UnmappedPrefixes) != 0 {
		t.Errorf("unmapped on FRNSW = %+v", r.UnmappedPrefixes)
	}
}

func TestBuildAllChannels(t *testing.T) {
	in := reportFixture()
	in.params.Channel = ChannelAll
	r := build(in, in.until)
	if len(r.Districts) != 4 || len(r.Transmitters) != 7 {
		t.Errorf("all channels: %d districts, %d transmitters; want 4, 7", len(r.Districts), len(r.Transmitters))
	}
}

// The JSON field names and empty-array (not null) encoding are the UI
// contract.
func TestResponseJSONShape(t *testing.T) {
	in := reportInputs{params: Params{Days: 7, Channel: ChannelRFS}}
	b, err := json.Marshal(build(in, time.Unix(0, 0)))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"window", "receptions_since", "generated_at", "total_pages", "receivers", "transmitters", "links", "districts", "unmapped_prefixes", "heat"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q in %s", k, b)
		}
	}
	for _, k := range []string{"receivers", "transmitters", "links", "districts", "unmapped_prefixes", "heat"} {
		if _, ok := m[k].([]any); !ok {
			t.Errorf("%q = %v, want an array", k, m[k])
		}
	}

	full := build(reportFixture(), time.Unix(0, 0))
	b, _ = json.Marshal(full)
	for _, k := range []string{`"located_since"`, `"districts_heard"`, `"reach_km"`, `"last_heard_at"`, `"eirp_w"`, `"height_m"`, `"elevation_m"`,
		`"district_source"`, `"district_confidence"`, `"heard_by"`, `"transmitter_id"`, `"distance_km"`, `"score_db"`,
		`"capcodes_heard"`, `"capcodes_known"`, `"unknown_capcode_pages"`, `"prefix"`, `"agency_id"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("response JSON lacks %s", k)
		}
	}
}

func TestTTLCache(t *testing.T) {
	c := newTTLCache[Params, int](time.Minute)
	now := time.Unix(1000, 0)
	k := Params{Days: 30, Channel: "RFS"}
	c.put(k, 1, now)
	if v, ok := c.get(k, now.Add(59*time.Second)); !ok || v != 1 {
		t.Errorf("fresh entry = %v %v", v, ok)
	}
	if _, ok := c.get(k, now.Add(time.Minute)); ok {
		t.Error("expired entry served")
	}
	c.put(k, 2, now)
	c.clear()
	if _, ok := c.get(k, now); ok {
		t.Error("entry survived clear")
	}
}
