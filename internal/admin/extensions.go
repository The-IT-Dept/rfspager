package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/the-it-dept/rfspager/internal/iap"
)

// "Give free time" on the admin user page. The method follows the account:
// an active Apple subscription gets its renewal date extended and an active
// Google one deferred, so the store doesn't charge until the free time runs
// out (editing subscription_expiry would not stop the charge); everyone else
// gets a gift in our own columns. Every grant is recorded in
// account_extensions (migration 038), failed ones included.

const (
	giftedProductID = "io.devlan.rfspager.gifted"

	// Apple takes at most 90 days per extension and allows two per
	// subscription in any 365 days, so a grant is at most 180 days.
	appleDaysPerCall = 90
	appleCallsA365   = 2
	maxAppleDays     = appleDaysPerCall * appleCallsA365
	maxGoogleDays    = 365
	maxGiftDays      = 730
	maxNoteLength    = 1000

	// Store calls run inside the grant; bound the whole thing.
	grantTimeout = 60 * time.Second
)

// AppleExtender extends an Apple subscription's renewal date.
// *iap.AppleClient implements it.
type AppleExtender interface {
	ExtendRenewalDate(environment, originalTransactionID string, days, reason int, requestID string) (*iap.AppleExtendResult, error)
}

// GoogleDeferrer defers a Google Play subscription's renewal.
// *iap.GoogleClient implements it.
type GoogleDeferrer interface {
	DeferSubscription(ctx context.Context, purchaseToken string, days int) (time.Time, error)
}

// Extenders are the store clients behind "Give free time". A nil one means
// that store isn't configured; its subscribers can't be given time.
type Extenders struct {
	Apple  AppleExtender
	Google GoogleDeferrer
}

// AccountExtension is one "Give free time" grant.
type AccountExtension struct {
	ID            int64          `db:"id" json:"id"`
	UserID        int64          `db:"user_id" json:"user_id"`
	AdminUserID   *int64         `db:"admin_user_id" json:"admin_user_id"`
	AdminEmail    *string        `db:"admin_email" json:"admin_email"`
	Method        string         `db:"method" json:"method"`
	Status        string         `db:"status" json:"status"`
	DaysRequested int            `db:"days_requested" json:"days_requested"`
	DaysApplied   int            `db:"days_applied" json:"days_applied"`
	ReasonCode    *int           `db:"reason_code" json:"reason_code"`
	Note          string         `db:"note" json:"note"`
	ExpiryBefore  *time.Time     `db:"expiry_before" json:"expiry_before"`
	ExpiryAfter   *time.Time     `db:"expiry_after" json:"expiry_after"`
	StoreRequests pq.StringArray `db:"store_requests" json:"store_requests"`
	Error         *string        `db:"error" json:"error"`
	CreatedAt     time.Time      `db:"created_at" json:"created_at"`
}

// extensionsResponse is the history plus what a new grant would do, so the
// dialog can say which method applies and its limits.
type extensionsResponse struct {
	Data                []AccountExtension `json:"data"`
	Method              string             `json:"method"`
	MaxDays             int                `json:"max_days"`
	AppleExtensionsLeft int                `json:"apple_extensions_left"`
	Unavailable         string             `json:"unavailable,omitempty"`
}

type extensionHandlers struct {
	db        *sqlx.DB
	extenders Extenders
}

func newExtensionHandlers(db *sqlx.DB, extenders Extenders) *extensionHandlers {
	return &extensionHandlers{db: db, extenders: extenders}
}

// extensionAccount is what the method choice reads from users.
type extensionAccount struct {
	Valid       bool       `db:"subscription_valid"`
	Expiry      *time.Time `db:"subscription_expiry"`
	Platform    *string    `db:"subscription_platform"`
	AppleOTX    *string    `db:"apple_original_transaction_id"`
	GoogleToken *string    `db:"google_purchase_token"`
}

// method picks how free time is given, or explains why it can't be.
func (a extensionAccount) method(now time.Time) (method string, unavailable string) {
	if a.Valid && a.Expiry == nil {
		return "", "this account already has access with no expiry"
	}
	active := a.Valid && a.Expiry.After(now)
	platform := ""
	if a.Platform != nil {
		platform = *a.Platform
	}
	switch {
	case active && platform == "apple" && a.AppleOTX != nil && *a.AppleOTX != "":
		return "apple", ""
	case active && platform == "google" && a.GoogleToken != nil && *a.GoogleToken != "":
		return "google", ""
	default:
		return "gift", ""
	}
}

func maxDaysFor(method string) int {
	switch method {
	case "apple":
		return maxAppleDays
	case "google":
		return maxGoogleDays
	case "gift":
		return maxGiftDays
	}
	return 0
}

type queryer interface {
	GetContext(ctx context.Context, dest any, query string, args ...any) error
}

func loadExtensionAccount(ctx context.Context, q queryer, userID int64) (extensionAccount, error) {
	var a extensionAccount
	err := q.GetContext(ctx, &a, `
		SELECT subscription_valid, subscription_expiry, subscription_platform,
		       apple_original_transaction_id, google_purchase_token
		  FROM users WHERE id = $1`, userID)
	return a, err
}

// appleExtensionsUsed counts the Apple extension calls that succeeded for the
// user in the last 365 days.
func appleExtensionsUsed(ctx context.Context, q queryer, userID int64) (int, error) {
	var used int
	err := q.GetContext(ctx, &used, `
		SELECT COALESCE(SUM(cardinality(store_requests)), 0)::int
		  FROM account_extensions
		 WHERE user_id = $1 AND method = 'apple' AND created_at > NOW() - INTERVAL '365 days'`, userID)
	return used, err
}

func extensionUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

// List returns the user's grants, newest first, and what a new grant would
// do.
func (h *extensionHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := extensionUserID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	account, err := loadExtensionAccount(ctx, h.db, userID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp := extensionsResponse{Data: []AccountExtension{}}
	resp.Method, resp.Unavailable = account.method(time.Now())
	resp.MaxDays = maxDaysFor(resp.Method)
	if resp.Unavailable == "" {
		resp.Unavailable = h.storeUnavailable(resp.Method)
	}
	used, err := appleExtensionsUsed(ctx, h.db, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp.AppleExtensionsLeft = max(appleCallsA365-used, 0)
	if resp.Method == "apple" {
		resp.MaxDays = min(resp.MaxDays, resp.AppleExtensionsLeft*appleDaysPerCall)
	}
	if err := h.db.SelectContext(ctx, &resp.Data, `
		SELECT e.id, e.user_id, e.admin_user_id, a.email AS admin_email, e.method, e.status,
		       e.days_requested, e.days_applied, e.reason_code, e.note, e.expiry_before,
		       e.expiry_after, e.store_requests, e.error, e.created_at
		  FROM account_extensions e
		  LEFT JOIN admin_users a ON a.id = e.admin_user_id
		 WHERE e.user_id = $1
		 ORDER BY e.created_at DESC, e.id DESC`, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *extensionHandlers) storeUnavailable(method string) string {
	switch {
	case method == "apple" && h.extenders.Apple == nil:
		return "the App Store Server API is not configured"
	case method == "google" && h.extenders.Google == nil:
		return "the Google Play API is not configured"
	}
	return ""
}

type grantBody struct {
	Days   int    `json:"days"`
	Note   string `json:"note"`
	Reason *int   `json:"reason"`
}

// Grant gives the user free time. 201 with the row when it applied; 502 with
// the row and the store's error when the store refused all or part of it.
func (h *extensionHandlers) Grant(w http.ResponseWriter, r *http.Request) {
	userID, ok := extensionUserID(w, r)
	if !ok {
		return
	}
	var body grantBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	body.Note = strings.TrimSpace(body.Note)
	if body.Note == "" || len(body.Note) > maxNoteLength {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("a note of 1-%d characters is required", maxNoteLength)})
		return
	}
	reason := iap.ExtendReasonServiceIssue
	if body.Reason != nil {
		if *body.Reason < iap.ExtendReasonUndeclared || *body.Reason > iap.ExtendReasonServiceIssue {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reason must be 0-3"})
			return
		}
		reason = *body.Reason
	}
	var adminID *int64
	adminEmail := ""
	if admin := GetAdminUser(r.Context()); admin != nil {
		adminID, adminEmail = &admin.ID, admin.Email
	}

	ctx, cancel := context.WithTimeout(r.Context(), grantTimeout)
	defer cancel()
	tx, err := h.db.BeginTxx(ctx, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	// One grant per user at a time, so a double click can't spend Apple's
	// allowance twice. Store paths don't lock the users row: the store's
	// webhook may arrive mid-grant and must not wait on it.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('account_extension:' || $1::text, 0))`, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	account, err := loadExtensionAccount(ctx, tx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	method, unavailable := account.method(time.Now())
	if unavailable == "" {
		unavailable = h.storeUnavailable(method)
	}
	if unavailable != "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": unavailable})
		return
	}
	if body.Days < 1 || body.Days > maxDaysFor(method) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("days must be 1-%d for %s", maxDaysFor(method), method)})
		return
	}

	ext := AccountExtension{
		UserID: userID, AdminUserID: adminID, Method: method, DaysRequested: body.Days,
		Note: body.Note, ExpiryBefore: account.Expiry, StoreRequests: pq.StringArray{},
	}
	switch method {
	case "apple":
		used, err := appleExtensionsUsed(ctx, tx, userID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		calls := (body.Days + appleDaysPerCall - 1) / appleDaysPerCall
		if left := appleCallsA365 - used; calls > left {
			writeJSON(w, http.StatusConflict, map[string]string{"error": fmt.Sprintf(
				"Apple allows %d extensions a year and %d are left; %d days needs %d", appleCallsA365, max(left, 0), body.Days, calls)})
			return
		}
		ext.ReasonCode = &reason
		h.extendApple(&ext, *account.AppleOTX, reason)
	case "google":
		expiry, err := h.extenders.Google.DeferSubscription(ctx, *account.GoogleToken, body.Days)
		if err != nil {
			msg := err.Error()
			ext.Error = &msg
		} else {
			ext.DaysApplied = body.Days
			ext.ExpiryAfter = &expiry
		}
	case "gift":
		expiry, err := giveGift(ctx, tx, userID, body.Days)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		ext.DaysApplied = body.Days
		ext.ExpiryAfter = &expiry
	}
	switch {
	case ext.DaysApplied == body.Days:
		ext.Status = "applied"
	case ext.DaysApplied > 0:
		ext.Status = "partial"
	default:
		ext.Status = "failed"
	}

	if err := tx.QueryRowxContext(ctx, `
		INSERT INTO account_extensions (user_id, admin_user_id, method, status, days_requested, days_applied,
		                                reason_code, note, expiry_before, expiry_after, store_requests, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, created_at`,
		ext.UserID, ext.AdminUserID, ext.Method, ext.Status, ext.DaysRequested, ext.DaysApplied,
		ext.ReasonCode, ext.Note, ext.ExpiryBefore, ext.ExpiryAfter, ext.StoreRequests, ext.Error,
	).Scan(&ext.ID, &ext.CreatedAt); err == nil {
		err = tx.Commit()
	} else {
		// The store may already have given the time; say so loudly.
		slog.Error("give free time: recording the grant failed", "user_id", userID, "method", method,
			"days_applied", ext.DaysApplied, "store_requests", []string(ext.StoreRequests), "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "recording the grant failed: " + err.Error()})
		return
	}
	slog.Info("give free time", "user_id", userID, "admin", adminEmail, "method", method,
		"status", ext.Status, "days", ext.DaysApplied, "expiry_after", ext.ExpiryAfter)
	if ext.Status != "applied" {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": *ext.Error, "extension": ext})
		return
	}
	writeJSON(w, http.StatusCreated, ext)
}

// extendApple makes the 90-day-or-less Apple calls a grant needs, stopping
// at the first refusal. Each call has a fresh request identifier.
func (h *extensionHandlers) extendApple(ext *AccountExtension, otx string, reason int) {
	for left := ext.DaysRequested; left > 0; {
		days := min(left, appleDaysPerCall)
		requestID, err := newRequestID()
		if err == nil {
			var res *iap.AppleExtendResult
			res, err = h.extenders.Apple.ExtendRenewalDate("Production", otx, days, reason, requestID)
			if err == nil {
				ext.DaysApplied += days
				ext.StoreRequests = append(ext.StoreRequests, requestID)
				after := time.UnixMilli(res.EffectiveDate).UTC()
				ext.ExpiryAfter = &after
				left -= days
				continue
			}
		}
		msg := err.Error()
		ext.Error = &msg
		return
	}
}

// giveGift moves the account to the gifted product, running days past the
// current expiry when access is still running, otherwise from now, and
// records an iap_transactions row the way store events do.
func giveGift(ctx context.Context, tx *sqlx.Tx, userID int64, days int) (time.Time, error) {
	var expiry time.Time
	if err := tx.GetContext(ctx, &expiry, `
		UPDATE users SET
			subscription_valid      = true,
			subscription_expiry     = CASE WHEN subscription_valid AND subscription_expiry > NOW()
			                               THEN subscription_expiry ELSE NOW() END + make_interval(days => $2),
			subscription_product_id = $3,
			subscription_platform   = NULL,
			will_renew              = NULL,
			updated_at              = NOW()
		 WHERE id = $1
		RETURNING subscription_expiry`, userID, days, giftedProductID); err != nil {
		return time.Time{}, fmt.Errorf("giving gift: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO iap_transactions (user_id, platform, product_id, event_type, is_valid, expires_at, raw_payload)
		VALUES ($1, 'gift', $2, 'admin:gift', true, $3, $4)`,
		userID, giftedProductID, expiry, fmt.Sprintf("given %d days from the admin user page", days)); err != nil {
		return time.Time{}, fmt.Errorf("recording gift: %w", err)
	}
	return expiry.UTC(), nil
}

func newRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
