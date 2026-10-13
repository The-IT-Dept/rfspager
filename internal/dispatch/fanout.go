package dispatch

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
)

// fanoutLockKey is the namespace passed to pg_try_advisory_xact_lock alongside
// the message_id so cross-pod fan-out is mutually exclusive per message.
const fanoutLockKey = int32(7423100)

// reconcileWindow is how far back the catch-up scan looks for orphan messages
// (messages that have no notification rows yet). LISTEN/NOTIFY is the primary
// wake-up path; reconciliation only matters when a NOTIFY is missed (e.g. all
// pods restarting in the moment a message arrived).
const reconcileWindow = time.Hour

// fanoutSQL is the entire pipeline as one statement:
//
//	advisory lock → look up message + capcode + agency
//	              → union recipients from subscriptions and alertzones
//	              → INSERT into notifications, pre-computed title/body
//	              → stamp messages.fanned_out_at so reconcile won't retry.
//
// The AFTER INSERT trigger on notifications then fires NOTIFY rfs_dispatch,
// waking the dispatch workers.
//
// `fanned_out_at` is set independent of recipient count — messages with no
// eligible Pro subscribers also count as "done" and must not be reconciled
// again every minute.
const fanoutSQL = `
WITH locked AS (
    SELECT pg_try_advisory_xact_lock($1::int4, $2::int4) AS got
),
msg AS (
    SELECT m.id, m.system_id, m.capcode_id, m.address, m.message, m.created_at,
           m.lat, m.lng, m.incident_key, m.quality,
           c.alias  AS capcode_alias,
           a.agency AS agency_name
      FROM messages m
      LEFT JOIN capcodes c ON c.id = m.capcode_id
      LEFT JOIN agencies a ON a.id = c.agency_id
     WHERE m.id = $2
       AND m.deleted_at IS NULL
       AND (SELECT got FROM locked)
),
recipients AS (
    SELECT s.user_id
      FROM subscriptions s
      JOIN users u ON u.id = s.user_id
      JOIN msg            ON msg.capcode_id IS NOT NULL
     WHERE s.capcode_id = msg.capcode_id
       AND u.subscription_valid = true

    UNION

    SELECT az.user_id
      FROM alertzones az
      JOIN users u ON u.id = az.user_id
      JOIN msg            ON msg.lat IS NOT NULL AND msg.lng IS NOT NULL
     WHERE u.subscription_valid = true
       AND ST_DWithin(
              ST_SetSRID(ST_MakePoint(az.lng, az.lat), 4326)::geography,
              ST_SetSRID(ST_MakePoint(msg.lng::double precision, msg.lat::double precision), 4326)::geography,
              az.radius * 1000
            )
),
inserted AS (
    INSERT INTO notifications
        (id, type, notifiable_type, notifiable_id, data, message_id, push_required, created_at, updated_at)
    SELECT
        gen_random_uuid(),
        'App\Notifications\PagerNotification',
        'App\Models\User',
        r.user_id,
        json_build_object(
            'id',         msg.id,
            'system_id',  msg.system_id,
            'capcode_id', msg.capcode_id,
            'address',    msg.address,
            'message',    msg.message,
            'created_at', msg.created_at,
            'title', COALESCE(
                NULLIF(msg.agency_name, '') || ' | ' || NULLIF(msg.capcode_alias, ''),
                'Capcode ' || msg.address
            ),
            'body', msg.message
        )::text,
        msg.id,
        true,
        NOW(), NOW()
      FROM recipients r, msg
     WHERE NOT EXISTS (
            SELECT 1 FROM notifications n
             WHERE n.message_id = msg.id AND n.notifiable_id = r.user_id
           )
       -- One push per incident message per user: a dispatch that went to
       -- several capcodes this user follows is notified once. Copies of
       -- the same dispatch share an incident_key (see handler.IncidentKey).
       AND NOT (msg.incident_key IS NOT NULL AND EXISTS (
            SELECT 1 FROM notifications n2
              JOIN messages m2 ON m2.id = n2.message_id
             WHERE n2.notifiable_id = r.user_id
               AND n2.notifiable_type = 'App\Models\User'
               AND m2.id <> msg.id
               AND m2.incident_key = msg.incident_key
               AND m2.created_at > msg.created_at - INTERVAL '15 minutes'
           ))
       -- A copy the receiver had to repair heavily (anything unrepairable
       -- or cut short, or half its codewords corrected), arriving within
       -- three minutes of another page on the same capcode, is a re-send
       -- whose damage defeated the dedupe. It stays in the feed; nobody is
       -- woken for it.
       AND NOT (
            msg.quality IS NOT NULL
            AND (COALESCE((msg.quality->>'uncorrectable')::int, 0) > 0
                 OR COALESCE((msg.quality->>'truncated')::bool, false)
                 OR COALESCE((msg.quality->>'corrected')::int, 0) * 2
                    >= GREATEST(COALESCE((msg.quality->>'codewords')::int, 0), 1))
            AND EXISTS (
                SELECT 1 FROM messages m3
                 WHERE m3.address = msg.address
                   AND m3.id <> msg.id
                   AND m3.deleted_at IS NULL
                   AND m3.created_at BETWEEN msg.created_at - INTERVAL '3 minutes' AND msg.created_at
           ))
    RETURNING notifiable_id
),
marked AS (
    UPDATE messages
       SET fanned_out_at = NOW()
     WHERE id = $2
       AND (SELECT got FROM locked)
       AND fanned_out_at IS NULL
    RETURNING 1
)
SELECT notifiable_id FROM inserted;
`

// FanoutWorker turns "a new message arrived" events into notification rows.
// Multiple pods can run a FanoutWorker — pg advisory locks ensure only one
// pod actually fans out for any given message.
type FanoutWorker struct {
	db     *sqlx.DB
	wakeCh <-chan string
}

func NewFanoutWorker(db *sqlx.DB, wakeCh <-chan string) *FanoutWorker {
	return &FanoutWorker{db: db, wakeCh: wakeCh}
}

func (w *FanoutWorker) Run(ctx context.Context) {
	slog.Info("fanout worker started")
	defer slog.Info("fanout worker stopped")

	// Catch up anything that arrived while the worker was down before we
	// trust the LISTEN stream.
	w.reconcile(ctx)

	tick := time.NewTicker(reconcileWindow / 60) // ~60s
	defer tick.Stop()

	for {
		select {
		case payload, ok := <-w.wakeCh:
			if !ok {
				return
			}
			id, err := strconv.ParseInt(payload, 10, 64)
			if err != nil {
				slog.Error("fanout: bad payload", "payload", payload, "error", err)
				continue
			}
			w.processMessage(ctx, id)
		case <-tick.C:
			w.reconcile(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// processMessage fans out a single message. Idempotent: a no-op if another
// pod is already on it (advisory lock not granted) or recipients already
// exist (NOT EXISTS subquery).
func (w *FanoutWorker) processMessage(ctx context.Context, messageID int64) {
	rows, err := w.db.QueryxContext(ctx, fanoutSQL, fanoutLockKey, messageID)
	if err != nil {
		slog.Error("fanout: query failed", "message_id", messageID, "error", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		slog.Error("fanout: scan failed", "message_id", messageID, "error", err)
		return
	}
	slog.Info("fanout: processed", "message_id", messageID, "recipients", count)
}

// reconcile catches messages that didn't trigger a NOTIFY we received
// (typically because all pods were restarting). Runs on a slow tick; the
// partial-index `idx_messages_fanout_pending` keeps this cheap when there's
// nothing to do.
//
// A message is considered done as soon as `fanned_out_at` is set — recipient
// count is irrelevant. Without that flag, messages with zero eligible Pro
// subscribers would be re-processed every minute forever.
func (w *FanoutWorker) reconcile(ctx context.Context) {
	const q = `
		SELECT m.id
		  FROM messages m
		 WHERE m.fanned_out_at IS NULL
		   AND m.deleted_at IS NULL
		   AND m.created_at > NOW() - $1::interval
		   AND (m.capcode_id IS NOT NULL OR (m.lat IS NOT NULL AND m.lng IS NOT NULL))
		 ORDER BY m.id
		 LIMIT 500
	`
	var ids []int64
	if err := w.db.SelectContext(ctx, &ids, q, reconcileWindow.String()); err != nil {
		slog.Error("fanout: reconcile select failed", "error", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	slog.Info("fanout: reconciling orphans", "count", len(ids))
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		w.processMessage(ctx, id)
	}
}
