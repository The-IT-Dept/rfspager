package dispatch

import (
	"context"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/notify"
)

// Tuning knobs. Conservative defaults — bump batchSize once we're sure APN/FCM
// keep up. leaseDuration is the window after a claim during which a row is
// invisible to other claimers; if the worker dies, the row reappears for retry.
const (
	batchSize       = 50
	leaseDuration   = 2 * time.Minute
	maxAttempts     = 5
	reconcileTick   = 30 * time.Second
	backoffBaseSecs = 30 // exp backoff: 30, 60, 120, 240, 480 (then give up)
)

const claimSQL = `
UPDATE notifications
   SET push_locked_until = NOW() + ($1 * interval '1 second'),
       push_attempts     = push_attempts + 1
 WHERE id IN (
       SELECT id FROM notifications
        WHERE push_required
          AND pushed_at IS NULL
          AND push_failed_at IS NULL
          AND (push_locked_until IS NULL OR push_locked_until < NOW())
        ORDER BY created_at
        FOR UPDATE SKIP LOCKED
        LIMIT $2
 )
 RETURNING id::text, notifiable_id, data, push_attempts;
`

// markSentSQL stamps a delivered push. If a better copy of the page carried
// its text to the row while the push was in flight (see
// db.carryToNotifications), the device has the old text:
//
//   - the carried text fixes the page (carried_fix): the row becomes a
//     correction, keeping the text that went out (the claimed data, $3) plus
//     correction_body and first_pushed_at, and is queued again; but if some
//     device missed this send ($4) it is queued again as an ordinary push
//     with the new text, because a loud repeat on one device beats a quiet
//     first alert on another;
//   - otherwise (a better copy that is still damaged): delivered, recording
//     the text that went out; a retry would have used the new one.
//
// A correction that was itself in flight is simply delivered, recording the
// correction that went out: one correction per push. Every SET expression
// reads the row as it was.
// Returns whether the row was queued again.
//
// The decision is made from the row itself in every SET expression, not in a
// CTE: if this waited on db.carryToNotifications' lock, Postgres rechecks
// the updated row but a CTE would still hold the old one.
const markSentSQL = `
UPDATE notifications
   SET pushed_at = CASE WHEN data <> $3 AND NOT ($3::jsonb ? 'correction')
                             AND COALESCE((data::jsonb->>'carried_fix')::boolean, false)
                        THEN NULL ELSE NOW() END,
       data = CASE
                WHEN data = $3 THEN data
                WHEN $3::jsonb ? 'correction' THEN $3
                WHEN COALESCE((data::jsonb->>'carried_fix')::boolean, false) AND $4 THEN data
                WHEN COALESCE((data::jsonb->>'carried_fix')::boolean, false)
                  THEN ($3::jsonb || jsonb_build_object(
                          'correction', true,
                          'correction_body', data::jsonb->>'body',
                          'first_pushed_at', NOW()))::text
                ELSE $3
              END,
       push_attempts = CASE WHEN data <> $3 AND NOT ($3::jsonb ? 'correction')
                                 AND COALESCE((data::jsonb->>'carried_fix')::boolean, false)
                            THEN 0 ELSE push_attempts END,
       push_locked_until = NULL,
       push_last_error   = NULLIF($2, '')
 WHERE id = $1::uuid
RETURNING pushed_at IS NULL;
`

// markFailedSQL gives up on a push. A correction that can't be delivered
// leaves the row as it was after the original push (delivered, at
// first_pushed_at, not failed), flagged correction_failed for the admin page.
const markFailedSQL = `
UPDATE notifications
   SET push_failed_at    = CASE WHEN data::jsonb ? 'first_pushed_at' THEN NULL ELSE NOW() END,
       pushed_at         = CASE WHEN data::jsonb ? 'first_pushed_at'
                                THEN (data::jsonb->>'first_pushed_at')::timestamptz ELSE pushed_at END,
       data              = CASE WHEN data::jsonb ? 'first_pushed_at'
                                THEN (data::jsonb || '{"correction_failed": true}'::jsonb)::text ELSE data END,
       push_locked_until = NULL,
       push_last_error   = $2
 WHERE id = $1::uuid;
`

const retryLaterSQL = `
UPDATE notifications
   SET push_locked_until = NOW() + ($2 * interval '1 second'),
       push_last_error   = $3
 WHERE id = $1::uuid;
`

// DispatchWorker pulls claimed notification rows off the queue in batches and
// hands them to a Notifier for actual push delivery. Many pods can run a
// DispatchWorker concurrently; SKIP LOCKED keeps them from stepping on each
// other.
type DispatchWorker struct {
	db       *sqlx.DB
	notifier *notify.Notifier
	wakeCh   <-chan string
}

func NewDispatchWorker(db *sqlx.DB, notifier *notify.Notifier, wakeCh <-chan string) *DispatchWorker {
	return &DispatchWorker{db: db, notifier: notifier, wakeCh: wakeCh}
}

func (w *DispatchWorker) Run(ctx context.Context) {
	slog.Info("dispatch worker started")
	defer slog.Info("dispatch worker stopped")

	// Catch any rows left pending across a restart before trusting NOTIFY.
	for w.drainOnce(ctx) {
	}

	tick := time.NewTicker(reconcileTick)
	defer tick.Stop()

	for {
		select {
		case <-w.wakeCh:
			for w.drainOnce(ctx) {
				if ctx.Err() != nil {
					return
				}
			}
		case <-tick.C:
			for w.drainOnce(ctx) {
				if ctx.Err() != nil {
					return
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// drainOnce claims up to batchSize rows, dispatches them, records outcomes,
// and returns true if it claimed a full batch (caller should loop to drain
// the queue without waiting for another NOTIFY).
func (w *DispatchWorker) drainOnce(ctx context.Context) bool {
	rows, err := w.db.QueryxContext(ctx, claimSQL, int(leaseDuration.Seconds()), batchSize)
	if err != nil {
		slog.Error("dispatch: claim failed", "error", err)
		return false
	}

	var claimed []notify.ClaimedNotification
	for rows.Next() {
		var c notify.ClaimedNotification
		if err := rows.Scan(&c.ID, &c.UserID, &c.Data, &c.Attempts); err != nil {
			slog.Error("dispatch: scan failed", "error", err)
			continue
		}
		claimed = append(claimed, c)
	}
	rows.Close()

	if len(claimed) == 0 {
		return false
	}

	outcomes := w.notifier.SendBatch(ctx, claimed)
	requeued := w.recordOutcomes(ctx, claimed, outcomes)
	return len(claimed) >= batchSize || requeued > 0
}

// recordOutcomes updates each notifications row with the result of its push,
// and (best-effort) soft-deletes installations whose tokens the provider
// declared invalid so we stop re-pushing to them. It returns how many sent
// rows were left pending because a correction landed while they were in
// flight.
func (w *DispatchWorker) recordOutcomes(ctx context.Context, claimed []notify.ClaimedNotification, outcomes []notify.DispatchOutcome) (requeued int) {
	for i, o := range outcomes {
		var attempts int
		var data []byte
		if i < len(claimed) {
			attempts = claimed[i].Attempts
			data = claimed[i].Data
		}
		switch {
		case o.Sent:
			var pending bool
			if err := w.db.QueryRowContext(ctx, markSentSQL, o.ID, o.Err, string(data), o.SomeRetryable).Scan(&pending); err != nil {
				slog.Error("dispatch: mark sent failed", "id", o.ID, "error", err)
			} else if pending {
				requeued++
				slog.Info("dispatch: page corrected while in flight, sending again", "id", o.ID)
			}
		case o.PermanentFail || attempts >= maxAttempts:
			if _, err := w.db.ExecContext(ctx, markFailedSQL, o.ID, truncate(o.Err, 500)); err != nil {
				slog.Error("dispatch: mark failed failed", "id", o.ID, "error", err)
			}
		default:
			backoff := exponentialBackoff(attempts)
			if _, err := w.db.ExecContext(ctx, retryLaterSQL, o.ID, backoff, truncate(o.Err, 500)); err != nil {
				slog.Error("dispatch: retry-later failed", "id", o.ID, "error", err)
			}
		}

		for _, tok := range o.BadTokens {
			if _, err := w.db.ExecContext(ctx,
				`UPDATE installations SET deleted_at = NOW() WHERE push_token = $1 AND deleted_at IS NULL`,
				tok); err != nil {
				slog.Warn("dispatch: failed to retire bad token", "error", err)
			}
		}
	}
	return requeued
}

func exponentialBackoff(attempts int) int {
	// attempts is post-increment (1 on first try). 1→30s, 2→60s, 3→120s, …
	d := backoffBaseSecs
	for i := 1; i < attempts; i++ {
		d *= 2
		if d > 3600 {
			d = 3600
			break
		}
	}
	return d
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
