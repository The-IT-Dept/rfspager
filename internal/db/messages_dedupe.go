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

// findDuplicate looks for a stored copy of the same page on this address:
// identical text within 5 minutes, the same first 30 characters within 30 s,
// or the same RFS header within 5 minutes.
func findDuplicate(ctx context.Context, q queryer, address, message string, ts time.Time) (*duplicateRow, error) {
	hash := fmt.Sprintf("%x", md5.Sum([]byte(message)))
	var row duplicateRow
	err := q.GetContext(ctx, &row,
		`SELECT id, message, quality::text AS quality FROM messages
		  WHERE address = $1 AND md5 = $2 AND deleted_at IS NULL
		    AND created_at BETWEEN $3 AND $4
		  ORDER BY created_at LIMIT 1`,
		address, hash, ts.Add(-5*time.Minute), ts.Add(5*time.Minute))
	if err == nil {
		return &row, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if len(message) < 12 {
		return nil, nil
	}
	if h := rfsHeader.FindString(message); h != "" && len(h) >= 20 {
		err = q.GetContext(ctx, &row,
			`SELECT id, message, quality::text AS quality FROM messages
			  WHERE address = $1 AND substr(message, 1, $2) = $3 AND deleted_at IS NULL
			    AND created_at BETWEEN $4 AND $5
			  ORDER BY created_at LIMIT 1`,
			address, len(h), h, ts.Add(-5*time.Minute), ts.Add(5*time.Minute))
		if err == nil {
			return &row, nil
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
	}
	prefix := message
	if len(prefix) > 30 {
		prefix = prefix[:30]
	}
	err = q.GetContext(ctx, &row,
		`SELECT id, message, quality::text AS quality FROM messages
		  WHERE address = $1 AND substr(message, 1, 30) = $2 AND deleted_at IS NULL
		    AND created_at BETWEEN $3 AND $4
		  ORDER BY created_at LIMIT 1`,
		address, prefix, ts.Add(-30*time.Second), ts.Add(30*time.Second))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// garbleChars are characters that never appear in a clean RFS or FRNSW
// dispatch; their count is the fallback damage estimate for copies stored
// without a quality record.
var garbleChars = regexp.MustCompile("[@^\\\\{}|`~$%<>!\";?_\x00-\x08\x0b\x0c\x0e-\x1f]")

// damageScore ranks a copy: lower is better. A truncated page is worse than
// any complete one; otherwise unrepairable codewords dominate, and a text
// heuristic stands in when the receiver sent no quality.
func damageScore(text string, q *rxproto.PageQuality) int {
	if q != nil {
		score := q.Uncorrectable * 10
		if q.Truncated {
			score += 1000
		}
		return score
	}
	return len(garbleChars.FindAllStringIndex(text, -1)) * 10
}

// betterCopy reports whether the new copy should replace the stored one.
func betterCopy(newText string, newQ *rxproto.PageQuality, oldText string, oldQ *rxproto.PageQuality) bool {
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

	existing, err := findDuplicate(ctx, tx, msg.Address, msg.Message, msg.CreatedAt)
	if err != nil {
		return "", err
	}
	if existing != nil {
		if !betterCopy(msg.Message, msg.Quality, existing.Message, decodeQuality(existing.Quality)) {
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
