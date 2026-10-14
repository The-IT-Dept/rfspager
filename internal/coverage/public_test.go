package coverage

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestPublicDays(t *testing.T) {
	for in, want := range map[int]int{30: 30, 90: 90, 0: 90, 7: 90, 365: 90, -30: 90} {
		if got := PublicDays(in); got != want {
			t.Errorf("PublicDays(%d) = %d, want %d", in, got, want)
		}
	}
}

func publicFixture() *Response {
	now := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	heard := now.Add(-time.Hour)
	lastNB := time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)
	agency := func(s string) *string { return &s }
	id := func(n int64) *int64 { return &n }
	return &Response{
		Window:      Window{Days: 90, Since: now.Add(-90 * 24 * time.Hour), Until: now},
		GeneratedAt: now,
		TotalPages:  14815,
		Receivers: []Receiver{
			{ID: 1, Name: "Budgewoi", Lat: -33.234567, Lng: 151.556789, Pages: 900, DistrictsHeard: 4, ReachKm: 61.2, LastHeardAt: &heard},
			{ID: 6, Name: "Hornsby", Lat: -33.7049, Lng: 151.0985, Pages: 0},
		},
		Transmitters: []TransmitterOut{
			{ID: 101, Channel: ChannelRFS, Name: "Kincumber", Licensee: "NSW RURAL FIRE SERVICE", Lat: -33.4612, Lng: 151.3901, EIRPW: f(331), AgencyID: id(1), Agency: agency("Gosford"), Status: StatusProbable, HeardBy: []int64{1}, Pages: 40},
			{ID: 102, Channel: ChannelRFS, Name: "Erina", Lat: -33.43, Lng: 151.42, AgencyID: id(1), Agency: agency("Gosford"), Status: StatusPossible, HeardBy: []int64{1}},
			{ID: 103, Channel: ChannelRFS, Name: "Berowra", Lat: -33.62, Lng: 151.16, AgencyID: id(2), Agency: agency("Hornsby"), Status: StatusDistrictHeard, HeardBy: []int64{}},
			{ID: 104, Channel: ChannelRFS, Name: "Avalon", Lat: -33.63, Lng: 151.32, AgencyID: id(5), Agency: agency("Northern Beaches"), Status: StatusNotHeard, HeardBy: []int64{}},
			{ID: 105, Channel: ChannelRFS, Name: "Bourke", Lat: -30.09, Lng: 145.94, Status: StatusUnmapped, HeardBy: []int64{}},
			{ID: 106, Channel: ChannelFRNSW, Name: "Sugarloaf", Lat: -32.89, Lng: 151.54, AgencyID: id(3), Agency: agency("FRNSW"), Status: StatusProbable, HeardBy: []int64{1}},
		},
		Links: []Link{{SystemID: 1, TransmitterID: 101, Kind: StatusProbable, AgencyID: 1, Pages: 40, DistanceKm: 25.1, ScoreDB: 12.3}},
		Districts: []District{
			{AgencyID: 1, Agency: "Gosford", Channel: ChannelRFS, Pages: 2495, Receivers: []DistrictHeard{{SystemID: 1, Pages: 40}}, CapcodesHeard: 12, CapcodesKnown: 30, Transmitters: 2, LastHeardAt: &heard},
			{AgencyID: 2, Agency: "Hornsby", Channel: ChannelRFS, Pages: 2, Receivers: []DistrictHeard{}, LastHeardAt: &heard},
			{AgencyID: 5, Agency: "Northern Beaches", Channel: ChannelRFS, Pages: 0, UnknownCapcodePages: 3, Receivers: []DistrictHeard{}, LastHeardAt: &lastNB},
			{AgencyID: 7, Agency: "Far West", Channel: ChannelRFS, Receivers: []DistrictHeard{}},
			{AgencyID: 3, Agency: "FRNSW", Channel: ChannelFRNSW, Pages: 50, Receivers: []DistrictHeard{}},
		},
		UnmappedPrefixes: []PrefixPages{{Prefix: "1999", Pages: 4}},
		Heat:             [][3]float64{{151.77, -32.74, 3}, {151.4, -33.45, 1}},
	}
}

func TestPublicizeReducesTheAdminReport(t *testing.T) {
	in := publicFixture()
	before, _ := json.Marshal(in)
	got := Publicize(in)

	if after, _ := json.Marshal(in); string(after) != string(before) {
		t.Fatal("Publicize modified the (cached) admin report")
	}

	if got.Window != in.Window || !got.GeneratedAt.Equal(in.GeneratedAt) || got.TotalPages != 14815 {
		t.Errorf("header = %+v / %v / %d", got.Window, got.GeneratedAt, got.TotalPages)
	}

	wantRx := []PublicReceiver{
		{Name: "Budgewoi", Lat: -33.23, Lng: 151.56, LastHeardAt: in.Receivers[0].LastHeardAt},
		{Name: "Hornsby", Lat: -33.70, Lng: 151.10},
	}
	if !reflect.DeepEqual(got.Receivers, wantRx) {
		t.Errorf("receivers = %+v, want %+v", got.Receivers, wantRx)
	}
	if got.Receivers[0].LastHeardAt == in.Receivers[0].LastHeardAt {
		t.Error("receiver last_heard_at aliases the cached report")
	}

	type tx struct {
		lat, lng float64
		heard    bool
		district string
	}
	var txs []tx
	for _, t := range got.Transmitters {
		d := "<nil>"
		if t.District != nil {
			d = *t.District
		}
		txs = append(txs, tx{t.Lat, t.Lng, t.Heard, d})
	}
	wantTx := []tx{
		{-33.4612, 151.3901, true, "Gosford"},       // probable
		{-33.43, 151.42, true, "Gosford"},           // possible
		{-33.62, 151.16, true, "Hornsby"},           // district heard
		{-33.63, 151.32, false, "Northern Beaches"}, // not heard
		{-30.09, 145.94, false, "<nil>"},            // unmapped
		// FRNSW dropped
	}
	if !reflect.DeepEqual(txs, wantTx) {
		t.Errorf("transmitters = %+v, want %+v", txs, wantTx)
	}

	var districts []string
	for _, d := range got.Districts {
		districts = append(districts, d.Name)
	}
	if strings.Join(districts, ",") != "Gosford,Hornsby,Northern Beaches,Far West" {
		t.Errorf("districts = %v (report order, FRNSW dropped)", districts)
	}
	if d := got.Districts[0]; !d.Heard || d.Pages != 2495 || d.LastHeardAt == nil {
		t.Errorf("Gosford = %+v", d)
	}
	if d := got.Districts[2]; d.Heard || d.Pages != 0 || d.LastHeardAt == nil || !d.LastHeardAt.Equal(time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("Northern Beaches = %+v (unknown-capcode pages do not count as heard)", d)
	}
	if d := got.Districts[3]; d.Heard || d.LastHeardAt != nil {
		t.Errorf("Far West = %+v", d)
	}

	if !reflect.DeepEqual(got.Heat, in.Heat) {
		t.Errorf("heat = %v", got.Heat)
	}
	got.Heat[0][2] = 99
	if in.Heat[0][2] != 3 {
		t.Error("heat aliases the cached report")
	}
}

// The wire format is the contract with the web UI: exactly these keys, and
// nothing private (ids, owners, licensees, EIRP, links, model scores).
func TestPublicizeJSONShape(t *testing.T) {
	data, err := json.Marshal(Publicize(publicFixture()))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("public coverage JSON: %s", data)
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	keysOf := func(raw json.RawMessage) string {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return strings.Join(keys, ",")
	}
	firstOf := func(key string) json.RawMessage {
		var list []json.RawMessage
		if err := json.Unmarshal(doc[key], &list); err != nil || len(list) == 0 {
			t.Fatalf("%s: %v (%d items)", key, err, len(list))
		}
		return list[0]
	}
	for _, tc := range []struct{ name, got, want string }{
		{"top level", keysOf(data), "districts,generated_at,heat,receivers,total_pages,transmitters,window"},
		{"window", keysOf(doc["window"]), "days,since,until"},
		{"receiver", keysOf(firstOf("receivers")), "last_heard_at,lat,lng,name"},
		{"transmitter", keysOf(firstOf("transmitters")), "district,heard,lat,lng"},
		{"district", keysOf(firstOf("districts")), "heard,last_heard_at,name,pages"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s keys = %s, want %s", tc.name, tc.got, tc.want)
		}
	}
	if heat := string(firstOf("heat")); heat != "[151.77,-32.74,3]" {
		t.Errorf("heat cell = %s", heat)
	}
	for _, private := range []string{"NSW RURAL FIRE SERVICE", "Kincumber", "licensee", "eirp", "heard_by", "score", "reach", "unmapped_prefixes", "links", "FRNSW", "Sugarloaf"} {
		if strings.Contains(string(data), private) {
			t.Errorf("public JSON contains %q: %s", private, data)
		}
	}
}

// An empty report still serialises lists as [], never null.
func TestPublicizeEmptyReport(t *testing.T) {
	data, _ := json.Marshal(Publicize(&Response{}))
	for _, key := range []string{`"receivers":[]`, `"transmitters":[]`, `"districts":[]`, `"heat":[]`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("missing %s in %s", key, data)
		}
	}
}

// From the report builder end to end: every RFS district and transmitter
// the admin report lists survives, nothing FRNSW does.
func TestPublicizeBuiltReport(t *testing.T) {
	in := reportFixture()
	r := build(in, in.until)
	got := Publicize(r)
	rfsTx, rfsDistricts := 0, 0
	for _, t := range r.Transmitters {
		if t.Channel == ChannelRFS {
			rfsTx++
		}
	}
	for _, d := range r.Districts {
		if d.Channel == ChannelRFS {
			rfsDistricts++
		}
	}
	if len(got.Transmitters) != rfsTx || len(got.Districts) != rfsDistricts || len(got.Receivers) != len(r.Receivers) {
		t.Errorf("public = %d tx, %d districts, %d receivers; report = %d RFS tx, %d RFS districts, %d receivers",
			len(got.Transmitters), len(got.Districts), len(got.Receivers), rfsTx, rfsDistricts, len(r.Receivers))
	}
}
