package mobile

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type Handlers struct {
	db *sqlx.DB
}

func NewHandlers(db *sqlx.DB) *Handlers {
	return &Handlers{db: db}
}

// --- Me ---

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())

	var result struct {
		AuthUser
		NotificationsCount       int `db:"notifications_count" json:"notifications_count"`
		UnreadNotificationsCount int `db:"unread_notifications_count" json:"unread_notifications_count"`
	}
	err := h.db.GetContext(r.Context(), &result,
		`SELECT id, user_id, name, email, subscription_valid, subscription_product_id,
		 subscription_expiry, will_renew, subscription_platform, superuser,
		 (SELECT COUNT(*) FROM notifications WHERE notifiable_id = u.id AND notifiable_type = 'App\Models\User') as notifications_count,
		 (SELECT COUNT(*) FROM notifications WHERE notifiable_id = u.id AND notifiable_type = 'App\Models\User' AND read_at IS NULL) as unread_notifications_count
		 FROM users u WHERE u.id = $1`, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch user"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Messages ---

func (h *Handlers) ListMessages(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 { page = 1 }
	if perPage < 1 { perPage = 25 }
	if perPage > 100 { perPage = 100 }

	where := "m.deleted_at IS NULL"
	args := []any{}
	argIdx := 1

	// Capcode filter
	if capcodeID := q.Get("capcode_id"); capcodeID != "" {
		where += " AND m.capcode_id = $" + strconv.Itoa(argIdx)
		args = append(args, capcodeID)
		argIdx++
	}

	// Show only user's filtered capcodes
	if q.Get("show_only_filtered") == "true" && user.SubscriptionValid {
		where += " AND m.capcode_id IN (SELECT capcode_id FROM filters WHERE user_id = $" + strconv.Itoa(argIdx) + ")"
		args = append(args, user.ID)
		argIdx++
	}

	// Show unknown (no capcode) messages — only paid users can opt in
	if q.Get("show_unknown") == "true" && user.SubscriptionValid {
		// Show only unknown messages
		where += " AND m.capcode_id IS NULL"
	} else {
		// Default: hide unknown messages
		where += " AND m.capcode_id IS NOT NULL"
	}

	// Search
	if search := q.Get("search"); search != "" {
		where += " AND (m.message ILIKE $" + strconv.Itoa(argIdx) + " OR m.address ILIKE $" + strconv.Itoa(argIdx) + ")"
		args = append(args, "%"+search+"%")
		argIdx++
	}

	// Date filter
	if after := q.Get("created_after"); after != "" {
		where += " AND m.created_at >= $" + strconv.Itoa(argIdx)
		args = append(args, after)
		argIdx++
	}

	// Fetch perPage+1 to detect if there's a next page (avoids expensive COUNT)
	offset := (page - 1) * perPage
	fetchLimit := perPage + 1
	// Add user ID for responder check
	args = append(args, user.ID)
	userArgIdx := argIdx
	argIdx++

	dataQuery := `SELECT m.id, m.system_id, m.capcode_id, m.address, m.message, m.lat, m.lng, m.created_at,
		c.alias as capcode_alias, c.icon as capcode_icon, c.colour as capcode_colour,
		a.agency as agency_name,
		(SELECT COUNT(*) FROM responding r WHERE r.message_id = m.id) as responder_count,
		EXISTS(SELECT 1 FROM responding r WHERE r.message_id = m.id AND r.user_id = $` + strconv.Itoa(userArgIdx) + `) as am_i_responding
		FROM messages m
		LEFT JOIN capcodes c ON c.id = m.capcode_id
		LEFT JOIN agencies a ON a.id = c.agency_id
		WHERE ` + where + ` ORDER BY m.created_at DESC LIMIT $` + strconv.Itoa(argIdx) + ` OFFSET $` + strconv.Itoa(argIdx+1)
	args = append(args, fetchLimit, offset)

	rows, err := h.db.QueryxContext(r.Context(), dataQuery, args...)
	if err != nil {
		slog.Error("list messages failed", "error", err)
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

	// If we got perPage+1 results, there's a next page — trim to perPage
	hasMore := len(data) > perPage
	if hasMore {
		data = data[:perPage]
	}
	totalPages := page
	if hasMore {
		totalPages = page + 1
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "page": page, "per_page": perPage,
		"total_pages": totalPages,
	})
}

func (h *Handlers) GetMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row := h.db.QueryRowxContext(r.Context(),
		`SELECT m.id, m.system_id, m.capcode_id, m.address, m.message, m.lat, m.lng, m.created_at,
		        c.alias  AS capcode_alias,
		        c.icon   AS capcode_icon,
		        c.colour AS capcode_colour,
		        a.agency AS agency_name,
		        s.system_id AS system_uuid,
		        s.name      AS system_name,
		        s.suburb    AS system_suburb
		   FROM messages m
		   LEFT JOIN capcodes c ON c.id = m.capcode_id
		   LEFT JOIN agencies a ON a.id = c.agency_id
		   LEFT JOIN systems  s ON s.id = m.system_id AND s.deleted_at IS NULL
		  WHERE m.id = $1 AND m.deleted_at IS NULL`, id)
	result := make(map[string]any)
	if err := row.MapScan(result); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	cleanMapScan(result)

	// Get responders
	var responders []map[string]any
	resRows, _ := h.db.QueryxContext(r.Context(),
		`SELECT r.id, r.user_id, u.name, u.email FROM responding r JOIN users u ON u.id = r.user_id WHERE r.message_id = $1`, id)
	if resRows != nil {
		defer resRows.Close()
		for resRows.Next() {
			rr := make(map[string]any)
			resRows.MapScan(rr)
			cleanMapScan(rr)
			responders = append(responders, rr)
		}
	}
	result["responders"] = responders

	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) MapMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 500 { limit = 100 }

	where := "m.deleted_at IS NULL AND m.lat IS NOT NULL AND m.lng IS NOT NULL"
	args := []any{}
	argIdx := 1
	if after := q.Get("created_after"); after != "" {
		where += " AND m.created_at >= $" + strconv.Itoa(argIdx)
		args = append(args, after)
		argIdx++
	}

	args = append(args, limit)
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT m.id, m.address, m.message, m.lat, m.lng, m.created_at,
		 c.alias as capcode_alias, c.icon as capcode_icon, c.colour as capcode_colour,
		 a.agency as agency_name
		 FROM messages m
		 LEFT JOIN capcodes c ON c.id = m.capcode_id
		 LEFT JOIN agencies a ON a.id = c.agency_id
		 WHERE `+where+` ORDER BY m.created_at DESC LIMIT $`+strconv.Itoa(argIdx), args...)
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

func (h *Handlers) Respond(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	messageID := chi.URLParam(r, "id")

	// Toggle: delete if exists, create if not
	var exists bool
	h.db.GetContext(r.Context(), &exists,
		`SELECT EXISTS(SELECT 1 FROM responding WHERE user_id = $1 AND message_id = $2)`,
		user.ID, messageID)

	if exists {
		h.db.ExecContext(r.Context(), `DELETE FROM responding WHERE user_id = $1 AND message_id = $2`, user.ID, messageID)
	} else {
		h.db.ExecContext(r.Context(), `INSERT INTO responding (user_id, message_id, created_at, updated_at) VALUES ($1, $2, NOW(), NOW())`, user.ID, messageID)
	}

	writeJSON(w, http.StatusOK, map[string]any{"responding": !exists})
}

// --- Capcodes ---

func (h *Handlers) ListCapcodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 { page = 1 }
	if perPage < 1 { perPage = 50 }
	if perPage > 2500 { perPage = 2500 }

	where := "c.deleted_at IS NULL"
	args := []any{}
	argIdx := 1
	if agencyID := q.Get("agency_id"); agencyID != "" {
		where += " AND c.agency_id = $" + strconv.Itoa(argIdx)
		args = append(args, agencyID)
		argIdx++
	}
	if search := q.Get("search"); search != "" {
		where += " AND (c.alias ILIKE $" + strconv.Itoa(argIdx) + " OR c.address ILIKE $" + strconv.Itoa(argIdx) + " OR a.agency ILIKE $" + strconv.Itoa(argIdx) + ")"
		args = append(args, "%"+search+"%")
		argIdx++
	}

	// Use LIMIT n+1 trick for fast pagination
	offset := (page - 1) * perPage
	fetchLimit := perPage + 1
	args = append(args, fetchLimit, offset)
	rows, _ := h.db.QueryxContext(r.Context(),
		`SELECT c.id, c.address, c.alias, c.icon, c.colour, c.agency_id, a.agency as agency_name
		 FROM capcodes c LEFT JOIN agencies a ON a.id = c.agency_id
		 WHERE `+where+` ORDER BY c.alias ASC LIMIT $`+strconv.Itoa(argIdx)+` OFFSET $`+strconv.Itoa(argIdx+1), args...)
	defer rows.Close()

	data := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		rows.MapScan(row)
		cleanMapScan(row)
		data = append(data, row)
	}
	hasMore := len(data) > perPage
	if hasMore { data = data[:perPage] }
	totalPages := page
	if hasMore { totalPages = page + 1 }
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "page": page, "per_page": perPage, "total_pages": totalPages,
	})
}

// --- Agencies ---

func (h *Handlers) ListAgencies(w http.ResponseWriter, r *http.Request) {
	rows, _ := h.db.QueryxContext(r.Context(),
		`SELECT a.id, a.agency as name,
		 (SELECT COUNT(*) FROM capcodes c WHERE c.agency_id = a.id AND c.deleted_at IS NULL) as capcode_count
		 FROM agencies a ORDER BY a.agency ASC`)
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

// --- Subscriptions (capcode) ---

func (h *Handlers) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	ids := []int64{}
	h.db.SelectContext(r.Context(), &ids, `SELECT capcode_id FROM subscriptions WHERE user_id = $1`, user.ID)
	writeJSON(w, http.StatusOK, ids)
}

func (h *Handlers) CreateSubscription(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	var req struct{ CapcodeID int64 `json:"capcode_id"` }
	json.NewDecoder(r.Body).Decode(&req)

	_, err := h.db.ExecContext(r.Context(),
		`INSERT INTO subscriptions (user_id, capcode_id, created_at, updated_at) VALUES ($1, $2, NOW(), NOW()) ON CONFLICT DO NOTHING`,
		user.ID, req.CapcodeID)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *Handlers) DeleteSubscription(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	capcodeID := chi.URLParam(r, "capcode_id")
	h.db.ExecContext(r.Context(), `DELETE FROM subscriptions WHERE user_id = $1 AND capcode_id = $2`, user.ID, capcodeID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Filters (capcode) ---

func (h *Handlers) ListFilters(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	ids := []int64{}
	h.db.SelectContext(r.Context(), &ids, `SELECT capcode_id FROM filters WHERE user_id = $1`, user.ID)
	writeJSON(w, http.StatusOK, ids)
}

func (h *Handlers) CreateFilter(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	var req struct{ CapcodeID int64 `json:"capcode_id"` }
	json.NewDecoder(r.Body).Decode(&req)

	h.db.ExecContext(r.Context(),
		`INSERT INTO filters (user_id, capcode_id, created_at, updated_at) VALUES ($1, $2, NOW(), NOW()) ON CONFLICT DO NOTHING`,
		user.ID, req.CapcodeID)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *Handlers) DeleteFilter(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	capcodeID := chi.URLParam(r, "capcode_id")
	h.db.ExecContext(r.Context(), `DELETE FROM filters WHERE user_id = $1 AND capcode_id = $2`, user.ID, capcodeID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Capcode Suggestions ---

// CreateCapcodeSuggestion lets any logged-in user propose either a new
// capcode (capcode_id null, address required) or an amendment to an existing
// one (capcode_id set). Admins review and approve via the admin queue.
//
// We don't validate field membership beyond "must have address" — the form
// can submit a partial change (e.g. just an alias correction) and the admin
// reviewer fills in the rest.
func (h *Handlers) CreateCapcodeSuggestion(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	var req struct {
		Address   string `json:"address"`
		Alias     string `json:"alias"`
		Agency    string `json:"agency"`
		Icon      string `json:"icon"`
		Colour    string `json:"colour"`
		Note      string `json:"note"`
		CapcodeID *int64 `json:"capcode_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.Address == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "address is required"})
		return
	}

	// At least one of alias / agency / note must be populated; otherwise the
	// submission is empty noise.
	if req.Alias == "" && req.Agency == "" && req.Note == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "provide an alias, agency, or note",
		})
		return
	}

	var id int64
	err := h.db.QueryRowxContext(r.Context(), `
		INSERT INTO capcode_suggestions
		    (user_id, capcode_id, address, alias, agency, icon, colour, note, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NOW(), NOW())
		RETURNING id`,
		user.ID, req.CapcodeID, req.Address, req.Alias, req.Agency,
		req.Icon, req.Colour, req.Note).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"status": "ok",
		"id":     id,
	})
}

// ListMyCapcodeSuggestions returns the current user's submitted suggestions
// still waiting for review (rows are deleted on approve or reject).
func (h *Handlers) ListMyCapcodeSuggestions(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	rows, err := h.db.QueryxContext(r.Context(), `
		SELECT cs.id, cs.capcode_id, cs.address, cs.alias, cs.agency,
		       cs.icon, cs.colour, cs.note, cs.created_at,
		       c.alias  AS current_alias,
		       a.agency AS current_agency
		  FROM capcode_suggestions cs
		  LEFT JOIN capcodes c ON c.id = cs.capcode_id
		  LEFT JOIN agencies a ON a.id = c.agency_id
		 WHERE cs.user_id = $1
		 ORDER BY cs.created_at DESC
		 LIMIT 200`, user.ID)
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

// --- Systems ---

// ListSystems returns paging systems with public-safe fields plus an
// `is_owner` marker (true if the system belongs to the current user) and
// `is_pending` (true while awaiting admin approval). Sorted: owned first,
// then pending, then approved by name.
func (h *Handlers) ListSystems(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())

	// Correlated subquery (not JOIN+GROUP BY) so the planner can use the
	// (system_id, created_at DESC) index for an O(log n) probe per system,
	// instead of joining the full 2M+ row messages table and aggregating.
	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT s.system_id AS uuid,
		        s.name,
		        s.suburb,
		        (s.user_id = $1)        AS is_owner,
		        (s.approved_at IS NULL) AS is_pending,
		        (SELECT MAX(m.created_at)
		           FROM messages m
		          WHERE m.system_id = s.id
		            AND m.deleted_at IS NULL) AS last_message_at
		 FROM systems s
		 WHERE s.deleted_at IS NULL
		 ORDER BY (s.user_id = $1) DESC,
		          (s.approved_at IS NULL) DESC,
		          s.name`, user.ID)
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

// CreateSystem registers an interest. Body: { name, suburb, notes,
// accepted_terms }. Inserts a pending row (approved_at NULL, no api_key).
// An admin must approve it before the api_key is issued.
func (h *Handlers) CreateSystem(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())

	var req struct {
		Name           string `json:"name"`
		Suburb         string `json:"suburb"`
		Notes          string `json:"notes"`
		AcceptedTerms  bool   `json:"accepted_terms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Suburb = strings.TrimSpace(req.Suburb)
	req.Notes = strings.TrimSpace(req.Notes)

	if !req.AcceptedTerms {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "you must accept the hosting terms"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "name is required"})
		return
	}
	if req.Suburb == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "suburb is required"})
		return
	}

	systemUUID := uuid.New().String()

	var insertedID int64
	err := h.db.QueryRowxContext(r.Context(),
		`INSERT INTO systems
		   (user_id, system_id, name, suburb, request_notes,
		    sync, frequency, steal_capcodes, created_at, updated_at)
		 VALUES
		   ($1, $2, $3, $4, NULLIF($5, ''),
		    false, 0, '', NOW(), NOW())
		 RETURNING id`,
		user.ID, systemUUID, req.Name, req.Suburb, req.Notes).Scan(&insertedID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         insertedID,
		"uuid":       systemUUID,
		"name":       req.Name,
		"suburb":     req.Suburb,
		"is_pending": true,
		"is_owner":   true,
	})
}

// MySystems returns the current user's systems including pending ones, with
// the api_key + install command surfaced once approved. Owner-only — this
// is the ONLY endpoint that exposes api_key, and only to the actual owner.
func (h *Handlers) MySystems(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())

	rows, err := h.db.QueryxContext(r.Context(),
		`SELECT s.system_id AS uuid,
		        s.name,
		        s.suburb,
		        s.request_notes,
		        s.api_key,
		        s.approved_at,
		        s.created_at,
		        (s.approved_at IS NULL) AS is_pending,
		        (SELECT MAX(m.created_at) FROM messages m
		           WHERE m.system_id = s.id AND m.deleted_at IS NULL) AS last_message_at
		 FROM systems s
		 WHERE s.user_id = $1 AND s.deleted_at IS NULL
		 ORDER BY s.created_at DESC`, user.ID)
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

		// Attach a ready-to-run install command if we have an api_key (i.e. approved).
		if key, ok := row["api_key"].(string); ok && key != "" {
			row["install_command"] = "curl -fsSL https://rfspager.app/install.sh | sudo bash -s -- --api-key=" + key
		}

		data = append(data, row)
	}
	writeJSON(w, http.StatusOK, data)
}

// --- Alertzones ---

func (h *Handlers) ListAlertzones(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	rows, _ := h.db.QueryxContext(r.Context(),
		`SELECT id, name, lat, lng, radius FROM alertzones WHERE user_id = $1`, user.ID)
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

func (h *Handlers) CreateAlertzone(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	var req struct {
		Name   string  `json:"name"`
		Lat    float64 `json:"lat"`
		Lng    float64 `json:"lng"`
		Radius int     `json:"radius"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	var id int64
	h.db.QueryRowContext(r.Context(),
		`INSERT INTO alertzones (user_id, name, lat, lng, radius, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, NOW(), NOW()) RETURNING id`,
		user.ID, req.Name, req.Lat, req.Lng, req.Radius).Scan(&id)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": req.Name, "lat": req.Lat, "lng": req.Lng, "radius": req.Radius})
}

func (h *Handlers) DeleteAlertzone(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	id := chi.URLParam(r, "id")
	h.db.ExecContext(r.Context(), `DELETE FROM alertzones WHERE id = $1 AND user_id = $2`, id, user.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Installations ---

func (h *Handlers) UpsertInstallation(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	installID := chi.URLParam(r, "id")

	var req struct {
		PushType   string `json:"push_type"`
		PushToken  string `json:"push_token"`
		ExpoToken  string `json:"expo_token"`
		Vendor     string `json:"vendor"`
		Model      string `json:"model"`
		Version    string `json:"version"`
		AppID      string `json:"app_id"`
		AppName    string `json:"app_name"`
		AppVersion string `json:"app_version"`
		Timezone   string `json:"timezone"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	_, err := h.db.ExecContext(r.Context(),
		`INSERT INTO installations (id, user_id, push_type, push_token, expo_token, vendor, model, version, app_id, app_name, app_version, timezone, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW())
		 ON CONFLICT (id) DO UPDATE SET
		 user_id = EXCLUDED.user_id, push_type = EXCLUDED.push_type, push_token = EXCLUDED.push_token,
		 expo_token = EXCLUDED.expo_token, vendor = EXCLUDED.vendor, model = EXCLUDED.model,
		 version = EXCLUDED.version, app_id = EXCLUDED.app_id, app_name = EXCLUDED.app_name,
		 app_version = EXCLUDED.app_version, timezone = EXCLUDED.timezone, updated_at = NOW(), deleted_at = NULL`,
		installID, user.ID, req.PushType, nullStr(req.PushToken), nullStr(req.ExpoToken),
		req.Vendor, req.Model, req.Version, req.AppID, req.AppName, req.AppVersion, req.Timezone)
	if err != nil {
		slog.Error("upsert installation failed", "error", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) DeleteInstallation(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	id := chi.URLParam(r, "id")
	h.db.ExecContext(r.Context(), `UPDATE installations SET deleted_at = NOW() WHERE id = $1 AND user_id = $2`, id, user.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Notifications ---

func (h *Handlers) MarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	h.db.ExecContext(r.Context(),
		`UPDATE notifications SET read_at = NOW() WHERE notifiable_id = $1 AND notifiable_type = 'App\Models\User' AND read_at IS NULL`,
		user.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Major Incidents ---

func (h *Handlers) ListMajorIncidents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 { page = 1 }
	if perPage < 1 { perPage = 25 }

	fetchLimit := perPage + 1
	offset := (page - 1) * perPage
	rows, _ := h.db.QueryxContext(r.Context(),
		`SELECT id, title, category, geometry, data, created_at, updated_at
		 FROM majorincidents ORDER BY created_at DESC LIMIT $1 OFFSET $2`, fetchLimit, offset)
	defer rows.Close()
	data := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		rows.MapScan(row)
		cleanMapScan(row)
		data = append(data, row)
	}
	hasMore := len(data) > perPage
	if hasMore { data = data[:perPage] }
	totalPages := page
	if hasMore { totalPages = page + 1 }
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "page": page, "per_page": perPage, "total_pages": totalPages,
	})
}

// --- Hazard Reductions ---

func (h *Handlers) ListHazardReductions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 { page = 1 }
	if perPage < 1 { perPage = 25 }

	fetchLimit := perPage + 1
	offset := (page - 1) * perPage
	rows, _ := h.db.QueryxContext(r.Context(),
		`SELECT id, title, "leadAgency", "supportingAgencies", lga, size, tenure, geometry, start_at, end_at, created_at
		 FROM hazardreductions ORDER BY start_at DESC LIMIT $1 OFFSET $2`, fetchLimit, offset)
	defer rows.Close()
	data := []map[string]any{}
	for rows.Next() {
		row := make(map[string]any)
		rows.MapScan(row)
		cleanMapScan(row)
		data = append(data, row)
	}
	hasMore := len(data) > perPage
	if hasMore { data = data[:perPage] }
	totalPages := page
	if hasMore { totalPages = page + 1 }
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "page": page, "per_page": perPage, "total_pages": totalPages,
	})
}

// --- Photos ---

func (h *Handlers) ListPhotos(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 { page = 1 }
	if perPage < 1 { perPage = 25 }

	where := "p.upload_complete = true"
	args := []any{}
	argIdx := 1
	if msgID := q.Get("message_id"); msgID != "" {
		where += " AND p.message_id = $" + strconv.Itoa(argIdx)
		args = append(args, msgID)
		argIdx++
	}
	if ccID := q.Get("capcode_id"); ccID != "" {
		where += " AND p.capcode_id = $" + strconv.Itoa(argIdx)
		args = append(args, ccID)
		argIdx++
	}

	offset := (page - 1) * perPage
	args = append(args, perPage, offset)
	rows, _ := h.db.QueryxContext(r.Context(),
		`SELECT p.id, p.photo_id, p.url, p.thumb, p.created_at, p.user_id
		 FROM photos p WHERE `+where+` ORDER BY p.created_at DESC LIMIT $`+strconv.Itoa(argIdx)+` OFFSET $`+strconv.Itoa(argIdx+1), args...)
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

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func cleanMapScan(m map[string]any) {
	for k, v := range m {
		switch val := v.(type) {
		case []byte:
			var jsonVal any
			if err := json.Unmarshal(val, &jsonVal); err == nil {
				m[k] = jsonVal
			} else {
				m[k] = string(val)
			}
		}
	}
}
