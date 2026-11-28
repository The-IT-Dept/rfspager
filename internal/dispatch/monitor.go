package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

// FanoutMonitor watches the notification pipeline end to end and alerts
// the people running the service when it stalls: pages not fanned out a few
// minutes after they arrived, pages skipped as too late to push, or
// notifications waiting minutes to be pushed. It reminds every
// monitorRealert while the stall lasts and says when it has recovered.
//
// Written after 2026-11-27, when a planner change made fan-out take minutes
// per page for 19 hours and nobody knew until users wrote in.
type FanoutMonitor struct {
	db    *sqlx.DB
	alert func(ctx context.Context, title, body string)
	now   func() time.Time

	stalledSince time.Time // zero while healthy
	lastAlert    time.Time
}

const (
	monitorInterval = time.Minute
	// monitorStallAfter is how long a page may wait for fan-out, or a
	// notification for its push, before it counts as stalled. Fan-out
	// normally takes under a second (holdMax for a held copy) and pushes
	// a second or two.
	monitorStallAfter = 3 * time.Minute
	// monitorLateWindow is how far back skipped-as-late pages count.
	monitorLateWindow = 15 * time.Minute
	monitorRealert    = 30 * time.Minute
)

// NewFanoutMonitor returns a monitor that calls alert on stalls and
// recovery. alert should reach a person (a push), not only the log.
func NewFanoutMonitor(db *sqlx.DB, alert func(ctx context.Context, title, body string)) *FanoutMonitor {
	return &FanoutMonitor{db: db, alert: alert, now: time.Now}
}

// Run checks every monitorInterval until ctx is cancelled.
func (m *FanoutMonitor) Run(ctx context.Context) {
	t := time.NewTicker(monitorInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Check(ctx)
		}
	}
}

type pipelineState struct {
	Waiting      int        `db:"waiting"`
	OldestWait   *time.Time `db:"oldest_wait"`
	Late         int        `db:"late"`
	PushWaiting  int        `db:"push_waiting"`
	OldestPushAt *time.Time `db:"oldest_push"`
}

// Waiting pages are those fan-out would pick up (as reconcile selects them)
// from the last hour, stored more than monitorStallAfter ago (stored, not
// heard: a receiver's buffered pages arrive with old timestamps). Older ones
// it never will, and the late count covers those it skipped.
const pipelineStateSQL = `
SELECT
  (SELECT count(*) FROM messages
    WHERE fanned_out_at IS NULL AND deleted_at IS NULL
      AND (capcode_id IS NOT NULL OR (lat IS NOT NULL AND lng IS NOT NULL))
      AND created_at > $1::timestamptz - INTERVAL '1 hour'
      AND updated_at < $1::timestamptz - $2::interval) AS waiting,
  (SELECT min(updated_at) FROM messages
    WHERE fanned_out_at IS NULL AND deleted_at IS NULL
      AND (capcode_id IS NOT NULL OR (lat IS NOT NULL AND lng IS NOT NULL))
      AND created_at > $1::timestamptz - INTERVAL '1 hour'
      AND updated_at < $1::timestamptz - $2::interval) AS oldest_wait,
  (SELECT count(*) FROM messages
    WHERE push_skip_reason = 'late' AND deleted_at IS NULL AND capcode_id IS NOT NULL
      AND created_at > $1::timestamptz - INTERVAL '1 day'
      AND fanned_out_at > $1::timestamptz - $3::interval) AS late,
  (SELECT count(*) FROM notifications
    WHERE push_required AND pushed_at IS NULL AND push_failed_at IS NULL
      AND created_at > $1::timestamptz - INTERVAL '1 hour'
      AND created_at < $1::timestamptz - $2::interval) AS push_waiting,
  (SELECT min(created_at) FROM notifications
    WHERE push_required AND pushed_at IS NULL AND push_failed_at IS NULL
      AND created_at > $1::timestamptz - INTERVAL '1 hour'
      AND created_at < $1::timestamptz - $2::interval) AS oldest_push`

// Check looks at the pipeline once and alerts on a change, or a reminder
// due.
func (m *FanoutMonitor) Check(ctx context.Context) {
	now := m.now()
	var s pipelineState
	if err := m.db.GetContext(ctx, &s, pipelineStateSQL, now,
		monitorStallAfter.String(), monitorLateWindow.String()); err != nil {
		slog.Error("pipeline monitor: check failed", "error", err)
		return
	}
	stalled := s.Waiting > 0 || s.Late > 0 || s.PushWaiting > 0

	switch {
	case stalled && m.stalledSince.IsZero():
		m.stalledSince = now
		m.send(ctx, now, "RFSPager: notifications stalled", describe(s, now))
	case stalled && now.Sub(m.lastAlert) >= monitorRealert:
		m.send(ctx, now, "RFSPager: notifications still stalled",
			describe(s, now)+" Stalled for "+minutes(now.Sub(m.stalledSince))+".")
	case !stalled && !m.stalledSince.IsZero():
		m.send(ctx, now, "RFSPager: notifications recovered",
			"Pages are being pushed on time again, after "+minutes(now.Sub(m.stalledSince))+".")
		m.stalledSince = time.Time{}
	}
}

func (m *FanoutMonitor) send(ctx context.Context, now time.Time, title, body string) {
	slog.Error("pipeline monitor: "+title, "detail", body)
	m.lastAlert = now
	if m.alert != nil {
		m.alert(ctx, title, body)
	}
}

func describe(s pipelineState, now time.Time) string {
	out := ""
	if s.Waiting > 0 {
		out += fmt.Sprintf("%s waiting to be fanned out, oldest %s. ", plural(s.Waiting, "page"), age(s.OldestWait, now))
	}
	if s.Late > 0 {
		out += fmt.Sprintf("%s skipped as too late to push in the last %s. ", plural(s.Late, "page"), minutes(monitorLateWindow))
	}
	if s.PushWaiting > 0 {
		out += fmt.Sprintf("%s waiting to be pushed, oldest %s. ", plural(s.PushWaiting, "notification"), age(s.OldestPushAt, now))
	}
	return out[:max(len(out)-1, 0)]
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func age(t *time.Time, now time.Time) string {
	if t == nil {
		return "unknown"
	}
	return minutes(now.Sub(*t)) + " old"
}

func minutes(d time.Duration) string {
	return fmt.Sprintf("%d min", int(d.Round(time.Minute).Minutes()))
}
