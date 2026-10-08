package mobile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/blobstore"
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
// units, call recordings and live audio. Radio is a paid feature, so every
// route needs a subscription.
//
// Mount it OUTSIDE the server's chimw.Timeout group (cmd/server/main.go):
// /events and /live are long-lived SSE streams that a 30s request timeout
// would cut. The other routes get the usual 30s timeout here instead.
//
// live and audio may be nil, in which case /live answers 503 and
// /calls/{id}/audio 404.
func RadioRouter(db *sqlx.DB, jwtSecret string, store *radio.Store, broker *radio.Broker, live *radio.LiveBroker, audio blobstore.Store) chi.Router {
	h := &radioHandlers{db: db, jwtSecret: jwtSecret, store: store, broker: broker, live: live, audio: audio}

	r := chi.NewRouter()
	// Bearer only.
	r.Group(func(r chi.Router) {
		r.Use(JWTUserAuth(db, jwtSecret), RequireSubscription, chimw.Timeout(30*time.Second))
		r.Post("/stream-token", h.StreamToken)
		r.Get("/calls", h.ListCalls)
		r.Get("/calls/{id}", h.GetCall)
		r.Get("/talkgroups", h.ListTalkgroups)
		r.Get("/sites", h.ListSites)
		r.Get("/units/{rid}", h.GetUnit)
	})
	// What a browser loads directly (EventSource, <audio>) also takes a
	// ?token= stream token.
	r.Group(func(r chi.Router) {
		r.Use(StreamOrBearerAuth(db, jwtSecret), RequireSubscription)
		r.Get("/events", h.Events)
		r.Get("/live", h.Live)
		r.With(chimw.Timeout(30*time.Second)).Get("/calls/{id}/audio", h.Audio)
	})
	return r
}

// StreamToken: POST /stream-token → {token, expires_at}. See issueStreamToken.
func (h *radioHandlers) StreamToken(w http.ResponseWriter, r *http.Request) {
	tok, exp, err := issueStreamToken(h.jwtSecret, GetUser(r.Context()))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "expires_at": exp.UTC()})
}

type radioHandlers struct {
	db        *sqlx.DB
	jwtSecret string
	store     *radio.Store
	broker    *radio.Broker
	live      *radio.LiveBroker
	audio     blobstore.Store
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

// Audio: GET /calls/{id}/audio — the call's recording (8 kHz mono WAV
// today), with Range support for seeking. 404 until the call has ended and
// its audio is stored, for calls without audio (encrypted, or voice not
// followed), and for hidden talkgroups.
func (h *radioHandlers) Audio(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	if h.audio == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	a, err := h.store.GetCallAudio(r.Context(), id)
	if radio.IsNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		slog.Error("get radio call audio failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load audio"})
		return
	}
	obj, err := h.audio.Open(r.Context(), a.Key)
	if errors.Is(err, blobstore.ErrNotFound) {
		slog.Warn("radio call audio missing from storage", "call", id, "key", a.Key)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		slog.Error("open radio call audio failed", "call", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load audio"})
		return
	}
	defer obj.Close()

	contentType := a.ContentType
	if contentType == "" {
		contentType = obj.ContentType
	}
	if contentType == "" {
		contentType = "audio/wav"
	}
	w.Header().Set("Content-Type", contentType)
	// A recording never changes once stored.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("ETag", fmt.Sprintf(`"radio-call-%d-%d"`, id, obj.Size))
	http.ServeContent(w, r, "", obj.ModTime, obj)
}

// startSSE writes the headers of an SSE response and returns a func that
// sends chunks, each write under its own deadline (replacing the server's
// 30s WriteTimeout, which would otherwise end the stream), then flushes.
// The func returns false once the client is gone.
func startSSE(w http.ResponseWriter) (func(chunks ...[]byte) bool, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return nil, false
	}
	rc := http.NewResponseController(w)
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
		return nil, false
	}
	return write, true
}

// Events streams calls as they start and end, and as their audio and
// transcript land:
//
//	event: call
//	data: {...same shape as GET /calls/{id}...}
//
// A call is sent on start (ended_at null), on end, when its audio is stored
// (audio_url set) and when it is transcribed; clients upsert by id. Calls on
// hidden talkgroups are not sent.
func (h *radioHandlers) Events(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	write, ok := startSSE(w)
	if !ok {
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

// maxLiveTalkgroups bounds the ?tg= list on /live.
const maxLiveTalkgroups = 100

// Live streams decoded voice as receivers send it:
//
//	event: audio
//	data: {"call_id":"<receiver call id>","call":123,"tgid":10047,"seq":0,"pcm":"<base64>"}
//
//	event: end
//	data: {"call_id":"...","call":123,"tgid":10047}
//
// pcm is 8 kHz mono signed 16-bit little-endian. seq counts a call's chunks
// from 0; a gap means chunks were lost. "end" follows a call's last chunk.
// ?tg=10047,10048 limits the stream to those talkgroups (unit-to-unit calls
// are only in the unfiltered stream). Hidden talkgroups are never sent.
//
// Each active call is about 21 kB/s of base64. A client that can't keep up
// is disconnected rather than sent audio with holes; it should reconnect.
// Audio is fanned out in memory, so only receivers connected to this server
// instance are heard.
func (h *radioHandlers) Live(w http.ResponseWriter, r *http.Request) {
	tgs, ok := parseTalkgroupList(w, r.URL.Query().Get("tg"))
	if !ok {
		return
	}
	if h.live == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "live audio is not available"})
		return
	}
	user := GetUser(r.Context())
	// Subscribe first so nothing published after ": connected" is missed.
	sub, unsubscribe := h.live.Subscribe(tgs)
	defer unsubscribe()
	write, ok := startSSE(w)
	if !ok {
		return
	}

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	recheck := time.NewTicker(radioEntitlementRecheck)
	defer recheck.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.Gone():
			return // fell behind; the client reconnects
		case ev := <-sub.Events():
			if !write([]byte("event: "+ev.Event+"\ndata: "), ev.Data, []byte("\n\n")) {
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

// parseTalkgroupList parses "10047,10048" (empty: none). On a bad value it
// writes a 400 and returns false.
func parseTalkgroupList(w http.ResponseWriter, raw string) ([]int, bool) {
	if raw == "" {
		return nil, true
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxLiveTalkgroups {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too many talkgroups"})
		return nil, false
	}
	tgs := make([]int, 0, len(parts))
	for _, p := range parts {
		tg, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || tg < 1 || tg > 0xFFFF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tg"})
			return nil, false
		}
		tgs = append(tgs, tg)
	}
	return tgs, true
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
