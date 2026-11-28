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
	"github.com/lib/pq"
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
	// fanoutStatementTimeout bounds one fan-out. It normally takes
	// milliseconds; a pathological plan must fail loudly and leave the page
	// to reconcile rather than hold up every page behind it for minutes.
	fanoutStatementTimeout = "30s"
	// maxPushAge is how long a page may have waited since it was stored
	// for fan-out to still wake anyone. Longer means fan-out was stalled or down
	// (it normally runs within a second, or holdMax for a held copy): the
	// page stays in the feed but is marked push_skip_reason 'late', because
	// a turnout pushed long after the event misleads more than it helps.
	maxPushAge = 15 * time.Minute
	// timeoutBackoff is how long a page whose fan-out hit the statement
	// timeout waits before its one retry; a second timeout gives it up as
	// push_skip_reason 'timeout'. Retrying every minute would keep holding
	// up the pages behind it.
	timeoutBackoff = 2 * time.Minute
)

// timeoutState tracks a page whose fan-out timed out.
type timeoutState struct {
	count   int
	retryAt time.Time
}

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
-- Who this incident has already woken: the users notified for the other
-- copies of the same dispatch stored from 15 minutes before this one.
-- Worked out once, from the incident's handful of messages and the
-- notifications of those messages. Asked per recipient instead, the
-- planner may walk each recipient's whole notification history (tens of
-- thousands of rows each); a statistics refresh flipped it to that on
-- 2026-11-27 and fan-out took minutes per page.
already_notified AS MATERIALIZED (
    SELECT DISTINCT n2.notifiable_id
      FROM msg
      JOIN messages m2 ON m2.incident_key = msg.incident_key
                      AND m2.id <> msg.id
                      AND m2.created_at > msg.created_at - INTERVAL '15 minutes'
      JOIN notifications n2 ON n2.message_id = m2.id
                           AND n2.notifiable_type = 'App\Models\User'
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
       AND NOT EXISTS (SELECT 1 FROM already_notified a WHERE a.notifiable_id = r.user_id)
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
	// timeouts are pages whose fan-out hit the statement timeout. Only the
	// Run goroutine touches it.
	timeouts         map[int64]timeoutState
	statementTimeout string
}

func NewFanoutWorker(db *sqlx.DB, wakeCh <-chan string) *FanoutWorker {
	return &FanoutWorker{db: db, wakeCh: wakeCh, held: map[int64]time.Time{}, now: time.Now, replays: pagetext.Replays,
		timeouts: map[int64]timeoutState{}, statementTimeout: fanoutStatementTimeout}
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
	Address   string    `db:"address"`
	Message   string    `db:"message"`
	CreatedAt time.Time `db:"created_at"`
	// StoredAt is when the server last stored the page (inserted, or a
	// better copy replaced it): what fan-out's own delay is measured from.
	StoredAt  time.Time      `db:"stored_at"`
	Quality   sql.NullString `db:"quality"`
	FannedOut bool           `db:"fanned_out"`
}

const candidateSQL = `
SELECT address, message, created_at, updated_at AS stored_at, quality::text AS quality,
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
	if ts, ok := w.timeouts[messageID]; ok && now.Before(ts.retryAt) {
		return
	}
	// A receiver clock running fast must not make a page look later than
	// it is: never count it as heard after now.
	heard := c.CreatedAt
	if heard.After(now) {
		heard = now
	}
	// Measured from when it was stored, not heard: a receiver delivering
	// its buffer after a network drop isn't fan-out running late.
	if now.Sub(c.StoredAt) > maxPushAge {
		delete(w.held, messageID)
		// Already pushed (a better copy reopened it) keeps its record;
		// otherwise say why nobody was woken.
		if _, err := w.db.ExecContext(ctx,
			`UPDATE messages m
			    SET fanned_out_at = NOW(),
			        push_skip_reason = CASE WHEN EXISTS (SELECT 1 FROM notifications n WHERE n.message_id = m.id)
			                                THEN push_skip_reason ELSE 'late' END
			  WHERE id = $1 AND fanned_out_at IS NULL
			    AND updated_at < $2`, messageID, now.Add(-maxPushAge)); err != nil {
			slog.Error("fanout: marking late page failed", "message_id", messageID, "error", err)
			return
		}
		slog.Warn("fanout: page too old to push", "message_id", messageID, "address", c.Address,
			"waited", now.Sub(c.StoredAt).Round(time.Second).String())
		return
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
		// A message fan-out reopened (a better copy arrived) may already
		// have been pushed, from a first copy whose header couldn't be
		// read: then it is a replay recognised too late, not "not pushed",
		// and its earlier hold stays on record.
		if _, err := w.db.ExecContext(ctx,
			`UPDATE messages m
			    SET fanned_out_at = NOW(),
			        push_skip_reason = CASE WHEN EXISTS (SELECT 1 FROM notifications n WHERE n.message_id = m.id)
			                                THEN 'replay_after_push' ELSE 'replay' END,
			        push_lag_s = $2,
			        push_hold_ms = COALESCE(NULLIF($3, 0), push_hold_ms)
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
	if timedOut := w.fanOut(ctx, messageID, heldFor, staleLag); !timedOut {
		delete(w.timeouts, messageID)
		return
	}
	ts := w.timeouts[messageID]
	ts.count++
	if ts.count < 2 {
		ts.retryAt = now.Add(timeoutBackoff)
		w.timeouts[messageID] = ts
		slog.Error("fanout: timed out, retrying later", "message_id", messageID, "timeout", w.statementTimeout, "retry_in", timeoutBackoff.String())
		return
	}
	delete(w.timeouts, messageID)
	if _, err := w.db.ExecContext(ctx,
		`UPDATE messages m
		    SET fanned_out_at = NOW(),
		        push_skip_reason = CASE WHEN EXISTS (SELECT 1 FROM notifications n WHERE n.message_id = m.id)
		                                THEN push_skip_reason ELSE 'timeout' END
		  WHERE id = $1 AND fanned_out_at IS NULL`, messageID); err != nil {
		slog.Error("fanout: giving up timed-out page failed", "message_id", messageID, "error", err)
		return
	}
	slog.Error("fanout: timed out twice, page not pushed", "message_id", messageID, "address", c.Address)
}

// fanOut runs fanoutSQL for one message in a transaction that also records
// how long it was held and, for a stale page pushed because no replay was
// corroborated, how late it was heard. It reports whether the statement
// timeout cut it off.
func (w *FanoutWorker) fanOut(ctx context.Context, messageID int64, heldFor, staleLag time.Duration) (timedOut bool) {
	tx, err := w.db.BeginTxx(ctx, nil)
	if err != nil {
		slog.Error("fanout: begin failed", "message_id", messageID, "error", err)
		return false
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '`+fanoutLockTimeout+`'; SET LOCAL statement_timeout = '`+w.statementTimeout+`'`); err != nil {
		slog.Error("fanout: lock timeout failed", "message_id", messageID, "error", err)
		return false
	}
	rows, err := tx.QueryxContext(ctx, fanoutSQL, fanoutLockKey, messageID)
	if err != nil {
		if isStatementTimeout(err) {
			return true
		}
		slog.Error("fanout: query failed", "message_id", messageID, "error", err)
		return false
	}
	count := 0
	for rows.Next() {
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		if isStatementTimeout(err) {
			return true
		}
		slog.Error("fanout: scan failed", "message_id", messageID, "error", err)
		return false
	}
	if heldFor > 0 || staleLag > 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE messages SET push_hold_ms = COALESCE(NULLIF($2, 0), push_hold_ms),
			        push_lag_s = COALESCE(NULLIF($3, 0), push_lag_s) WHERE id = $1`,
			messageID, int(heldFor.Milliseconds()), int(staleLag.Seconds())); err != nil {
			slog.Error("fanout: recording hold failed", "message_id", messageID, "error", err)
			return false
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("fanout: commit failed", "message_id", messageID, "error", err)
		return false
	}
	slog.Info("fanout: processed", "message_id", messageID, "recipients", count)
	return false
}

// isStatementTimeout reports whether err is Postgres cancelling a statement
// for statement_timeout (SQLSTATE 57014).
func isStatementTimeout(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "57014"
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
