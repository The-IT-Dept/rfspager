// Package pagetext reads what a page says about itself: the dispatch time in
// its own header, and whether the receiver's decode can be trusted. Both the
// ingest dedupe and the push fan-out need the same answers.
package pagetext

import (
	"regexp"
	"strconv"
	"sync"
	"time"
	_ "time/tzdata" // the static server binary must not depend on the host's zoneinfo

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// sydney is the zone the paging network writes its headers in.
var sydney = func() *time.Location {
	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		panic(err)
	}
	return loc
}()

var (
	// "19 November 2026 08:48:11 HVDO - ..." (RFS) and
	// "19 November 2026 11:14:22  NO PERSONS ..." (SES, two spaces).
	longHeader = regexp.MustCompile(`^(\d{1,2}) ([A-Z][a-z]+) (\d{4}) (\d{2}):(\d{2}):(\d{2})(?:\s|$)`)
	// "2026-11-19 12:42:53 PAGING TEST MESSAGE ..."
	isoHeader = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2}):(\d{2})(?:\s|$)`)
)

var months = map[string]time.Month{
	"January": time.January, "February": time.February, "March": time.March,
	"April": time.April, "May": time.May, "June": time.June, "July": time.July,
	"August": time.August, "September": time.September, "October": time.October,
	"November": time.November, "December": time.December,
}

// DispatchTime reads the date and time a page starts with, in Sydney time,
// and returns the instant and the header's length in bytes. near (normally
// when the page was heard) picks between the two readings of the hour that
// repeats when daylight saving ends; a time in the hour skipped when it
// starts does not exist and is not parsed.
func DispatchTime(text string, near time.Time) (at time.Time, headerLen int, ok bool) {
	var y, mo, d, h, mi, s int
	if m := longHeader.FindStringSubmatchIndex(text); m != nil {
		month, known := months[text[m[4]:m[5]]]
		if !known {
			return time.Time{}, 0, false
		}
		d, mo, y = atoi(text[m[2]:m[3]]), int(month), atoi(text[m[6]:m[7]])
		h, mi, s = atoi(text[m[8]:m[9]]), atoi(text[m[10]:m[11]]), atoi(text[m[12]:m[13]])
		headerLen = m[13]
	} else if m := isoHeader.FindStringSubmatchIndex(text); m != nil {
		y, mo, d = atoi(text[m[2]:m[3]]), atoi(text[m[4]:m[5]]), atoi(text[m[6]:m[7]])
		h, mi, s = atoi(text[m[8]:m[9]]), atoi(text[m[10]:m[11]]), atoi(text[m[12]:m[13]])
		headerLen = m[13]
	} else {
		return time.Time{}, 0, false
	}
	if mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || s > 59 {
		return time.Time{}, 0, false
	}
	if time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC).Day() != d { // 31 November
		return time.Time{}, 0, false
	}
	for _, offset := range []int{10 * 3600, 11 * 3600} {
		candidate := time.Date(y, time.Month(mo), d, h, mi, s, 0, time.FixedZone("", offset))
		if _, actual := candidate.In(sydney).Zone(); actual != offset {
			continue
		}
		if !ok || absDuration(candidate.Sub(near)) < absDuration(at.Sub(near)) {
			at, ok = candidate, true
		}
	}
	if !ok {
		return time.Time{}, 0, false
	}
	return at, headerLen, true
}

// garbleChars never appear in a clean RFS, SES or FRNSW page.
var garbleChars = regexp.MustCompile("[@^\\\\{}|`~$%<>!\";?_\x00-\x08\x0b\x0c\x0e-\x1f]")

// GarbleCount is the number of characters in text that a clean page never
// contains; the damage estimate for a copy that came without a quality record.
func GarbleCount(text string) int {
	return len(garbleChars.FindAllStringIndex(text, -1))
}

// Suspect reports whether the receiver had to work hard enough on a copy that
// its text cannot be taken literally: anything unrepairable or cut short, or
// half or more of the codewords corrected. No quality record is not suspect.
func Suspect(q *rxproto.PageQuality) bool {
	if q == nil {
		return false
	}
	return q.Uncorrectable > 0 || q.Truncated || (q.Codewords > 0 && q.Corrected*2 >= q.Codewords)
}

// Damaged reports whether a copy is worth waiting to replace before anyone
// is woken for it: Suspect by its quality record or, with none, containing
// characters a clean page never has.
func Damaged(text string, q *rxproto.PageQuality) bool {
	if q == nil {
		return GarbleCount(text) > 0
	}
	return Suspect(q)
}

// HeaderTrusted reports whether the first headerLen bytes of a copy decoded
// cleanly enough to believe the date and time in them. Repaired codewords can
// land on the wrong digit too, so a heavily corrected copy is not trusted
// anywhere; otherwise the header must lie outside every unrepairable range.
func HeaderTrusted(text string, q *rxproto.PageQuality, headerLen int) bool {
	if q == nil {
		return GarbleCount(text) == 0
	}
	if q.Codewords > 0 && q.Corrected*2 >= q.Codewords {
		return false
	}
	if q.Uncorrectable > 0 && len(q.Damaged) == 0 {
		return false // unrepairable somewhere, but we can't tell where
	}
	for _, r := range q.Damaged {
		if r[0] < headerLen && r[1] > 0 {
			return false
		}
	}
	return true
}

const (
	// StaleAfter is how far behind its own header a page may be heard and
	// still be a live page. The network queues pages for up to ~10 minutes
	// when busy (p90 is under two minutes); replays run hours to days late.
	StaleAfter = 30 * time.Minute
	// staleHorizon bounds what we call stale: a header further back than
	// this is more likely a decode we shouldn't believe.
	staleHorizon = 7 * 24 * time.Hour
)

// StaleHeader reports whether a copy's header, decoded cleanly, says it was
// dispatched more than StaleAfter before heardAt. It returns the lag and the
// header's date and time (the page's identity for ReplayWatch). One stale
// page proves nothing on its own: see ReplayWatch.
func StaleHeader(text string, q *rxproto.PageQuality, heardAt time.Time) (lag time.Duration, header string, stale bool) {
	at, headerLen, ok := DispatchTime(text, heardAt)
	if !ok {
		return 0, "", false
	}
	lag = heardAt.Sub(at)
	if lag <= StaleAfter || lag >= staleHorizon || !HeaderTrusted(text, q, headerLen) {
		return lag, "", false
	}
	return lag, text[:headerLen], true
}

// Burst rules: a replay is the network re-sending its backlog, several
// different old pages within minutes whose ages are all over the place. A
// paging or receiver clock that is simply wrong makes every page look old by
// the same amount, and a header digit repaired wrongly makes one page look
// old; neither is a replay, and both must still be pushed.
const (
	burstWindow    = 10 * time.Minute
	burstMinPages  = 3
	burstMinSpread = 30 * time.Minute
)

// ReplayWatch remembers the stale pages heard recently (every receiver's
// copy, before the dedupe drops any) and says whether they add up to a
// replay burst. Safe for concurrent use.
type ReplayWatch struct {
	mu   sync.Mutex
	seen map[string]staleSighting
}

type staleSighting struct {
	at  time.Time
	lag time.Duration
}

// NewReplayWatch returns an empty watch.
func NewReplayWatch() *ReplayWatch {
	return &ReplayWatch{seen: map[string]staleSighting{}}
}

// Replays is the process-wide watch shared by ingest and fan-out.
var Replays = NewReplayWatch()

// Observe records a copy if StaleHeader says it is stale.
func (w *ReplayWatch) Observe(text string, q *rxproto.PageQuality, heardAt, now time.Time) {
	lag, header, stale := StaleHeader(text, q, heardAt)
	if !stale {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.seen[header]; !ok {
		w.seen[header] = staleSighting{at: now, lag: lag}
	}
	w.pruneLocked(now)
}

// Burst reports whether the stale pages seen in the last burstWindow are a
// replay: at least burstMinPages different pages whose lags span at least
// burstMinSpread.
func (w *ReplayWatch) Burst(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pruneLocked(now)
	if len(w.seen) < burstMinPages {
		return false
	}
	var lo, hi time.Duration
	first := true
	for _, s := range w.seen {
		if first || s.lag < lo {
			lo = s.lag
		}
		if first || s.lag > hi {
			hi = s.lag
		}
		first = false
	}
	return hi-lo >= burstMinSpread
}

func (w *ReplayWatch) pruneLocked(now time.Time) {
	for k, s := range w.seen {
		if now.Sub(s.at) > burstWindow {
			delete(w.seen, k)
		}
	}
}

var (
	rfsIncident   = regexp.MustCompile(`\b\d{2}-\d{6}\b`)
	frnswIncident = regexp.MustCompile(`\bINC:\s*(\d+-\d{8})\b`)
)

// IncidentID is the incident number a page names (RFS "26-134814", FRNSW
// "INC: 228486-17112026"), if one decoded cleanly: none of its characters
// came from an unrepairable codeword, and the copy was not so heavily
// corrected that a repair may have landed on the wrong digit. Two copies
// naming different incidents are different pages however alike they read.
func IncidentID(text string, q *rxproto.PageQuality) (string, bool) {
	if q != nil && q.Codewords > 0 && q.Corrected*2 >= q.Codewords {
		return "", false
	}
	if m := frnswIncident.FindStringSubmatchIndex(text); m != nil && intact(q, m[2], m[3]) {
		return "INC " + text[m[2]:m[3]], true
	}
	if m := rfsIncident.FindStringIndex(text); m != nil && intact(q, m[0], m[1]) {
		return text[m[0]:m[1]], true
	}
	return "", false
}

// intact reports whether bytes [start, end) of a copy decoded cleanly.
func intact(q *rxproto.PageQuality, start, end int) bool {
	if q == nil {
		return true
	}
	if q.Uncorrectable > 0 && len(q.Damaged) == 0 {
		return false
	}
	for _, r := range q.Damaged {
		if r[0] < end && r[1] > start {
			return false
		}
	}
	return true
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
