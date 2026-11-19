package dispatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/notify"
	"github.com/the-it-dept/rfspager/internal/pagetext"
)

// holdFixture is one subscribed Pro user on one capcode, and a fan-out
// worker on a clock the test moves.
type holdFixture struct {
	conn    *sqlx.DB
	worker  *FanoutWorker
	now     time.Time
	system  int64
	capcode int64
}

func newHoldFixture(t *testing.T) *holdFixture {
	t.Helper()
	conn := fanoutTestDB(t)
	ctx := context.Background()
	userID := mustID(t, conn, `INSERT INTO users (user_id, email, password, subscription_valid) VALUES (gen_random_uuid(), 'sub@example.invalid', '!', true) RETURNING id`)
	systemID := mustID(t, conn, `INSERT INTO systems (system_id, user_id, name, api_key, frequency, steal_capcodes, approved_at, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, 'rx', 'key-rx', 1, '0', NOW(), NOW(), NOW()) RETURNING id`, userID)
	capcode := mustID(t, conn, `INSERT INTO capcodes (address, alias, agency, icon, colour, "ignore") VALUES ('1310104', 'Example', 'Lower Hunter', 'fire', 'red', false) RETURNING id`)
	if _, err := conn.ExecContext(ctx, `INSERT INTO subscriptions (user_id, capcode_id, created_at, updated_at) VALUES ($1, $2, NOW(), NOW())`, userID, capcode); err != nil {
		t.Fatal(err)
	}
	f := &holdFixture{conn: conn, now: time.Now().UTC().Truncate(time.Second), system: systemID, capcode: capcode}
	f.worker = NewFanoutWorker(conn, nil)
	f.worker.now = func() time.Time { return f.now }
	f.worker.replays = pagetext.NewReplayWatch()
	return f
}

func (f *holdFixture) insert(t *testing.T, text string, quality *string) int64 {
	t.Helper()
	return mustID(t, f.conn, `INSERT INTO messages (system_id, capcode_id, address, message, md5, quality, created_at, updated_at)
		VALUES ($1, $2, '1310104', $3, md5($3), $4::jsonb, $5, $5) RETURNING id`, f.system, f.capcode, text, quality, f.now)
}

type messageState struct {
	notified  int
	fanned    bool
	skip      sql.NullString
	lag, hold sql.NullInt64
	body      sql.NullString
}

func (f *holdFixture) state(t *testing.T, id int64) messageState {
	t.Helper()
	var s messageState
	if err := f.conn.QueryRowContext(context.Background(),
		`SELECT (SELECT count(*) FROM notifications WHERE message_id = m.id),
		        m.fanned_out_at IS NOT NULL, m.push_skip_reason, m.push_lag_s, m.push_hold_ms,
		        (SELECT data::jsonb->>'body' FROM notifications WHERE message_id = m.id LIMIT 1)
		   FROM messages m WHERE m.id = $1`, id).
		Scan(&s.notified, &s.fanned, &s.skip, &s.lag, &s.hold, &s.body); err != nil {
		t.Fatal(err)
	}
	return s
}

func str(s string) *string { return &s }

// A damaged copy waits for a better one: replaced by an undamaged copy it is
// pushed at once with the clean text; never replaced, it goes at 30 s with
// what was stored.
func TestFanoutHoldsDamagedCopy(t *testing.T) {
	f := newHoldFixture(t)
	ctx := context.Background()
	damaged := str(`{"codewords":18,"corrected":6,"uncorrectable":2,"damaged":[[12,15]]}`)

	repaired := f.insert(t, "FRINC TYPE: q^C TURNOUT: 102 IC: 100002-17112P26", damaged)
	f.worker.processMessage(ctx, repaired)
	if s := f.state(t, repaired); s.notified != 0 || s.fanned {
		t.Fatalf("a damaged copy is held, got %+v", s)
	}
	f.now = f.now.Add(2 * time.Second)
	if _, err := f.conn.ExecContext(ctx, `UPDATE messages SET message = 'FRINC TYPE: MVA TURNOUT: 102 INC: 100002-17112026',
		quality = '{"codewords":18}' WHERE id = $1`, repaired); err != nil {
		t.Fatal(err)
	}
	f.worker.recheckHeld(ctx)
	s := f.state(t, repaired)
	if s.notified != 1 || s.body.String != "FRINC TYPE: MVA TURNOUT: 102 INC: 100002-17112026" || s.hold.Int64 != 2000 {
		t.Fatalf("released by the clean copy: %+v", s)
	}

	// (Four minutes on, so fanoutSQL's damaged-re-send rule, which needs
	// another page on the capcode in the previous three, stays out of it.)
	f.now = f.now.Add(4 * time.Minute)
	alone := f.insert(t, "FRINC TYPE: q^C TURNOUT: 103 IC: 100003-17112P26", damaged)
	f.worker.processMessage(ctx, alone)
	f.now = f.now.Add(29 * time.Second)
	f.worker.recheckHeld(ctx)
	if s := f.state(t, alone); s.notified != 0 {
		t.Fatalf("still within the hold: %+v", s)
	}
	f.now = f.now.Add(2 * time.Second)
	f.worker.recheckHeld(ctx)
	if s := f.state(t, alone); s.notified != 1 || s.hold.Int64 != 31000 {
		t.Fatalf("pushed when the hold ran out: %+v", s)
	}
	if len(f.worker.held) != 0 {
		t.Fatalf("nothing left held: %v", f.worker.held)
	}

	clean := f.insert(t, "FRINC TYPE: AFA TURNOUT: 104 INC: 100004-17112026", str(`{"codewords":18}`))
	f.worker.processMessage(ctx, clean)
	if s := f.state(t, clean); s.notified != 1 || s.hold.Valid {
		t.Fatalf("a clean copy goes at once: %+v", s)
	}
}

// sydneyHeader writes the RFS header for an instant.
func sydneyHeader(at time.Time) string {
	loc, _ := time.LoadLocation("Australia/Sydney")
	return at.In(loc).Format("2 January 2006 15:04:05")
}

// A page whose clean header is hours old is a replay only when other old
// pages of different ages are arriving with it. On its own (a misread digit,
// a paging clock that is wrong) it is pushed, after waiting for company.
func TestFanoutSuppressesOnlyCorroboratedReplays(t *testing.T) {
	f := newHoldFixture(t)
	ctx := context.Background()
	clean := str(`{"codewords":48,"corrected":1}`)

	lone := f.insert(t, sydneyHeader(f.now.Add(-5*time.Hour))+" LHDO - 26-100050 - Grass - FIRECALL - EXAMPLE RD", clean)
	f.worker.processMessage(ctx, lone)
	if s := f.state(t, lone); s.notified != 0 || s.fanned {
		t.Fatalf("a lone stale page waits for corroboration: %+v", s)
	}
	f.now = f.now.Add(31 * time.Second)
	f.worker.recheckHeld(ctx)
	s := f.state(t, lone)
	if s.notified != 1 || s.skip.Valid || s.lag.Int64 < 5*3600 {
		t.Fatalf("an uncorroborated stale page is pushed, with its lag recorded: %+v", s)
	}

	// The replay: copies of three other old pages arrive at ingest (most of
	// which the dedupe folds into their originals) ...
	for _, ago := range []time.Duration{28 * time.Hour, 15 * time.Hour, 3 * time.Hour} {
		f.worker.replays.Observe(sydneyHeader(f.now.Add(-ago))+" LHDO - 26-100060 - Car fire - EXAMPLE DR", nil, f.now, f.now)
	}
	// ... and one that wasn't stored before reaches fan-out.
	replayed := f.insert(t, sydneyHeader(f.now.Add(-9*time.Hour))+" LHDO - 26-100061 - MVA - EXAMPLE RD", clean)
	f.worker.processMessage(ctx, replayed)
	s = f.state(t, replayed)
	if s.notified != 0 || !s.fanned || s.skip.String != "replay" || s.lag.Int64 < 9*3600 {
		t.Fatalf("a corroborated replay is stored and not pushed: %+v", s)
	}

	// Fan-out reopened (a better copy arrived) on a page already pushed: no
	// new pushes, and labelled a replay recognised too late, not "not pushed".
	if _, err := f.conn.ExecContext(ctx, `UPDATE messages SET fanned_out_at = NULL, push_skip_reason = NULL WHERE id = $1`, lone); err != nil {
		t.Fatal(err)
	}
	f.worker.processMessage(ctx, lone)
	s = f.state(t, lone)
	if s.notified != 1 || !s.fanned || s.skip.String != "replay_after_push" {
		t.Fatalf("an already-pushed page re-decided during a burst: %+v", s)
	}
}

// Fan-out locks the message row: a better copy being stored at that moment
// commits first, and the push carries its text.
func TestFanoutWaitsForReplacementInProgress(t *testing.T) {
	f := newHoldFixture(t)
	ctx := context.Background()
	id := f.insert(t, "FRINC TYPE: AFA TURNOUT: 105 INC: 100005-17112026", str(`{"codewords":18,"corrected":2}`))

	tx, err := f.conn.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET message = 'FRINC TYPE: AFA TURNOUT: 105 INC: 100005-17112026 EXTRA' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		f.worker.fanOut(ctx, id, 0, 0)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("fan-out read the message while a replacement held it")
	case <-time.After(400 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	<-done
	if s := f.state(t, id); s.notified != 1 || s.body.String != "FRINC TYPE: AFA TURNOUT: 105 INC: 100005-17112026 EXTRA" {
		t.Fatalf("push should carry the replacement's text: %+v", s)
	}
}

// markSent turns a push whose text changed in flight into a correction,
// keeping what went out; a correction is sent once; a device that missed the
// send gets an ordinary push again; a correction that can't be delivered
// leaves the original delivery in place.
func TestDispatchCorrectionBookkeeping(t *testing.T) {
	f := newHoldFixture(t)
	ctx := context.Background()
	w := NewDispatchWorker(f.conn, nil, nil)
	msg := f.insert(t, "FRINC TYPE: MVA TURNOUT: 106 INC: 100006-17112026", str(`{"codewords":18}`))
	userID := mustID(t, f.conn, `SELECT id FROM users LIMIT 1`)
	original := `{"id": 1, "title": "t", "message": "FRINC TYPE: q^C", "body": "FRINC TYPE: q^C"}`

	newRow := func() string {
		var id string
		if err := f.conn.QueryRowContext(ctx,
			`INSERT INTO notifications (id, type, notifiable_type, notifiable_id, data, message_id, push_required, push_attempts, push_locked_until, created_at, updated_at)
			 VALUES (gen_random_uuid(), 'App\Notifications\PagerNotification', 'App\Models\User', $1, $2, $3, true, 1, NOW() + interval '2 minutes', NOW(), NOW())
			 RETURNING id::text`, userID, original, msg).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	carry := func(id string, fix bool) { // a better copy lands while the push is in flight
		if _, err := f.conn.ExecContext(ctx,
			`UPDATE notifications SET data = (data::jsonb || jsonb_build_object('message', 'FRINC TYPE: MVA', 'body', 'FRINC TYPE: MVA', 'carried_fix', $2::boolean))::text WHERE id = $1::uuid`, id, fix); err != nil {
			t.Fatal(err)
		}
	}
	type row struct {
		Data     map[string]any
		Pushed   bool
		Failed   bool
		Attempts int
	}
	read := func(id string) row {
		var raw string
		var r row
		if err := f.conn.QueryRowContext(ctx, `SELECT data, pushed_at IS NOT NULL, push_failed_at IS NOT NULL, push_attempts FROM notifications WHERE id = $1::uuid`, id).
			Scan(&raw, &r.Pushed, &r.Failed, &r.Attempts); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal([]byte(raw), &r.Data)
		return r
	}
	sent := func(id, data string, someRetryable bool) int {
		return w.recordOutcomes(ctx,
			[]notify.ClaimedNotification{{ID: id, Data: []byte(data), Attempts: 1}},
			[]notify.DispatchOutcome{{ID: id, Sent: true, SomeRetryable: someRetryable}})
	}

	unchanged := newRow()
	if n := sent(unchanged, original, false); n != 0 || !read(unchanged).Pushed {
		t.Fatal("an unchanged push is simply delivered")
	}

	changed := newRow()
	carry(changed, true)
	if n := sent(changed, original, false); n != 1 {
		t.Fatal("a push whose text changed in flight is queued again")
	}
	r := read(changed)
	if r.Pushed || r.Attempts != 0 || r.Data["body"] != "FRINC TYPE: q^C" || r.Data["correction"] != true ||
		r.Data["correction_body"] != "FRINC TYPE: MVA" || r.Data["first_pushed_at"] == nil {
		t.Fatalf("as a correction keeping what went out: %+v", r)
	}
	// The correction goes out; a further change in flight doesn't earn a second one.
	var claimed string
	if err := f.conn.QueryRowContext(ctx, `SELECT data FROM notifications WHERE id = $1::uuid`, changed).Scan(&claimed); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.ExecContext(ctx, `UPDATE notifications SET data = (data::jsonb || '{"correction_body": "even better"}')::text WHERE id = $1::uuid`, changed); err != nil {
		t.Fatal(err)
	}
	if n := sent(changed, claimed, false); n != 0 || !read(changed).Pushed {
		t.Fatal("a correction is sent once")
	}
	if r := read(changed); r.Data["correction_body"] != "FRINC TYPE: MVA" {
		t.Fatalf("the row records the correction that went out, got %+v", r.Data)
	}

	partial := newRow()
	carry(partial, true)
	if n := sent(partial, original, true); n != 1 {
		t.Fatal("requeued")
	}
	if r := read(partial); r.Data["correction"] != nil || r.Data["body"] != "FRINC TYPE: MVA" {
		t.Fatalf("a device missed the first send: an ordinary push with the new text, got %+v", r)
	}

	// A better copy that is still damaged: no correction; the row records
	// what went out.
	notFix := newRow()
	carry(notFix, false)
	if n := sent(notFix, original, false); n != 0 {
		t.Fatal("a damaged copy landing in flight queues nothing")
	}
	if r := read(notFix); !r.Pushed || r.Data["body"] != "FRINC TYPE: q^C" || r.Data["correction"] != nil {
		t.Fatalf("delivered with the text it carried: %+v", r)
	}

	// markSent waiting on a carry in progress decides from the row the carry
	// commits, not the one it first saw.
	racing := newRow()
	tx, err := f.conn.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM notifications WHERE id = $1::uuid FOR UPDATE`, racing); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE notifications SET data = (data::jsonb || '{"message": "FRINC TYPE: MVA", "body": "FRINC TYPE: MVA", "carried_fix": true}')::text WHERE id = $1::uuid`, racing); err != nil {
		t.Fatal(err)
	}
	requeued := make(chan int)
	go func() { requeued <- sent(racing, original, false) }()
	time.Sleep(300 * time.Millisecond)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := <-requeued; n != 1 {
		t.Fatal("markSent must see the carried fix it waited for and queue the correction")
	}
	if r := read(racing); r.Data["correction"] != true || r.Data["body"] != "FRINC TYPE: q^C" {
		t.Fatalf("correction after the wait: %+v", r)
	}

	failing := newRow()
	carry(failing, true)
	sent(failing, original, false)
	w.recordOutcomes(ctx,
		[]notify.ClaimedNotification{{ID: failing, Attempts: 1}},
		[]notify.DispatchOutcome{{ID: failing, PermanentFail: true, Err: "no push tokens"}})
	if r := read(failing); !r.Pushed || r.Failed || r.Data["correction_failed"] != true {
		t.Fatalf("an undeliverable correction leaves the original delivered, flagged: %+v", r)
	}
}
