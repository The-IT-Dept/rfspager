package dispatch

import (
	"context"
	"strings"
	"testing"
	"time"
)

type opsRecorder struct {
	titles, bodies []string
}

func (r *opsRecorder) alert(_ context.Context, title, body string) {
	r.titles = append(r.titles, title)
	r.bodies = append(r.bodies, body)
}

func (r *opsRecorder) last(t *testing.T) (string, string) {
	t.Helper()
	if len(r.titles) == 0 {
		t.Fatal("no alert sent")
	}
	return r.titles[len(r.titles)-1], r.bodies[len(r.bodies)-1]
}

// The monitor alerts when pages stop being fanned out on time, reminds
// every half hour while it lasts, and says when it has recovered; a healthy
// pipeline is quiet.
func TestFanoutMonitorAlertsOnStallAndRecovery(t *testing.T) {
	f := newHoldFixture(t)
	ctx := context.Background()
	rec := &opsRecorder{}
	clock := time.Now().UTC().Truncate(time.Second)
	m := NewFanoutMonitor(f.conn, rec.alert)
	m.now = func() time.Time { return clock }

	m.Check(ctx)
	if len(rec.titles) != 0 {
		t.Fatalf("healthy pipeline alerted: %v", rec.titles)
	}

	// A page stored five minutes ago that nobody has fanned out.
	id := mustID(t, f.conn, `INSERT INTO messages (system_id, capcode_id, address, message, md5, created_at, updated_at)
		VALUES ($1, $2, '1310104', 'stuck page', md5('stuck page'), $3, $3) RETURNING id`, f.system, f.capcode, clock.Add(-5*time.Minute))
	m.Check(ctx)
	if len(rec.titles) != 0 {
		t.Fatalf("alerted on a single bad sample: %v", rec.titles)
	}
	clock = clock.Add(time.Minute)
	m.Check(ctx)
	if title, body := rec.last(t); len(rec.titles) != 1 || !strings.Contains(title, "stalled") || !strings.Contains(body, "1 page") {
		t.Fatalf("stall alert = %q / %q (%d alerts)", title, body, len(rec.titles))
	}

	clock = clock.Add(10 * time.Minute)
	m.Check(ctx)
	if len(rec.titles) != 1 {
		t.Fatalf("re-alerted within half an hour: %v", rec.titles)
	}
	clock = clock.Add(21 * time.Minute)
	m.Check(ctx)
	if len(rec.titles) != 2 {
		t.Fatalf("no reminder after half an hour: %v", rec.titles)
	}

	if _, err := f.conn.ExecContext(ctx, `UPDATE messages SET fanned_out_at = NOW() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	m.Check(ctx)
	if len(rec.titles) != 2 {
		t.Fatalf("announced recovery on a single good sample: %v", rec.titles)
	}
	clock = clock.Add(time.Minute)
	m.Check(ctx)
	if title, _ := rec.last(t); len(rec.titles) != 3 || !strings.Contains(title, "recovered") {
		t.Fatalf("recovery alert = %v", rec.titles)
	}
	m.Check(ctx)
	if len(rec.titles) != 3 {
		t.Fatalf("healthy again but alerted: %v", rec.titles)
	}
}

// Pages skipped as too late, and notifications that have waited minutes to
// be pushed, are stalls too.
func TestFanoutMonitorAlertsOnLateSkipsAndPushBacklog(t *testing.T) {
	f := newHoldFixture(t)
	ctx := context.Background()
	clock := time.Now().UTC().Truncate(time.Second)

	rec := &opsRecorder{}
	m := NewFanoutMonitor(f.conn, rec.alert)
	m.now = func() time.Time { return clock }
	mustID(t, f.conn, `INSERT INTO messages (system_id, capcode_id, address, message, md5, created_at, updated_at, fanned_out_at, push_skip_reason)
		VALUES ($1, $2, '1310104', 'late page', md5('late page'), $3, $3, $4, 'late') RETURNING id`, f.system, f.capcode, clock.Add(-20*time.Minute), clock.Add(-time.Minute))
	m.Check(ctx)
	m.Check(ctx)
	if _, body := rec.last(t); !strings.Contains(body, "too late") {
		t.Fatalf("late-skip alert body = %q", body)
	}

	if _, err := f.conn.ExecContext(ctx, `UPDATE messages SET push_skip_reason = NULL`); err != nil {
		t.Fatal(err)
	}
	userID := mustID(t, f.conn, `SELECT id FROM users LIMIT 1`)
	if _, err := f.conn.ExecContext(ctx, `INSERT INTO notifications (id, type, notifiable_type, notifiable_id, data, push_required, created_at, updated_at)
		VALUES (gen_random_uuid(), 'App\Notifications\PagerNotification', 'App\Models\User', $1, '{}', true, $2, $2)`, userID, clock.Add(-4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// A push waiting out a retry backoff is the dispatcher working, not a
	// stall.
	if _, err := f.conn.ExecContext(ctx, `UPDATE notifications SET push_locked_until = $1`, clock.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	rec2 := &opsRecorder{}
	m2 := NewFanoutMonitor(f.conn, rec2.alert)
	m2.now = func() time.Time { return clock }
	m2.Check(ctx)
	m2.Check(ctx)
	if len(rec2.titles) != 0 {
		t.Fatalf("alerted on a push in backoff: %v", rec2.bodies)
	}
	// Its lease long expired and nobody has picked it up: stalled.
	if _, err := f.conn.ExecContext(ctx, `UPDATE notifications SET push_locked_until = $1`, clock.Add(-4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	m2.Check(ctx)
	m2.Check(ctx)
	if _, body := rec2.last(t); !strings.Contains(body, "waiting to be pushed") {
		t.Fatalf("push backlog alert body = %q", body)
	}
}
