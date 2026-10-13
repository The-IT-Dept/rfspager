package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/install"
	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/recvhub"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// Receiver remote management: the admin side of
// docs/plans/2026-10-13-receiver-remote-management.md. Config revisions,
// sampled stats, commands and captures for one system, plus the newest
// published receiver release. The live streams (stats, logs) are mounted by
// cmd/server outside the request-timeout group; see ReceiverStatsStream.

// ReceiverStore is the slice of *db.DB these handlers use. Tests substitute
// an in-memory fake.
type ReceiverStore interface {
	GetSystemByID(ctx context.Context, id int64) (*models.System, error)
	CurrentReceiverConfig(ctx context.Context, systemID int64) (version int, raw []byte, found bool, err error)
	GetReceiverConfigRevision(ctx context.Context, systemID int64, version int) (*db.ReceiverConfigRevision, error)
	InsertReceiverConfigRevision(ctx context.Context, systemID int64, raw []byte, source, note string) (int, error)
	ListReceiverConfigRevisions(ctx context.Context, systemID int64, limit int) ([]db.ReceiverConfigRevision, error)
	GetSystemConnection(ctx context.Context, systemID int64) (*db.SystemConnection, error)
	QuerySystemStats(ctx context.Context, systemID int64, since, step time.Duration) ([]db.SystemStatsRow, error)
	InsertSystemCommand(ctx context.Context, cmd *db.SystemCommand) error
	InsertSystemCaptureCommand(ctx context.Context, capture *db.SystemCapture, cmd *db.SystemCommand) error
	ListSystemCommands(ctx context.Context, systemID int64, limit int) ([]db.SystemCommand, error)
	GetSystemCapture(ctx context.Context, id string) (*db.SystemCapture, error)
	ListSystemCaptures(ctx context.Context, systemID int64, limit int) ([]db.SystemCapture, error)
}

// ReleaseLookup is what the latest-version endpoint needs from the install
// package's release cache.
type ReleaseLookup interface {
	LatestVersion(ctx context.Context) (string, error)
}

// ReceiverDeps wires the receiver management routes into Router. Hub and
// Releases may be nil (the hub-backed routes are then not registered and
// latest-version answers 503).
type ReceiverDeps struct {
	Store    ReceiverStore
	Hub      *recvhub.Hub
	Releases ReleaseLookup
}

type receiverHandlers struct {
	store    ReceiverStore
	sqlDB    *sqlx.DB
	hub      *recvhub.Hub
	releases ReleaseLookup
}

func newReceiverHandlers(sqlDB *sqlx.DB, deps ReceiverDeps) *receiverHandlers {
	return &receiverHandlers{store: deps.Store, sqlDB: sqlDB, hub: deps.Hub, releases: deps.Releases}
}

// mountReceiverRoutes registers the per-system routes inside the /systems
// route group; they inherit the caller's auth middleware. The fleet-level
// /receivers/latest-version route is registered by Router alongside.
func mountReceiverRoutes(systems chi.Router, h *receiverHandlers) {
	systems.Get("/{id}/config", h.GetConfig)
	systems.Put("/{id}/config", h.PutConfig)
	systems.Get("/{id}/stats", h.GetStats)
	systems.Get("/{id}/events", recvhub.SystemEvents(h.sqlDB, h.resolve))
	systems.Post("/{id}/commands", h.PostCommand)
	systems.Get("/{id}/commands", h.ListCommands)
	systems.Get("/{id}/captures", h.ListCaptures)
	systems.Get("/{id}/captures/{cid}/download", h.DownloadCapture)
	if h.hub != nil {
		systems.Get("/{id}/logs/recent", recvhub.SystemLogsRecent(h.hub, h.resolve))
	}
}

// adminSystemResolver is the recvhub.SystemResolver for admin routes: the
// numeric id from the path, 404 when no such live system exists.
func adminSystemResolver(store ReceiverStore) recvhub.SystemResolver {
	return func(r *http.Request) (int64, int, error) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			return 0, http.StatusBadRequest, errors.New("invalid system id")
		}
		if _, err := store.GetSystemByID(r.Context(), id); err != nil {
			return 0, http.StatusNotFound, errors.New("system not found")
		}
		return id, http.StatusOK, nil
	}
}

func (h *receiverHandlers) resolve(r *http.Request) (int64, int, error) {
	return adminSystemResolver(h.store)(r)
}

// ReceiverStatsStream is GET /systems/{id}/stats/stream: SSE of
// {"ts": unix millis, "stats": rxproto.Stats}, the newest 60 stored
// reports then live. cmd/server mounts it (and ReceiverLogStream) outside
// the request-timeout group with NewJWTAuthMiddleware, which also accepts
// the admin JWT as ?token= because EventSource cannot set headers.
func ReceiverStatsStream(store ReceiverStore, hub *recvhub.Hub) http.HandlerFunc {
	return recvhub.StatsSSE(hub, adminSystemResolver(store))
}

// ReceiverLogStream is GET /systems/{id}/logs/stream: SSE of log envelopes,
// the newest 200 stored lines then live. See ReceiverStatsStream.
func ReceiverLogStream(store ReceiverStore, hub *recvhub.Hub) http.HandlerFunc {
	return recvhub.SystemLogSSE(hub, adminSystemResolver(store))
}

// --- config ---

// configRevisionView is a revision with its config normalised over the
// defaults, so keys added since it was written read as their defaults.
type configRevisionView struct {
	Version   int                   `json:"version"`
	Config    rxproto.RuntimeConfig `json:"config"`
	Source    string                `json:"source"`
	Note      *string               `json:"note"`
	CreatedAt time.Time             `json:"created_at"`
}

func revisionView(rev db.ReceiverConfigRevision) configRevisionView {
	cfg, err := rxproto.Normalize(rev.Config)
	if err != nil {
		cfg = rxproto.Defaults()
	}
	return configRevisionView{Version: rev.Version, Config: cfg, Source: rev.Source, Note: rev.Note, CreatedAt: rev.CreatedAt}
}

// appliedView is what the connected receiver last reported for a revision.
type appliedView struct {
	Version int    `json:"version"`
	Status  string `json:"status"` // applied | rejected
}

// receiverView is the connection facts the config page needs.
type receiverView struct {
	Online  bool   `json:"online"`
	Proto   int    `json:"proto"`
	Version string `json:"version,omitempty"`
}

// GetConfig handles GET /systems/{id}/config:
//
//	{
//	  "current":   {version, config, source, note, created_at} | null,
//	  "defaults":  rxproto.RuntimeConfig,
//	  "revisions": [ newest 20, same shape as current ],
//	  "applied":   {version, status} | null,   // from the live connection
//	  "receiver":  {online, proto, version} | null
//	}
func (h *receiverHandlers) GetConfig(w http.ResponseWriter, r *http.Request) {
	id, status, err := h.resolve(r)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	revisions, err := h.store.ListReceiverConfigRevisions(r.Context(), id, 20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	views := make([]configRevisionView, 0, len(revisions))
	for _, rev := range revisions {
		views = append(views, revisionView(rev))
	}
	var current *configRevisionView
	if len(views) > 0 {
		current = &views[0]
	}

	var applied *appliedView
	var receiver *receiverView
	if conn, err := h.store.GetSystemConnection(r.Context(), id); err == nil && conn != nil {
		receiver = &receiverView{
			Online:  time.Since(conn.LastPingAt) < recvhub.PingTimeout,
			Proto:   conn.Proto,
			Version: conn.ReceiverVersion.String,
		}
		if conn.ConfigVersion.Valid {
			applied = &appliedView{Version: int(conn.ConfigVersion.Int64), Status: conn.ConfigStatus.String}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"current":   current,
		"defaults":  rxproto.Defaults(),
		"revisions": views,
		"applied":   applied,
		"receiver":  receiver,
	})
}

// maxConfigBody bounds a PUT /config body; a RuntimeConfig is well under
// a kilobyte.
const maxConfigBody = 64 << 10

// PutConfig handles PUT /systems/{id}/config. The body is either
// {"config": RuntimeConfig, "note": "optional"} or a bare RuntimeConfig.
// The config is normalised over the defaults and validated; problems come
// back as 422 {"errors": {"field": "reason"}} keyed by JSON name (channel
// problems as "channels[i].field"). On success a new revision is stored
// with source "admin:<email>" and returned as the new current.
func (h *receiverHandlers) PutConfig(w http.ResponseWriter, r *http.Request) {
	id, status, err := h.resolve(r)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxConfigBody))
	if err != nil || len(strings.TrimSpace(string(body))) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	raw := json.RawMessage(body)
	note := r.URL.Query().Get("note")
	if wrapped, ok := fields["config"]; ok {
		raw = wrapped
		if n, ok := fields["note"]; ok {
			_ = json.Unmarshal(n, &note)
		}
	} else if _, ok := fields["note"]; ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "config is required"})
		return
	}
	cfg, err := rxproto.Normalize(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid config: " + err.Error()})
		return
	}
	if errs := cfg.Validate(); errs != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errs})
		return
	}
	canonical, err := json.Marshal(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	source := "admin:" + requestedBy(r)
	version, err := h.store.InsertReceiverConfigRevision(r.Context(), id, canonical, source, strings.TrimSpace(note))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rev, err := h.store.GetReceiverConfigRevision(r.Context(), id, version)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, revisionView(*rev))
}

// requestedBy identifies the acting admin for audit columns.
func requestedBy(r *http.Request) string {
	if u := GetAdminUser(r.Context()); u != nil && u.Email != "" {
		return u.Email
	}
	return "unknown"
}

// --- stats ---

const (
	defaultStatsSince = 6 * time.Hour
	maxStatsSince     = 7 * 24 * time.Hour
	defaultStatsStep  = time.Minute
)

// parseDuration accepts Go durations plus a day suffix ("24h", "7d").
func parseDuration(s string, def time.Duration) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || days < 0 {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return d, nil
}

// GetStats handles GET /systems/{id}/stats?since=6h&step=1m. since is
// capped at 7 days and step floored at 1 s. Rows are sampled: the first
// report in each step-sized bucket. Response:
//
//	{"since": "6h0m0s", "step": "1m0s", "data": [{"ts": unix millis, "stats": rxproto.Stats}]}
func (h *receiverHandlers) GetStats(w http.ResponseWriter, r *http.Request) {
	id, status, err := h.resolve(r)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	since, err := parseDuration(r.URL.Query().Get("since"), defaultStatsSince)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	step, err := parseDuration(r.URL.Query().Get("step"), defaultStatsStep)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if since > maxStatsSince {
		since = maxStatsSince
	}
	if since < time.Minute {
		since = time.Minute
	}
	if step < time.Second {
		step = time.Second
	}
	if step > since {
		step = since
	}
	rows, err := h.store.QuerySystemStats(r.Context(), id, since, step)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	data := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		data = append(data, recvhub.StatsEnvelope(row.TS, row.Stats))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"since": since.String(),
		"step":  step.String(),
		"data":  data,
	})
}

// --- commands ---

// releaseTag is the binary proxy's validator, so the update command can
// never accept a version the download would then reject.
var releaseTag = install.ReleaseTag

// commandRequest is the body of POST /systems/{id}/commands.
type commandRequest struct {
	Type   string          `json:"type"`
	Params json.RawMessage `json:"params"`
}

// commandView is the stored command, plus the capture row a capture
// command created.
type commandView struct {
	db.SystemCommand
	Capture *db.SystemCapture `json:"capture,omitempty"`
}

// PostCommand handles POST /systems/{id}/commands {"type": ..., "params": {...}}.
//
//	restart, dongle_reset  params ignored
//	update                 {"version": "vX.Y.Z"}
//	capture                {"kind": "spectrum"} or
//	                       {"kind": "audio"|"iq", "channel": "<label>", "seconds": 1..30}
//
// Validation failures are 422 {"errors": {"field": "reason"}}. A capture
// creates its system_captures row in the same transaction and puts
// capture_id into the command params. The row is returned with 201; the
// insert trigger wakes the hub, which delivers it to a connected proto-2
// receiver or leaves it queued for the next connect (expires after 1 h).
func (h *receiverHandlers) PostCommand(w http.ResponseWriter, r *http.Request) {
	id, status, err := h.resolve(r)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	var req commandRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxConfigBody)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	by := requestedBy(r)
	cmd := &db.SystemCommand{ID: uuid.NewString(), SystemID: id, Type: strings.TrimSpace(req.Type), RequestedBy: by}
	params := req.Params
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage(`{}`)
	}

	switch cmd.Type {
	case rxproto.CmdRestart, rxproto.CmdDongleReset:
		cmd.Params = json.RawMessage(`{}`)
	case rxproto.CmdUpdate:
		var p rxproto.UpdateParams
		if err := json.Unmarshal(params, &p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid params"})
			return
		}
		p.Version = strings.TrimSpace(p.Version)
		if !releaseTag.MatchString(p.Version) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"errors": map[string]string{"version": "must be a release tag like v5.4.0"}})
			return
		}
		cmd.Params, _ = json.Marshal(p)
	case rxproto.CmdCapture:
		var p rxproto.CaptureParams
		if err := json.Unmarshal(params, &p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid params"})
			return
		}
		if errs := h.validateCapture(r.Context(), id, &p); errs != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errs})
			return
		}
		capture := &db.SystemCapture{ID: uuid.NewString(), SystemID: id, Kind: p.Kind, RequestedBy: by}
		if p.Kind != rxproto.CaptureSpectrum {
			channel, seconds := p.Channel, p.Seconds
			capture.Channel, capture.Seconds = &channel, &seconds
		}
		p.CaptureID = capture.ID
		cmd.Params, _ = json.Marshal(p)
		if err := h.store.InsertSystemCaptureCommand(r.Context(), capture, cmd); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, commandView{SystemCommand: *cmd, Capture: capture})
		return
	default:
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"errors": map[string]string{"type": "must be one of restart, dongle_reset, update, capture"}})
		return
	}

	if err := h.store.InsertSystemCommand(r.Context(), cmd); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, commandView{SystemCommand: *cmd})
}

// validateCapture checks capture params and, for audio/iq, that the channel
// label exists in the system's current config (or the defaults when it has
// none) so a capture is never queued for a channel the receiver is not
// decoding.
func (h *receiverHandlers) validateCapture(ctx context.Context, systemID int64, p *rxproto.CaptureParams) map[string]string {
	errs := map[string]string{}
	p.Kind = strings.TrimSpace(p.Kind)
	p.Channel = strings.TrimSpace(p.Channel)
	switch p.Kind {
	case rxproto.CaptureSpectrum:
		p.Channel, p.Seconds = "", 0
	case rxproto.CaptureAudio, rxproto.CaptureIQ:
		if p.Seconds < 1 || p.Seconds > rxproto.MaxCaptureSeconds {
			errs["seconds"] = fmt.Sprintf("must be between 1 and %d", rxproto.MaxCaptureSeconds)
		}
		if p.Channel == "" {
			errs["channel"] = "channel label is required for audio and iq captures"
		} else if cfg, ok := h.effectiveConfig(ctx, systemID); ok && !hasChannel(cfg, p.Channel) {
			errs["channel"] = fmt.Sprintf("no channel labelled %q in the current config", p.Channel)
		}
	default:
		errs["kind"] = "must be one of spectrum, audio, iq"
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// effectiveConfig is the current revision normalised, or the defaults.
func (h *receiverHandlers) effectiveConfig(ctx context.Context, systemID int64) (rxproto.RuntimeConfig, bool) {
	_, raw, found, err := h.store.CurrentReceiverConfig(ctx, systemID)
	if err != nil {
		return rxproto.RuntimeConfig{}, false
	}
	if !found {
		return rxproto.Defaults(), true
	}
	cfg, err := rxproto.Normalize(raw)
	if err != nil {
		return rxproto.RuntimeConfig{}, false
	}
	return cfg, true
}

func hasChannel(cfg rxproto.RuntimeConfig, label string) bool {
	for _, ch := range cfg.Channels {
		// Exact match after trimming: the receiver looks channels up by the
		// literal label, so anything looser here would be accepted and then
		// fail on the Pi.
		if strings.TrimSpace(ch.Label) == strings.TrimSpace(label) {
			return true
		}
	}
	return false
}

func listLimit(r *http.Request, def, max int) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > max {
		return def
	}
	return limit
}

// ListCommands handles GET /systems/{id}/commands?limit= (default 50, max
// 200), newest first: {"data": [system_commands rows]}.
func (h *receiverHandlers) ListCommands(w http.ResponseWriter, r *http.Request) {
	id, status, err := h.resolve(r)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	rows, err := h.store.ListSystemCommands(r.Context(), id, listLimit(r, 50, 200))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []db.SystemCommand{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

// --- captures ---

// ListCaptures handles GET /systems/{id}/captures?limit= (default 50, max
// 200), newest first: {"data": [system_captures rows]}.
func (h *receiverHandlers) ListCaptures(w http.ResponseWriter, r *http.Request) {
	id, status, err := h.resolve(r)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	rows, err := h.store.ListSystemCaptures(r.Context(), id, listLimit(r, 50, 200))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []db.SystemCapture{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

// DownloadCapture handles GET /systems/{id}/captures/{cid}/download. Ready
// captures stream as an attachment (application/json for spectrum,
// application/octet-stream otherwise) with X-Capture-Kind, X-Capture-Rate
// and X-Capture-Channel headers so cmd/replay can be pointed at the file
// without a side channel. 409 while the capture is not ready.
func (h *receiverHandlers) DownloadCapture(w http.ResponseWriter, r *http.Request) {
	id, status, err := h.resolve(r)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	cid := chi.URLParam(r, "cid")
	if _, err := uuid.Parse(cid); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid capture id"})
		return
	}
	capture, err := h.store.GetSystemCapture(r.Context(), cid)
	if err != nil || capture.SystemID != id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "capture not found"})
		return
	}
	if capture.Status != db.CaptureReady || capture.Path == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "capture is not ready: " + capture.Status})
		return
	}
	f, err := os.Open(*capture.Path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "capture file is missing"})
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ext, ctype := "bin", "application/octet-stream"
	if capture.Kind == rxproto.CaptureSpectrum {
		ext, ctype = "json", "application/json"
	}
	name := fmt.Sprintf("system%d-%s", id, capture.Kind)
	if capture.Channel != nil && *capture.Channel != "" {
		name += "-" + sanitiseFilename(*capture.Channel)
		w.Header().Set("X-Capture-Channel", *capture.Channel)
	}
	name += "-" + cid[:8] + "." + ext
	if capture.SampleRate != nil {
		w.Header().Set("X-Capture-Rate", strconv.Itoa(*capture.SampleRate))
	}
	w.Header().Set("X-Capture-Kind", capture.Kind)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// sanitiseFilename keeps a channel label safe inside a download name.
func sanitiseFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// --- releases ---

// LatestVersion handles GET /receivers/latest-version: {"version": "vX.Y.Z"}
// from GitHub via the install package's 5-minute cache. 503 when binary
// distribution is not configured, 502 when GitHub cannot be reached.
func (h *receiverHandlers) LatestVersion(w http.ResponseWriter, r *http.Request) {
	if h.releases == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "binary distribution is not configured on this server"})
		return
	}
	version, err := h.releases.LatestVersion(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": version})
}
