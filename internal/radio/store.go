// Package radio stores what P25 receivers hear on the NSW Public Safety
// Network's trunking control channels — sites, voice calls and unit events —
// and serves it back to the API. Frames arrive over the receiver websocket
// (internal/recvhub) in the shapes defined by internal/p25/wire.go.
package radio

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/p25"
)

// DefaultSysID is the NSW PSN's P25 system ID (0x2D1). The network only has
// the one today, so it is the default wherever a caller doesn't name one.
const DefaultSysID = 0x2D1

// ErrInvalidPayload marks a frame that can never be stored, as opposed to a
// database failure. The hub logs these and carries on.
var ErrInvalidPayload = errors.New("invalid radio payload")

// maxClockSkew is how far a receiver's timestamp may stray from ours before
// it is ignored in favour of the server clock. A Pi without an RTC boots in
// 1970 until NTP syncs, and those rows would otherwise sort to the end of
// every feed forever.
const maxClockSkew = 24 * time.Hour

// maxCallUIDLen bounds the receiver-assigned call id (24 hex chars today).
const maxCallUIDLen = 64

type Store struct {
	db *sqlx.DB
}

func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// HandleFrame decodes one p25.* websocket frame and stores it. sys is the
// receiver that sent it. Matches recvhub.RadioHandler.
func (s *Store) HandleFrame(ctx context.Context, sys *models.System, frameType string, data json.RawMessage) error {
	var receiverID int64
	if sys != nil {
		receiverID = sys.ID
	}
	switch frameType {
	case p25.FrameSite:
		var p p25.SitePayload
		if err := decode(data, &p); err != nil {
			return err
		}
		return s.UpsertSite(ctx, receiverID, p, time.Now())
	case p25.FrameCallStart:
		var p p25.CallPayload
		if err := decode(data, &p); err != nil {
			return err
		}
		_, err := s.StartCall(ctx, receiverID, p)
		return err
	case p25.FrameCallEnd:
		var p p25.CallPayload
		if err := decode(data, &p); err != nil {
			return err
		}
		_, err := s.EndCall(ctx, receiverID, p)
		return err
	case p25.FrameUnit:
		var p p25.UnitPayload
		if err := decode(data, &p); err != nil {
			return err
		}
		return s.RecordUnit(ctx, receiverID, p)
	default:
		return fmt.Errorf("%w: unknown frame type %q", ErrInvalidPayload, frameType)
	}
}

func decode(data json.RawMessage, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	return nil
}

// --- Sites ---

// UpsertSite records a site a receiver is decoding. Name and location are
// admin-owned and never touched here. A zero WACN or NAC means the receiver
// hasn't decoded it yet, so it doesn't overwrite a known value.
func (s *Store) UpsertSite(ctx context.Context, receiverID int64, p p25.SitePayload, at time.Time) error {
	if p.SysID == 0 {
		return fmt.Errorf("%w: site without sysid", ErrInvalidPayload)
	}
	neighbors := p.Neighbors
	if neighbors == nil {
		neighbors = []p25.NeighborSite{}
	}
	plan := p.Plan
	if plan == nil {
		plan = []p25.PlanEntry{}
	}
	neighborsJSON, err := json.Marshal(neighbors)
	if err != nil {
		return err
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO radio_sites (sysid, rfss, site, wacn, nac, control_hz, neighbors, plan,
		                          first_seen_at, last_seen_at, last_receiver_system_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, $10)
		 ON CONFLICT (sysid, rfss, site) DO UPDATE SET
		     wacn         = COALESCE(NULLIF(EXCLUDED.wacn, 0), radio_sites.wacn),
		     nac          = COALESCE(NULLIF(EXCLUDED.nac, 0), radio_sites.nac),
		     control_hz   = EXCLUDED.control_hz,
		     neighbors    = EXCLUDED.neighbors,
		     plan         = EXCLUDED.plan,
		     last_seen_at = GREATEST(radio_sites.last_seen_at, EXCLUDED.last_seen_at),
		     last_receiver_system_id = COALESCE(EXCLUDED.last_receiver_system_id, radio_sites.last_receiver_system_id)`,
		int(p.SysID), int(p.RFSS), int(p.Site), int64(p.WACN), int(p.NAC), int64(p.ControlHz),
		neighborsJSON, planJSON, at, nullID(receiverID))
	return err
}

// --- Calls ---

// callRow is a CallPayload checked and converted to column values.
type callRow struct {
	uid              string
	sysid, rfss      int
	site             int
	tgid             sql.NullInt64
	target, source   sql.NullInt64
	freqHz           sql.NullInt64
	slot             int
	encrypted        bool
	emergency, patch bool
	startedAt        time.Time
	endedAt          sql.NullTime
}

// newCallRow validates p. ended is whether p is a call_end, in which case
// endedAt is set (never before startedAt). now stands in for missing or
// implausible receiver timestamps.
func newCallRow(p p25.CallPayload, ended bool, now time.Time) (callRow, error) {
	switch {
	case p.CallID == "":
		return callRow{}, fmt.Errorf("%w: call without call_id", ErrInvalidPayload)
	case len(p.CallID) > maxCallUIDLen:
		return callRow{}, fmt.Errorf("%w: call_id too long", ErrInvalidPayload)
	case p.SysID == 0:
		return callRow{}, fmt.Errorf("%w: call without sysid", ErrInvalidPayload)
	case p.Group == 0 && p.Target == 0:
		return callRow{}, fmt.Errorf("%w: call with neither talkgroup nor target", ErrInvalidPayload)
	}
	row := callRow{
		uid:       p.CallID,
		sysid:     int(p.SysID),
		rfss:      int(p.RFSS),
		site:      int(p.Site),
		tgid:      nullNonZero(int64(p.Group)),
		target:    nullNonZero(int64(p.Target)),
		source:    nullNonZero(int64(p.Source)),
		freqHz:    nullNonZero(int64(p.FreqHz)),
		slot:      p.Slot,
		encrypted: p.Encrypted,
		emergency: p.Emergency,
		patch:     p.Patch,
		startedAt: msTime(p.StartMS, now),
	}
	if ended {
		end := msTime(p.EndMS, now)
		if end.Before(row.startedAt) {
			end = row.startedAt
		}
		row.endedAt = sql.NullTime{Time: end, Valid: true}
	}
	return row, nil
}

const insertCallSQL = `
	INSERT INTO radio_calls (receiver_system_id, call_uid, sysid, rfss, site, tgid, target_rid,
	                         source_rid, freq_hz, slot, encrypted, emergency, patch, started_at, ended_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	ON CONFLICT (call_uid) DO NOTHING
	RETURNING id`

func insertCall(ctx context.Context, tx *sqlx.Tx, receiverID int64, c callRow) (bool, error) {
	var id int64
	err := tx.GetContext(ctx, &id, insertCallSQL,
		nullID(receiverID), c.uid, c.sysid, c.rfss, c.site, c.tgid, c.target,
		c.source, c.freqHz, c.slot, c.encrypted, c.emergency, c.patch, c.startedAt, c.endedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // already have it
	}
	return err == nil, err
}

// StartCall records a call_start. A repeat of a call already stored is a
// no-op and returns false.
func (s *Store) StartCall(ctx context.Context, receiverID int64, p p25.CallPayload) (bool, error) {
	c, err := newCallRow(p, false, time.Now())
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	inserted, err := insertCall(ctx, tx, receiverID, c)
	if err != nil || !inserted {
		return false, err
	}
	if c.tgid.Valid {
		if err := touchTalkgroup(ctx, tx, c.sysid, c.tgid.Int64, c.startedAt, 0, 0); err != nil {
			return false, err
		}
	}
	if c.source.Valid {
		if err := touchUnit(ctx, tx, c.sysid, c.source.Int64, c.startedAt, sql.NullInt64{}); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// endCallSQL ends a call that is still open, filling in whatever the end frame
// knows that the start didn't. The ended_at IS NULL guard is what makes
// ending idempotent: a concurrent duplicate waits on the row lock, re-checks
// the guard against the committed row and matches nothing.
const endCallSQL = `
	UPDATE radio_calls SET
	    ended_at   = GREATEST($2, started_at),
	    encrypted  = encrypted OR $3,
	    emergency  = emergency OR $4,
	    patch      = patch OR $5,
	    source_rid = COALESCE(source_rid, $6),
	    freq_hz    = COALESCE(freq_hz, $7)
	WHERE call_uid = $1 AND ended_at IS NULL
	RETURNING sysid, tgid, source_rid, encrypted, ended_at`

type endedCall struct {
	SysID     int           `db:"sysid"`
	TGID      sql.NullInt64 `db:"tgid"`
	Source    sql.NullInt64 `db:"source_rid"`
	Encrypted bool          `db:"encrypted"`
	EndedAt   time.Time     `db:"ended_at"`
}

// EndCall records a call_end and reports whether this was the call's end
// transition. Only that transition counts the call against its talkgroup's
// calls_clear / calls_encrypted, so a repeated call_end changes nothing. A
// call_end whose start was never stored (receiver reconnected mid-call, say)
// inserts the whole call.
func (s *Store) EndCall(ctx context.Context, receiverID int64, p p25.CallPayload) (bool, error) {
	c, err := newCallRow(p, true, time.Now())
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	end := func() (endedCall, bool, error) {
		var e endedCall
		err := tx.GetContext(ctx, &e, endCallSQL,
			c.uid, c.endedAt.Time, c.encrypted, c.emergency, c.patch, c.source, c.freqHz)
		if errors.Is(err, sql.ErrNoRows) {
			return e, false, nil
		}
		return e, err == nil, err
	}

	e, ok, err := end()
	if err != nil {
		return false, err
	}
	if !ok {
		// Either the call was never started here, or it has already ended.
		inserted, err := insertCall(ctx, tx, receiverID, c)
		if err != nil {
			return false, err
		}
		if inserted {
			e, ok = endedCall{SysID: c.sysid, TGID: c.tgid, Source: c.source, Encrypted: c.encrypted, EndedAt: c.endedAt.Time}, true
		} else {
			// The row exists: it has already ended, or a call_start for it
			// committed after the first UPDATE ran. Retrying ends the
			// latter and matches nothing for the former.
			if e, ok, err = end(); err != nil {
				return false, err
			}
		}
	}
	if !ok {
		return false, nil // already ended: nothing to count
	}

	if e.TGID.Valid {
		clearCalls, encryptedCalls := 1, 0
		if e.Encrypted {
			clearCalls, encryptedCalls = 0, 1
		}
		if err := touchTalkgroup(ctx, tx, e.SysID, e.TGID.Int64, e.EndedAt, clearCalls, encryptedCalls); err != nil {
			return false, err
		}
	}
	if e.Source.Valid {
		if err := touchUnit(ctx, tx, e.SysID, e.Source.Int64, e.EndedAt, sql.NullInt64{}); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// --- Units ---

// RecordUnit stores an affiliation / registration / deregistration and
// updates the unit (and, for an affiliation, its talkgroup).
func (s *Store) RecordUnit(ctx context.Context, receiverID int64, p p25.UnitPayload) error {
	switch {
	case p.Kind == "" || len(p.Kind) > 32:
		return fmt.Errorf("%w: bad unit event kind %q", ErrInvalidPayload, p.Kind)
	case p.SysID == 0:
		return fmt.Errorf("%w: unit event without sysid", ErrInvalidPayload)
	case p.Unit == 0:
		return fmt.Errorf("%w: unit event without unit", ErrInvalidPayload)
	}
	at := msTime(p.TSMS, time.Now())
	sysid, rid, tgid := int(p.SysID), int64(p.Unit), nullNonZero(int64(p.Group))
	// Only an accepted affiliation says which talkgroup the unit is now on.
	var lastTG sql.NullInt64
	if p.Kind == p25.UnitKindAffiliation && p.Result == 0 {
		lastTG = tgid
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO radio_unit_events (receiver_system_id, sysid, rfss, site, kind, rid, tgid, result, at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		nullID(receiverID), sysid, int(p.RFSS), int(p.Site), p.Kind, rid, tgid, int(p.Result), at); err != nil {
		return err
	}
	// Talkgroup before unit, the same order as the call paths, so concurrent
	// transactions can't take the two row locks in opposite orders.
	if lastTG.Valid {
		if err := touchTalkgroup(ctx, tx, sysid, lastTG.Int64, at, 0, 0); err != nil {
			return err
		}
	}
	if err := touchUnit(ctx, tx, sysid, rid, at, lastTG); err != nil {
		return err
	}
	return tx.Commit()
}

// --- Shared upserts ---

// touchTalkgroup marks a talkgroup heard at `at` and adds to its call
// counters. LEAST/GREATEST ignore NULLs, so a talkgroup imported by name
// (never heard, both NULL) takes `at` for both.
func touchTalkgroup(ctx context.Context, tx *sqlx.Tx, sysid int, tgid int64, at time.Time, clearCalls, encryptedCalls int) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO radio_talkgroups (sysid, tgid, first_seen_at, last_seen_at, calls_clear, calls_encrypted)
		 VALUES ($1, $2, $3, $3, $4, $5)
		 ON CONFLICT (sysid, tgid) DO UPDATE SET
		     first_seen_at   = LEAST(radio_talkgroups.first_seen_at, EXCLUDED.first_seen_at),
		     last_seen_at    = GREATEST(radio_talkgroups.last_seen_at, EXCLUDED.last_seen_at),
		     calls_clear     = radio_talkgroups.calls_clear + EXCLUDED.calls_clear,
		     calls_encrypted = radio_talkgroups.calls_encrypted + EXCLUDED.calls_encrypted`,
		sysid, tgid, at, clearCalls, encryptedCalls)
	return err
}

// touchUnit marks a radio heard at `at`, and records lastTG as its current
// talkgroup when set.
func touchUnit(ctx context.Context, tx *sqlx.Tx, sysid int, rid int64, at time.Time, lastTG sql.NullInt64) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO radio_units (sysid, rid, first_seen_at, last_seen_at, last_tgid)
		 VALUES ($1, $2, $3, $3, $4)
		 ON CONFLICT (sysid, rid) DO UPDATE SET
		     first_seen_at = LEAST(radio_units.first_seen_at, EXCLUDED.first_seen_at),
		     last_seen_at  = GREATEST(radio_units.last_seen_at, EXCLUDED.last_seen_at),
		     last_tgid     = COALESCE(EXCLUDED.last_tgid, radio_units.last_tgid)`,
		sysid, rid, at, lastTG)
	return err
}

// --- Helpers ---

// msTime converts a receiver's unix-millisecond timestamp, falling back to
// now when it is missing or further than maxClockSkew from now.
func msTime(ms int64, now time.Time) time.Time {
	if ms <= 0 {
		return now
	}
	t := time.UnixMilli(ms)
	if d := t.Sub(now); d > maxClockSkew || d < -maxClockSkew {
		return now
	}
	return t
}

func nullNonZero(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: v != 0}
}

// nullID maps "no receiver" (0) to NULL for the systems foreign keys.
func nullID(id int64) sql.NullInt64 {
	return nullNonZero(id)
}
