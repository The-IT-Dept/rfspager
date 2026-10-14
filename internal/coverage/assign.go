package coverage

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
)

// incidentLookback is how far back geocoded incidents label districts.
const incidentLookback = "365 days"

// AssignResult summarises one district assignment pass.
type AssignResult struct {
	Assigned int // RFS transmitters given a district
	Unmapped int // RFS transmitters with no labelled point in range
	FRNSW    int // FRNSW transmitters given the FRNSW agency
}

// AssignDistricts recomputes agency_id for every district_source = 'auto'
// transmitter. FRNSW-channel transmitters all take the FRNSW agency;
// RFS-channel ones take the district voted by nearby brigade stations and
// geocoded incidents (voteDistrict). Manual rows are never touched.
func AssignDistricts(ctx context.Context, db *sqlx.DB) (AssignResult, error) {
	var res AssignResult

	var agencies []Agency
	if err := db.SelectContext(ctx, &agencies, `SELECT id, agency, coverage_district FROM agencies`); err != nil {
		return res, fmt.Errorf("load agencies: %w", err)
	}
	ix := newAgencyIndex(agencies)
	isDistrict := func(id int64) bool {
		a, ok := ix.byID[id]
		return ok && a.CoverageDistrict && id != ix.frnswID
	}

	var capcodes []Capcode
	if err := db.SelectContext(ctx, &capcodes,
		`SELECT id, address, alias, agency_id, "ignore" FROM capcodes WHERE deleted_at IS NULL`); err != nil {
		return res, fmt.Errorf("load capcodes: %w", err)
	}
	var stations []Station
	if err := db.SelectContext(ctx, &stations,
		`SELECT name, lat::float8 AS lat, lng::float8 AS lng FROM emergency_stations WHERE category = 'rfs'`); err != nil {
		return res, fmt.Errorf("load stations: %w", err)
	}
	points := stationLabels(stations, capcodes, isDistrict)
	nStations := len(points)

	// One point per ~1 km cell per agency. Coordinates come from the stored
	// lat/lng or, for rows without them, the "[lng,lat]" tail of the body.
	var cells []struct {
		AgencyID int64   `db:"agency_id"`
		Lat      float64 `db:"lat"`
		Lng      float64 `db:"lng"`
	}
	if err := db.SelectContext(ctx, &cells, `
		SELECT DISTINCT agency_id, round(lat::numeric, 2)::float8 AS lat, round(lng::numeric, 2)::float8 AS lng
		  FROM (
		        SELECT c.agency_id,
		               CASE WHEN m.lat ~ '^-?[0-9]+(\.[0-9]+)?$' AND m.lng ~ '^-?[0-9]+(\.[0-9]+)?$'
		                    THEN m.lat::float8
		                    ELSE (regexp_match(m.message, '\[(1[0-9]{2}\.[0-9]+),(-[0-9]{2}\.[0-9]+)\]'))[2]::float8 END AS lat,
		               CASE WHEN m.lat ~ '^-?[0-9]+(\.[0-9]+)?$' AND m.lng ~ '^-?[0-9]+(\.[0-9]+)?$'
		                    THEN m.lng::float8
		                    ELSE (regexp_match(m.message, '\[(1[0-9]{2}\.[0-9]+),(-[0-9]{2}\.[0-9]+)\]'))[1]::float8 END AS lng
		          FROM messages m
		          JOIN capcodes c ON c.id = m.capcode_id
		         WHERE m.created_at >= NOW() - $1::interval
		           AND m.deleted_at IS NULL
		           AND m.capcode_id IS NOT NULL
		           AND c.agency_id IS NOT NULL
		  ) g
		 WHERE lat IS NOT NULL AND lng IS NOT NULL`, incidentLookback); err != nil {
		return res, fmt.Errorf("load incident cells: %w", err)
	}
	for _, c := range cells {
		if isDistrict(c.AgencyID) && inNSW(c.Lat, c.Lng) {
			points = append(points, LabelPoint{Lat: c.Lat, Lng: c.Lng, AgencyID: c.AgencyID, Weight: incidentWeight})
		}
	}

	var txs []Transmitter
	if err := db.SelectContext(ctx, &txs, `
		SELECT id, channel, name, licensee, lat, lng, agency_id, district_source
		  FROM paging_transmitters
		 WHERE district_source = 'auto'`); err != nil {
		return res, fmt.Errorf("load transmitters: %w", err)
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	for _, t := range txs {
		var agencyID *int64
		var confidence *float64
		switch t.Channel {
		case ChannelFRNSW:
			if ix.frnswID != 0 {
				id, one := ix.frnswID, 1.0
				agencyID, confidence = &id, &one
				res.FRNSW++
			}
		default:
			if id, conf, ok := voteDistrict(t.Lat, t.Lng, points); ok {
				conf = round(conf, 3)
				agencyID, confidence = &id, &conf
				res.Assigned++
			} else {
				res.Unmapped++
			}
		}
		// The district_source guard loses gracefully to an admin override
		// made while this pass ran.
		if _, err := tx.ExecContext(ctx, `
			UPDATE paging_transmitters
			   SET agency_id = $2,
			       district_confidence = $3,
			       updated_at = CASE WHEN agency_id IS DISTINCT FROM $2 THEN NOW() ELSE updated_at END
			 WHERE id = $1 AND district_source = 'auto'`, t.ID, agencyID, confidence); err != nil {
			return res, fmt.Errorf("update transmitter %d: %w", t.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	slog.Info("transmitter districts assigned",
		"assigned", res.Assigned, "unmapped", res.Unmapped, "frnsw", res.FRNSW,
		"station_points", nStations, "incident_points", len(points)-nStations)
	return res, nil
}
