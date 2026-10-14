package handler

import (
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

func TestPageReception(t *testing.T) {
	at := time.Date(2026, 10, 14, 1, 54, 54, 700_000_000, time.UTC)
	msg := "14 October 2026 01:54:54 HVDO - 26-131331 - Grass Fire - 551 SANDY CREEK RD - [151.77,-32.74]"

	r := pageReception(2, "0950048", msg, at, &models.Capcode{ID: 17}, &rxproto.PageQuality{Corrected: 3})
	if r.SystemID != 2 || r.Address != "0950048" || !r.HeardAt.Equal(at.Truncate(time.Second)) {
		t.Errorf("reception = %+v", r)
	}
	if !r.CapcodeID.Valid || r.CapcodeID.Int64 != 17 {
		t.Errorf("capcode = %+v", r.CapcodeID)
	}
	if !r.Lat.Valid || r.Lat.Float64 != -32.74 || !r.Lng.Valid || r.Lng.Float64 != 151.77 {
		t.Errorf("coords = %v,%v, want lat -32.74 lng 151.77", r.Lat, r.Lng)
	}
	if !r.IncidentKey.Valid || r.IncidentKey.String != IncidentKey(msg) {
		t.Errorf("incident key = %+v", r.IncidentKey)
	}
	if r.Damaged {
		t.Error("corrected-only page marked damaged")
	}

	for _, tc := range []struct {
		name    string
		quality *rxproto.PageQuality
		damaged bool
	}{
		{"pagermon (no quality)", nil, false},
		{"uncorrectable", &rxproto.PageQuality{Uncorrectable: 1}, true},
		{"truncated", &rxproto.PageQuality{Truncated: true}, true},
	} {
		if got := pageReception(1, "1", "x", at, nil, tc.quality).Damaged; got != tc.damaged {
			t.Errorf("%s: damaged = %v, want %v", tc.name, got, tc.damaged)
		}
	}

	bare := pageReception(1, "0521839", "FRNSW beacon", at, nil, nil)
	if bare.CapcodeID.Valid || bare.Lat.Valid || bare.Lng.Valid {
		t.Errorf("bare page = %+v", bare)
	}
}

func TestIsDispatchText(t *testing.T) {
	for msg, want := range map[string]bool{
		"1129":   false,
		" 2011 ": false,
		"":       false,
		"ISDO - 26-131336 - AFA - INCIDENT CALL - KIAMA": true,
		"FUC8y:KM": true, // garbled, but filtered elsewhere (known capcodes only)
	} {
		if got := isDispatchText(msg); got != want {
			t.Errorf("isDispatchText(%q) = %v, want %v", msg, got, want)
		}
	}
}
