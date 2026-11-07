// Package hostcomp gives free Pro to people hosting a receiver that is online
// and decoding (the "host comp"), and to the receiver's supporters
// (system_supporters: others it comps, e.g. whoever paid for the gear).
// Design and policy in docs/plans/2026-10-14-public-coverage-and-hosting.md,
// section 4.
//
// The comp lives in users.host_comp_until / host_comp_since, apart from the
// store entitlement (subscription_valid, _expiry, _product_id, _platform).
// This package only ever writes the host columns, and the store paths never
// touch them. Everywhere access is checked or reported, effective access is
// subscription_valid OR host_comp_until > NOW() (see Access).
package hostcomp

import (
	"time"

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

const (
	// ProductID is reported as the subscription product when access comes
	// only from the comp, the way gifts report io.devlan.rfspager.gifted.
	ProductID = "io.devlan.rfspager.host"

	// Grace is how far each run pushes host_comp_until past now while the
	// user qualifies, so the comp lapses this long after they stop.
	Grace = 7 * 24 * time.Hour

	// Window is the longest history a receiver is judged on.
	Window = 72 * time.Hour
	// MinHistory is how much stats history a receiver needs before it can
	// qualify; until it has Window of history, its window is all of it.
	MinHistory = 24 * time.Hour

	// Bucket is the uptime resolution. A connected receiver writes a
	// system_stats row every stats interval, so a bucket as long as the
	// largest allowed interval always holds a row while it is connected.
	Bucket = time.Duration(rxproto.MaxStatsIntervalS) * time.Second

	// A receiver must be connected for at least UptimeNum/UptimeDen of the
	// buckets in its window (integer maths, so exactly 90% qualifies).
	UptimeNum = 9
	UptimeDen = 10

	// Interval is how often the job runs; StartDelay is the first run after
	// boot.
	Interval   = 15 * time.Minute
	StartDelay = time.Minute
)

// Access is the effective-access predicate for a users row, qualified with
// alias ("" for an unaliased users table). It is never NULL.
func Access(alias string) string {
	p := ""
	if alias != "" {
		p = alias + "."
	}
	return "(COALESCE(" + p + "subscription_valid, false) OR COALESCE(" + p + "host_comp_until > NOW(), false))"
}

// CompOnly is true when the comp is the only source of access: the store
// slot is not valid and the comp has not run out.
func CompOnly(alias string) string {
	p := ""
	if alias != "" {
		p = alias + "."
	}
	return "(NOT COALESCE(" + p + "subscription_valid, false) AND COALESCE(" + p + "host_comp_until > NOW(), false))"
}

// window is a receiver's judging window in unix seconds, aligned to Bucket:
// [start, end). end is the start of the bucket now falls in, so the current,
// partly elapsed bucket is never counted against the receiver.
type window struct {
	start, end int64
}

func (w window) buckets() int64 { return (w.end - w.start) / bucketSeconds }

const bucketSeconds = int64(Bucket / time.Second)

func floorBucket(t time.Time) int64 {
	s := t.Unix()
	b := s / bucketSeconds
	if s%bucketSeconds < 0 {
		b--
	}
	return b * bucketSeconds
}

// receiverWindow is the window a receiver whose first stats row is at
// firstStats is judged on at now: the shorter of Window and its history,
// and ok is false while it has less than MinHistory of history. History is
// counted in whole buckets from the start of the bucket the first row fell
// in (that bucket holds a row, so it counts as connected); this is lenient
// by under one Bucket.
func receiverWindow(now, firstStats time.Time) (window, bool) {
	end := floorBucket(now)
	first := floorBucket(firstStats)
	if end-first < int64(MinHistory/time.Second) {
		return window{}, false
	}
	start := max(end-int64(Window/time.Second), first)
	return window{start: start, end: end}, true
}

// meetsUptime reports whether connected of total buckets is at least 90%.
func meetsUptime(connected, total int64) bool {
	return total > 0 && connected*UptimeDen >= total*UptimeNum
}

// receiver is an approved, non-deleted receiver with an owner, as the job
// sees it.
type receiver struct {
	SystemID     int64      `db:"system_id"`
	UserID       int64      `db:"user_id"`
	Name         string     `db:"name"`
	Conversation string     `db:"conversation"`
	ApprovedAt   time.Time  `db:"approved_at"`
	FirstStatsAt *time.Time `db:"first_stats_at"`
	// Supporter: UserID is one of the receiver's supporters, not its owner,
	// and Conversation is that supporter's own thread.
	Supporter bool `db:"-"`

	window    window
	connected int64
	decoded   bool
}

// qualifies is the per-receiver rule: enough history, connected for at
// least 90% of its window, and at least one decoded page in it.
func (r *receiver) qualifies() bool {
	return r.window.end > r.window.start && r.decoded && meetsUptime(r.connected, r.window.buckets())
}

// qualifyingReceivers picks, for each user with at least one qualifying
// receiver, the receiver their start email is about: their own over one they
// support, then one that already has a support conversation, else the most
// recently approved.
func qualifyingReceivers(receivers []*receiver) map[int64]*receiver {
	out := map[int64]*receiver{}
	for _, r := range receivers {
		if !r.qualifies() {
			continue
		}
		if cur, ok := out[r.UserID]; !ok || better(r, cur) {
			out[r.UserID] = r
		}
	}
	return out
}

func better(a, b *receiver) bool {
	if a.Supporter != b.Supporter {
		return !a.Supporter
	}
	if (a.Conversation != "") != (b.Conversation != "") {
		return a.Conversation != ""
	}
	if !a.ApprovedAt.Equal(b.ApprovedAt) {
		return a.ApprovedAt.After(b.ApprovedAt)
	}
	return a.SystemID > b.SystemID
}
