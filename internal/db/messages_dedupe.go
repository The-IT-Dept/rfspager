package db

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/pagetext"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// Several receivers hear the same page, and the district paging systems
// re-send it from other sites seconds to a minute later. Only one copy
// belongs in the feed, and it should be the best one: complete rather than
// truncated, and with the fewest unrepairable codewords. Ingest therefore
// upserts: a new copy of a page already stored replaces it when it is
// better, keeping the row id so clients see the repaired text on refresh.

// Upsert outcomes.
const (
	MessageCreated   = "created"
	MessageDuplicate = "duplicate"
	MessageReplaced  = "replaced"
)

// duplicateRow is the stored copy a new page collided with.
type duplicateRow struct {
	ID        int64          `db:"id"`
	Message   string         `db:"message"`
	Quality   sql.NullString `db:"quality"`
	Lat       sql.NullString `db:"lat"`
	Lng       sql.NullString `db:"lng"`
	CreatedAt time.Time      `db:"created_at"`
}

// rfsHeader is the page's own timestamp and first word at the start of an
// RFS dispatch, e.g. "13 October 2026 22:47:41 LHPOKOL1" (SES pages put two
// spaces after the time; the paging test pages use an ISO date). Two copies
// with the same header are the same page whenever they are heard: the header
// carries the dispatch time to the second.
var rfsHeader = regexp.MustCompile(`^(?:\d{1,2} [A-Z][A-Za-z]+ \d{4}|\d{4}-\d{2}-\d{2}) \d{2}:\d{2}:\d{2} +\S+`)

// Similarity thresholds for the text-distance rules below, as a fraction of
// the text that matches after edits.
const (
	// sameAddressSimilarity: a copy on the same capcode within three
	// minutes that reads the same bar a few characters. Different pages to
	// one capcode share the header format and little else (~0.4-0.7).
	sameAddressSimilarity = 0.85
	// anyAddressSimilarity: a badly damaged page to an unknown capcode that
	// still reads like a page just stored on a real one. Unrelated pages
	// score ~0.3-0.4 against such garbage.
	anyAddressSimilarity = 0.55
	// resendWindow is how long the district systems keep re-sending a page
	// from other sites; the third transmission lands about a minute later.
	resendWindow = 3 * time.Minute
	// headerLookback is how far back a page with an RFS header is matched
	// against stored copies. The paging network now and then re-transmits
	// a day or more of backlog in a few minutes (seven times between
	// 8 October and 19 November 2026); a replayed page is the page we
	// already have, not a new one.
	headerLookback = 72 * time.Hour
	// minFragment is the shortest fragment fragmentOfSameTransmission will
	// match; anything shorter says too little about which page it came from.
	minFragment = 16
	// simulcastWindow is how far apart the receivers' copies of a single
	// transmission land (their timestamps agree to the second or two).
	simulcastWindow = 15 * time.Second
	// correctionWindow is how long after a push went out a better copy
	// still earns a correction push. Re-sends from other sites land within
	// a few minutes; past this, a repaired row (say, by a replayed copy)
	// must not wake anyone.
	correctionWindow = 10 * time.Minute
)

// dupColumns are what findDuplicate reads of a stored copy.
const dupColumns = `id, message, quality::text AS quality, lat, lng, created_at`

// findDuplicate looks for a stored copy of the same page on this address,
// most specific evidence first:
//
//   - identical text within 5 minutes;
//   - the same RFS header within 5 minutes;
//   - the same first 30 characters (within 5 minutes when the text starts
//     with an RFS header, whose dispatch time is exact to the second;
//     otherwise 30 s);
//   - a near-identical text within the resend window when either copy
//     needed heavy repair. This catches a re-send whose repair went wrong in
//     one or two characters: "LHRaYE" for "LHRAYTE", "26-1149" for
//     "26-131349". It also catches a damaged fragment stored a few seconds
//     before a complete copy, which the plain score cannot (see
//     fragmentOfSameTransmission). Copies that name different dispatch times or
//     incidents in text that decoded cleanly are never matched this way;
//   - the same RFS header within the last 72 hours: the network re-sending
//     old backlog (see headerLookback). This comes after the recent rules
//     so a clean copy of a replay still repairs a damaged copy of the same
//     replay stored a second earlier, rather than matching the original.
//
// A heavily damaged page to an unknown capcode that resembles a page just
// stored on a real one is returned with dropOnly: its address codeword was
// repaired into a phantom capcode along with the text, so it must neither
// be stored nor replace anything.
func findDuplicate(ctx context.Context, q queryer, address, message string, ts time.Time, quality *rxproto.PageQuality, capcodeKnown bool) (row *duplicateRow, dropOnly bool, err error) {
	one := func(query string, args ...any) (*duplicateRow, error) {
		var r duplicateRow
		err := q.GetContext(ctx, &r, query, args...)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &r, nil
	}
	hash := fmt.Sprintf("%x", md5.Sum([]byte(message)))
	if r, err := one(`SELECT `+dupColumns+` FROM messages
		  WHERE address = $1 AND md5 = $2 AND deleted_at IS NULL
		    AND created_at BETWEEN $3 AND $4
		  ORDER BY created_at LIMIT 1`,
		address, hash, ts.Add(-5*time.Minute), ts.Add(5*time.Minute)); r != nil || err != nil {
		return r, false, err
	}
	if len(message) < 12 {
		return nil, false, nil
	}
	header := rfsHeader.FindString(message)
	byHeader := func(from, to time.Time) (*duplicateRow, error) {
		return one(`SELECT `+dupColumns+` FROM messages
			  WHERE address = $1 AND substr(message, 1, $2) = $3 AND deleted_at IS NULL
			    AND created_at BETWEEN $4 AND $5
			  ORDER BY created_at LIMIT 1`,
			address, len(header), header, from, to)
	}
	if len(header) >= 20 {
		if r, err := byHeader(ts.Add(-5*time.Minute), ts.Add(5*time.Minute)); r != nil || err != nil {
			return r, false, err
		}
	}
	prefix := message
	if len(prefix) > 30 {
		prefix = prefix[:30]
	}
	prefixWindow := 30 * time.Second
	if header != "" {
		prefixWindow = 5 * time.Minute
	}
	// Two FRNSW turnouts to one station share their first 30 characters;
	// only the incident number tells them apart, so a vetoed row must not
	// hide a later one that is this page.
	var sameStart []duplicateRow
	if err := q.SelectContext(ctx, &sameStart, `SELECT `+dupColumns+` FROM messages
		  WHERE address = $1 AND substr(message, 1, 30) = $2 AND deleted_at IS NULL
		    AND created_at BETWEEN $3 AND $4
		  ORDER BY created_at LIMIT 5`,
		address, prefix, ts.Add(-prefixWindow), ts.Add(prefixWindow)); err != nil {
		return nil, false, err
	}
	for i := range sameStart {
		r := sameStart[i]
		if !differentPages(message, quality, r.Message, decodeQuality(r.Quality), ts, ts.Sub(r.CreatedAt)) {
			return &r, false, nil
		}
	}

	// Heavy repair: trust the timing more than the text. Any page on this
	// address inside the resend window that reads the same bar a few
	// characters is this page, whichever of the two copies is the damaged
	// one.
	var cands []duplicateRow
	if err := q.SelectContext(ctx, &cands,
		`SELECT `+dupColumns+` FROM messages
		  WHERE address = $1 AND deleted_at IS NULL AND created_at BETWEEN $2 AND $3
		  ORDER BY created_at LIMIT 10`,
		address, ts.Add(-resendWindow), ts.Add(resendWindow)); err != nil {
		return nil, false, err
	}
	for i := range cands {
		candQuality := decodeQuality(cands[i].Quality)
		if !suspectCopy(quality) && !suspectCopy(candQuality) {
			continue
		}
		if differentPages(message, quality, cands[i].Message, candQuality, ts, ts.Sub(cands[i].CreatedAt)) {
			continue
		}
		if textSimilarity(message, cands[i].Message) >= sameAddressSimilarity ||
			fragmentOfSameTransmission(cands[i], message, ts) {
			return &cands[i], false, nil
		}
	}

	if len(header) >= 20 {
		if r, err := byHeader(ts.Add(-headerLookback), ts.Add(-5*time.Minute)); r != nil || err != nil {
			return r, false, err
		}
	}

	if capcodeKnown || !heavilyDamaged(quality) {
		return nil, false, nil
	}
	cands = cands[:0]
	if err := q.SelectContext(ctx, &cands,
		`SELECT `+dupColumns+` FROM messages
		  WHERE deleted_at IS NULL AND capcode_id IS NOT NULL AND created_at BETWEEN $1 AND $2
		  ORDER BY created_at DESC LIMIT 50`,
		ts.Add(-resendWindow), ts.Add(resendWindow)); err != nil {
		return nil, false, err
	}
	for i := range cands {
		if textSimilarity(message, cands[i].Message) >= anyAddressSimilarity {
			return &cands[i], true, nil
		}
	}
	return nil, false, nil
}

// repairs reports whether a better copy fixes the text already stored (and
// pushed), rather than just replacing it: the new copy is undamaged, the
// stored one was not (it needed repairs, lost codewords, was cut short or,
// with no quality record, shows garble), the new text isn't shorter (unless
// the stored copy was cut short: a "better" copy that lost words fixes
// nothing), and they are not sibling pages. Only then is a correction push
// or a second fan-out warranted.
func repairs(oldText string, oldQ *rxproto.PageQuality, newText string, newQ *rxproto.PageQuality) bool {
	if pagetext.Damaged(newText, newQ) || damageScore(oldText, oldQ) == 0 {
		return false
	}
	if len(strings.TrimSpace(newText)) < len(strings.TrimSpace(oldText)) && (oldQ == nil || !oldQ.Truncated) {
		return false
	}
	return !siblingPages(oldText, newText)
}

var (
	// unitCode is an RFS unit or turnout group: district prefix, station
	// abbreviation, optional appliance suffix ("CCKARIO", "LHMEDOW7").
	unitCode = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{3,10}$`)
	// clockTime is a page's own time of day ("17:26:35").
	clockTime = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}$`)
)

// siblingPages reports whether two copies are different pages of one
// dispatch rather than two decodes of one page. RFS pages each unit of a
// brigade in turn on the same capcode, a second or two apart, so the texts
// differ only in the unit code ("CCKARIO" and "CCKARIOP", "LHMEDOW1" and
// "LHMEDOW7"), perhaps the page's own clock time, and where the network cut
// the last word ("-33.1589" and "-33.158"). The dedupe keeps one of them;
// swapping one for the other corrects nothing.
func siblingPages(a, b string) bool {
	at, bt := strings.Fields(a), strings.Fields(b)
	if len(at) != len(bt) {
		return false
	}
	units := 0
	for i := range at {
		x, y := at[i], bt[i]
		if x == y {
			continue
		}
		switch {
		case clockTime.MatchString(x) && clockTime.MatchString(y) && secondsApart(x, y) <= 5:
		case i == len(at)-1 && (strings.HasPrefix(x, y) || strings.HasPrefix(y, x)):
		case i <= 5 && unitCode.MatchString(x) && unitCode.MatchString(y) && sameUnitStem(x, y):
			// The unit code comes early: after the clock time, or after the
			// incident number in a stop message, or first of all.
			units++
		default:
			return false
		}
	}
	return units == 1
}

// sameUnitStem: one code is the other with up to two more characters, or
// they differ only in the last one.
func sameUnitStem(x, y string) bool {
	if len(x) > len(y) {
		x, y = y, x
	}
	if strings.HasPrefix(y, x) {
		return len(y)-len(x) <= 2
	}
	return len(x) == len(y) && x[:len(x)-1] == y[:len(y)-1]
}

func secondsApart(x, y string) int {
	parse := func(s string) int {
		var h, m, sec int
		fmt.Sscanf(s, "%d:%d:%d", &h, &m, &sec)
		return h*3600 + m*60 + sec
	}
	d := parse(x) - parse(y)
	if d < 0 {
		d = -d
	}
	return d
}

// differentPages reports whether two copies name different dispatches in
// text that decoded cleanly: header times more than two seconds apart, or
// different incident numbers. Re-sends of one page repeat both exactly;
// a follow-up page to the same brigade differs in at least one.
//
// Incident numbers are not compared between copies of the same
// transmission (heard within simulcastWindow, apart): a repair, even a light
// one, can land on the wrong digit, and the receivers' copies of one page
// arrive a second or two apart far more often than two calls to one station
// do. Outside that window the number counts: for an FRNSW turnout it is all
// that tells two calls to one station apart, and merging them would silence
// the second; a re-send that kept a wrong digit becomes its own row, which
// fanoutSQL's damaged-re-send rule keeps from waking anyone.
func differentPages(aText string, aQ *rxproto.PageQuality, bText string, bQ *rxproto.PageQuality, near time.Time, apart time.Duration) bool {
	if at, an, ok := pagetext.DispatchTime(aText, near); ok && pagetext.HeaderTrusted(aText, aQ, an) {
		if bt, bn, ok := pagetext.DispatchTime(bText, near); ok && pagetext.HeaderTrusted(bText, bQ, bn) {
			if d := at.Sub(bt); d > 2*time.Second || d < -2*time.Second {
				return true
			}
		}
	}
	if apart < 0 {
		apart = -apart
	}
	if apart <= simulcastWindow {
		return false
	}
	if a, ok := pagetext.IncidentID(aText, aQ); ok {
		if b, ok := pagetext.IncidentID(bText, bQ); ok && a != b {
			return true
		}
	}
	return false
}

// fragmentOfSameTransmission reports whether one of two copies is a damaged
// fragment of the other from the same transmission: the receivers' copies
// land within a second or two, and the one cut short may arrive first
// ("LZmNC TYPE: AFA TURNOUT: " a second before "FRINC TYPE: AFA TURNOUT: 432
// INC: ...") or last. The plain score divides by the longer text and can't
// see it. Limited to simulcastWindow, because a fragment's opening reads like
// any page of the same format to the same capcode.
func fragmentOfSameTransmission(stored duplicateRow, message string, ts time.Time) bool {
	if d := ts.Sub(stored.CreatedAt); d > simulcastWindow || d < -simulcastWindow {
		return false
	}
	fragment, full := strings.TrimSpace(stored.Message), strings.TrimSpace(message)
	if len(fragment) > len(full) {
		fragment, full = full, fragment
	}
	if len(fragment) < minFragment || len(fragment) == len(full) {
		return false
	}
	return prefixSimilarity(fragment, full) >= sameAddressSimilarity
}

// suspectCopy reports whether the receiver had to work hard enough on a
// copy that its text cannot be taken literally: anything unrepairable or cut
// short, or half or more of the codewords corrected.
func suspectCopy(q *rxproto.PageQuality) bool {
	return pagetext.Suspect(q)
}

// heavilyDamaged is the bar for doubting the address as well as the text.
func heavilyDamaged(q *rxproto.PageQuality) bool {
	return q != nil && (q.Uncorrectable >= 3 || q.Truncated)
}

// textSimilarity is 1 minus the edit distance between sample and the
// matching prefix of reference, as a fraction of their length, so a copy
// cut short still scores against the part of the page it did decode.
func textSimilarity(sample, reference string) float64 {
	a, b := []byte(strings.TrimSpace(sample)), []byte(strings.TrimSpace(reference))
	if len(b) > len(a)+3 {
		b = b[:len(a)+3]
	}
	n := max(len(a), len(b))
	if n == 0 {
		return 1
	}
	return 1 - float64(levenshtein(a, b))/float64(n)
}

// prefixSimilarity scores a fragment against the best-matching prefix of a
// longer text: 1 minus the edit distance as a fraction of the fragment.
func prefixSimilarity(fragment, full string) float64 {
	a, b := []byte(fragment), []byte(full)
	if len(a) == 0 {
		return 0
	}
	// No prefix longer than twice the fragment can be closer than the
	// empty one, so the rest of the longer text never needs scoring.
	if len(b) > 2*len(a) {
		b = b[:2*len(a)]
	}
	return 1 - float64(prefixDistance(a, b))/float64(len(a))
}

// prefixDistance is the smallest edit distance between a and any prefix of b.
func prefixDistance(a, b []byte) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	best := prev[0]
	for _, d := range prev[1:] {
		best = min(best, d)
	}
	return best
}

func levenshtein(a, b []byte) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// damageScore ranks a copy: lower is better. A truncated page is worse than
// any complete one; otherwise unrepairable codewords dominate, then the
// number of corrections, because a copy that needed forty of them has had
// forty chances for a repair to land on the wrong characters. A text
// heuristic stands in when the receiver sent no quality.
func damageScore(text string, q *rxproto.PageQuality) int {
	if q != nil {
		score := q.Uncorrectable*100 + q.Corrected
		if q.Truncated {
			score += 10000
		}
		return score
	}
	return pagetext.GarbleCount(text) * 100
}

// betterCopy reports whether the new copy should replace the stored one.
// Identical text has nothing to offer, however it was decoded.
func betterCopy(newText string, newQ *rxproto.PageQuality, oldText string, oldQ *rxproto.PageQuality) bool {
	if strings.TrimSpace(newText) == strings.TrimSpace(oldText) {
		return false
	}
	ns, os := damageScore(newText, newQ), damageScore(oldText, oldQ)
	if ns != os {
		return ns < os
	}
	// Equal damage: a longer text is a more complete one (the shorter was
	// cut), as long as it is not just the same text with trailing noise.
	return len(strings.TrimSpace(newText)) > len(strings.TrimSpace(oldText)) && strings.HasPrefix(newText, oldText[:min(len(oldText), 30)])
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

// reopenFanout sends a message back to fan-out after an undamaged copy
// replaced (or vouched for) the one fan-out decided from soon before. That
// decision rested on the stored copy: its coordinates (alertzones), its
// quality (a damaged re-send wakes nobody), its header (a replay wakes
// nobody). fanoutSQL's per-user NOT EXISTS keeps everyone already notified
// from a second push; carryToNotifications updates what they were sent.
func reopenFanout(ctx context.Context, tx *sqlx.Tx, messageID int64) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE messages SET fanned_out_at = NULL, push_skip_reason = NULL
		  WHERE id = $1 AND fanned_out_at > NOW() - ($2 * interval '1 second')`,
		messageID, int(correctionWindow.Seconds()))
	if err != nil {
		return fmt.Errorf("reopening fan-out: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		if _, err := tx.ExecContext(ctx, `SELECT pg_notify('rfs_refanout', $1::text)`, messageID); err != nil {
			return fmt.Errorf("waking fan-out: %w", err)
		}
	}
	return nil
}

// carryToNotifications brings a message's pushes up to date after a better
// copy replaced its text. It runs in the upsert's transaction, after the
// UPDATE that locked the message row (fan-out locks the same row, so the
// notification rows it writes are either committed or not yet begun), and
// it first locks the message's live notification rows, so the dispatcher
// can't move one between the statements below: a claim skips them and
// markSent waits, then rechecks against what this transaction wrote.
//
//  1. Unsent: carry the new text; nothing has gone out yet.
//  2. Pending correction: carry the new text as the correction's text.
//  3. In flight or waiting to retry: carry the new text, recording whether
//     it fixes the page (carried_fix). A retry then sends the best text
//     there is. If the send in flight succeeds, the dispatcher finds the
//     data changed under it: a fix turns the row into a correction, anything
//     else just records what was sent (markSentSQL).
//  4. Delivered, when this copy fixes the text: queue a correction. The
//     row keeps the text that was pushed (the admin page compares it with
//     the stored copy) and gains correction_body and first_pushed_at.
//     Only once per notification, and only within correctionWindow.
//
// "fixes" means the new copy is undamaged. A delivered push that is not
// corrected keeps the text it went out with.
func carryToNotifications(ctx context.Context, tx *sqlx.Tx, messageID int64, text string, fixes bool) error {
	var live int
	if err := tx.GetContext(ctx, &live,
		`SELECT count(*) FROM (SELECT 1 FROM notifications
		    WHERE message_id = $1 AND push_failed_at IS NULL FOR UPDATE) locked`, messageID); err != nil {
		return fmt.Errorf("locking pushes: %w", err)
	}
	if live == 0 {
		return nil
	}
	var touched int64
	exec := func(what, query string, args ...any) (int64, error) {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", what, err)
		}
		n, _ := res.RowsAffected()
		touched += n
		return n, nil
	}
	window := int(correctionWindow.Seconds())
	if _, err := exec("updating unsent pushes",
		`UPDATE notifications
		    SET data = (data::jsonb || jsonb_build_object('message', $2::text, 'body', $2::text))::text,
		        updated_at = NOW()
		  WHERE message_id = $1 AND push_failed_at IS NULL
		    AND pushed_at IS NULL AND push_attempts = 0
		    AND NOT COALESCE((data::jsonb->>'correction')::boolean, false)`,
		messageID, text); err != nil {
		return err
	}
	if _, err := exec("updating pending corrections",
		`UPDATE notifications
		    SET data = (data::jsonb || jsonb_build_object('correction_body', $2::text))::text,
		        updated_at = NOW()
		  WHERE message_id = $1 AND push_failed_at IS NULL AND pushed_at IS NULL
		    AND COALESCE((data::jsonb->>'correction')::boolean, false)`,
		messageID, text); err != nil {
		return err
	}
	inFlight, err := exec("updating pushes in flight",
		`UPDATE notifications
		    SET data = (data::jsonb || jsonb_build_object('message', $2::text, 'body', $2::text,
		            'carried_fix', $3::boolean AND created_at > NOW() - ($4 * interval '1 second')))::text,
		        updated_at = NOW()
		  WHERE message_id = $1 AND push_failed_at IS NULL AND pushed_at IS NULL AND push_attempts > 0
		    AND NOT COALESCE((data::jsonb->>'correction')::boolean, false)
		    AND (data::jsonb->>'body') IS DISTINCT FROM $2::text`,
		messageID, text, fixes, window)
	if err != nil {
		return err
	}
	if fixes {
		corrected, err := exec("queueing correction pushes",
			`UPDATE notifications
			    SET data = (data::jsonb || jsonb_build_object(
			            'correction', true, 'correction_body', $2::text, 'first_pushed_at', pushed_at))::text,
			        pushed_at = NULL, push_attempts = 0, push_last_error = NULL, updated_at = NOW()
			  WHERE message_id = $1 AND push_failed_at IS NULL AND pushed_at IS NOT NULL
			    AND created_at > NOW() - ($3 * interval '1 second')
			    AND NOT COALESCE((data::jsonb->>'correction')::boolean, false)
			    AND (data::jsonb->>'body') IS DISTINCT FROM $2::text`,
			messageID, text, window)
		if err != nil {
			return err
		}
		if corrected > 0 || inFlight > 0 {
			slog.Info("correction pushes queued", "message_id", messageID, "delivered", corrected, "in_flight", inFlight)
		}
	}
	// Wake the dispatcher: its trigger fires only on INSERT, and any claim
	// that ran meanwhile skipped the rows locked here.
	if _, err := tx.ExecContext(ctx, `SELECT pg_notify('rfs_dispatch', '')`); err != nil {
		return fmt.Errorf("waking dispatch: %w", err)
	}
	return nil
}

// UpsertMessageDeduped stores msg unless a copy of the same page is already
// stored, in which case the better of the two is kept. The advisory lock on
// the address serialises the receivers that hear the same page. On
// MessageReplaced, msg.ID is the id of the row that was updated.
func (d *DB) UpsertMessageDeduped(ctx context.Context, msg *models.Message) (string, error) {
	tx, err := d.conn.BeginTxx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, msg.Address); err != nil {
		return "", fmt.Errorf("locking address for dedupe: %w", err)
	}

	msg.MD5 = fmt.Sprintf("%x", md5.Sum([]byte(msg.Message)))
	msg.UpdatedAt = time.Now()
	var qualityJSON any
	if msg.Quality != nil {
		b, _ := json.Marshal(msg.Quality)
		qualityJSON = string(b)
	}
	var incidentKey any
	if msg.IncidentKey != "" {
		incidentKey = msg.IncidentKey
	}

	existing, dropOnly, err := findDuplicate(ctx, tx, msg.Address, msg.Message, msg.CreatedAt, msg.Quality, msg.CapcodeID.Valid)
	if err != nil {
		return "", err
	}
	if existing != nil {
		if dropOnly {
			return MessageDuplicate, tx.Commit()
		}
		existingQuality := decodeQuality(existing.Quality)
		if !betterCopy(msg.Message, msg.Quality, existing.Message, existingQuality) {
			// The same text, decoded more cleanly: nothing changes for anyone
			// reading it, but fan-out may be holding the stored copy as
			// damaged when this one shows its text was right all along.
			if strings.TrimSpace(msg.Message) == strings.TrimSpace(existing.Message) &&
				damageScore(msg.Message, msg.Quality) < damageScore(existing.Message, existingQuality) {
				if _, err := tx.ExecContext(ctx,
					`UPDATE messages SET quality = $2::jsonb, system_id = $3, updated_at = $4 WHERE id = $1`,
					existing.ID, qualityJSON, msg.SystemID, msg.UpdatedAt); err != nil {
					return "", fmt.Errorf("upgrading quality: %w", err)
				}
				if pagetext.Damaged(existing.Message, existingQuality) && !pagetext.Damaged(msg.Message, msg.Quality) {
					if err := reopenFanout(ctx, tx, existing.ID); err != nil {
						return "", err
					}
				}
			}
			return MessageDuplicate, tx.Commit()
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE messages
			    SET message = $2, md5 = $3, lat = $4, lng = $5, quality = $6::jsonb, system_id = $7,
			        incident_key = COALESCE($8, incident_key), replaced_count = replaced_count + 1, updated_at = $9
			  WHERE id = $1`,
			existing.ID, msg.Message, msg.MD5, msg.Lat, msg.Lng, qualityJSON, msg.SystemID, incidentKey, msg.UpdatedAt); err != nil {
			return "", err
		}
		fixes := repairs(existing.Message, existingQuality, msg.Message, msg.Quality)
		if err := carryToNotifications(ctx, tx, existing.ID, msg.Message, fixes); err != nil {
			return "", err
		}
		if fixes {
			if err := reopenFanout(ctx, tx, existing.ID); err != nil {
				return "", err
			}
		}
		msg.ID = existing.ID
		return MessageReplaced, tx.Commit()
	}

	if err := tx.QueryRowContext(ctx,
		`INSERT INTO messages (system_id, capcode_id, address, message, md5, lat, lng, quality, incident_key, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11) RETURNING id`,
		msg.SystemID, msg.CapcodeID, msg.Address, msg.Message, msg.MD5,
		msg.Lat, msg.Lng, qualityJSON, incidentKey, msg.CreatedAt, msg.UpdatedAt).Scan(&msg.ID); err != nil {
		return "", err
	}
	return MessageCreated, tx.Commit()
}
