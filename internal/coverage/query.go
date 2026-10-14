package coverage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// load reads everything build needs for p. Receptions are aggregated in SQL
// to one row per (system, address) and per address, counting distinct
// minutes; district and channel are resolved in Go from the current
// capcodes, then the heat map is aggregated in SQL for the addresses on the
// requested channel.
func (s *Service) load(ctx context.Context, p Params, now time.Time) (reportInputs, error) {
	in := reportInputs{
		params: p,
		since:  now.Add(-time.Duration(p.Days) * 24 * time.Hour),
		until:  now,
	}
	db := s.db

	if err := db.SelectContext(ctx, &in.agencies,
		`SELECT id, agency, coverage_district FROM agencies ORDER BY agency`); err != nil {
		return in, fmt.Errorf("load agencies: %w", err)
	}
	if err := db.SelectContext(ctx, &in.capcodes,
		`SELECT id, address, alias, agency_id, "ignore" FROM capcodes WHERE deleted_at IS NULL`); err != nil {
		return in, fmt.Errorf("load capcodes: %w", err)
	}
	if err := db.SelectContext(ctx, &in.receivers, `
		SELECT id, name, lat, lng, located_since
		  FROM systems
		 WHERE deleted_at IS NULL AND approved_at IS NOT NULL
		   AND lat IS NOT NULL AND lng IS NOT NULL AND NOT (lat = 0 AND lng = 0)
		   AND ($1::bigint = 0 OR id = $1)
		 ORDER BY name, id`, p.SystemID); err != nil {
		return in, fmt.Errorf("load receivers: %w", err)
	}
	if err := db.SelectContext(ctx, &in.transmitters, `
		SELECT id, channel, name, licensee, lat, lng, eirp_w, height_m, elevation_m,
		       agency_id, district_source, district_confidence
		  FROM paging_transmitters
		 ORDER BY id`); err != nil {
		return in, fmt.Errorf("load transmitters: %w", err)
	}

	if err := db.SelectContext(ctx, &in.systemPages, `
		SELECT r.system_id, r.address, MAX(r.capcode_id) AS capcode_id,
		       (s.located_since IS NULL OR r.heard_at >= s.located_since) AS attributable,
		       COUNT(DISTINCT date_trunc('minute', r.heard_at)) AS pages,
		       MAX(r.heard_at) AS last_heard
		  FROM page_receptions r
		  JOIN systems s ON s.id = r.system_id
		 WHERE r.heard_at >= $1 AND r.heard_at <= $2
		   AND ($3::bigint = 0 OR r.system_id = $3)
		 GROUP BY r.system_id, r.address, 4`, in.since, in.until, p.SystemID); err != nil {
		return in, fmt.Errorf("aggregate receptions by system: %w", err)
	}
	if err := db.SelectContext(ctx, &in.addressPages, `
		SELECT address, MAX(capcode_id) AS capcode_id,
		       COUNT(DISTINCT date_trunc('minute', heard_at)) AS pages
		  FROM page_receptions
		 WHERE heard_at >= $1 AND heard_at <= $2
		   AND ($3::bigint = 0 OR system_id = $3)
		 GROUP BY address`, in.since, in.until, p.SystemID); err != nil {
		return in, fmt.Errorf("aggregate receptions by address: %w", err)
	}

	heat, err := s.loadHeat(ctx, in)
	if err != nil {
		return in, err
	}
	in.heat = heat

	g, err := s.globals(ctx, now)
	if err != nil {
		return in, err
	}
	in.lastHeard, in.receptionsSince = g.lastHeard, g.receptionsSince
	return in, nil
}

// heatKeySQL identifies one incident for the heat weight. RFS keys carry the
// incident number plus a hash of the message ("rfs:26-131320:ab12..."); the
// number alone is the incident, so the call and its stop message count once.
// Pages without a key count once per address and minute.
const heatKeySQL = `COALESCE(substring(incident_key from '^(rfs:[0-9]{2}-[0-9]{6})'), incident_key,
	address || '@' || date_trunc('minute', heard_at)::text)`

// loadHeat counts distinct incidents per ~1 km cell among the window's
// geocoded receptions on the requested channel.
func (s *Service) loadHeat(ctx context.Context, in reportInputs) ([][3]float64, error) {
	filter := in.params.Channel != ChannelAll
	var addresses []string
	if filter {
		addresses = addressesOnChannel(in)
		if len(addresses) == 0 {
			return [][3]float64{}, nil
		}
	}
	var rows []struct {
		Lng    float64 `db:"lng"`
		Lat    float64 `db:"lat"`
		Weight int     `db:"weight"`
	}
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT round(lng::numeric, 2)::float8 AS lng, round(lat::numeric, 2)::float8 AS lat,
		       COUNT(DISTINCT `+heatKeySQL+`) AS weight
		  FROM page_receptions
		 WHERE heard_at >= $1 AND heard_at <= $2
		   AND ($3::bigint = 0 OR system_id = $3)
		   AND lat BETWEEN -38.5 AND -27.5 AND lng BETWEEN 140.5 AND 154
		   AND (NOT $4 OR address = ANY($5))
		 GROUP BY 1, 2
		 ORDER BY 1, 2`, in.since, in.until, in.params.SystemID, filter, pq.Array(addresses)); err != nil {
		return nil, fmt.Errorf("aggregate heat: %w", err)
	}
	out := make([][3]float64, 0, len(rows))
	for _, r := range rows {
		out = append(out, [3]float64{r.Lng, r.Lat, float64(r.Weight)})
	}
	return out, nil
}

// addressesOnChannel lists the window's addresses whose pages go out on the
// requested channel.
func addressesOnChannel(in reportInputs) []string {
	ix := newAgencyIndex(in.agencies)
	resolver := NewResolver(in.capcodes)
	out := make([]string, 0, len(in.addressPages))
	for _, ap := range in.addressPages {
		res := resolver.Resolve(ap.Address, derefID(ap.CapcodeID))
		if in.params.includes(ix.channelOf(res.AgencyID)) {
			out = append(out, ap.Address)
		}
	}
	return out
}

// globals are the report inputs that do not depend on the parameters,
// cached on their own so each parameter set does not repeat them.
type globals struct {
	// lastHeard is each agency's newest stored message, all time.
	lastHeard map[int64]time.Time
	// receptionsSince is the oldest reception recorded per receiver (live or
	// from receiver logs). Before it only the stored copy's receiver is known.
	receptionsSince *time.Time
}

func (s *Service) globals(ctx context.Context, now time.Time) (globals, error) {
	if g, ok := s.globalsCache.get(struct{}{}, now); ok {
		return g, nil
	}
	var g globals
	// One backward probe per capcode on messages(capcode_id, deleted_at,
	// created_at), then the newest per agency. The ORDER BY names deleted_at
	// so it matches that index exactly: ordered by created_at alone, the
	// planner walks idx_messages_feed_known backwards filtering on
	// capcode_id, which reads the whole feed for every capcode never heard
	// (minutes on prod's 2.1M rows, against 30 ms this way).
	var rows []struct {
		AgencyID int64     `db:"agency_id"`
		At       time.Time `db:"at"`
	}
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT c.agency_id, MAX(l.created_at) AS at
		  FROM capcodes c
		  CROSS JOIN LATERAL (
		        SELECT m.created_at
		          FROM messages m
		         WHERE m.capcode_id = c.id AND m.deleted_at IS NULL
		         ORDER BY m.deleted_at DESC, m.created_at DESC
		         LIMIT 1
		  ) l
		 WHERE c.deleted_at IS NULL AND c.agency_id IS NOT NULL AND l.created_at IS NOT NULL
		 GROUP BY c.agency_id`); err != nil {
		return g, fmt.Errorf("last heard per agency: %w", err)
	}
	g.lastHeard = make(map[int64]time.Time, len(rows))
	for _, r := range rows {
		g.lastHeard[r.AgencyID] = r.At
	}

	// The backfilled 'messages' rows are the oldest, so walking the
	// heard_at index to the first other row is short.
	var since sql.NullTime
	if err := s.db.GetContext(ctx, &since, `
		SELECT heard_at FROM page_receptions WHERE source <> 'messages' ORDER BY heard_at LIMIT 1`); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return g, fmt.Errorf("receptions since: %w", err)
	}
	if since.Valid {
		t := since.Time.UTC()
		g.receptionsSince = &t
	}
	s.globalsCache.put(struct{}{}, g, now)
	return g, nil
}

// ErrNotFound is returned for an unknown transmitter.
var ErrNotFound = errors.New("not found")

// ErrUnknownAgency is returned when a manual district names no agency.
var ErrUnknownAgency = errors.New("unknown agency")

// SetManualDistrict pins a transmitter to agencyID (nil = unmapped) so the
// nightly assignment leaves it alone.
func (s *Service) SetManualDistrict(ctx context.Context, transmitterID int64, agencyID *int64) error {
	if agencyID != nil {
		var exists bool
		if err := s.db.GetContext(ctx, &exists, `SELECT EXISTS (SELECT 1 FROM agencies WHERE id = $1)`, *agencyID); err != nil {
			return err
		}
		if !exists {
			return ErrUnknownAgency
		}
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE paging_transmitters
		   SET agency_id = $2, district_source = 'manual', district_confidence = NULL, updated_at = NOW()
		 WHERE id = $1`, transmitterID, agencyID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.reports.clear()
	return nil
}

// ResetDistrict returns a transmitter to automatic assignment and recomputes.
func (s *Service) ResetDistrict(ctx context.Context, transmitterID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE paging_transmitters SET district_source = 'auto', updated_at = NOW() WHERE id = $1`, transmitterID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := AssignDistricts(ctx, s.db); err != nil {
		return err
	}
	s.reports.clear()
	return nil
}

// pruneReceptions deletes receptions older than receptionRetention.
func pruneReceptions(ctx context.Context, db *sqlx.DB, now time.Time) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM page_receptions WHERE heard_at < $1`, now.Add(-receptionRetention))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
