package radio

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx/types"
	"github.com/lib/pq"
)

// Read side, for the subscriber API. Everything here leaves out hidden
// talkgroups; admins see them through internal/admin.

// Call is a call with its talkgroup, site and radios named where known.
type Call struct {
	ID          int64      `db:"id" json:"id"`
	SysID       int        `db:"sysid" json:"sysid"`
	RFSS        int        `db:"rfss" json:"rfss"`
	Site        int        `db:"site" json:"site"`
	SiteID      *int64     `db:"site_id" json:"site_id"`
	SiteName    *string    `db:"site_name" json:"site_name"`
	TGID        *int       `db:"tgid" json:"tgid"`
	TGAlphaTag  *string    `db:"tg_alpha_tag" json:"tg_alpha_tag"`
	TGAgency    *string    `db:"tg_agency" json:"tg_agency"`
	TGCategory  *string    `db:"tg_category" json:"tg_category"`
	TargetRID   *int       `db:"target_rid" json:"target_rid"`
	TargetAlias *string    `db:"target_alias" json:"target_alias"`
	SourceRID   *int       `db:"source_rid" json:"source_rid"`
	SourceAlias *string    `db:"source_alias" json:"source_alias"`
	FreqHz      *int64     `db:"freq_hz" json:"freq_hz"`
	Slot        int        `db:"slot" json:"slot"`
	Encrypted   bool       `db:"encrypted" json:"encrypted"`
	Emergency   bool       `db:"emergency" json:"emergency"`
	Patch       bool       `db:"patch" json:"patch"`
	StartedAt   time.Time  `db:"started_at" json:"started_at"`
	EndedAt     *time.Time `db:"ended_at" json:"ended_at"`
	// AudioURL is the API path of the recording (GET /calls/{id}/audio),
	// null until the call has ended and its audio is stored.
	AudioURL *string `db:"audio_url" json:"audio_url"`
	AudioMS  *int    `db:"audio_ms" json:"audio_ms"`
	// Transcript is null until transcribed, and "" if no speech was heard.
	Transcript    *string    `db:"transcript" json:"transcript"`
	TranscribedAt *time.Time `db:"transcribed_at" json:"transcribed_at"`
	// TranscriptError says why there's no transcript; "poor_audio" means the
	// recording was too broken up to try.
	TranscriptError *string `db:"transcript_error" json:"transcript_error"`
	// PatchedWith names the other talkgroups the same keyup went out on.
	PatchedWith pq.StringArray `db:"patched_with" json:"patched_with"`
}

const callSelect = `
	SELECT c.id, c.sysid, c.rfss, c.site, s.id AS site_id, s.name AS site_name,
	       c.tgid, tg.alpha_tag AS tg_alpha_tag, tg.agency AS tg_agency, tg.category AS tg_category,
	       c.target_rid, tu.alias AS target_alias, c.source_rid, su.alias AS source_alias,
	       c.freq_hz, c.slot, c.encrypted, c.emergency, c.patch, c.started_at, c.ended_at,
	       c.audio_url, c.audio_ms, c.transcript, c.transcribed_at, c.transcript_error,
	       COALESCE((SELECT array_agg(COALESCE(dt.alpha_tag, 'Talkgroup ' || d.tgid) ORDER BY d.started_at, d.id)
	                   FROM radio_calls d
	                   LEFT JOIN radio_talkgroups dt ON dt.sysid = d.sysid AND dt.tgid = d.tgid
	                  WHERE d.patch_of = c.id AND d.tgid IS NOT NULL), '{}') AS patched_with
	  FROM radio_calls c
	  LEFT JOIN radio_sites s       ON s.sysid = c.sysid AND s.rfss = c.rfss AND s.site = c.site
	  LEFT JOIN radio_talkgroups tg ON tg.sysid = c.sysid AND tg.tgid = c.tgid
	  LEFT JOIN radio_units su      ON su.sysid = c.sysid AND su.rid = c.source_rid
	  LEFT JOIN radio_units tu      ON tu.sysid = c.sysid AND tu.rid = c.target_rid
	 WHERE NOT COALESCE(tg.hidden, false) AND c.patch_of IS NULL`

// CallFilter narrows ListCalls. Zero values mean "any".
type CallFilter struct {
	SysID    int   // with TGID; DefaultSysID if zero
	TGID     int   // talkgroup
	SiteID   int64 // radio_sites.id
	BeforeID int64 // cursor: calls older than this one
	Limit    int
}

// ListCalls returns calls newest first. It fetches one row past Limit so the
// caller learns whether there is another page without a COUNT, the same as
// the message feed. The cursor is a call id, but the page boundary is that
// call's (started_at, id), so paging follows the ORDER BY and its indexes.
func (s *Store) ListCalls(ctx context.Context, f CallFilter) (calls []Call, hasMore bool, err error) {
	args := []any{}
	add := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	where := ""
	if f.TGID != 0 {
		sysid := f.SysID
		if sysid == 0 {
			sysid = DefaultSysID
		}
		// A patched keyup is stored once, under the first talkgroup; it
		// belongs to the others' timelines too.
		sp, tp := add(sysid), add(f.TGID)
		where += " AND c.sysid = " + sp + " AND (c.tgid = " + tp +
			" OR EXISTS (SELECT 1 FROM radio_calls d WHERE d.patch_of = c.id AND d.tgid = " + tp + "))"
	}
	if f.SiteID != 0 {
		where += " AND (c.sysid, c.rfss, c.site) = (SELECT sysid, rfss, site FROM radio_sites WHERE id = " + add(f.SiteID) + ")"
	}
	if f.BeforeID != 0 {
		where += " AND (c.started_at, c.id) < (SELECT started_at, id FROM radio_calls WHERE id = " + add(f.BeforeID) + ")"
	}
	query := vis(callSelect) + where + " ORDER BY c.started_at DESC, c.id DESC LIMIT " + add(f.Limit+1)

	calls = []Call{}
	if err := s.db.SelectContext(ctx, &calls, query, args...); err != nil {
		return nil, false, err
	}
	if len(calls) > f.Limit {
		return calls[:f.Limit], true, nil
	}
	return calls, false, nil
}

// GetCall returns sql.ErrNoRows for an unknown call or one on a hidden
// talkgroup.
func (s *Store) GetCall(ctx context.Context, id int64) (*Call, error) {
	var c Call
	if err := s.db.GetContext(ctx, &c, vis(callSelect)+" AND c.id = $1", id); err != nil {
		return nil, err
	}
	return &c, nil
}

type Talkgroup struct {
	ID             int64      `db:"id" json:"id"`
	SysID          int        `db:"sysid" json:"sysid"`
	TGID           int        `db:"tgid" json:"tgid"`
	AlphaTag       *string    `db:"alpha_tag" json:"alpha_tag"`
	Description    *string    `db:"description" json:"description"`
	Agency         *string    `db:"agency" json:"agency"`
	Category       *string    `db:"category" json:"category"`
	FirstSeenAt    *time.Time `db:"first_seen_at" json:"first_seen_at"`
	LastSeenAt     *time.Time `db:"last_seen_at" json:"last_seen_at"`
	CallsClear     int        `db:"calls_clear" json:"calls_clear"`
	CallsEncrypted int        `db:"calls_encrypted" json:"calls_encrypted"`
}

// ListTalkgroups returns every visible talkgroup, most recently heard first;
// ones only known by name (never heard) come last.
func (s *Store) ListTalkgroups(ctx context.Context) ([]Talkgroup, error) {
	tgs := []Talkgroup{}
	err := s.db.SelectContext(ctx, &tgs, vis(
		`SELECT id, sysid, tgid, alpha_tag, description, agency, category,
		        first_seen_at, last_seen_at, calls_clear, calls_encrypted
		   FROM radio_talkgroups tg
		  WHERE NOT COALESCE(tg.hidden, false)
		  ORDER BY last_seen_at DESC NULLS LAST, sysid, tgid`))
	return tgs, err
}

type Site struct {
	ID          int64          `db:"id" json:"id"`
	SysID       int            `db:"sysid" json:"sysid"`
	RFSS        int            `db:"rfss" json:"rfss"`
	Site        int            `db:"site" json:"site"`
	WACN        int64          `db:"wacn" json:"wacn"`
	NAC         int            `db:"nac" json:"nac"`
	Name        *string        `db:"name" json:"name"`
	ControlHz   int64          `db:"control_hz" json:"control_hz"`
	Neighbors   types.JSONText `db:"neighbors" json:"neighbors"`
	Plan        types.JSONText `db:"plan" json:"plan"`
	Lat         *float64       `db:"lat" json:"lat"`
	Lng         *float64       `db:"lng" json:"lng"`
	FirstSeenAt time.Time      `db:"first_seen_at" json:"first_seen_at"`
	LastSeenAt  time.Time      `db:"last_seen_at" json:"last_seen_at"`
	// Heard recently: receivers re-send each site every 5 minutes
	// (p25.Reporter), so a few missed refreshes means nobody hears it now.
	Active bool `db:"active" json:"active"`
}

func (s *Store) ListSites(ctx context.Context) ([]Site, error) {
	sites := []Site{}
	err := s.db.SelectContext(ctx, &sites,
		`SELECT id, sysid, rfss, site, wacn, nac, name, control_hz, neighbors, plan, lat, lng,
		        first_seen_at, last_seen_at, last_seen_at > NOW() - INTERVAL '15 minutes' AS active
		   FROM radio_sites
		  ORDER BY sysid, rfss, site`)
	return sites, err
}

type Unit struct {
	ID           int64     `db:"id" json:"id"`
	SysID        int       `db:"sysid" json:"sysid"`
	RID          int       `db:"rid" json:"rid"`
	Alias        *string   `db:"alias" json:"alias"`
	Agency       *string   `db:"agency" json:"agency"`
	FirstSeenAt  time.Time `db:"first_seen_at" json:"first_seen_at"`
	LastSeenAt   time.Time `db:"last_seen_at" json:"last_seen_at"`
	LastTGID     *int      `db:"last_tgid" json:"last_tgid"`
	LastAlphaTag *string   `db:"last_tg_alpha_tag" json:"last_tg_alpha_tag"`
}

type UnitEvent struct {
	ID         int64     `db:"id" json:"id"`
	RFSS       int       `db:"rfss" json:"rfss"`
	Site       int       `db:"site" json:"site"`
	Kind       string    `db:"kind" json:"kind"`
	TGID       *int      `db:"tgid" json:"tgid"`
	TGAlphaTag *string   `db:"tg_alpha_tag" json:"tg_alpha_tag"`
	Result     int       `db:"result" json:"result"`
	At         time.Time `db:"at" json:"at"`
}

type UnitDetail struct {
	Unit
	Events []UnitEvent `json:"events"`
	Calls  []Call      `json:"calls"`
}

// GetUnit returns a radio with its recent unit events and the calls it
// recently transmitted on, or sql.ErrNoRows if it has never been heard.
func (s *Store) GetUnit(ctx context.Context, sysid, rid, limit int) (*UnitDetail, error) {
	var d UnitDetail
	err := s.db.GetContext(ctx, &d.Unit, vis(
		`SELECT u.id, u.sysid, u.rid, u.alias, u.agency, u.first_seen_at, u.last_seen_at,
		        CASE WHEN COALESCE(tg.hidden, false) THEN NULL ELSE u.last_tgid END AS last_tgid,
		        CASE WHEN COALESCE(tg.hidden, false) THEN NULL ELSE tg.alpha_tag END AS last_tg_alpha_tag
		   FROM radio_units u
		   LEFT JOIN radio_talkgroups tg ON tg.sysid = u.sysid AND tg.tgid = u.last_tgid
		  WHERE u.sysid = $1 AND u.rid = $2`), sysid, rid)
	if err != nil {
		return nil, err
	}
	d.Events = []UnitEvent{}
	if err := s.db.SelectContext(ctx, &d.Events, vis(
		`SELECT e.id, e.rfss, e.site, e.kind, e.tgid, tg.alpha_tag AS tg_alpha_tag, e.result, e.at
		   FROM radio_unit_events e
		   LEFT JOIN radio_talkgroups tg ON tg.sysid = e.sysid AND tg.tgid = e.tgid
		  WHERE e.rid = $2 AND e.sysid = $1 AND NOT COALESCE(tg.hidden, false)
		  ORDER BY e.at DESC
		  LIMIT $3`), sysid, rid, limit); err != nil {
		return nil, err
	}
	// Calls it transmitted. Calls it was the target of aren't indexed, and
	// unit-to-unit calls are rare enough to leave out for now.
	d.Calls = []Call{}
	if err := s.db.SelectContext(ctx, &d.Calls,
		vis(callSelect)+` AND c.source_rid = $2 AND c.sysid = $1 ORDER BY c.started_at DESC LIMIT $3`,
		sysid, rid, limit); err != nil {
		return nil, err
	}
	return &d, nil
}

// IsNotFound reports whether err is a lookup that matched nothing.
func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
