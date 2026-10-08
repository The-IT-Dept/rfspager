package p25

import (
	"os"
	"testing"
	"time"
)

type sent struct {
	typ     string
	payload any
}

// replay feeds a fixture through Decoder → Reporter, advancing a fake clock
// at the capture's real rate.
func replay(t *testing.T, name string) []sent {
	t.Helper()
	iq, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var out []sent
	r := NewReporter(func(typ string, p any) { out = append(out, sent{typ, p}) })
	now := time.Date(2026, 10, 8, 11, 41, 0, 0, time.UTC)
	dec := NewDecoder(func(m Message) { r.Handle(m, now) })
	const chunk = SampleRate / 10 * 2 // 100 ms of cs8
	for i := 0; i < len(iq); i += chunk {
		dec.ProcessCS8(iq[i:min(i+chunk, len(iq))])
		now = now.Add(100 * time.Millisecond)
		r.Tick(now)
	}
	return out
}

func TestReporterSiteAndUnits(t *testing.T) {
	out := replay(t, "cc_419.0250.cs8")

	var site *SitePayload
	var units []UnitPayload
	for _, s := range out {
		switch p := s.payload.(type) {
		case SitePayload:
			site = &p
		case UnitPayload:
			units = append(units, p)
		}
	}
	if site == nil {
		t.Fatal("no site payload")
	}
	if site.WACN != 0xBEE00 || site.SysID != 0x2D1 || site.RFSS != 4 || site.Site != 51 || site.ControlHz != 419_025_000 {
		t.Errorf("site = %+v", *site)
	}
	if len(site.Plan) < 10 || len(site.Neighbors) < 8 {
		t.Errorf("plan %d entries, %d neighbours", len(site.Plan), len(site.Neighbors))
	}

	// The capture holds one registration and one affiliation to 10047 by
	// 2039407, each repeated on air; we must report each once.
	if len(units) != 2 {
		t.Fatalf("units = %+v", units)
	}
	aff := units[1]
	if aff.Kind != UnitKindAffiliation || aff.Unit != 2039407 || aff.Group != 10047 || aff.Site != 51 {
		t.Errorf("affiliation = %+v", aff)
	}
}

func TestCallTrackerFoldsUpdates(t *testing.T) {
	var started, ended []Call
	ct := CallTracker{OnStart: func(c Call) { started = append(started, c) }, OnEnd: func(c Call) { ended = append(ended, c) }}
	ct.Handle(IdentifierUpdate{ID: 5, BaseHz: 420_000_000, SpacingHz: 6_250, Slots: 1}, time.Unix(0, 0))
	ch := Channel(0x5000 | 48)
	t0 := time.Unix(100, 0)
	ct.Handle(GroupVoiceGrant{Options: 0x40, Channel: ch, Group: 12052, Source: 2197507}, t0)
	for i := 1; i <= 5; i++ {
		ct.Handle(GroupVoiceGrant{Channel: ch, Group: 12052, Update: true}, t0.Add(time.Duration(i)*time.Second))
	}
	ct.Expire(t0.Add(20 * time.Second))
	if len(started) != 1 || len(ended) != 1 {
		t.Fatalf("started %d ended %d", len(started), len(ended))
	}
	c := ended[0]
	if !c.Encrypted || c.Source != 2197507 || c.FreqHz != 420_300_000 || c.LastSeen.Sub(c.Start) != 5*time.Second {
		t.Errorf("call = %+v", c)
	}
}

func TestCallTrackerSplitsTalkers(t *testing.T) {
	var started []Call
	ct := CallTracker{OnStart: func(c Call) { started = append(started, c) }}
	ch := Channel(0x5000 | 48)
	t0 := time.Unix(100, 0)
	ct.Handle(GroupVoiceGrant{Channel: ch, Group: 10047, Source: 2017875}, t0)
	ct.Handle(GroupVoiceGrant{Channel: ch, Group: 10047, Source: 2017875}, t0.Add(time.Second)) // repeat
	ct.Handle(GroupVoiceGrant{Channel: ch, Group: 10047, Update: true}, t0.Add(2*time.Second))
	ct.Handle(GroupVoiceGrant{Channel: ch, Group: 10047, Source: 2039407}, t0.Add(3*time.Second)) // reply
	if len(started) != 2 || started[1].Source != 2039407 {
		t.Fatalf("started = %+v", started)
	}
}
