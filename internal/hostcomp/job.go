package hostcomp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// Kind is a comp transition.
type Kind string

const (
	// Started: host_comp_since went from NULL to now.
	Started Kind = "started"
	// Lapsed: host_comp_until passed while host_comp_since was still set;
	// host_comp_since has been cleared.
	Lapsed Kind = "lapsed"
)

// Transition is one user starting or losing the comp, with what the push and
// email need. SystemID is the receiver the email is about (0 when the user
// has none left), and Conversation its Libredesk support thread ("" = none).
// Supporter: the user supports that receiver rather than owning it, and
// Conversation is their own thread, never the owner's (which holds the
// install command).
type Transition struct {
	Kind         Kind   `db:"-"`
	UserID       int64  `db:"user_id"`
	Email        string `db:"email"`
	Name         string `db:"name"`
	SystemID     int64  `db:"system_id"`
	SystemName   string `db:"system_name"`
	Conversation string `db:"conversation"`
	Supporter    bool   `db:"supporter"`
}

// Notifier pushes the transitions. *notify.Notifier implements it. A
// supporter's lapse push is the owner's: it names the receiver either way.
type Notifier interface {
	NotifyHostCompStarted(ctx context.Context, userID int64, systemName string)
	NotifyHostCompSupporterStarted(ctx context.Context, userID int64, systemName string)
	NotifyHostCompLapsed(ctx context.Context, userID int64, systemName string)
}

// Mailer emails the transitions: a reply in conversationUUID when set,
// otherwise a new conversation, whose uuid is returned.
// *mail.LibredeskApplicationMailer implements it.
type Mailer interface {
	SendHostCompStarted(ctx context.Context, conversationUUID, to, name, systemName string) (string, error)
	SendHostCompLapsed(ctx context.Context, conversationUUID, to, name, systemName string) (string, error)
	SendHostCompSupporterStarted(ctx context.Context, conversationUUID, to, name, systemName string) (string, error)
	SendHostCompSupporterLapsed(ctx context.Context, conversationUUID, to, name, systemName string) (string, error)
}

const (
	// applyTimeout bounds one evaluation + update transaction.
	applyTimeout = 5 * time.Minute
	// notifyTimeout bounds the push and email for one transition.
	notifyTimeout = 30 * time.Second
)

// Job grants, extends and lapses the comp. Either collaborator may be nil
// (push or email not configured), and that side is skipped.
type Job struct {
	db       *sqlx.DB
	notifier Notifier
	mailer   Mailer

	// now is the clock; tests replace it.
	now        func() time.Time
	interval   time.Duration
	startDelay time.Duration

	notifying sync.WaitGroup
}

func NewJob(db *sqlx.DB, notifier Notifier, mailer Mailer) *Job {
	return &Job{db: db, notifier: notifier, mailer: mailer, now: time.Now, interval: Interval, startDelay: StartDelay}
}

// WithClock replaces the job's clock (tests).
func (j *Job) WithClock(now func() time.Time) *Job {
	j.now = now
	return j
}

// Run applies the comp shortly after boot and then every Interval until ctx
// is cancelled. Notifications go out after each commit, in the background.
func (j *Job) Run(ctx context.Context) {
	t := time.NewTimer(j.startDelay)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.tick(ctx)
			t.Reset(j.interval)
		}
	}
}

func (j *Job) tick(ctx context.Context) {
	applyCtx, cancel := context.WithTimeout(ctx, applyTimeout)
	transitions, err := j.Apply(applyCtx)
	cancel()
	if err != nil {
		slog.Error("host comp: run failed", "error", err)
		return
	}
	if len(transitions) == 0 {
		return
	}
	j.notifying.Add(1)
	go func() {
		defer j.notifying.Done()
		for _, t := range transitions {
			// Detached: shutting down must not cut a push or email off
			// half-sent, and each gets its own bound.
			nctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
			j.Notify(nctx, t)
			cancel()
		}
	}()
}

// Wait blocks until notifications already handed to the background are done.
func (j *Job) Wait() { j.notifying.Wait() }

// Apply evaluates every receiver and, in one transaction, extends the comp
// for qualifying users and lapses it for the rest whose comp has run out.
// It returns the start and lapse transitions; the caller notifies after it
// returns (the transaction is committed by then). When another server is
// already running it, it does nothing.
func (j *Job) Apply(ctx context.Context) ([]Transition, error) {
	now := j.now().UTC()
	tx, err := j.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// One runner across servers: a second one would only repeat the
	// extension, but it could also announce the same transition twice.
	var got bool
	if err := tx.GetContext(ctx, &got, `SELECT pg_try_advisory_xact_lock(hashtextextended('host_comp', 0))`); err != nil {
		return nil, fmt.Errorf("locking host comp: %w", err)
	}
	if !got {
		return nil, nil
	}

	receivers, err := evaluate(ctx, tx, now)
	if err != nil {
		return nil, err
	}
	if receivers, err = withSupporters(ctx, tx, receivers); err != nil {
		return nil, err
	}
	qualifying := qualifyingReceivers(receivers)

	var transitions []Transition

	if len(qualifying) > 0 {
		ids := make([]int64, 0, len(qualifying))
		for id := range qualifying {
			ids = append(ids, id)
		}
		// GREATEST never shortens a comp; started is decided on the row as
		// locked, so a NULL host_comp_since is the start transition.
		var extended []struct {
			UserID  int64  `db:"id"`
			Email   string `db:"email"`
			Name    string `db:"name"`
			Started bool   `db:"started"`
		}
		if err := tx.SelectContext(ctx, &extended, `
			WITH prev AS (
				SELECT id, host_comp_since FROM users WHERE id = ANY($1) FOR UPDATE
			)
			UPDATE users u
			   SET host_comp_until = GREATEST(u.host_comp_until, $2),
			       host_comp_since = COALESCE(prev.host_comp_since, $3)
			  FROM prev
			 WHERE u.id = prev.id
			RETURNING u.id, u.email, COALESCE(u.name, '') AS name, prev.host_comp_since IS NULL AS started`,
			pq.Array(ids), now.Add(Grace), now); err != nil {
			return nil, fmt.Errorf("extending host comps: %w", err)
		}
		for _, e := range extended {
			if !e.Started {
				continue
			}
			r := qualifying[e.UserID]
			transitions = append(transitions, Transition{
				Kind: Started, UserID: e.UserID, Email: e.Email, Name: e.Name,
				SystemID: r.SystemID, SystemName: r.Name, Conversation: r.Conversation, Supporter: r.Supporter,
			})
		}
	}

	// Lapse: the comp ran out and nobody re-qualified it above (those were
	// just pushed a week ahead). host_comp_until is kept as the record of
	// when it ended. The email is about one of their own receivers if they
	// have any, else one they support (in their own thread).
	var lapsed []Transition
	if err := tx.SelectContext(ctx, &lapsed, `
		WITH lapsed AS (
			UPDATE users
			   SET host_comp_since = NULL
			 WHERE host_comp_since IS NOT NULL
			   AND (host_comp_until IS NULL OR host_comp_until <= $1)
			RETURNING id, email, COALESCE(name, '') AS name
		)
		SELECT l.id AS user_id, l.email, l.name,
		       COALESCE(s.id, 0) AS system_id, COALESCE(s.name, '') AS system_name,
		       COALESCE(s.conversation, '') AS conversation, COALESCE(s.supporter, false) AS supporter
		  FROM lapsed l
		  LEFT JOIN LATERAL (
			SELECT id, name, conversation, supporter
			  FROM (
				SELECT id, name, COALESCE(support_conversation_uuid, '') AS conversation, false AS supporter, approved_at
				  FROM systems
				 WHERE user_id = l.id AND approved_at IS NOT NULL
				UNION ALL
				SELECT s.id, s.name, COALESCE(sp.conversation_uuid, '') AS conversation, true AS supporter, s.approved_at
				  FROM system_supporters sp
				  JOIN systems s ON s.id = sp.system_id
				 WHERE sp.user_id = l.id AND s.approved_at IS NOT NULL AND s.user_id IS DISTINCT FROM l.id
			  ) c
			 ORDER BY supporter, (conversation <> '') DESC, approved_at DESC, id DESC
			 LIMIT 1
		  ) s ON true
		 ORDER BY l.id`, now); err != nil {
		return nil, fmt.Errorf("lapsing host comps: %w", err)
	}
	for i := range lapsed {
		lapsed[i].Kind = Lapsed
	}
	transitions = append(transitions, lapsed...)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing host comps: %w", err)
	}
	slog.Info("host comp: applied", "qualifying_users", len(qualifying), "transitions", len(transitions))
	return transitions, nil
}

// evaluate loads every approved, non-deleted receiver that has an owner and
// stats history, works out its window, and measures its uptime and decodes.
func evaluate(ctx context.Context, q sqlx.QueryerContext, now time.Time) ([]*receiver, error) {
	var all []*receiver
	if err := sqlx.SelectContext(ctx, q, &all, `
		SELECT s.id AS system_id, s.user_id, s.name,
		       COALESCE(s.support_conversation_uuid, '') AS conversation,
		       s.approved_at,
		       (SELECT st.ts FROM system_stats st
		         WHERE st.system_id = s.id AND st.ts <= $1
		         ORDER BY st.ts LIMIT 1) AS first_stats_at
		  FROM systems s
		  JOIN users u ON u.id = s.user_id
		 WHERE s.approved_at IS NOT NULL
		   AND s.deleted_at IS NULL
		   AND s.user_id IS NOT NULL`, now); err != nil {
		return nil, fmt.Errorf("loading receivers: %w", err)
	}

	var (
		measured     []*receiver
		ids          []int64
		starts, ends []int64
		bySystem     = map[int64]*receiver{}
	)
	for _, r := range all {
		if r.FirstStatsAt == nil {
			continue
		}
		w, ok := receiverWindow(now, *r.FirstStatsAt)
		if !ok {
			continue
		}
		r.window = w
		measured = append(measured, r)
		bySystem[r.SystemID] = r
		ids, starts, ends = append(ids, r.SystemID), append(starts, w.start), append(ends, w.end)
	}
	if len(measured) == 0 {
		return nil, nil
	}

	var rows []struct {
		SystemID  int64 `db:"system_id"`
		Connected int64 `db:"connected"`
		Decoded   bool  `db:"decoded"`
	}
	if err := sqlx.SelectContext(ctx, q, &rows, measureSQL,
		pq.Array(ids), pq.Array(starts), pq.Array(ends), bucketSeconds, now); err != nil {
		return nil, fmt.Errorf("measuring receivers: %w", err)
	}
	for _, row := range rows {
		if r := bySystem[row.SystemID]; r != nil {
			r.connected, r.decoded = row.Connected, row.Decoded
		}
	}
	return measured, nil
}

// withSupporters adds an entry for each supporter of the measured receivers:
// the same receiver and measurement, with the supporter as the user and
// their own thread as the conversation. An owner listed as a supporter of
// their own receiver adds nothing.
func withSupporters(ctx context.Context, q sqlx.QueryerContext, receivers []*receiver) ([]*receiver, error) {
	if len(receivers) == 0 {
		return receivers, nil
	}
	bySystem := make(map[int64]*receiver, len(receivers))
	ids := make([]int64, 0, len(receivers))
	for _, r := range receivers {
		bySystem[r.SystemID] = r
		ids = append(ids, r.SystemID)
	}
	var rows []struct {
		SystemID     int64  `db:"system_id"`
		UserID       int64  `db:"user_id"`
		Conversation string `db:"conversation"`
	}
	if err := sqlx.SelectContext(ctx, q, &rows, `
		SELECT system_id, user_id, COALESCE(conversation_uuid, '') AS conversation
		  FROM system_supporters
		 WHERE system_id = ANY($1)`, pq.Array(ids)); err != nil {
		return nil, fmt.Errorf("loading supporters: %w", err)
	}
	out := receivers
	for _, row := range rows {
		r := bySystem[row.SystemID]
		if r == nil || row.UserID == r.UserID {
			continue
		}
		s := *r
		s.UserID, s.Conversation, s.Supporter = row.UserID, row.Conversation, true
		out = append(out, &s)
	}
	return out, nil
}

// measureSQL counts, per receiver window [start, end) (unix seconds, bucket
// aligned), the distinct Bucket-long buckets holding at least one
// system_stats row, and whether page_receptions has a page from it between
// the window start and now.
const measureSQL = `
	SELECT w.system_id,
	       (SELECT COUNT(DISTINCT floor(extract(epoch FROM st.ts) / $4))
	          FROM system_stats st
	         WHERE st.system_id = w.system_id
	           AND st.ts >= to_timestamp(w.start_s)
	           AND st.ts <  to_timestamp(w.end_s)) AS connected,
	       EXISTS (SELECT 1 FROM page_receptions pr
	                WHERE pr.system_id = w.system_id
	                  AND pr.heard_at >= to_timestamp(w.start_s)
	                  AND pr.heard_at <= $5) AS decoded
	  FROM unnest($1::bigint[], $2::bigint[], $3::bigint[]) AS w(system_id, start_s, end_s)`

// Notify pushes and emails one transition. Best-effort: failures are logged.
// A new conversation is stored where the transition's thread lives: on the
// receiver for its owner (when it had none), on the supporter's row for a
// supporter.
func (j *Job) Notify(ctx context.Context, t Transition) {
	if j.notifier != nil {
		switch {
		case t.Kind == Started && t.Supporter:
			j.notifier.NotifyHostCompSupporterStarted(ctx, t.UserID, t.SystemName)
		case t.Kind == Started:
			j.notifier.NotifyHostCompStarted(ctx, t.UserID, t.SystemName)
		case t.Kind == Lapsed:
			j.notifier.NotifyHostCompLapsed(ctx, t.UserID, t.SystemName)
		}
	}
	if j.mailer == nil {
		return
	}
	send := j.mailer.SendHostCompStarted
	switch {
	case t.Supporter && t.Kind == Lapsed:
		send = j.mailer.SendHostCompSupporterLapsed
	case t.Supporter:
		send = j.mailer.SendHostCompSupporterStarted
	case t.Kind == Lapsed:
		send = j.mailer.SendHostCompLapsed
	}
	conversation, err := send(ctx, t.Conversation, t.Email, t.Name, t.SystemName)
	if err != nil {
		slog.Warn("host comp: email failed", "kind", t.Kind, "supporter", t.Supporter, "user_id", t.UserID, "system_id", t.SystemID, "conversation", t.Conversation, "error", err)
		return
	}
	if conversation == t.Conversation || t.SystemID == 0 {
		return
	}
	// Only fill an empty slot: an application email may have started a
	// thread on this receiver in the meantime, and a supporter's may have
	// been started by an earlier run.
	store := `UPDATE systems SET support_conversation_uuid = $1
	           WHERE id = $2 AND COALESCE(support_conversation_uuid, '') = $3`
	args := []any{conversation, t.SystemID, t.Conversation}
	if t.Supporter {
		store = `UPDATE system_supporters SET conversation_uuid = $1
		          WHERE system_id = $2 AND COALESCE(conversation_uuid, '') = $3 AND user_id = $4`
		args = append(args, t.UserID)
	}
	if _, err := j.db.ExecContext(ctx, store, args...); err != nil {
		slog.Warn("host comp: storing conversation failed", "system_id", t.SystemID, "supporter", t.Supporter, "conversation", conversation, "error", err)
	}
}
