package hostcomp_test

import (
	"context"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/hostcomp"
	"github.com/the-it-dept/rfspager/internal/mail"
	"github.com/the-it-dept/rfspager/internal/notify"
)

// The production collaborators satisfy the job's interfaces.
var (
	_ hostcomp.Notifier = (*notify.Notifier)(nil)
	_ hostcomp.Mailer   = (*mail.LibredeskApplicationMailer)(nil)
)

func hostCompTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	rawURL := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(rawURL)
	if err != nil || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "rfspager_test") {
		t.Fatalf("RFSPAGER_TEST_DATABASE_URL must name a rfspager_test* database")
	}
	d, err := db.New(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()
	if _, err := d.Conn().ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d.Conn()
}

type fixture struct {
	t    *testing.T
	conn *sqlx.DB
}

func (f fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.conn.ExecContext(context.Background(), q, args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

func (f fixture) id(q string, args ...any) int64 {
	f.t.Helper()
	var id int64
	if err := f.conn.QueryRowContext(context.Background(), q, args...).Scan(&id); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	return id
}

func (f fixture) user(email string) int64 {
	return f.id(`INSERT INTO users (user_id, name, email, password) VALUES (gen_random_uuid(), $1, $2, '!') RETURNING id`,
		strings.SplitN(email, "@", 2)[0], email)
}

// system adds a receiver. approved/deleted control approved_at/deleted_at.
func (f fixture) system(userID int64, name string, approved, deleted bool, conversation string) int64 {
	var conv any
	if conversation != "" {
		conv = conversation
	}
	return f.id(`INSERT INTO systems (system_id, user_id, name, approved_at, deleted_at, support_conversation_uuid, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2,
		        CASE WHEN $3 THEN NOW() - INTERVAL '30 days' END,
		        CASE WHEN $4 THEN NOW() END, $5, NOW(), NOW()) RETURNING id`,
		userID, name, approved, deleted, conv)
}

// stats writes a stats row every minute over [from, to), skipping any
// minute inside a gap.
func (f fixture) stats(systemID int64, from, to time.Time, gaps ...[2]time.Time) {
	f.t.Helper()
	f.exec(`INSERT INTO system_stats (system_id, ts, stats)
		SELECT $1, g, '{}'::jsonb FROM generate_series($2::timestamptz, $3::timestamptz - INTERVAL '1 second', INTERVAL '1 minute') g`,
		systemID, from, to)
	for _, gap := range gaps {
		f.exec(`DELETE FROM system_stats WHERE system_id = $1 AND ts >= $2 AND ts < $3`, systemID, gap[0], gap[1])
	}
}

func (f fixture) page(systemID int64, at time.Time) {
	f.exec(`INSERT INTO page_receptions (system_id, heard_at, address) VALUES ($1, $2, '1234567')`, systemID, at)
}

// supporter lists userID as a supporter of systemID, with their own comp
// email thread when conversation is set.
func (f fixture) supporter(systemID, userID int64, conversation string) {
	var conv any
	if conversation != "" {
		conv = conversation
	}
	f.exec(`INSERT INTO system_supporters (system_id, user_id, note, conversation_uuid) VALUES ($1, $2, 'test', $3)`, systemID, userID, conv)
}

type comp struct {
	Until *time.Time `db:"host_comp_until"`
	Since *time.Time `db:"host_comp_since"`
}

func (f fixture) comp(userID int64) comp {
	f.t.Helper()
	var c comp
	if err := f.conn.GetContext(context.Background(), &c, `SELECT host_comp_until, host_comp_since FROM users WHERE id = $1`, userID); err != nil {
		f.t.Fatal(err)
	}
	return c
}

type storeSlot struct {
	Valid     bool       `db:"subscription_valid"`
	Expiry    *time.Time `db:"subscription_expiry"`
	Product   *string    `db:"subscription_product_id"`
	Platform  *string    `db:"subscription_platform"`
	WillRenew *bool      `db:"will_renew"`
	UpdatedAt *time.Time `db:"updated_at"`
}

func (f fixture) store(userID int64) storeSlot {
	f.t.Helper()
	var s storeSlot
	if err := f.conn.GetContext(context.Background(), &s, `SELECT subscription_valid, subscription_expiry, subscription_product_id, subscription_platform, will_renew, updated_at FROM users WHERE id = $1`, userID); err != nil {
		f.t.Fatal(err)
	}
	return s
}

func kinds(ts []hostcomp.Transition) map[int64]hostcomp.Kind {
	out := map[int64]hostcomp.Kind{}
	for _, t := range ts {
		out[t.UserID] = t.Kind
	}
	return out
}

func sameTime(a *time.Time, b time.Time) bool { return a != nil && a.Equal(b) }

// The whole lifecycle against Postgres: who qualifies, start, extend, lapse,
// re-qualify, and the store slot never being written.
func TestJobLifecycle(t *testing.T) {
	conn := hostCompTestDB(t)
	f := fixture{t, conn}
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	clock := t0
	job := hostcomp.NewJob(conn, nil, nil).WithClock(func() time.Time { return clock })

	since := t0.Add(-5 * 24 * time.Hour)
	full := func(sys int64) { f.stats(sys, since, t0); f.page(sys, t0.Add(-time.Hour)) }

	// A: free user, one healthy receiver → starts.
	userA := f.user("a@example.invalid")
	sysA := f.system(userA, "Budgewoi", true, false, "conv-a")
	full(sysA)

	// B: paying Apple subscriber with a healthy receiver → comp starts too,
	// store slot untouched.
	userB := f.user("b@example.invalid")
	storeExpiry := t0.Add(200 * 24 * time.Hour).Truncate(time.Second)
	f.exec(`UPDATE users SET subscription_valid = true, subscription_expiry = $2, subscription_product_id = 'io.devlan.rfspager.yearly',
		subscription_platform = 'apple', will_renew = true, updated_at = '2026-01-01T00:00:00Z' WHERE id = $1`, userB, storeExpiry)
	full(f.system(userB, "Figtree", true, false, ""))
	storeB := f.store(userB)

	// C: unapproved receiver. D: deleted receiver. E: receiver 20 h old.
	// F: online but never decoded a page. G: 85% uptime.
	userC := f.user("c@example.invalid")
	full(f.system(userC, "Pending", false, false, ""))
	userD := f.user("d@example.invalid")
	full(f.system(userD, "Gone", true, true, ""))
	userE := f.user("e@example.invalid")
	sysE := f.system(userE, "New", true, false, "")
	f.stats(sysE, t0.Add(-20*time.Hour), t0)
	f.page(sysE, t0.Add(-time.Hour))
	userF := f.user("f@example.invalid")
	f.stats(f.system(userF, "Quiet", true, false, ""), since, t0)
	userG := f.user("g@example.invalid")
	sysG := f.system(userG, "Flaky", true, false, "")
	f.stats(sysG, since, t0, [2]time.Time{t0.Add(-40 * time.Hour), t0.Add(-40*time.Hour + 11*time.Hour)}) // 11 of 72 h missing
	f.page(sysG, t0.Add(-time.Hour))

	// H: two receivers, only the second qualifies.
	userH := f.user("h@example.invalid")
	sysH1 := f.system(userH, "Down", true, false, "conv-h1")
	f.stats(sysH1, since, t0.Add(-48*time.Hour))
	f.page(sysH1, t0.Add(-50*time.Hour))
	sysH2 := f.system(userH, "Up", true, false, "")
	full(sysH2)

	// I: receiver 30 h old at ~95%: judged on 30 h, qualifies.
	userI := f.user("i@example.invalid")
	sysI := f.system(userI, "Young", true, false, "")
	f.stats(sysI, t0.Add(-30*time.Hour), t0, [2]time.Time{t0.Add(-10 * time.Hour), t0.Add(-10*time.Hour + 90*time.Minute)})
	f.page(sysI, t0.Add(-2*time.Hour))

	// J: already comped long ago and the comp ran out while the job was
	// down, but the receiver still qualifies: extended, no lapse/start.
	userJ := f.user("j@example.invalid")
	full(f.system(userJ, "Steady", true, false, ""))
	jSince := t0.Add(-60 * 24 * time.Hour)
	f.exec(`UPDATE users SET host_comp_since = $2, host_comp_until = $3 WHERE id = $1`, userJ, jSince, t0.Add(-time.Hour))

	// 1. Start.
	ts, err := job.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := kinds(ts)
	if len(got) != 4 || got[userA] != hostcomp.Started || got[userB] != hostcomp.Started || got[userH] != hostcomp.Started || got[userI] != hostcomp.Started {
		t.Fatalf("transitions = %+v", ts)
	}
	for _, tr := range ts {
		switch tr.UserID {
		case userA:
			if tr.SystemID != sysA || tr.SystemName != "Budgewoi" || tr.Conversation != "conv-a" || tr.Email != "a@example.invalid" || tr.Name != "a" {
				t.Errorf("A transition = %+v", tr)
			}
		case userH:
			if tr.SystemID != sysH2 || tr.Conversation != "" {
				t.Errorf("H transition is about the wrong receiver: %+v", tr)
			}
		}
	}
	for _, u := range []int64{userA, userB, userH, userI} {
		if c := f.comp(u); !sameTime(c.Until, t0.Add(hostcomp.Grace)) || !sameTime(c.Since, t0) {
			t.Errorf("user %d comp = %+v, want until %v since %v", u, c, t0.Add(hostcomp.Grace), t0)
		}
	}
	for name, u := range map[string]int64{"unapproved": userC, "deleted": userD, "new": userE, "no pages": userF, "85%": userG} {
		if c := f.comp(u); c.Until != nil || c.Since != nil {
			t.Errorf("%s receiver's owner was comped: %+v", name, c)
		}
	}
	if c := f.comp(userJ); !sameTime(c.Until, t0.Add(hostcomp.Grace)) || !sameTime(c.Since, jSince) {
		t.Errorf("J comp = %+v, want extended with since kept", c)
	}
	if after := f.store(userB); after.Valid != storeB.Valid || !sameTime(after.Expiry, *storeB.Expiry) || *after.Product != *storeB.Product ||
		*after.Platform != *storeB.Platform || *after.WillRenew != *storeB.WillRenew || !sameTime(after.UpdatedAt, *storeB.UpdatedAt) {
		t.Fatalf("store slot changed: %+v → %+v", storeB, after)
	}
	if a := f.store(userA); a.Valid || a.Product != nil || a.Platform != nil || a.Expiry != nil {
		t.Fatalf("comp wrote A's store slot: %+v", a)
	}

	// 2. Extend: 15 minutes later nothing transitions, until moves, since stays.
	clock = t0.Add(15 * time.Minute)
	ts, err = job.Apply(ctx)
	if err != nil || len(ts) != 0 {
		t.Fatalf("second run: %+v, %v", ts, err)
	}
	if c := f.comp(userA); !sameTime(c.Until, clock.Add(hostcomp.Grace)) || !sameTime(c.Since, t0) {
		t.Errorf("A after extend = %+v", c)
	}

	// 3. Lapse: eight days on with no new stats, every comp has run out.
	clock = t0.Add(8 * 24 * time.Hour)
	ts, err = job.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got = kinds(ts)
	if len(got) != 5 || got[userA] != hostcomp.Lapsed || got[userB] != hostcomp.Lapsed || got[userH] != hostcomp.Lapsed || got[userI] != hostcomp.Lapsed || got[userJ] != hostcomp.Lapsed {
		t.Fatalf("lapse transitions = %+v", ts)
	}
	for _, tr := range ts {
		if tr.UserID == userH && (tr.SystemID != sysH1 || tr.Conversation != "conv-h1") {
			t.Errorf("H lapse should reply in the receiver with a thread: %+v", tr)
		}
		if tr.UserID == userA && (tr.SystemID != sysA || tr.Conversation != "conv-a" || tr.Email != "a@example.invalid") {
			t.Errorf("A lapse = %+v", tr)
		}
	}
	if c := f.comp(userA); c.Since != nil || !sameTime(c.Until, t0.Add(15*time.Minute).Add(hostcomp.Grace)) {
		t.Errorf("A after lapse = %+v", c)
	}
	if after := f.store(userB); after.Valid != true || !sameTime(after.UpdatedAt, *storeB.UpdatedAt) {
		t.Fatalf("lapse touched the store slot: %+v", after)
	}
	// Lapsing is a transition, not a state: the next run is quiet.
	if ts, err = job.Apply(ctx); err != nil || len(ts) != 0 {
		t.Fatalf("run after lapse: %+v, %v", ts, err)
	}

	// 4. Re-qualify: A's receiver comes back and runs steadily for 3 days.
	clock = t0.Add(12 * 24 * time.Hour)
	f.stats(sysA, clock.Add(-73*time.Hour), clock)
	f.page(sysA, clock.Add(-time.Hour))
	ts, err = job.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got = kinds(ts); len(got) != 1 || got[userA] != hostcomp.Started {
		t.Fatalf("re-qualify transitions = %+v", ts)
	}
	if c := f.comp(userA); !sameTime(c.Since, clock) || !sameTime(c.Until, clock.Add(hostcomp.Grace)) {
		t.Errorf("A after re-qualify = %+v", c)
	}
}

// A receiver's supporters get the comp while it qualifies, start and lapse
// with it, and are emailed in their own thread rather than the owner's.
func TestJobSupporters(t *testing.T) {
	conn := hostCompTestDB(t)
	f := fixture{t, conn}
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	clock := t0
	job := hostcomp.NewJob(conn, nil, nil).WithClock(func() time.Time { return clock })
	since := t0.Add(-5 * 24 * time.Hour)
	full := func(sys int64) { f.stats(sys, since, t0); f.page(sys, t0.Add(-time.Hour)) }

	owner := f.user("owner@example.invalid")
	hilltop := f.system(owner, "Hilltop", true, false, "conv-owner")
	full(hilltop)
	// The owner listed as their own supporter changes nothing.
	f.supporter(hilltop, owner, "")
	funder := f.user("funder@example.invalid")
	f.supporter(hilltop, funder, "")

	// Supporting a receiver that doesn't qualify, or isn't approved, gets nothing.
	quietOwner := f.user("quiet@example.invalid")
	quiet := f.system(quietOwner, "Quiet", true, false, "")
	f.stats(quiet, since, t0) // online, never decoded a page
	quietFan := f.user("quietfan@example.invalid")
	f.supporter(quiet, quietFan, "")
	pendingOwner := f.user("pending@example.invalid")
	pending := f.system(pendingOwner, "Pending", false, false, "")
	full(pending)
	pendingFan := f.user("pendingfan@example.invalid")
	f.supporter(pending, pendingFan, "")

	// Owns a qualifying receiver and supports Hilltop: emailed as an owner.
	both := f.user("both@example.invalid")
	own := f.system(both, "Own", true, false, "")
	full(own)
	f.supporter(hilltop, both, "")

	ts, err := job.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byUser := map[int64]hostcomp.Transition{}
	for _, tr := range ts {
		byUser[tr.UserID] = tr
	}
	if len(ts) != 3 || len(byUser) != 3 {
		t.Fatalf("start transitions = %+v", ts)
	}
	if tr := byUser[owner]; tr.Kind != hostcomp.Started || tr.Supporter || tr.SystemID != hilltop || tr.Conversation != "conv-owner" {
		t.Errorf("owner = %+v", tr)
	}
	if tr := byUser[funder]; tr.Kind != hostcomp.Started || !tr.Supporter || tr.SystemID != hilltop || tr.SystemName != "Hilltop" ||
		tr.Conversation != "" || tr.Email != "funder@example.invalid" {
		t.Errorf("supporter = %+v, want a supporter start about Hilltop in a thread of their own", tr)
	}
	if tr := byUser[both]; tr.Kind != hostcomp.Started || tr.Supporter || tr.SystemID != own {
		t.Errorf("owner who also supports = %+v, want their own receiver", tr)
	}
	for name, u := range map[string]int64{"quiet": quietFan, "pending": pendingFan} {
		if c := f.comp(u); c.Until != nil || c.Since != nil {
			t.Errorf("supporter of the %s receiver was comped: %+v", name, c)
		}
	}
	if c := f.comp(funder); !sameTime(c.Until, t0.Add(hostcomp.Grace)) || !sameTime(c.Since, t0) {
		t.Errorf("supporter comp = %+v", c)
	}

	// Their start email opened a thread of their own; later runs extend quietly.
	f.exec(`UPDATE system_supporters SET conversation_uuid = 'conv-funder' WHERE system_id = $1 AND user_id = $2`, hilltop, funder)
	clock = t0.Add(15 * time.Minute)
	if ts, err := job.Apply(ctx); err != nil || len(ts) != 0 {
		t.Fatalf("second run: %+v, %v", ts, err)
	}
	if c := f.comp(funder); !sameTime(c.Until, clock.Add(hostcomp.Grace)) {
		t.Errorf("supporter not extended: %+v", c)
	}

	// Hilltop goes quiet: the supporter lapses with it, in their own thread.
	clock = t0.Add(8 * 24 * time.Hour)
	ts, err = job.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byUser = map[int64]hostcomp.Transition{}
	for _, tr := range ts {
		byUser[tr.UserID] = tr
	}
	if tr := byUser[funder]; tr.Kind != hostcomp.Lapsed || !tr.Supporter || tr.SystemID != hilltop || tr.SystemName != "Hilltop" || tr.Conversation != "conv-funder" {
		t.Errorf("supporter lapse = %+v", tr)
	}
	if tr := byUser[owner]; tr.Kind != hostcomp.Lapsed || tr.Supporter || tr.Conversation != "conv-owner" {
		t.Errorf("owner lapse = %+v", tr)
	}
	if tr := byUser[both]; tr.Kind != hostcomp.Lapsed || tr.Supporter || tr.SystemID != own {
		t.Errorf("owner-and-supporter lapse = %+v, want their own receiver", tr)
	}
}

// A second runner while one holds the lock does nothing.
func TestJobSkipsWhileAnotherRunHoldsTheLock(t *testing.T) {
	conn := hostCompTestDB(t)
	f := fixture{t, conn}
	t0 := time.Now().UTC().Truncate(time.Second)
	u := f.user("a@example.invalid")
	sys := f.system(u, "Budgewoi", true, false, "")
	f.stats(sys, t0.Add(-4*24*time.Hour), t0)
	f.page(sys, t0.Add(-time.Hour))

	tx, err := conn.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('host_comp', 0))`); err != nil {
		t.Fatal(err)
	}
	job := hostcomp.NewJob(conn, nil, nil).WithClock(func() time.Time { return t0 })
	if ts, err := job.Apply(context.Background()); err != nil || len(ts) != 0 {
		t.Fatalf("locked run: %+v, %v", ts, err)
	}
	if c := f.comp(u); c.Until != nil {
		t.Fatalf("locked run wrote a comp: %+v", c)
	}
	_ = tx.Rollback()
	if ts, err := job.Apply(context.Background()); err != nil || len(ts) != 1 {
		t.Fatalf("unlocked run: %+v, %v", ts, err)
	}
}

type recorder struct {
	mu     sync.Mutex
	pushes []string
	mails  []string
	reply  string // conversation the fake mailer returns when given none
	fail   bool
}

func (r *recorder) NotifyHostCompStarted(_ context.Context, userID int64, systemName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pushes = append(r.pushes, "started:"+systemName)
}

func (r *recorder) NotifyHostCompLapsed(_ context.Context, userID int64, systemName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pushes = append(r.pushes, "lapsed:"+systemName)
}

func (r *recorder) send(kind, conversation, to, systemName string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mails = append(r.mails, kind+":"+conversation+":"+to+":"+systemName)
	if r.fail {
		return "", context.DeadlineExceeded
	}
	if conversation == "" {
		return r.reply, nil
	}
	return conversation, nil
}

func (r *recorder) SendHostCompStarted(_ context.Context, conversation, to, _, systemName string) (string, error) {
	return r.send("started", conversation, to, systemName)
}

func (r *recorder) SendHostCompLapsed(_ context.Context, conversation, to, _, systemName string) (string, error) {
	return r.send("lapsed", conversation, to, systemName)
}

func (r *recorder) NotifyHostCompSupporterStarted(_ context.Context, userID int64, systemName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pushes = append(r.pushes, "supporter-started:"+systemName)
}

func (r *recorder) SendHostCompSupporterStarted(_ context.Context, conversation, to, _, systemName string) (string, error) {
	return r.send("supporter-started", conversation, to, systemName)
}

func (r *recorder) SendHostCompSupporterLapsed(_ context.Context, conversation, to, _, systemName string) (string, error) {
	return r.send("supporter-lapsed", conversation, to, systemName)
}

func supporterConversationOf(t *testing.T, conn *sqlx.DB, systemID, userID int64) string {
	t.Helper()
	var c *string
	if err := conn.Get(&c, `SELECT conversation_uuid FROM system_supporters WHERE system_id = $1 AND user_id = $2`, systemID, userID); err != nil {
		t.Fatal(err)
	}
	if c == nil {
		return ""
	}
	return *c
}

// A supporter gets the supporter wording, and their new thread is kept on
// their supporter row; the receiver's own thread is the owner's and is left
// alone.
func TestJobNotifySupporter(t *testing.T) {
	conn := hostCompTestDB(t)
	f := fixture{t, conn}
	ctx := context.Background()
	owner := f.user("owner@example.invalid")
	sys := f.system(owner, "Hilltop", true, false, "")
	funder := f.user("funder@example.invalid")
	f.supporter(sys, funder, "")

	rec := &recorder{reply: "conv-funder"}
	job := hostcomp.NewJob(conn, rec, rec)
	job.Notify(ctx, hostcomp.Transition{Kind: hostcomp.Started, UserID: funder, Email: "funder@example.invalid", SystemID: sys, SystemName: "Hilltop", Supporter: true})
	if got := supporterConversationOf(t, conn, sys, funder); got != "conv-funder" {
		t.Errorf("supporter thread = %q, want conv-funder", got)
	}
	if got := conversationOf(t, conn, sys); got != "" {
		t.Errorf("supporter's thread was stored on the receiver: %q", got)
	}
	job.Notify(ctx, hostcomp.Transition{Kind: hostcomp.Lapsed, UserID: funder, Email: "funder@example.invalid", SystemID: sys, SystemName: "Hilltop", Conversation: "conv-funder", Supporter: true})

	wantPushes := []string{"supporter-started:Hilltop", "lapsed:Hilltop"}
	wantMails := []string{"supporter-started::funder@example.invalid:Hilltop", "supporter-lapsed:conv-funder:funder@example.invalid:Hilltop"}
	if strings.Join(rec.pushes, "|") != strings.Join(wantPushes, "|") || strings.Join(rec.mails, "|") != strings.Join(wantMails, "|") {
		t.Fatalf("pushes %v, mails %v", rec.pushes, rec.mails)
	}
}

func conversationOf(t *testing.T, conn *sqlx.DB, systemID int64) string {
	t.Helper()
	var c *string
	if err := conn.Get(&c, `SELECT support_conversation_uuid FROM systems WHERE id = $1`, systemID); err != nil {
		t.Fatal(err)
	}
	if c == nil {
		return ""
	}
	return *c
}

// Notify pushes and emails, replies in an existing thread, and stores a new
// conversation only on a receiver that had none.
func TestJobNotify(t *testing.T) {
	conn := hostCompTestDB(t)
	f := fixture{t, conn}
	ctx := context.Background()
	u := f.user("a@example.invalid")
	withThread := f.system(u, "Budgewoi", true, false, "conv-a")
	without := f.system(u, "Figtree", true, false, "")

	rec := &recorder{reply: "conv-new"}
	job := hostcomp.NewJob(conn, rec, rec)
	job.Notify(ctx, hostcomp.Transition{Kind: hostcomp.Started, UserID: u, Email: "a@example.invalid", SystemID: withThread, SystemName: "Budgewoi", Conversation: "conv-a"})
	job.Notify(ctx, hostcomp.Transition{Kind: hostcomp.Lapsed, UserID: u, Email: "a@example.invalid", SystemID: without, SystemName: "Figtree"})
	// No receiver left: the email still goes, nothing is stored.
	job.Notify(ctx, hostcomp.Transition{Kind: hostcomp.Lapsed, UserID: u, Email: "a@example.invalid"})

	wantPushes := []string{"started:Budgewoi", "lapsed:Figtree", "lapsed:"}
	wantMails := []string{"started:conv-a:a@example.invalid:Budgewoi", "lapsed::a@example.invalid:Figtree", "lapsed::a@example.invalid:"}
	if strings.Join(rec.pushes, "|") != strings.Join(wantPushes, "|") || strings.Join(rec.mails, "|") != strings.Join(wantMails, "|") {
		t.Fatalf("pushes %v, mails %v", rec.pushes, rec.mails)
	}
	if got := conversationOf(t, conn, withThread); got != "conv-a" {
		t.Errorf("existing thread overwritten: %q", got)
	}
	if got := conversationOf(t, conn, without); got != "conv-new" {
		t.Errorf("new conversation not stored: %q", got)
	}

	// A thread that appeared in the meantime is not overwritten.
	f.exec(`UPDATE systems SET support_conversation_uuid = 'conv-race' WHERE id = $1`, without)
	rec.reply = "conv-other"
	job.Notify(ctx, hostcomp.Transition{Kind: hostcomp.Started, UserID: u, Email: "a@example.invalid", SystemID: without, SystemName: "Figtree"})
	if got := conversationOf(t, conn, without); got != "conv-race" {
		t.Errorf("conversation = %q, want conv-race kept", got)
	}

	// Email failure is only logged; nil collaborators are skipped.
	rec.fail = true
	job.Notify(ctx, hostcomp.Transition{Kind: hostcomp.Started, UserID: u, Email: "a@example.invalid", SystemID: withThread, Conversation: "conv-a"})
	hostcomp.NewJob(conn, nil, nil).Notify(ctx, hostcomp.Transition{Kind: hostcomp.Lapsed, UserID: u})
}

// Effective access is what the rest of the server reads: a comped user with
// no store subscription is a Pro recipient; an expired comp is not.
func TestEffectiveAccessSQL(t *testing.T) {
	conn := hostCompTestDB(t)
	f := fixture{t, conn}
	comped := f.user("comped@example.invalid")
	f.exec(`UPDATE users SET host_comp_until = NOW() + INTERVAL '7 days', host_comp_since = NOW() WHERE id = $1`, comped)
	expired := f.user("expired@example.invalid")
	f.exec(`UPDATE users SET host_comp_until = NOW() - INTERVAL '1 minute' WHERE id = $1`, expired)
	paying := f.user("paying@example.invalid")
	f.exec(`UPDATE users SET subscription_valid = true WHERE id = $1`, paying)
	free := f.user("free@example.invalid")

	var ids []int64
	if err := conn.Select(&ids, `SELECT u.id FROM users u WHERE `+hostcomp.Access("u")+` ORDER BY u.id`); err != nil {
		t.Fatal(err)
	}
	want := []int64{comped, paying}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("access = %v, want %v (free %d, expired %d)", ids, want, free, expired)
	}
	if err := conn.Select(&ids, `SELECT id FROM users WHERE `+hostcomp.CompOnly("")+` ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != comped {
		t.Fatalf("comp-only = %v, want [%d]", ids, comped)
	}
}
