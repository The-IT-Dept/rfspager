package handler

import (
	"context"
	"database/sql"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// recordReception notes that system heard this page, for the admin coverage
// map. It runs before dedupe so every receiver's copy counts; a failure is
// logged and never fails ingest.
func (h *MessageHandler) recordReception(ctx context.Context, system *models.System, address, message string, timestamp time.Time, capcode *models.Capcode, quality *rxproto.PageQuality) {
	if !isDispatchText(message) {
		return
	}
	if err := h.db.RecordPageReception(ctx, pageReception(system.ID, address, message, timestamp, capcode, quality)); err != nil {
		slog.Warn("record page reception failed", "system_id", system.ID, "address", address, "error", err)
	}
}

// isDispatchText reports whether a page body could be a dispatch. Numeric
// beacons such as FRNSW's clock tick on 0180111 ("1129") say nothing about
// which district we hear, and their addresses sit under RFS district
// prefixes. The migration 034 backfill applies the same rule.
func isDispatchText(message string) bool {
	return strings.ContainsFunc(message, func(r rune) bool {
		return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
	})
}

// pageReception builds the reception row for one received page. The
// timestamp is truncated to the second so the row lines up with the
// backfill and with the same page re-posted by the same receiver.
func pageReception(systemID int64, address, message string, timestamp time.Time, capcode *models.Capcode, quality *rxproto.PageQuality) db.PageReception {
	r := db.PageReception{
		SystemID: systemID,
		HeardAt:  timestamp.Truncate(time.Second),
		Address:  address,
		Damaged:  quality != nil && (quality.Uncorrectable > 0 || quality.Truncated),
	}
	if capcode != nil {
		r.CapcodeID = sql.NullInt64{Int64: capcode.ID, Valid: true}
	}
	if key := IncidentKey(message); key != "" {
		r.IncidentKey = sql.NullString{String: key, Valid: true}
	}
	// coordRegex captures "[lng,lat]".
	if m := coordRegex.FindStringSubmatch(message); len(m) == 3 {
		lng, errLng := strconv.ParseFloat(m[1], 64)
		lat, errLat := strconv.ParseFloat(m[2], 64)
		if errLng == nil && errLat == nil {
			r.Lat = sql.NullFloat64{Float64: lat, Valid: true}
			r.Lng = sql.NullFloat64{Float64: lng, Valid: true}
		}
	}
	return r
}
