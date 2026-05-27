package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/middleware"
	"github.com/the-it-dept/rfspager/internal/models"
)

var coordRegex = regexp.MustCompile(`\[(-?[0-9]+\.[0-9]+),(-?[0-9]+\.[0-9]+)\]`)

// MessageHandler is the ingress for upstream message systems. It writes the
// message row and returns. A Postgres AFTER INSERT trigger fires NOTIFY
// rfs_message, and the dispatch package's fan-out worker picks it up from
// there — keeps the request path O(1 INSERT).
type MessageHandler struct {
	db *db.DB
}

func NewMessageHandler(database *db.DB) *MessageHandler {
	return &MessageHandler{db: database}
}

// ProcessV1 handles POST /api/messages/{system}
// This is the unauthenticated v1 endpoint where {system} is the system_id UUID.
func (h *MessageHandler) ProcessV1(w http.ResponseWriter, r *http.Request) {
	systemID := chi.URLParam(r, "system")
	ctx := r.Context()

	system, err := h.db.GetSystemByUUID(ctx, systemID)
	if err != nil {
		slog.Error("system not found", "system_id", systemID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "system not found"})
		return
	}

	var ingress models.MessageIngress
	if err := json.NewDecoder(r.Body).Decode(&ingress); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	timestamp := parseTimestamp(ingress.Timestamp)

	result, _ := h.createMessage(ctx, system, ingress.Address, ingress.Message, timestamp, &ingress)

	// Notification fan-out happens via the AFTER INSERT trigger on messages
	// — see internal/dispatch/fanout.go.

	writeJSON(w, http.StatusOK, map[string]string{"status": result})
}

// ProcessPagermon handles POST /api/messages — the drop-in replacement for
// the Pagermon ingress endpoint. The system is identified by the `apikey`
// header (Pagermon's convention; also accepted on `apiKey` and as a body
// field for sloppy clients). Body shape matches Pagermon's:
//
//   {
//     "address":  "0521839",
//     "message":  "<alpha>",
//     "datetime": "1717926215",
//     "source":   "ESPN",        // ignored
//     "UUID":     "...",         // ignored — system comes from the key
//     "pluginData": {}           // ignored
//   }
func (h *MessageHandler) ProcessPagermon(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var body struct {
		Address    string          `json:"address"`
		Message    string          `json:"message"`
		Datetime   any             `json:"datetime"` // unix string OR int OR RFC3339
		APIKey     string          `json:"apikey"`   // fallback if not in header
		PluginData json.RawMessage `json:"pluginData"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	apiKey := r.Header.Get("apikey")
	if apiKey == "" {
		apiKey = r.Header.Get("apiKey")
	}
	if apiKey == "" {
		apiKey = r.Header.Get("X-Api-Key")
	}
	if apiKey == "" {
		apiKey = body.APIKey
	}
	if apiKey == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "apikey required"})
		return
	}

	system, err := h.db.GetSystemByAPIKey(ctx, apiKey)
	if err != nil {
		slog.Warn("pagermon ingress: unknown api key", "error", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unknown api key"})
		return
	}

	if body.Address == "" || body.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "address and message required"})
		return
	}

	timestamp := parseTimestamp(body.Datetime)
	// Pagermon doesn't supply the agency/alias/icon/color overrides, so we
	// always pass nil for the ingress struct — capcode stealing is a no-op
	// on this path (matches Pagermon's behavior of just relaying).
	result, _ := h.createMessage(ctx, system, body.Address, body.Message, timestamp, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": result})
}

// MultimonMessage handles POST /api/v2/system/multimon_message
// This is the authenticated v2 endpoint.
func (h *MessageHandler) MultimonMessage(w http.ResponseWriter, r *http.Request) {
	system := middleware.GetSystem(r.Context())
	if system == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var ingress models.MultimonIngress
	if err := json.NewDecoder(r.Body).Decode(&ingress); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Parse timestamp and convert to UTC
	t, err := time.Parse(time.RFC3339, ingress.Timestamp)
	if err != nil {
		// Try other common formats
		t, err = time.Parse("2006-01-02 15:04:05", ingress.Timestamp)
		if err != nil {
			t = time.Now()
		}
	}
	timestamp := t.UTC()

	ctx := r.Context()
	result, _ := h.createMessage(ctx, system, ingress.Address, ingress.Alpha, timestamp, nil)

	// Notification fan-out happens via the AFTER INSERT trigger on messages
	// — see internal/dispatch/fanout.go.

	writeJSON(w, http.StatusOK, map[string]string{"status": result})
}

// IngestPage processes a decoded page from a receiver agent over the websocket
// transport. Same write path as ProcessPagermon / MultimonMessage but no HTTP
// concerns. Returns an ack status — "ok" if accepted, "dup" if deduped against
// a recent message, "err" otherwise.
func (h *MessageHandler) IngestPage(ctx context.Context, system *models.System, address, message string, timestamp time.Time) (string, error) {
	if address == "" || message == "" {
		return "err", fmt.Errorf("address and message required")
	}
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	result, msg := h.createMessage(ctx, system, address, message, timestamp, nil)
	if msg == nil {
		if result == "Duplicate message." {
			return "dup", nil
		}
		return "err", fmt.Errorf("%s", result)
	}
	return "ok", nil
}

// ReceiverMe handles GET /api/receiver/me. The receiver agent calls this on
// boot with its apikey header so it can confirm the key is valid and learn
// which frequency / system to listen to. Returns a small JSON describing the
// system the key belongs to.
func (h *MessageHandler) ReceiverMe(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("apikey")
	if apiKey == "" {
		apiKey = r.Header.Get("X-Api-Key")
	}
	if apiKey == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "apikey required"})
		return
	}

	system, err := h.db.GetSystemByAPIKey(r.Context(), apiKey)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unknown api key"})
		return
	}

	out := map[string]any{
		"id":          system.ID,
		"system_id":   system.SystemID,
		"name":        system.Name,
		"frequency":   system.Frequency,
	}
	if system.Lat.Valid {
		out["lat"] = system.Lat.Float64
	}
	if system.Lng.Valid {
		out["lng"] = system.Lng.Float64
	}
	writeJSON(w, http.StatusOK, out)
}

// Ping handles POST /api/v2/system/ping
func (h *MessageHandler) Ping(w http.ResponseWriter, r *http.Request) {
	system := middleware.GetSystem(r.Context())
	if system == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	_ = h.db.CreateSystemCheckin(r.Context(), system.ID, r.RemoteAddr, nil)

	writeJSON(w, http.StatusOK, map[string]string{
		"status": fmt.Sprintf("Hello, %s (%s)!", system.Name, system.SystemID),
	})
}

func (h *MessageHandler) createMessage(ctx context.Context, system *models.System, address, message string, timestamp time.Time, ingress *models.MessageIngress) (string, *models.Message) {
	// Reject messages older than 10 minutes
	if timestamp.Before(time.Now().Add(-10 * time.Minute)) {
		return fmt.Sprintf("Message is too old.. given %s vs now %s",
			timestamp.Format(time.RFC3339), time.Now().Format(time.RFC3339)), nil
	}

	// Check for duplicates
	isDup, err := h.db.IsDuplicateMessage(ctx, address, message, timestamp)
	if err != nil {
		slog.Error("duplicate check failed", "error", err)
	}
	if isDup {
		return "Duplicate message.", nil
	}

	// Find matching capcode
	capcode, err := h.db.FindCapcodeByAddress(ctx, address)
	if err != nil && system.StealCapcodes != "" && system.StealCapcodes != "0" && ingress != nil {
		// Try to steal/create the capcode if all fields are present
		if ingress.Agency != "" && ingress.Alias != "" && ingress.Address != "" && ingress.Icon != "" && ingress.Color != "" {
			agencyID, agErr := h.db.CreateAgency(ctx, ingress.Agency)
			if agErr != nil {
				slog.Error("failed to create agency", "error", agErr)
			} else {
				ignore := false
				if ingress.Ignore != nil {
					ignore = *ingress.Ignore
				}
				capcode = &models.Capcode{
					Address:  ingress.Address,
					Alias:    ingress.Alias,
					AgencyID: sql.NullInt64{Int64: agencyID, Valid: true},
					Icon:     ingress.Icon,
					Colour:   ingress.Color,
					Ignore:   ignore,
				}
				if err := h.db.CreateCapcode(ctx, capcode); err != nil {
					slog.Error("failed to create capcode", "error", err)
					capcode = nil
				}
			}
		}
	}

	// Create the message
	msg := &models.Message{
		SystemID:  system.ID,
		Address:   address,
		Message:   message,
		CreatedAt: timestamp,
	}

	if capcode != nil {
		msg.CapcodeID = sql.NullInt64{Int64: capcode.ID, Valid: true}
	}

	// Extract lat/lng from message text
	if matches := coordRegex.FindStringSubmatch(message); len(matches) == 3 {
		msg.Lat = sql.NullString{String: matches[2], Valid: true}
		msg.Lng = sql.NullString{String: matches[1], Valid: true}
	}

	if err := h.db.CreateMessage(ctx, msg); err != nil {
		slog.Error("failed to create message", "error", err)
		return "error creating message", nil
	}

	return strconv.FormatInt(msg.ID, 10), msg
}

// parseTimestamp handles the v1 timestamp field which can be a unix timestamp (int/float) or a string.
func parseTimestamp(v any) time.Time {
	switch ts := v.(type) {
	case float64:
		return time.Unix(int64(ts), 0)
	case string:
		if ts == "" {
			return time.Now()
		}
		// Try unix timestamp as string
		if i, err := strconv.ParseInt(ts, 10, 64); err == nil {
			return time.Unix(i, 0)
		}
		// Try RFC3339
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t
		}
		return time.Now()
	case nil:
		return time.Now()
	default:
		return time.Now()
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
