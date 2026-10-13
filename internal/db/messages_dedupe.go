package db

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/the-it-dept/rfspager/internal/models"
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
	ID      int64          `db:"id"`
	Message string         `db:"message"`
	Quality sql.NullString `db:"quality"`
}

// rfsHeader is the page's own timestamp and unit code at the start of an
// RFS dispatch, e.g. "13 October 2026 22:47:41 LHPOKOL1". Two copies with
// the same header are the same page even a minute apart: the header carries
// the dispatch time to the second.
var rfsHeader = regexp.MustCompile(`^\d{1,2} [A-Z][a-z]+ \d{4} \d{2}:\d{2}:\d{2} \S+`)

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
)

// findDuplicate looks for a stored copy of the same page on this address:
// identical text within 5 minutes, the same RFS header within 5 minutes,
// the same first 30 characters (within 5 minutes when the text starts with
// an RFS header, whose dispatch time is exact to the second; otherwise
// 30 s), or a near-identical text within the resend window when either copy
// needed heavy repair. The last rule is what catches a re-send whose repair
// went wrong in one or two characters: "LHRaYE" for "LHRAYTE", "26-1149"
// for "26-131349".
//
// A heavily damaged page to an unknown capcode that resembles a page just
// stored on a real one is returned with dropOnly: its address codeword was
// repaired into a phantom capcode along with the text, so it must neither
// be stored nor replace anything.
func findDuplicate(ctx context.Context, q queryer, address, message string, ts time.Time, quality *rxproto.PageQuality, capcodeKnown bool) (row *duplicateRow, dropOnly bool, err error) {
	hash := fmt.Sprintf("%x", md5.Sum([]byte(message)))
	var r duplicateRow
	err = q.GetContext(ctx, &r,
		`SELECT id, message, quality::text AS quality FROM messages
		  WHERE address = $1 AND md5 = $2 AND deleted_at IS NULL
		    AND created_at BETWEEN $3 AND $4
		  ORDER BY created_at LIMIT 1`,
		address, hash, ts.Add(-5*time.Minute), ts.Add(5*time.Minute))
	if err == nil {
		return &r, false, nil
	}
	if err != sql.ErrNoRows {
		return nil, false, err
	}
	if len(message) < 12 {
		return nil, false, nil
	}
	header := rfsHeader.FindString(message)
	if len(header) >= 20 {
		err = q.GetContext(ctx, &r,
			`SELECT id, message, quality::text AS quality FROM messages
			  WHERE address = $1 AND substr(message, 1, $2) = $3 AND deleted_at IS NULL
			    AND created_at BETWEEN $4 AND $5
			  ORDER BY created_at LIMIT 1`,
			address, len(header), header, ts.Add(-5*time.Minute), ts.Add(5*time.Minute))
		if err == nil {
			return &r, false, nil
		}
		if err != sql.ErrNoRows {
			return nil, false, err
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
	err = q.GetContext(ctx, &r,
		`SELECT id, message, quality::text AS quality FROM messages
		  WHERE address = $1 AND substr(message, 1, 30) = $2 AND deleted_at IS NULL
		    AND created_at BETWEEN $3 AND $4
		  ORDER BY created_at LIMIT 1`,
		address, prefix, ts.Add(-prefixWindow), ts.Add(prefixWindow))
	if err == nil {
		return &r, false, nil
	}
	if err != sql.ErrNoRows {
		return nil, false, err
	}

	// Heavy repair: trust the timing more than the text. Any page on this
	// address inside the resend window that reads the same bar a few
	// characters is this page, whichever of the two copies is the damaged
	// one.
	var cands []duplicateRow
	if err := q.SelectContext(ctx, &cands,
		`SELECT id, message, quality::text AS quality FROM messages
		  WHERE address = $1 AND deleted_at IS NULL AND created_at BETWEEN $2 AND $3
		  ORDER BY created_at LIMIT 10`,
		address, ts.Add(-resendWindow), ts.Add(resendWindow)); err != nil {
		return nil, false, err
	}
	for i := range cands {
		if !suspectCopy(quality) && !suspectCopy(decodeQuality(cands[i].Quality)) {
			continue
		}
		if textSimilarity(message, cands[i].Message) >= sameAddressSimilarity {
			return &cands[i], false, nil
		}
	}
	if capcodeKnown || !heavilyDamaged(quality) {
		return nil, false, nil
	}
	cands = cands[:0]
	if err := q.SelectContext(ctx, &cands,
		`SELECT id, message, quality::text AS quality FROM messages
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

// suspectCopy reports whether the receiver had to work hard enough on a
// copy that its text cannot be taken literally: anything unrepairable or cut
// short, or half or more of the codewords corrected.
func suspectCopy(q *rxproto.PageQuality) bool {
	if q == nil {
		return false
	}
	return q.Uncorrectable > 0 || q.Truncated || (q.Codewords > 0 && q.Corrected*2 >= q.Codewords)
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

// garbleChars are characters that never appear in a clean RFS or FRNSW
// dispatch; their count is the fallback damage estimate for copies stored
// without a quality record.
var garbleChars = regexp.MustCompile("[@^\\\\{}|`~$%<>!\";?_\x00-\x08\x0b\x0c\x0e-\x1f]")

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
	return len(garbleChars.FindAllStringIndex(text, -1)) * 100
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
		if dropOnly || !betterCopy(msg.Message, msg.Quality, existing.Message, decodeQuality(existing.Quality)) {
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
		// Pushes not yet sent for this message should carry the repaired text.
		if _, err := tx.ExecContext(ctx,
			`UPDATE notifications
			    SET data = (data::jsonb || jsonb_build_object('message', $2::text, 'body', $2::text))::text,
			        updated_at = NOW()
			  WHERE message_id = $1 AND pushed_at IS NULL AND push_failed_at IS NULL`,
			existing.ID, msg.Message); err != nil {
			return "", err
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
