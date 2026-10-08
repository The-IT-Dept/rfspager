package radio

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/p25"
)

func TestMsTime(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		ms   int64
		want time.Time
	}{
		{"receiver time", now.Add(-3 * time.Second).UnixMilli(), now.Add(-3 * time.Second)},
		{"missing", 0, now},
		{"negative", -5, now},
		{"unsynced Pi clock in 1970", 86_400_000, now},
		{"far future", now.Add(48 * time.Hour).UnixMilli(), now},
	}
	for _, tc := range cases {
		if got := msTime(tc.ms, now); !got.Equal(tc.want) {
			t.Errorf("%s: msTime = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNewCallRow(t *testing.T) {
	now := time.Now()
	start := now.Add(-10 * time.Second)
	p := p25.CallPayload{
		CallID: "abc", SysID: 0x2D1, RFSS: 1, Site: 51, Group: 10047, Source: 2039407,
		StartMS: start.UnixMilli(),
	}

	c, err := newCallRow(p, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if c.endedAt.Valid {
		t.Error("call_start produced an ended_at")
	}
	if !c.tgid.Valid || c.tgid.Int64 != 10047 || !c.source.Valid || c.target.Valid || c.freqHz.Valid {
		t.Errorf("nullable ids = tg %v src %v target %v freq %v; zero must map to NULL", c.tgid, c.source, c.target, c.freqHz)
	}

	// An end stamped before its start (two clocks, or rounding) is clamped.
	p.EndMS = start.Add(-time.Second).UnixMilli()
	c, err = newCallRow(p, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if !c.endedAt.Valid || !c.endedAt.Time.Equal(c.startedAt) {
		t.Errorf("ended_at = %v, want clamped to started_at %v", c.endedAt, c.startedAt)
	}

	// A call_end without end_ms ends now.
	p.EndMS = 0
	c, _ = newCallRow(p, true, now)
	if !c.endedAt.Time.Equal(now) {
		t.Errorf("ended_at = %v, want now", c.endedAt.Time)
	}

	bad := map[string]p25.CallPayload{
		"no call_id":         {SysID: 0x2D1, Group: 1},
		"long call_id":       {CallID: strings.Repeat("x", maxCallUIDLen+1), SysID: 0x2D1, Group: 1},
		"no sysid":           {CallID: "a", Group: 1},
		"no group or target": {CallID: "a", SysID: 0x2D1, Source: 5},
	}
	for name, p := range bad {
		if _, err := newCallRow(p, false, now); !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("%s: err = %v, want ErrInvalidPayload", name, err)
		}
	}
	// Unit-to-unit calls have a target and no talkgroup.
	if _, err := newCallRow(p25.CallPayload{CallID: "a", SysID: 0x2D1, Target: 2039407}, false, now); err != nil {
		t.Errorf("private call rejected: %v", err)
	}
}

// Malformed frames must be rejected as invalid before any database work, so
// the hub can tell them from outages. The nil-db Store would panic otherwise.
func TestHandleFrameRejectsBadPayloadsWithoutDB(t *testing.T) {
	s := NewStore(nil)
	frames := map[string]string{
		p25.FrameSite:      `{"sysid": 0}`,
		p25.FrameCallStart: `{"call_id": ""}`,
		p25.FrameCallEnd:   `"not an object"`,
		p25.FrameUnit:      `{"kind": "affiliation", "sysid": 721, "unit": 0}`,
		"p25.mystery":      `{}`,
	}
	for typ, data := range frames {
		if err := s.HandleFrame(context.Background(), nil, typ, json.RawMessage(data)); !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("%s %s: err = %v, want ErrInvalidPayload", typ, data, err)
		}
	}
}

func TestParseTalkgroupCSV(t *testing.T) {
	in := "\ufeffTGID, Alpha_Tag ,description,agency,category,extra\n" +
		"10047,RFS Central Coast A,Fireground,RFS,Fire,x\n" +
		"10048,RFS CC B,,,,\n" +
		"\n" +
		"abc,Bad,,,,\n" +
		"70000,Too big,,,,\n" +
		",No id,,,,\n" +
		"10048,RFS Central Coast B,,RFS,,\n"
	rows, problems, err := ParseTalkgroupCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want 2", rows)
	}
	if r := rows[0]; r.TGID != 10047 || *r.AlphaTag != "RFS Central Coast A" || *r.Category != "Fire" {
		t.Errorf("row 0 = %+v", r)
	}
	// The repeated 10048 replaced the first; its empty cells stay nil so the
	// import leaves those columns alone.
	if r := rows[1]; r.TGID != 10048 || *r.AlphaTag != "RFS Central Coast B" || r.Description != nil || *r.Agency != "RFS" {
		t.Errorf("row 1 = %+v", r)
	}
	if len(problems) != 4 {
		t.Errorf("problems = %q, want 4 (abc, 70000, missing tgid, repeat)", problems)
	}

	if _, _, err := ParseTalkgroupCSV(strings.NewReader("alpha_tag,agency\nX,RFS\n")); err == nil {
		t.Error("header without tgid accepted")
	}
	if _, _, err := ParseTalkgroupCSV(strings.NewReader("")); err == nil {
		t.Error("empty file accepted")
	}
}
