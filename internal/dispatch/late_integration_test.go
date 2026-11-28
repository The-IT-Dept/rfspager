package dispatch

import (
	"context"
	"testing"
	"time"
)

// A page fan-out only gets to long after it arrived (the worker was stalled
// or down) wakes nobody: a turnout pushed hours late is worse than none. It
// is marked done with the reason 'late'. A page reached in time still goes.
func TestFanoutSkipsLatePages(t *testing.T) {
	f := newHoldFixture(t)
	ctx := context.Background()

	late := f.insert(t, "LHCENTR1 - 26-140001 - Grass fire - FIRECALL - MAIN RD,CESSNOCK", nil)
	onTime := f.insert(t, "LHCENTR1 - 26-140002 - Grass fire - FIRECALL - HIGH ST,CESSNOCK", nil)

	f.now = f.now.Add(maxPushAge - time.Minute)
	f.worker.processMessage(ctx, onTime)
	if s := f.state(t, onTime); s.notified != 1 || !s.fanned || s.skip.Valid {
		t.Fatalf("page reached in time: %+v", s)
	}

	f.now = f.now.Add(2 * time.Minute)
	f.worker.processMessage(ctx, late)
	if s := f.state(t, late); s.notified != 0 || !s.fanned || s.skip.String != "late" {
		t.Fatalf("late page: %+v, want no notifications, fanned out, reason late", s)
	}
}

// Lateness is fan-out's own delay, counted from when the page was stored,
// not from when the receiver heard it: a receiver delivering its buffer
// after a network drop isn't a stalled fan-out, and its pages still go.
func TestFanoutPushesPagesDeliveredLate(t *testing.T) {
	f := newHoldFixture(t)
	ctx := context.Background()
	id := mustID(t, f.conn, `INSERT INTO messages (system_id, capcode_id, address, message, md5, created_at, updated_at)
		VALUES ($1, $2, '1310104', 'LHCENTR1 - 26-140003 - MVA - HIGH ST,CESSNOCK', md5('x'), $3, $4) RETURNING id`,
		f.system, f.capcode, f.now.Add(-20*time.Minute), f.now)
	f.worker.processMessage(ctx, id)
	if s := f.state(t, id); s.notified != 1 || s.skip.Valid {
		t.Fatalf("page heard 20 min ago, stored just now: %+v, want pushed", s)
	}
}
