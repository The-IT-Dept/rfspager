package dispatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/hostcomp"
	"github.com/the-it-dept/rfspager/internal/pagetext"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// fanoutLockKey is the namespace passed to pg_try_advisory_xact_lock alongside
// the message_id so cross-pod fan-out is mutually exclusive per message.
const fanoutLockKey = int32(7423100)

// A damaged copy is held back from fan-out for up to holdMax so that the
// other receivers' copies of the same page, which reach the dedupe within a
// second or two, can replace it first: the push then carries the least
// damaged copy stored when the hold ends, or goes out at once if an
// undamaged copy replaces it. A page whose clean header says it is more than
// half an hour old is held the same way while ReplayWatch decides whether
// the network is re-sending backlog; it is late already. Held messages are
// re-checked every holdRecheck. A damaged copy older than
// holdOnlyYoungerThan by its own timestamp (backlog found by reconcile after
// a restart, a receiver catching up) is late already and is not held.
const (
	holdMax             = 30 * time.Second
	holdRecheck         = time.Second
	holdOnlyYoungerThan = 2 * time.Minute
	// fanoutLockTimeout bounds the wait for a better copy's transaction to
	// release the message row; past it the message is left for the next
	// recheck or reconcile instead of stalling every other page.
	fanoutLockTimeout = "5s"
)

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
//
// Recipients need Pro: effective access, a valid store subscription or a
// running receiver-host comp (hostcomp.Access).
var fanoutSQL = `
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
       -- Lock the row: a better copy being stored right now commits first
       -- and this reads its text; one stored later waits for this to
       -- commit and then sees, and corrects, these notification rows.
       FOR NO KEY UPDATE OF m
),
recipients AS (
    SELECT s.user_id
      FROM subscriptions s
      JOIN users u ON u.id = s.user_id
      JOIN msg            ON msg.capcode_id IS NOT NULL
     WHERE s.capcode_id = msg.capcode_id
       AND ` + hostcomp.Access("u") + `

    UNION

    SELECT az.user_id
      FROM alertzones az
      JOIN users u ON u.id = az.user_id
      JOIN msg            ON msg.lat IS NOT NULL AND msg.lng IS NOT NULL
     WHERE ` + hostcomp.Access("u") + `
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
	// refanoutCh carries message ids whose coordinates a better copy
	// corrected after fan-out (NOTIFY rfs_refanout); nil if not listening.
	refanoutCh <-chan string
	// held maps a held message to when fan-out first saw it. Only the Run
	// goroutine touches it.
	held    map[int64]time.Time
	now     func() time.Time
	replays *pagetext.ReplayWatch
}

func NewFanoutWorker(db *sqlx.DB, wakeCh <-chan string) *FanoutWorker {
	return &FanoutWorker{db: db, wakeCh: wakeCh, held: map[int64]time.Time{}, now: time.Now, replays: pagetext.Replays}
}

// ListenRefanout makes the worker fan out again the messages named on ch
// (NOTIFY rfs_refanout). Call before Run. Missed events are still caught by
// reconcile, since the dedupe clears fanned_out_at first.
func (w *FanoutWorker) ListenRefanout(ch <-chan string) {
	w.refanoutCh = ch
}

func (w *FanoutWorker) Run(ctx context.Context) {
	slog.Info("fanout worker started")
	defer slog.Info("fanout worker stopped")

	// Catch up anything that arrived while the worker was down before we
	// trust the LISTEN stream.
	w.reconcile(ctx)

	tick := time.NewTicker(reconcileWindow / 60) // ~60s
	defer tick.Stop()
	holdTick := time.NewTicker(holdRecheck)
	defer holdTick.Stop()

	for {
		select {
		case payload, ok := <-w.wakeCh:
			if !ok {
				return
			}
			w.processPayload(ctx, payload)
		case payload := <-w.refanoutCh: // a nil channel never fires
			w.processPayload(ctx, payload)
		case <-tick.C:
			w.reconcile(ctx)
		case <-holdTick.C:
			w.recheckHeld(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (w *FanoutWorker) processPayload(ctx context.Context, payload string) {
	id, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		slog.Error("fanout: bad payload", "payload", payload, "error", err)
		return
	}
	w.processMessage(ctx, id)
}

// recheckHeld re-runs every held message: each one either goes now (an
// undamaged copy replaced it, or its hold is up) or stays held.
func (w *FanoutWorker) recheckHeld(ctx context.Context) {
	if len(w.held) == 0 {
		return
	}
	ids := make([]int64, 0, len(w.held))
	for id := range w.held {
		ids = append(ids, id)
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		w.processMessage(ctx, id)
	}
}

// fanoutCandidate is what fan-out needs to know about a message before it
// decides whether, and when, to push it.
type fanoutCandidate struct {
	Address   string         `db:"address"`
	Message   string         `db:"message"`
	CreatedAt time.Time      `db:"created_at"`
	Quality   sql.NullString `db:"quality"`
	FannedOut bool           `db:"fanned_out"`
}

const candidateSQL = `
SELECT address, message, created_at, quality::text AS quality,
       fanned_out_at IS NOT NULL AS fanned_out
  FROM messages
 WHERE id = $1 AND deleted_at IS NULL`

// processMessage fans out a single message, or decides not to yet (held) or
// at all (a replayed page). Idempotent: a no-op once the message is fanned
// out, if another pod is already on it (advisory lock not granted), or for
// recipients who already have a notification row.
func (w *FanoutWorker) processMessage(ctx context.Context, messageID int64) {
	var c fanoutCandidate
	err := w.db.GetContext(ctx, &c, candidateSQL, messageID)
	if errors.Is(err, sql.ErrNoRows) {
		delete(w.held, messageID)
		return
	}
	if err != nil {
		slog.Error("fanout: load failed", "message_id", messageID, "error", err)
		return
	}
	if c.FannedOut {
		delete(w.held, messageID)
		return
	}
	quality := decodeQuality(c.Quality)
	now := w.now()
	// A receiver clock running fast must not make a page look later than
	// it is: never count it as heard after now.
	heard := c.CreatedAt
	if heard.After(now) {
		heard = now
	}
	firstSeen, wasHeld := w.held[messageID]
	if !wasHeld {
		firstSeen = now
	}
	holdStart := firstSeen
	if heard.Before(holdStart) {
		holdStart = heard
	}

	damaged := pagetext.Damaged(c.Message, quality) && now.Sub(heard) < holdOnlyYoungerThan
	lag, _, stale := pagetext.StaleHeader(c.Message, quality, heard)
	if stale {
		w.replays.Observe(c.Message, quality, heard, now)
	}
	replay := stale && w.replays.Burst(now)

	// Hold while a better copy may still land or a replay may still show
	// itself; a replay already corroborated doesn't need to wait.
	if (damaged || (stale && !replay)) && now.Sub(holdStart) < holdMax {
		if !wasHeld {
			w.held[messageID] = now
			slog.Info("fanout: holding", "message_id", messageID, "address", c.Address,
				"damaged", damaged, "stale", stale, "quality", c.Quality.String)
		}
		return
	}
	delete(w.held, messageID)
	var heldFor time.Duration
	if wasHeld {
		heldFor = now.Sub(holdStart)
	}

	if replay {
		if _, err := w.db.ExecContext(ctx,
			`UPDATE messages
			    SET fanned_out_at = NOW(), push_skip_reason = 'replay', push_lag_s = $2,
			        push_hold_ms = NULLIF($3, 0)
			  WHERE id = $1 AND fanned_out_at IS NULL`,
			messageID, int(lag.Seconds()), int(heldFor.Milliseconds())); err != nil {
			slog.Error("fanout: marking replayed page failed", "message_id", messageID, "error", err)
			return
		}
		slog.Info("fanout: replayed page not pushed", "message_id", messageID, "address", c.Address,
			"lag", lag.Round(time.Second).String())
		return
	}
	if wasHeld {
		slog.Info("fanout: releasing held message", "message_id", messageID,
			"held", heldFor.Round(100*time.Millisecond).String(),
			"still_damaged", pagetext.Damaged(c.Message, quality), "stale", stale)
	}
	var staleLag time.Duration
	if stale {
		staleLag = lag
	}
	w.fanOut(ctx, messageID, heldFor, staleLag)
}

// fanOut runs fanoutSQL for one message in a transaction that also records
// how long it was held and, for a stale page pushed because no replay was
// corroborated, how late it was heard.
func (w *FanoutWorker) fanOut(ctx context.Context, messageID int64, heldFor, staleLag time.Duration) {
	tx, err := w.db.BeginTxx(ctx, nil)
	if err != nil {
		slog.Error("fanout: begin failed", "message_id", messageID, "error", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '`+fanoutLockTimeout+`'`); err != nil {
		slog.Error("fanout: lock timeout failed", "message_id", messageID, "error", err)
		return
	}
	rows, err := tx.QueryxContext(ctx, fanoutSQL, fanoutLockKey, messageID)
	if err != nil {
		slog.Error("fanout: query failed", "message_id", messageID, "error", err)
		return
	}
	count := 0
	for rows.Next() {
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		slog.Error("fanout: scan failed", "message_id", messageID, "error", err)
		return
	}
	if heldFor > 0 || staleLag > 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE messages SET push_hold_ms = NULLIF($2, 0), push_lag_s = NULLIF($3, 0) WHERE id = $1`,
			messageID, int(heldFor.Milliseconds()), int(staleLag.Seconds())); err != nil {
			slog.Error("fanout: recording hold failed", "message_id", messageID, "error", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("fanout: commit failed", "message_id", messageID, "error", err)
		return
	}
	slog.Info("fanout: processed", "message_id", messageID, "recipients", count)
}

func decodeQuality(s sql.NullString) *rxproto.PageQuality {
	if !s.Valid || s.String == "" || s.String == "null" {
		return nil
	}
	var q rxproto.PageQuality
	if err := json.Unmarshal([]byte(s.String), &q); err != nil {
		return nil
	}
	return &q
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
