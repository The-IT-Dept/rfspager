package mobile

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"github.com/the-it-dept/rfspager/internal/recvhub"
)

// resolveSystemForUser looks up the system by UUID and confirms the
// requesting user owns it. Returns the internal system_id + an http error
// status that the caller should propagate.
func resolveSystemForUser(r *http.Request, db *sqlx.DB) (systemID int64, status int, err error) {
	user := GetUser(r.Context())
	if user == nil {
		return 0, http.StatusUnauthorized, fmt.Errorf("unauthorized")
	}
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		return 0, http.StatusBadRequest, fmt.Errorf("uuid required")
	}
	var (
		id     int64
		userID sql.NullInt64
	)
	// systems.user_id is the owner column (Laravel naming).
	row := db.QueryRowContext(r.Context(),
		`SELECT id, user_id FROM systems WHERE system_id = $1 AND deleted_at IS NULL`, uuid)
	if err := row.Scan(&id, &userID); err != nil {
		return 0, http.StatusNotFound, fmt.Errorf("system not found")
	}
	if !userID.Valid || userID.Int64 != user.ID {
		return 0, http.StatusForbidden, fmt.Errorf("not your system")
	}
	return id, http.StatusOK, nil
}

// SystemEvents returns the full event history for a system the user owns.
// Newest first. Capped at 500 events.
func SystemEvents(db *sqlx.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		systemID, status, err := resolveSystemForUser(r, db)
		if err != nil {
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit < 1 || limit > 500 {
			limit = 200
		}
		rows, err := db.QueryxContext(r.Context(), `
			SELECT id, type, severity, message, metadata, created_at
			  FROM system_events
			 WHERE system_id = $1
			 ORDER BY created_at DESC
			 LIMIT $2`, systemID, limit)
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
}

// SystemLogsRecent returns the most recent persisted log lines for a system
// the user owns. Plain GET (no streaming) — mobile clients can poll this
// without needing an SSE polyfill.
func SystemLogsRecent(db *sqlx.DB, hub *recvhub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		systemID, status, err := resolveSystemForUser(r, db)
		if err != nil {
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit < 1 || limit > 500 {
			limit = 200
		}
		rows, err := hub.Logs.RecentLogs(r.Context(), systemID, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": rows})
	}
}

// SystemLogSSE streams persisted + live receiver log lines for a system the
// user owns. The first events the client receives are the most recent
// `limit` log rows (oldest-first); subsequent events are live broadcasts
// from the broker. Same envelope shape in both cases.
func SystemLogSSE(db *sqlx.DB, hub *recvhub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		systemID, status, err := resolveSystemForUser(r, db)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		// Disable per-request write deadline — streaming.
		if rc := http.NewResponseController(w); rc != nil {
			_ = rc.SetWriteDeadline(time.Time{})
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(": connected\n\n"))
		flusher.Flush()

		// Backfill last 200 rows so the client has context immediately.
		if rows, err := hub.Logs.RecentLogs(r.Context(), systemID, 200); err == nil {
			for _, row := range rows {
				envelope, _ := json.Marshal(row)
				_, _ = w.Write([]byte("data: "))
				_, _ = w.Write(envelope)
				_, _ = w.Write([]byte("\n\n"))
			}
			flusher.Flush()
		}

		ch, cancel := hub.Logs.Subscribe(systemID)
		defer cancel()

		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case data, ok := <-ch:
				if !ok {
					return
				}
				_, _ = w.Write([]byte("data: "))
				_, _ = w.Write(data)
				_, _ = w.Write([]byte("\n\n"))
				flusher.Flush()
			case <-keepalive.C:
				_, _ = w.Write([]byte(": ping\n\n"))
				flusher.Flush()
			}
		}
	}
}
