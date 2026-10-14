package coverage

import (
	"math"
	"testing"
)

func f(v float64) *float64 { return &v }
func id(v int64) *int64    { return &v }

func TestDistanceKm(t *testing.T) {
	// Budgewoi to Newcastle receiver pins, ~38 km.
	d := distanceKm(-33.23, 151.56, -32.93, 151.75)
	if d < 37 || d > 39 {
		t.Errorf("Budgewoi-Newcastle = %.1f km, want ~38", d)
	}
	if d := distanceKm(-33, 151, -33, 151); d != 0 {
		t.Errorf("zero distance = %v", d)
	}
}

func TestRadioHorizonKm(t *testing.T) {
	for _, tc := range []struct {
		name string
		tx   Transmitter
		want float64
	}{
		// 4.12 * (sqrt(10) + sqrt(30)) with the default 10 m mast at sea level.
		{"defaults", Transmitter{}, 4.12 * (math.Sqrt(10) + math.Sqrt(30))},
		{"hilltop", Transmitter{ElevationM: f(410), HeightM: f(15)}, 4.12 * (math.Sqrt(425) + math.Sqrt(30))},
		{"no mast height", Transmitter{ElevationM: f(90)}, 4.12 * (math.Sqrt(100) + math.Sqrt(30))},
	} {
		if got := radioHorizonKm(&tc.tx); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: horizon = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLinkScoreDB(t *testing.T) {
	high := &Transmitter{EIRPW: f(331), ElevationM: f(400), HeightM: f(15)} // horizon ~107 km
	low := &Transmitter{EIRPW: f(331), ElevationM: f(0), HeightM: f(10)}    // horizon ~35.6 km
	for _, tc := range []struct {
		name string
		tx   *Transmitter
		d    float64
		want float64
	}{
		{"within 1 km clamps to 1", high, 0.2, 10 * math.Log10(331)},
		{"10 km", high, 10, 10*math.Log10(331) - 35},
		{"100 km inside horizon", high, 100, 10*math.Log10(331) - 70},
		{"just inside 1.3x horizon", low, 46, 10*math.Log10(331) - 35*math.Log10(46)},
		{"beyond 1.3x horizon", low, 47, 10*math.Log10(331) - 35*math.Log10(47) - 20},
		{"missing EIRP uses 50 W", &Transmitter{ElevationM: f(400)}, 10, 10*math.Log10(50) - 35},
	} {
		if got := linkScoreDB(tc.tx, tc.d); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: score = %.3f, want %.3f", tc.name, got, tc.want)
		}
	}
}

func TestRankTransmitters(t *testing.T) {
	rx := struct{ lat, lng float64 }{-33.23, 151.56}
	at := func(id int64, km, eirp, elev float64) *Transmitter {
		// Place the transmitter km due north of the receiver.
		return &Transmitter{ID: id, Lat: rx.lat + km/111.2, Lng: rx.lng, EIRPW: f(eirp), ElevationM: f(elev), HeightM: f(15)}
	}

	t.Run("empty", func(t *testing.T) {
		if _, _, ok := rankTransmitters(rx.lat, rx.lng, nil); ok {
			t.Fatal("ok with no transmitters")
		}
	})

	t.Run("nearest wins, runners-up within 6 dB, max 3", func(t *testing.T) {
		txs := []*Transmitter{
			at(1, 20, 331, 400), // best
			at(2, 22, 331, 400), // -1.4 dB
			at(3, 25, 331, 400), // -3.4 dB
			at(4, 28, 331, 400), // -5.1 dB
			at(5, 29, 331, 400), // -5.6 dB but 4th runner-up
			at(6, 60, 331, 400), // -16.7 dB
		}
		best, possible, ok := rankTransmitters(rx.lat, rx.lng, txs)
		if !ok || best.T.ID != 1 {
			t.Fatalf("probable = %+v", best)
		}
		var ids []int64
		for _, c := range possible {
			ids = append(ids, c.T.ID)
		}
		if len(ids) != 3 || ids[0] != 2 || ids[1] != 3 || ids[2] != 4 {
			t.Errorf("possible = %v, want [2 3 4]", ids)
		}
	})

	t.Run("power beats distance", func(t *testing.T) {
		// 10x the EIRP buys 10 dB, worth ~1.93x the distance at 35 dB/decade.
		txs := []*Transmitter{at(1, 20, 33, 400), at(2, 35, 331, 400)}
		best, _, _ := rankTransmitters(rx.lat, rx.lng, txs)
		if best.T.ID != 2 {
			t.Errorf("probable = %d, want the stronger 2", best.T.ID)
		}
	})

	t.Run("over the horizon loses", func(t *testing.T) {
		// A low site at 50 km is past 1.3x its ~36 km horizon (-20 dB), so a
		// hilltop at 90 km wins despite the distance.
		txs := []*Transmitter{at(1, 50, 331, 0), at(2, 90, 331, 600)}
		txs[0].HeightM = f(10)
		best, _, _ := rankTransmitters(rx.lat, rx.lng, txs)
		if best.T.ID != 2 {
			t.Errorf("probable = %d, want the hilltop 2", best.T.ID)
		}
	})

	t.Run("ties are stable", func(t *testing.T) {
		a, b := at(9, 20, 331, 400), at(4, 20, 331, 400)
		best, possible, _ := rankTransmitters(rx.lat, rx.lng, []*Transmitter{a, b})
		if best.T.ID != 4 || len(possible) != 1 || possible[0].T.ID != 9 {
			t.Errorf("tie: probable %d possible %+v, want 4 then 9", best.T.ID, possible)
		}
	})
}
