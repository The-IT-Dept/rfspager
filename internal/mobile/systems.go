package mobile

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/install"
)

// SystemHandlers exposes the user-facing system endpoints for the mobile app.
type SystemHandlers struct {
	db *sqlx.DB
}

func NewSystemHandlers(db *sqlx.DB) *SystemHandlers { return &SystemHandlers{db: db} }

// systemRow is the joined shape we read from systems + system_connections.
// All sensitive fields stay private to this file and are only emitted to
// the owning user.
type systemRow struct {
	ID            int64    `db:"id"`
	UserID        int64    `db:"user_id"`
	SystemID      string   `db:"system_id"`
	Name          string   `db:"name"`
	Suburb        *string  `db:"suburb"`
	RequestNotes  *string  `db:"request_notes"`
	Frequency     int      `db:"frequency"`
	Lat           *float64 `db:"lat"`
	Lng           *float64 `db:"lng"`
	CreatedAt     *string  `db:"created_at"`
	ApprovedAt    *string  `db:"approved_at"`
	IsPending     bool     `db:"is_pending"`
	LastMessageAt *string  `db:"last_message_at"`
	APIKey        *string  `db:"api_key"`
	PodID         *string  `db:"receiver_pod_id"`
	ConnectedAt   *string  `db:"receiver_connected_at"`
	LastPingAt    *string  `db:"receiver_last_ping_at"`
	Version       *string  `db:"receiver_version"`
	OS            *string  `db:"receiver_os"`
	Arch          *string  `db:"receiver_arch"`
	Hostname      *string  `db:"receiver_hostname"`
	Dongles       *string  `db:"receiver_dongles"`
	Online        *bool    `db:"receiver_online"`
}

// GetSystem handles GET /api/v2/mobile/systems/{uuid}.
//
// The {uuid} segment matches systems.system_id (the public UUID handle the
// mobile app already knows). Any authenticated user gets a public
// projection of the system; the owner additionally gets receiver-agent
// diagnostics (api_key, hostname, version, dongles, pod_id…).
func (h *SystemHandlers) GetSystem(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad uuid"})
		return
	}

	var row systemRow
	err := h.db.GetContext(r.Context(), &row,
		`SELECT
		     s.id, s.user_id, s.system_id, s.name, s.suburb, s.request_notes,
		     s.frequency, s.lat, s.lng,
		     s.created_at::text  AS created_at,
		     s.approved_at::text AS approved_at,
		     (s.approved_at IS NULL) AS is_pending,
		     (SELECT MAX(m.created_at)::text FROM messages m
		        WHERE m.system_id = s.id AND m.deleted_at IS NULL) AS last_message_at,
		     s.api_key,
		     sc.pod_id            AS receiver_pod_id,
		     sc.connected_at::text AS receiver_connected_at,
		     sc.last_ping_at::text AS receiver_last_ping_at,
		     sc.receiver_version,
		     sc.receiver_os,
		     sc.receiver_arch,
		     sc.receiver_hostname,
		     sc.dongles::text     AS receiver_dongles,
		     (sc.system_id IS NOT NULL AND sc.last_ping_at > NOW() - INTERVAL '90 seconds') AS receiver_online
		 FROM systems s
		 LEFT JOIN system_connections sc ON sc.system_id = s.id
		 WHERE s.system_id = $1 AND s.deleted_at IS NULL`, uuid)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "system not found"})
		return
	}

	isOwner := row.UserID == user.ID
	resp := map[string]any{
		"id":         row.ID,
		"system_id":  row.SystemID,
		"name":       row.Name,
		"suburb":     row.Suburb,
		"frequency":  row.Frequency,
		"lat":        row.Lat,
		"lng":        row.Lng,
		"is_owner":   isOwner,
		"is_pending": row.IsPending,
		"online":     row.Online != nil && *row.Online,
		// Connection metadata that's safe to expose to anyone watching: just
		// the timestamps. Hardware identifiers stay owner-gated below.
		"connected_at":    row.ConnectedAt,
		"last_ping_at":    row.LastPingAt,
		"last_message_at": row.LastMessageAt,
	}

	if isOwner {
		var dongles any
		if row.Dongles != nil && *row.Dongles != "" {
			_ = json.Unmarshal([]byte(*row.Dongles), &dongles)
		}
		resp["api_key"] = row.APIKey
		resp["created_at"] = row.CreatedAt
		resp["approved_at"] = row.ApprovedAt
		resp["request_notes"] = row.RequestNotes
		if row.APIKey != nil && *row.APIKey != "" {
			resp["install_command"] = install.CommandFor(r, *row.APIKey)
		}
		resp["receiver"] = map[string]any{
			"pod_id":   row.PodID,
			"version":  row.Version,
			"os":       row.OS,
			"arch":     row.Arch,
			"hostname": row.Hostname,
			"dongles":  dongles,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
