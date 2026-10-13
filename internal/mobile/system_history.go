package mobile

import (
	"database/sql"
	"fmt"
	"net/http"

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

// ownerResolver adapts resolveSystemForUser to the resolver the shared
// recvhub history handlers take. The handlers themselves live in recvhub
// so the owner and admin routes cannot drift apart; only the question
// "which system, and may this caller see it" differs.
func ownerResolver(db *sqlx.DB) recvhub.SystemResolver {
	return func(r *http.Request) (int64, int, error) { return resolveSystemForUser(r, db) }
}

// SystemEvents returns the full event history for a system the user owns.
// Newest first. Capped at 500 events.
func SystemEvents(db *sqlx.DB) http.HandlerFunc {
	return recvhub.SystemEvents(db, ownerResolver(db))
}

// SystemLogsRecent returns the most recent persisted log lines for a system
// the user owns. Plain GET (no streaming) — mobile clients can poll this
// without needing an SSE polyfill.
func SystemLogsRecent(db *sqlx.DB, hub *recvhub.Hub) http.HandlerFunc {
	return recvhub.SystemLogsRecent(hub, ownerResolver(db))
}

// SystemLogSSE streams persisted + live receiver log lines for a system the
// user owns. The first events the client receives are the most recent
// `limit` log rows (oldest-first); subsequent events are live broadcasts
// from the broker. Same envelope shape in both cases.
func SystemLogSSE(db *sqlx.DB, hub *recvhub.Hub) http.HandlerFunc {
	return recvhub.SystemLogSSE(hub, ownerResolver(db))
}

// SystemConfig returns the receiver config the server holds for a system
// the user owns: {"version": n, "config": rxproto.RuntimeConfig}, version 0
// with the defaults when nothing has been stored. Read-only for owners;
// tuning changes go through the admin API.
func SystemConfig(db *sqlx.DB) http.HandlerFunc {
	return recvhub.SystemConfig(db, ownerResolver(db))
}
