package radio

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// TalkgroupName is one row of a talkgroup import: the admin-owned
// descriptive columns. A nil field leaves the stored value as it is.
type TalkgroupName struct {
	TGID        int
	AlphaTag    *string
	Description *string
	Agency      *string
	Category    *string
}

// ImportResult summarises a talkgroup import. Errors are per-line problems;
// those lines were skipped and the rest imported.
type ImportResult struct {
	Inserted int      `json:"inserted"`
	Updated  int      `json:"updated"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
}

var talkgroupColumns = []string{"tgid", "alpha_tag", "description", "agency", "category"}

// ParseTalkgroupCSV reads a talkgroup list. The first row is a header naming
// any of tgid, alpha_tag, description, agency, category in any order; tgid
// is required and unknown columns are ignored. An empty cell means "leave as
// is", so a file with only tgid,alpha_tag renames without touching the rest.
// Bad lines are reported in the returned messages and skipped; the error is
// only for a file that can't be read at all.
func ParseTalkgroupCSV(r io.Reader) ([]TalkgroupName, []string, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if err == io.EOF {
		return nil, nil, errors.New("empty file")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		if i == 0 {
			h = strings.TrimPrefix(h, "\ufeff") // Excel's UTF-8 BOM
		}
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	if _, ok := col["tgid"]; !ok {
		return nil, nil, fmt.Errorf("header must include tgid (columns: %s)", strings.Join(talkgroupColumns, ","))
	}

	cell := func(rec []string, name string) *string {
		i, ok := col[name]
		if !ok || i >= len(rec) {
			return nil
		}
		v := strings.TrimSpace(rec[i])
		if v == "" {
			return nil
		}
		return &v
	}

	var (
		rows     []TalkgroupName
		problems []string
		seen     = map[int]int{} // tgid -> index in rows
	)
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			var perr *csv.ParseError
			if errors.As(err, &perr) {
				problems = append(problems, perr.Error())
				continue
			}
			return nil, nil, err
		}
		line, _ := cr.FieldPos(0)
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue // blank line
		}
		raw := cell(rec, "tgid")
		if raw == nil {
			problems = append(problems, fmt.Sprintf("line %d: missing tgid", line))
			continue
		}
		tgid, err := strconv.Atoi(*raw)
		if err != nil || tgid < 1 || tgid > 0xFFFF {
			problems = append(problems, fmt.Sprintf("line %d: tgid %q is not a talkgroup (1-65535)", line, *raw))
			continue
		}
		row := TalkgroupName{
			TGID:        tgid,
			AlphaTag:    cell(rec, "alpha_tag"),
			Description: cell(rec, "description"),
			Agency:      cell(rec, "agency"),
			Category:    cell(rec, "category"),
		}
		// A repeated tgid keeps the later line, as reading top to bottom
		// would, and is reported so a copy-paste slip doesn't go unnoticed.
		if i, ok := seen[tgid]; ok {
			rows[i] = row
			problems = append(problems, fmt.Sprintf("line %d: tgid %d repeated; using this line", line, tgid))
			continue
		}
		seen[tgid] = len(rows)
		rows = append(rows, row)
	}
	return rows, problems, nil
}

// ImportTalkgroups upserts talkgroup names for sysid in one transaction.
// Only the descriptive columns are written: counters, hidden and the seen
// timestamps are left alone, and new rows stay "never heard" (NULL seen
// times) until traffic arrives.
func (s *Store) ImportTalkgroups(ctx context.Context, sysid int, rows []TalkgroupName) (inserted, updated int, err error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PreparexContext(ctx,
		`INSERT INTO radio_talkgroups (sysid, tgid, alpha_tag, description, agency, category)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (sysid, tgid) DO UPDATE SET
		     alpha_tag   = COALESCE(EXCLUDED.alpha_tag, radio_talkgroups.alpha_tag),
		     description = COALESCE(EXCLUDED.description, radio_talkgroups.description),
		     agency      = COALESCE(EXCLUDED.agency, radio_talkgroups.agency),
		     category    = COALESCE(EXCLUDED.category, radio_talkgroups.category)
		 RETURNING (xmax = 0) AS inserted`)
	if err != nil {
		return 0, 0, err
	}
	defer stmt.Close()

	for _, r := range rows {
		var isNew bool
		if err := stmt.GetContext(ctx, &isNew, sysid, r.TGID,
			nullString(r.AlphaTag), nullString(r.Description), nullString(r.Agency), nullString(r.Category)); err != nil {
			return 0, 0, fmt.Errorf("tgid %d: %w", r.TGID, err)
		}
		if isNew {
			inserted++
		} else {
			updated++
		}
	}
	return inserted, updated, tx.Commit()
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}
