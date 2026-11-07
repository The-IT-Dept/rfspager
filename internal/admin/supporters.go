package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/hostcomp"
)

// Supporters on the admin system page: people besides the owner who get the
// host comp while the receiver qualifies (migration 039). The host comp job
// does the granting; these handlers only keep the list.

// Supporter is one supporter of a receiver, with their Pro status.
type Supporter struct {
	UserID               int64      `db:"user_id" json:"user_id"`
	Name                 string     `db:"name" json:"name"`
	Email                string     `db:"email" json:"email"`
	Note                 string     `db:"note" json:"note"`
	AdminEmail           *string    `db:"admin_email" json:"admin_email"`
	CreatedAt            time.Time  `db:"created_at" json:"created_at"`
	HostCompSince        *time.Time `db:"host_comp_since" json:"host_comp_since"`
	HostCompUntil        *time.Time `db:"host_comp_until" json:"host_comp_until"`
	SubscriptionValid    bool       `db:"subscription_valid" json:"subscription_valid"`
	SubscriptionPlatform *string    `db:"subscription_platform" json:"subscription_platform"`
	SubscriptionExpiry   *time.Time `db:"subscription_expiry" json:"subscription_expiry"`
}

const supporterSelect = `
	SELECT sp.user_id, COALESCE(u.name, '') AS name, u.email, sp.note, a.email AS admin_email, sp.created_at,
	       u.host_comp_since, u.host_comp_until, COALESCE(u.subscription_valid, false) AS subscription_valid,
	       u.subscription_platform, u.subscription_expiry
	  FROM system_supporters sp
	  JOIN users u ON u.id = sp.user_id
	  LEFT JOIN admin_users a ON a.id = sp.admin_user_id`

type supporterHandlers struct {
	db *sqlx.DB
}

func newSupporterHandlers(db *sqlx.DB) *supporterHandlers {
	return &supporterHandlers{db: db}
}

// mountSupporterRoutes registers a receiver's supporters under the systems
// router. Superadmin only: they're people's accounts and free Pro.
func mountSupporterRoutes(r chi.Router, h *supporterHandlers, superadmin func(http.Handler) http.Handler) {
	r.With(superadmin).Get("/{id}/supporters", h.List)
	r.With(superadmin).Post("/{id}/supporters", h.Add)
	r.With(superadmin).Delete("/{id}/supporters/{userId}", h.Remove)
}

func supporterSystemID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

// List returns the receiver's supporters, oldest first.
func (h *supporterHandlers) List(w http.ResponseWriter, r *http.Request) {
	systemID, ok := supporterSystemID(w, r)
	if !ok {
		return
	}
	var exists bool
	if err := h.db.GetContext(r.Context(), &exists, `SELECT EXISTS (SELECT 1 FROM systems WHERE id = $1)`, systemID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "receiver not found"})
		return
	}
	out := []Supporter{}
	if err := h.db.SelectContext(r.Context(), &out, supporterSelect+`
		 WHERE sp.system_id = $1
		 ORDER BY sp.created_at, sp.user_id`, systemID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

type addSupporterBody struct {
	Email string `json:"email"`
	Note  string `json:"note"`
}

// Add makes the account with that email a supporter of the receiver. Their
// comp starts on the job's next run if the receiver qualifies.
func (h *supporterHandlers) Add(w http.ResponseWriter, r *http.Request) {
	systemID, ok := supporterSystemID(w, r)
	if !ok {
		return
	}
	var body addSupporterBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	email, note := strings.TrimSpace(body.Email), strings.TrimSpace(body.Note)
	if email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email is required"})
		return
	}
	if note == "" || utf8.RuneCountInString(note) > maxNoteLength {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("a note of 1-%d characters is required", maxNoteLength)})
		return
	}
	ctx := r.Context()

	var owner sql.NullInt64
	err := h.db.GetContext(ctx, &owner, `SELECT user_id FROM systems WHERE id = $1 AND deleted_at IS NULL`, systemID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "receiver not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !owner.Valid {
		// The job only comps receivers with an owner.
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this receiver has no owner, so it can't give anyone free Pro"})
		return
	}
	// Some accounts' addresses differ only in case. The exact address wins,
	// as does the only match in any case; otherwise it's ambiguous, and
	// guessing could comp the wrong person.
	var matches []struct {
		ID    int64  `db:"id"`
		Email string `db:"email"`
	}
	if err := h.db.SelectContext(ctx, &matches, `
		SELECT id, email FROM users WHERE lower(email) = lower($1)
		 ORDER BY (email = $1) DESC, id DESC
		 LIMIT 2`, email); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(matches) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no account with that email"})
		return
	}
	if len(matches) > 1 && matches[0].Email != email {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "more than one account uses that address with different capitals; enter it exactly as the account has it"})
		return
	}
	userID := matches[0].ID
	if owner.Int64 == userID {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "that's the receiver's owner, who already gets free Pro from it"})
		return
	}

	var adminID *int64
	adminEmail := ""
	if admin := GetAdminUser(ctx); admin != nil {
		adminID, adminEmail = &admin.ID, admin.Email
	}
	var created time.Time
	err = h.db.GetContext(ctx, &created, `
		INSERT INTO system_supporters (system_id, user_id, note, admin_user_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (system_id, user_id) DO NOTHING
		RETURNING created_at`, systemID, userID, note, adminID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already a supporter of this receiver"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var s Supporter
	if err := h.db.GetContext(ctx, &s, supporterSelect+` WHERE sp.system_id = $1 AND sp.user_id = $2`, systemID, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("receiver supporter added", "system_id", systemID, "user_id", userID, "admin", adminEmail)
	writeJSON(w, http.StatusCreated, s)
}

// Remove takes the user off the receiver's supporters. When that leaves them
// no receiver of their own and none they support, their free Pro ends now,
// without a lapse email (the job would otherwise email them about a receiver
// they no longer support). Otherwise the job keeps deciding it.
func (h *supporterHandlers) Remove(w http.ResponseWriter, r *http.Request) {
	systemID, ok := supporterSystemID(w, r)
	if !ok {
		return
	}
	userID, err := strconv.ParseInt(chi.URLParam(r, "userId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}
	ctx := r.Context()
	tx, err := h.db.BeginTxx(ctx, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	// Wait out a running host comp job, so it can't re-extend the comp from
	// the row being removed after this ends it.
	if err := hostcomp.Lock(ctx, tx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM system_supporters WHERE system_id = $1 AND user_id = $2`, systemID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not a supporter of this receiver"})
		return
	}
	ended, err := hostcomp.EndIfNoReceiver(ctx, tx, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	adminEmail := ""
	if admin := GetAdminUser(ctx); admin != nil {
		adminEmail = admin.Email
	}
	slog.Info("receiver supporter removed", "system_id", systemID, "user_id", userID, "comp_ended", ended, "admin", adminEmail)
	writeJSON(w, http.StatusOK, map[string]any{"status": "removed", "comp_ended": ended})
}
