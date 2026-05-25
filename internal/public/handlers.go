package public

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

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
		CapcodeCount     int `json:"capcode_count" db:"capcode_count"`
		MessageCount30d  int `json:"message_count_30d" db:"message_count_30d"`
		IncidentCount    int `json:"incident_count" db:"incident_count"`
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

// ListMessages returns paginated messages with capcode info.
// Supports ?search=, ?agency_id=, ?page=, ?per_page= query params.
func (h *Handlers) ListMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 { page = 1 }
	if perPage < 1 || perPage > 100 { perPage = 50 }
	offset := (page - 1) * perPage

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

	args = append(args, perPage+1, offset)

	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT m.id, m.address, m.message, m.created_at,
		 c.alias as capcode_alias, c.icon as capcode_icon, c.colour as capcode_colour,
		 a.agency as agency_name
		 FROM messages m
		 LEFT JOIN capcodes c ON c.id = m.capcode_id
		 LEFT JOIN agencies a ON a.id = c.agency_id
		 WHERE `+where+`
		 ORDER BY m.created_at DESC
		 LIMIT $`+strconv.Itoa(argIdx)+` OFFSET $`+strconv.Itoa(argIdx+1), args...)
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
	if limit < 1 || limit > 500 { limit = 100 }

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

// ListSystems returns the public-safe list of paging systems. Only exposes
// uuid, name, suburb, is_pending (waiting on admin approval), and the
// timestamp of the most recent message. Owner, base url, api keys and other
// operational fields are deliberately not surfaced.
func (h *Handlers) ListSystems(w http.ResponseWriter, r *http.Request) {
	// See the mobile ListSystems handler for the rationale: correlated
	// subquery so the (system_id, created_at DESC) index is used for each
	// system instead of joining the full messages table.
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT s.system_id            AS uuid,
		        s.name,
		        s.suburb,
		        (s.approved_at IS NULL) AS is_pending,
		        (SELECT MAX(m.created_at)
		           FROM messages m
		          WHERE m.system_id = s.id
		            AND m.deleted_at IS NULL) AS last_message_at
		 FROM systems s
		 WHERE s.deleted_at IS NULL
		 ORDER BY (s.approved_at IS NULL) DESC, s.name`)
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

// cleanMapScan converts []byte values to strings for JSON encoding.
func cleanMapScan(row map[string]any) {
	for k, v := range row {
		if b, ok := v.([]byte); ok {
			row[k] = string(b)
		}
	}
}
