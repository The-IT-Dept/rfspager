package public

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

type Handlers struct {
	db *sqlx.DB
}

func NewHandlers(db *sqlx.DB) *Handlers {
	return &Handlers{db: db}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Stats returns aggregate counts for the homepage.
func (h *Handlers) Stats(w http.ResponseWriter, r *http.Request) {
	var stats struct {
		CapcodeCount    int `json:"capcode_count" db:"capcode_count"`
		MessageCount30d int `json:"message_count_30d" db:"message_count_30d"`
		IncidentCount   int `json:"incident_count" db:"incident_count"`
	}

	h.db.GetContext(r.Context(), &stats.CapcodeCount,
		`SELECT COUNT(*) FROM capcodes WHERE deleted_at IS NULL`)
	h.db.GetContext(r.Context(), &stats.MessageCount30d,
		`SELECT COUNT(*) FROM messages WHERE deleted_at IS NULL AND created_at >= $1`,
		time.Now().AddDate(0, 0, -30))
	h.db.GetContext(r.Context(), &stats.IncidentCount,
		`SELECT COUNT(*) FROM majorincidents`)

	writeJSON(w, http.StatusOK, stats)
}

// ListAgencies returns all agencies for the filter dropdown.
func (h *Handlers) ListAgencies(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT a.id, a.agency as name, COUNT(c.id) as capcode_count
		 FROM agencies a
		 LEFT JOIN capcodes c ON c.agency_id = a.id AND c.deleted_at IS NULL
		 GROUP BY a.id, a.agency
		 HAVING COUNT(c.id) > 0
		 ORDER BY a.agency`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	data := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		rows.MapScan(row)
		cleanMapScan(row)
		data = append(data, row)
	}
	writeJSON(w, http.StatusOK, data)
}

// limitClause builds the tail of the feed query. Cursor mode has no OFFSET —
// the WHERE clause already positions the reader.
func limitClause(useCursor bool, argIdx int) string {
	if useCursor {
		return "LIMIT $" + strconv.Itoa(argIdx)
	}
	return "LIMIT $" + strconv.Itoa(argIdx) + " OFFSET $" + strconv.Itoa(argIdx+1)
}

// ListMessages returns paginated messages with capcode info.
// Supports ?search=, ?agency_id=, ?page=, ?per_page= query params.
//
// For infinite scroll, prefer the keyset cursor (?before_created_at= &
// ?before_id=) over ?page=. The feed prepends live messages as they arrive, so
// a numeric OFFSET shifts under the reader between requests and silently
// duplicates or skips rows. The cursor is stable regardless of inserts.
func (h *Handlers) ListMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	offset := (page - 1) * perPage
	if offset < 0 {
		// A page near math.MaxInt64 overflows the int64 product and wraps
		// negative; Postgres rejects a negative OFFSET (2201X).
		offset = 0
	}

	where := "m.deleted_at IS NULL AND m.capcode_id IS NOT NULL"
	args := []any{}
	argIdx := 1

	if search := q.Get("search"); search != "" {
		where += " AND m.message ILIKE $" + strconv.Itoa(argIdx)
		args = append(args, "%"+search+"%")
		argIdx++
	}

	if agencyID := q.Get("agency_id"); agencyID != "" {
		where += " AND a.id = $" + strconv.Itoa(argIdx)
		args = append(args, agencyID)
		argIdx++
	}

	// Keyset cursor. created_at is not unique, so id is the tiebreaker and the
	// row comparison must match the ORDER BY exactly.
	useCursor := false
	if bc, bi := q.Get("before_created_at"), q.Get("before_id"); bc != "" && bi != "" {
		if ts, err := time.Parse(time.RFC3339Nano, bc); err == nil {
			if id, err := strconv.ParseInt(bi, 10, 64); err == nil {
				where += " AND (m.created_at, m.id) < ($" + strconv.Itoa(argIdx) +
					", $" + strconv.Itoa(argIdx+1) + ")"
				args = append(args, ts, id)
				argIdx += 2
				useCursor = true
			}
		}
	}

	if useCursor {
		args = append(args, perPage+1)
	} else {
		args = append(args, perPage+1, offset)
	}

	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT m.id, m.address, m.message, m.created_at,
		        c.alias  AS capcode_alias,
		        c.icon   AS capcode_icon,
		        c.colour AS capcode_colour,
		        a.agency AS agency_name,
		        s.system_id AS system_uuid,
		        s.name      AS system_name
		   FROM messages m
		   LEFT JOIN capcodes c ON c.id = m.capcode_id
		   LEFT JOIN agencies a ON a.id = c.agency_id
		   LEFT JOIN systems  s ON s.id = m.system_id AND s.deleted_at IS NULL
		  WHERE `+where+`
		  ORDER BY m.created_at DESC, m.id DESC
		  `+limitClause(useCursor, argIdx), args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	data := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		rows.MapScan(row)
		cleanMapScan(row)
		data = append(data, row)
	}

	hasMore := len(data) > perPage
	if hasMore {
		data = data[:perPage]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":     data,
		"page":     page,
		"per_page": perPage,
		"has_more": hasMore,
	})
}

// MapMessages returns messages with coordinates for the map.
func (h *Handlers) MapMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 500 {
		limit = 100
	}

	after := time.Now().Add(-24 * time.Hour)
	if a := q.Get("created_after"); a != "" {
		if t, err := time.Parse(time.RFC3339, a); err == nil {
			after = t
		}
	}

	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT m.id, m.address, m.message, m.lat, m.lng, m.created_at,
		 c.alias as capcode_alias, c.icon as capcode_icon, c.colour as capcode_colour,
		 a.agency as agency_name
		 FROM messages m
		 LEFT JOIN capcodes c ON c.id = m.capcode_id
		 LEFT JOIN agencies a ON a.id = c.agency_id
		 WHERE m.deleted_at IS NULL AND m.lat IS NOT NULL AND m.lng IS NOT NULL
		 AND m.created_at >= $1
		 ORDER BY m.created_at DESC LIMIT $2`, after, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	data := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		rows.MapScan(row)
		cleanMapScan(row)
		data = append(data, row)
	}
	writeJSON(w, http.StatusOK, data)
}

// ListSystems returns the public-safe list of approved paging systems
// (pending applications are private to the applicant). Only exposes
// uuid, name, suburb, is_pending (always false here; kept for API
// compatibility), the timestamp
// of the most recent message, and the live receiver WebSocket ping. Owner,
// base url, api keys and other operational fields are deliberately not
// surfaced.
//
// `last_ping_at` is what the frontend should use for the green online dot:
// a receiver that hasn't decoded a message in hours is still "online" if
// the WebSocket is up and pinging.
func (h *Handlers) ListSystems(w http.ResponseWriter, r *http.Request) {
	// See the mobile ListSystems handler for the rationale: correlated
	// subquery so the (system_id, created_at DESC) index is used for each
	// system instead of joining the full messages table.
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT s.system_id            AS uuid,
		        s.name,
		        s.suburb,
		        (s.approved_at IS NULL) AS is_pending,
		        sc.last_ping_at         AS last_ping_at,
		        sc.connected_at         AS connected_at,
		        (SELECT MAX(m.created_at)
		           FROM messages m
		          WHERE m.system_id = s.id
		            AND m.deleted_at IS NULL) AS last_message_at
		 FROM systems s
		 LEFT JOIN system_connections sc ON sc.system_id = s.id
		 WHERE s.deleted_at IS NULL
		   AND s.approved_at IS NOT NULL -- pending applications are private
		 ORDER BY s.name`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	data := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		rows.MapScan(row)
		cleanMapScan(row)
		data = append(data, row)
	}
	writeJSON(w, http.StatusOK, data)
}

// MajorIncidents returns current major incidents.
func (h *Handlers) MajorIncidents(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT id, title, category, geometry, data, created_at, updated_at
		 FROM majorincidents ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	data := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		rows.MapScan(row)
		cleanMapScan(row)
		data = append(data, row)
	}
	writeJSON(w, http.StatusOK, data)
}

// ListCapcodes returns the full capcode catalogue, paginated. Public read-
// only — anyone can browse what we're decoding. Supports ?search=, ?agency_id=,
// ?page=, ?per_page=. Same shape as the mobile endpoint so the web and the
// app can share a TypeScript type.
func (h *Handlers) ListCapcodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	if perPage > 2500 {
		perPage = 2500
	}

	where := "c.deleted_at IS NULL"
	args := []any{}
	argIdx := 1
	if agencyID := q.Get("agency_id"); agencyID != "" {
		where += " AND c.agency_id = $" + strconv.Itoa(argIdx)
		args = append(args, agencyID)
		argIdx++
	}
	if search := q.Get("search"); search != "" {
		where += " AND (c.alias ILIKE $" + strconv.Itoa(argIdx) +
			" OR c.address ILIKE $" + strconv.Itoa(argIdx) +
			" OR a.agency ILIKE $" + strconv.Itoa(argIdx) + ")"
		args = append(args, "%"+search+"%")
		argIdx++
	}

	offset := (page - 1) * perPage
	if offset < 0 {
		offset = 0
	}
	args = append(args, perPage+1, offset)
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT c.id, c.address, c.alias, c.icon, c.colour, c.agency_id,
		        a.agency AS agency_name
		 FROM capcodes c
		 LEFT JOIN agencies a ON a.id = c.agency_id
		 WHERE `+where+`
		 ORDER BY (c.alias IS NULL OR c.alias = '') ASC, c.alias ASC, c.address ASC
		 LIMIT $`+strconv.Itoa(argIdx)+` OFFSET $`+strconv.Itoa(argIdx+1),
		args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	data := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		rows.MapScan(row)
		cleanMapScan(row)
		data = append(data, row)
	}
	hasMore := len(data) > perPage
	if hasMore {
		data = data[:perPage]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":     data,
		"page":     page,
		"per_page": perPage,
		"has_more": hasMore,
	})
}

// UnknownCapcodes lists addresses that have shown up in recent messages but
// either don't have a capcodes row at all OR have a row with no alias. These
// are what the community can help us fill in. Default window: last 30 days.
// Result ordered by message_count DESC so the most-seen unknowns are first.
func (h *Handlers) UnknownCapcodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	if days < 1 || days > 365 {
		days = 30
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 1000 {
		limit = 200
	}

	rows, err := h.db.QueryxContext(r.Context(), `
		SELECT
		    COALESCE(c.id, 0)::bigint AS capcode_id,
		    COALESCE(c.address, m.address) AS address,
		    c.alias,
		    a.agency AS agency_name,
		    COUNT(m.id)::bigint AS message_count,
		    MAX(m.created_at) AS last_seen_at
		  FROM messages m
		  LEFT JOIN capcodes c ON c.id = m.capcode_id
		  LEFT JOIN agencies a ON a.id = c.agency_id
		 WHERE m.deleted_at IS NULL
		   AND m.created_at >= NOW() - ($1 || ' days')::interval
		   AND (m.capcode_id IS NULL OR c.alias IS NULL OR TRIM(c.alias) = '')
		 GROUP BY c.id, c.address, m.address, c.alias, a.agency
		 ORDER BY message_count DESC, last_seen_at DESC
		 LIMIT $2`, strconv.Itoa(days), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	data := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		rows.MapScan(row)
		cleanMapScan(row)
		// Normalise the sentinel 0 → null so clients can distinguish "no
		// capcode row" from "capcode row with id 0".
		if id, ok := row["capcode_id"].(int64); ok && id == 0 {
			row["capcode_id"] = nil
		}
		data = append(data, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data,
		"meta": map[string]any{
			"window_days": days,
			"limit":       limit,
		},
	})
}

// SystemUptime returns the last 7 days of online/offline transitions and an
// uptime percentage for a single receiver. Public — no log content, no IPs,
// no metadata about hardware. Just "was the receiver up?" — same level of
// detail as a status-page bar chart.
func (h *Handlers) SystemUptime(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "uuid required"})
		return
	}
	var systemID int64
	if err := h.db.GetContext(r.Context(), &systemID,
		`SELECT id FROM systems
		  WHERE system_id = $1 AND deleted_at IS NULL AND approved_at IS NOT NULL`, uuid); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "system not found"})
		return
	}

	// Pull the relevant transition events from the last 7 days, oldest-first.
	rows, err := h.db.QueryxContext(r.Context(), `
		SELECT type, created_at
		  FROM system_events
		 WHERE system_id = $1
		   AND type IN ('receiver_connected', 'receiver_disconnected', 'receiver_online', 'receiver_offline')
		   AND created_at >= NOW() - INTERVAL '7 days'
		 ORDER BY created_at ASC`, systemID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type ev struct {
		Type string
		At   time.Time
	}
	var evs []ev
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.Type, &e.At); err == nil {
			evs = append(evs, e)
		}
	}

	// Walk events, accumulating downtime intervals + uptime seconds. We treat
	// `receiver_offline` as down-start and `receiver_online` / `receiver_connected`
	// as down-end. `receiver_disconnected` alone doesn't count as down for the
	// public number until the 5-minute threshold fires.
	windowStart := time.Now().Add(-7 * 24 * time.Hour)
	totalWindow := time.Since(windowStart)

	downIntervals := []map[string]any{}
	var (
		totalDown time.Duration
		downStart *time.Time
	)
	for _, e := range evs {
		switch e.Type {
		case "receiver_offline":
			if downStart == nil {
				t := e.At
				downStart = &t
			}
		case "receiver_online", "receiver_connected":
			if downStart != nil {
				totalDown += e.At.Sub(*downStart)
				downIntervals = append(downIntervals, map[string]any{
					"from": *downStart,
					"to":   e.At,
				})
				downStart = nil
			}
		}
	}
	// Currently offline? Count the open interval up to now.
	openInterval := false
	if downStart != nil {
		totalDown += time.Since(*downStart)
		downIntervals = append(downIntervals, map[string]any{
			"from": *downStart,
			"to":   nil,
		})
		openInterval = true
	}

	uptimePct := 100.0
	if totalWindow > 0 {
		uptimePct = (1.0 - float64(totalDown)/float64(totalWindow)) * 100
		if uptimePct < 0 {
			uptimePct = 0
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"uuid":               uuid,
		"window_days":        7,
		"window_start":       windowStart,
		"uptime_percent":     uptimePct,
		"downtime_seconds":   int(totalDown.Seconds()),
		"downtime_intervals": downIntervals,
		"currently_offline":  openInterval,
	})
}

// CapcodeMessages returns the most recent messages for a single capcode
// address. Used by the suggestion form so reviewers (and suggesters) can see
// the kind of pages that come through this address before guessing what it
// represents. Public — no more sensitive than the existing pager feed.
func (h *Handlers) CapcodeMessages(w http.ResponseWriter, r *http.Request) {
	addr := strings.TrimSpace(r.URL.Query().Get("address"))
	if addr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "address is required"})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 50 {
		limit = 10
	}

	rows, err := h.db.QueryxContext(r.Context(), `
		SELECT m.id, m.address, m.message, m.created_at,
		       s.name AS system_name
		  FROM messages m
		  LEFT JOIN systems s ON s.id = m.system_id AND s.deleted_at IS NULL
		 WHERE m.deleted_at IS NULL
		   AND m.address = $1
		 ORDER BY m.created_at DESC
		 LIMIT $2`, addr, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	data := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		rows.MapScan(row)
		cleanMapScan(row)
		data = append(data, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// Stations returns NSW emergency service stations (RFS, FRNSW, police,
// ambulance, SES) as a GeoJSON FeatureCollection so the map can use it
// directly as a Mapbox/MapLibre source. Filtered client-side by category
// toggle. ~2,800 features for all-NSW, ~600 KB JSON — fine to ship in one
// response. Optional ?categories=rfs,police narrows the result.
//
// Data sourced from:
//   - Geoscience Australia Emergency Management Facilities Database (CC-BY 4.0)
//   - Fire and Rescue NSW station GeoRSS feed
//
// Attribution is required by both upstream licences and is included in the
// response envelope.
func (h *Handlers) Stations(w http.ResponseWriter, r *http.Request) {
	q := `SELECT id, source, category, name, operator, address, suburb,
	             postcode, status, lat, lng, revised_at
	      FROM emergency_stations
	      WHERE state = 'NSW'`
	args := []any{}
	if cat := r.URL.Query().Get("categories"); cat != "" {
		parts := strings.Split(cat, ",")
		clean := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				clean = append(clean, p)
			}
		}
		if len(clean) > 0 {
			placeholders := make([]string, len(clean))
			for i, p := range clean {
				args = append(args, p)
				placeholders[i] = fmt.Sprintf("$%d", i+1)
			}
			q += " AND category IN (" + strings.Join(placeholders, ",") + ")"
		}
	}
	q += " ORDER BY category, name"

	rows, err := h.db.QueryxContext(r.Context(), q, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	features := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		if err := rows.MapScan(row); err != nil {
			continue
		}
		cleanMapScan(row)

		lat, _ := asFloat(row["lat"])
		lng, _ := asFloat(row["lng"])
		delete(row, "lat")
		delete(row, "lng")

		features = append(features, map[string]any{
			"type":       "Feature",
			"id":         row["id"],
			"geometry":   map[string]any{"type": "Point", "coordinates": []float64{lng, lat}},
			"properties": row,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"type":     "FeatureCollection",
		"features": features,
		"meta": map[string]any{
			"attribution": "Stations sourced from Geoscience Australia Emergency Management Facilities Database (CC-BY 4.0) and Fire and Rescue NSW.",
			"count":       len(features),
		},
	})
}

// asFloat coerces a value (which might come back as string, []byte, or float)
// to a float64. Postgres NUMERIC columns are decoded as []byte by sqlx.
func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	case []byte:
		f, err := strconv.ParseFloat(string(x), 64)
		return f, err == nil
	}
	return 0, false
}

// cleanMapScan converts []byte values to strings for JSON encoding.
func cleanMapScan(row map[string]any) {
	for k, v := range row {
		if b, ok := v.([]byte); ok {
			row[k] = string(b)
		}
	}
}
