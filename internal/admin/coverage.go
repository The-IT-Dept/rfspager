package admin

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/the-it-dept/rfspager/internal/coverage"
)

// Coverage map endpoints (docs/plans/2026-10-14-coverage-map.md). The model
// and queries live in internal/coverage; these only parse and answer.

type coverageHandlers struct {
	svc *coverage.Service
}

func mountCoverageRoutes(r chi.Router, svc *coverage.Service) {
	h := &coverageHandlers{svc: svc}
	r.Route("/coverage", func(r chi.Router) {
		r.Get("/", h.Get)
		r.Post("/transmitters/refresh", h.Refresh)
		r.Put("/transmitters/{id}", h.UpdateTransmitter)
	})
}

// Get handles GET /coverage?days=30&system_id=&channel=RFS.
func (h *coverageHandlers) Get(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var p coverage.Params
	if v := q.Get("days"); v != "" {
		days, err := strconv.Atoi(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "days must be 1..365"})
			return
		}
		p.Days = days
	}
	if v := q.Get("system_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid system_id"})
			return
		}
		p.SystemID = id
	}
	p.Channel = q.Get("channel")
	p, ok := p.Normalize()
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "days must be 1..365 and channel RFS, FRNSW or all"})
		return
	}
	resp, err := h.svc.Report(r.Context(), p)
	if err != nil {
		slog.Error("coverage report failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "coverage report failed"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// Refresh handles POST /coverage/transmitters/refresh.
func (h *coverageHandlers) Refresh(w http.ResponseWriter, r *http.Request) {
	if !h.svc.StartRefresh() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "an ACMA import is already running"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// UpdateTransmitter handles PUT /coverage/transmitters/{id}:
// {"agency_id": 7} pins a district, {"agency_id": null} pins it unmapped,
// {"auto": true} returns it to automatic assignment.
func (h *coverageHandlers) UpdateTransmitter(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	if raw, ok := body["auto"]; ok {
		var auto bool
		if err := json.Unmarshal(raw, &auto); err != nil || !auto {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": `"auto" must be true`})
			return
		}
		err = h.svc.ResetDistrict(r.Context(), id)
	} else if raw, ok := body["agency_id"]; ok {
		var agencyID *int64
		if err := json.Unmarshal(raw, &agencyID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agency_id must be an integer or null"})
			return
		}
		err = h.svc.SetManualDistrict(r.Context(), id, agencyID)
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `body must set "agency_id" or "auto"`})
		return
	}

	switch {
	case errors.Is(err, coverage.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "transmitter not found"})
	case errors.Is(err, coverage.ErrUnknownAgency):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "unknown agency"})
	case err != nil:
		slog.Error("update transmitter district failed", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "update failed"})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// invalidateCoverage drops the coverage report cache after any write that
// reaches the wrapped routes. A nil service makes it a pass-through.
func invalidateCoverage(svc *coverage.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if svc == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				svc.Invalidate()
			}
		})
	}
}
