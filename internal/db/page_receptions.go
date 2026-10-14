package db

import (
	"context"
	"database/sql"
	"time"
)

// PageReception is one receiver's copy of one posted page, for the admin
// coverage map. Recorded at ingest before dedupe (see migration 034).
type PageReception struct {
	SystemID    int64
	HeardAt     time.Time
	Address     string
	CapcodeID   sql.NullInt64
	Lat         sql.NullFloat64
	Lng         sql.NullFloat64
	IncidentKey sql.NullString
	Damaged     bool
}

// RecordPageReception stores a live reception. A repeat of the same
// (system, address, second) is ignored, which also keeps it idempotent with
// the migration backfill.
func (d *DB) RecordPageReception(ctx context.Context, r PageReception) error {
	_, err := d.conn.ExecContext(ctx, `
		INSERT INTO page_receptions
		    (system_id, heard_at, address, capcode_id, lat, lng, incident_key, damaged, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'live')
		ON CONFLICT (system_id, address, heard_at) DO NOTHING`,
		r.SystemID, r.HeardAt, r.Address, r.CapcodeID, r.Lat, r.Lng, r.IncidentKey, r.Damaged)
	return err
}
