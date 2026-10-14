package coverage

import (
	"reflect"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	for in, want := range map[string]string{
		"Mt. Victoria":         "MT VICTORIA",
		"MOUNT VICTORIA":       "MT VICTORIA",
		"  wamboin -palerang":  "WAMBOIN PALERANG",
		"O'Connell":            "O CONNELL",
		"Broughton Vale/Berry": "BROUGHTON VALE BERRY",
		"":                     "",
	} {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAliasKeys(t *testing.T) {
	for _, tc := range []struct {
		alias string
		want  []string
	}{
		{"Kincumber Brigade", []string{"KINCUMBER"}},
		{"Mt. Victoria Brigade Officers", []string{"MT VICTORIA"}},
		{"Broughton Vale/Berry Brigade", []string{"BROUGHTON VALE BERRY", "BROUGHTON VALE", "BERRY"}},
		{"kincumber brigade", []string{"KINCUMBER"}},
		{"Lower Hunter Group 3", nil},
		{"Brigade Captains", nil},
	} {
		if got := aliasKeys(tc.alias); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("aliasKeys(%q) = %q, want %q", tc.alias, got, tc.want)
		}
	}
}

func TestStationLabels(t *testing.T) {
	gosford, hornsby, frnsw, ses := int64(1), int64(2), int64(3), int64(4)
	district := map[int64]bool{gosford: true, hornsby: true}
	caps := []Capcode{
		{Alias: "Kincumber Brigade", AgencyID: &gosford},
		{Alias: "Kincumber Brigade Officers", AgencyID: &gosford}, // same brigade, same district: fine
		{Alias: "Berowra Brigade", AgencyID: &hornsby},
		{Alias: "Mt Colah Brigade", AgencyID: &hornsby},
		{Alias: "Glenorie Brigade", AgencyID: &hornsby},
		{Alias: "Glenorie Brigade", AgencyID: &gosford}, // ambiguous across districts
		{Alias: "Broughton Vale/Berry Brigade", AgencyID: &gosford},
		{Alias: "Avalon Brigade", AgencyID: &frnsw}, // not a district
		{Alias: "Bateau Bay Brigade", AgencyID: &ses},
		{Alias: "No Agency Brigade", AgencyID: nil},
	}
	stations := []Station{
		{Name: "KINCUMBER", Lat: -33.47, Lng: 151.38},
		{Name: "MOUNT COLAH", Lat: -33.66, Lng: 151.11},
		{Name: "GLENORIE", Lat: -33.60, Lng: 151.01},
		{Name: "BERRY", Lat: -34.77, Lng: 150.69}, // matches the "A/B" part
		{Name: "AVALON", Lat: -33.63, Lng: 151.33},
		{Name: "BEROWRA", Lat: -33.62, Lng: 151.15},
		{Name: "Berowra", Lat: -30.00, Lng: 150.00}, // duplicate name: both skipped
		{Name: "NOWHERE", Lat: -33.0, Lng: 151.0},
	}
	got := stationLabels(stations, caps, func(id int64) bool { return district[id] })
	want := []LabelPoint{
		{Lat: -33.47, Lng: 151.38, AgencyID: gosford, Weight: stationWeight},
		{Lat: -33.66, Lng: 151.11, AgencyID: hornsby, Weight: stationWeight},
		{Lat: -34.77, Lng: 150.69, AgencyID: gosford, Weight: stationWeight},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stationLabels = %+v\nwant %+v", got, want)
	}
}

func TestVoteDistrict(t *testing.T) {
	a, b := int64(1), int64(2)
	tx := struct{ lat, lng float64 }{-33.0, 151.0}
	north := func(km float64) float64 { return tx.lat + km/111.2 }

	t.Run("weighted by inverse square distance", func(t *testing.T) {
		points := []LabelPoint{
			{Lat: north(2), Lng: tx.lng, AgencyID: a, Weight: 1},  // 1/9
			{Lat: north(5), Lng: tx.lng, AgencyID: b, Weight: 3},  // 3/36
			{Lat: north(10), Lng: tx.lng, AgencyID: b, Weight: 1}, // 1/121
		}
		got, conf, ok := voteDistrict(tx.lat, tx.lng, points)
		va, vb := 1.0/9, 3.0/36+1.0/121
		if !ok || got != a {
			t.Fatalf("winner = %d ok=%v, want %d", got, ok, a)
		}
		if want := va / (va + vb); conf < want-0.01 || conf > want+0.01 {
			t.Errorf("confidence = %.3f, want %.3f", conf, want)
		}
	})

	t.Run("many incidents outvote one station", func(t *testing.T) {
		points := []LabelPoint{{Lat: north(3), Lng: tx.lng, AgencyID: a, Weight: stationWeight}}
		for range 10 {
			points = append(points, LabelPoint{Lat: north(4), Lng: tx.lng, AgencyID: b, Weight: incidentWeight})
		}
		if got, _, _ := voteDistrict(tx.lat, tx.lng, points); got != b {
			t.Errorf("winner = %d, want %d", got, b)
		}
	})

	t.Run("30 km radius before 60 km fallback", func(t *testing.T) {
		points := []LabelPoint{
			{Lat: north(29), Lng: tx.lng, AgencyID: a, Weight: 1},
			// Much heavier, but outside 30 km so it does not vote while a
			// point is inside.
			{Lat: north(31), Lng: tx.lng, AgencyID: b, Weight: 100},
		}
		got, conf, _ := voteDistrict(tx.lat, tx.lng, points)
		if got != a || conf != 1 {
			t.Errorf("winner = %d (%.2f), want %d at 1.0", got, conf, a)
		}
	})

	t.Run("fallback to 60 km", func(t *testing.T) {
		points := []LabelPoint{{Lat: north(45), Lng: tx.lng, AgencyID: b, Weight: 1}}
		if got, _, ok := voteDistrict(tx.lat, tx.lng, points); !ok || got != b {
			t.Errorf("winner = %d ok=%v, want %d", got, ok, b)
		}
	})

	t.Run("nothing in range", func(t *testing.T) {
		points := []LabelPoint{{Lat: north(61), Lng: tx.lng, AgencyID: b, Weight: 1}}
		if _, _, ok := voteDistrict(tx.lat, tx.lng, points); ok {
			t.Error("ok with no point within 60 km")
		}
	})
}
