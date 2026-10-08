package mobile

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/radio"
)

// radioEntitlementRecheck is how often an open radio SSE stream re-checks
// that its user is still a subscriber with a live token. The stream can stay
// open for hours, so a check at connect time alone would let a lapsed
// subscription or a logout keep streaming indefinitely.
const radioEntitlementRecheck = 5 * time.Minute

// sseWriteTimeout bounds each write to an SSE client. The server-wide
// WriteTimeout is cleared for the stream, so without this a client that stops
// reading would park the handler on a full socket buffer forever.
const sseWriteTimeout = 30 * time.Second

// RadioRouter serves /api/v2/mobile/radio: P25 calls, talkgroups, sites and
// units. Radio is a paid feature, so every route needs a subscription.
//
// Mount it OUTSIDE the server's chimw.Timeout group (cmd/server/main.go):
// /events is a long-lived SSE stream that a 30s request timeout would cut.
// The other routes get the usual 30s timeout here instead.
func RadioRouter(db *sqlx.DB, jwtSecret string, store *radio.Store, broker *radio.Broker) chi.Router {
	h := &radioHandlers{db: db, store: store, broker: broker}

	r := chi.NewRouter()
	r.Use(JWTUserAuth(db, jwtSecret))
	r.Use(RequireSubscription)

	r.Get("/events", h.Events)

	r.Group(func(r chi.Router) {
		r.Use(chimw.Timeout(30 * time.Second))
		r.Get("/calls", h.ListCalls)
		r.Get("/calls/{id}", h.GetCall)
		r.Get("/talkgroups", h.ListTalkgroups)
		r.Get("/sites", h.ListSites)
		r.Get("/units/{rid}", h.GetUnit)
	})
	return r
}

type radioHandlers struct {
	db     *sqlx.DB
	store  *radio.Store
	broker *radio.Broker
}

// ListCalls: GET /calls?tg=&sysid=&site=&before_id=&limit=
//
// Newest first. tg is a talkgroup id (sysid defaults to the NSW PSN), site a
// radio_sites id from GET /sites, before_id the last call id of the previous
// page. Pages carry has_more instead of a total, like the message feed.
func (h *radioHandlers) ListCalls(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var f radio.CallFilter
	var ok bool
	if f.TGID, ok = queryInt(w, q.Get("tg"), "tg"); !ok {
		return
	}
	if f.SysID, ok = queryInt(w, q.Get("sysid"), "sysid"); !ok {
		return
	}
	site, ok := queryInt(w, q.Get("site"), "site")
	if !ok {
		return
	}
	before, ok := queryInt(w, q.Get("before_id"), "before_id")
	if !ok {
		return
	}
	f.SiteID, f.BeforeID = int64(site), int64(before)
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	if f.Limit < 1 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}

	calls, hasMore, err := h.store.ListCalls(r.Context(), f)
	if err != nil {
		slog.Error("list radio calls failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list calls"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": calls, "limit": f.Limit, "has_more": hasMore,
	})
}

func (h *radioHandlers) GetCall(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	call, err := h.store.GetCall(r.Context(), id)
	if radio.IsNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		slog.Error("get radio call failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load call"})
		return
	}
	writeJSON(w, http.StatusOK, call)
}

func (h *radioHandlers) ListTalkgroups(w http.ResponseWriter, r *http.Request) {
	tgs, err := h.store.ListTalkgroups(r.Context())
	if err != nil {
		slog.Error("list talkgroups failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list talkgroups"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": tgs})
}

func (h *radioHandlers) ListSites(w http.ResponseWriter, r *http.Request) {
	sites, err := h.store.ListSites(r.Context())
	if err != nil {
		slog.Error("list radio sites failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list sites"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": sites})
}

// GetUnit: GET /units/{rid}?sysid= — a radio with its recent unit events
// and the calls it recently transmitted on.
func (h *radioHandlers) GetUnit(w http.ResponseWriter, r *http.Request) {
	rid, err := strconv.Atoi(chi.URLParam(r, "rid"))
	if err != nil || rid < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid rid"})
		return
	}
	sysid, ok := queryInt(w, r.URL.Query().Get("sysid"), "sysid")
	if !ok {
		return
	}
	if sysid == 0 {
		sysid = radio.DefaultSysID
	}
	unit, err := h.store.GetUnit(r.Context(), sysid, rid, 50)
	if radio.IsNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		slog.Error("get radio unit failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load unit"})
		return
	}
	writeJSON(w, http.StatusOK, unit)
}

// Events streams calls as they start and end:
//
//	event: call
//	data: {...same shape as GET /calls/{id}...}
//
// A call is sent twice, on start (ended_at null) and on end; clients upsert
// by id. Calls on hidden talkgroups are not sent.
func (h *radioHandlers) Events(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	rc := http.NewResponseController(w)
	// write sends one chunk under its own deadline, replacing the server's
	// 30s WriteTimeout which would otherwise end the stream.
	write := func(chunks ...[]byte) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
		for _, c := range chunks {
			if _, err := w.Write(c); err != nil {
				return false
			}
		}
		flusher.Flush()
		return true
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if !write([]byte(": connected\n\n")) {
		return
	}

	ch, unsubscribe := h.broker.Subscribe()
	defer unsubscribe()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	recheck := time.NewTicker(radioEntitlementRecheck)
	defer recheck.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-ch:
			if !write([]byte("event: call\ndata: "), data, []byte("\n\n")) {
				return
			}
		case <-keepalive.C:
			if !write([]byte(": keepalive\n\n")) {
				return
			}
		case <-recheck.C:
			if !h.stillEntitled(r.Context(), user) {
				return
			}
		}
	}
}

// stillEntitled re-applies JWTUserAuth's token check and RequireSubscription
// for a stream that has been open a while. A database error keeps the
// stream: it is a check on an already-authorised connection, not a login.
func (h *radioHandlers) stillEntitled(ctx context.Context, user *AuthUser) bool {
	var current struct {
		TokenVersion      int64 `db:"token_version"`
		SubscriptionValid bool  `db:"subscription_valid"`
		Superuser         bool  `db:"superuser"`
	}
	err := h.db.GetContext(ctx, &current,
		`SELECT token_version, subscription_valid, superuser FROM users WHERE id = $1`, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		slog.Warn("radio SSE entitlement check failed", "user_id", user.ID, "error", err)
		return true
	}
	return current.TokenVersion == user.TokenVersion && (current.SubscriptionValid || current.Superuser)
}

// queryInt parses an optional integer query parameter; empty is 0. On a bad
// value it writes a 400 and returns false.
func queryInt(w http.ResponseWriter, raw, name string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid " + name})
		return 0, false
	}
	return v, true
}
