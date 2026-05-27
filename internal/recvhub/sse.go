package recvhub

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// SpectrumSSE serves GET /api/v1/admin/systems/:id/spectrum as a Server-Sent
// Events stream. Each event is one JSON SpectrumPayload from the receiver.
// Authn is enforced by the parent admin router via JWT middleware.
func (h *Hub) SpectrumSSE(w http.ResponseWriter, r *http.Request) {
	systemID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad system id", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Clear the per-request write deadline so http.Server.WriteTimeout
	// (set globally to 30s for non-streaming routes) doesn't cut us off.
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Time{})
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// SSE behind a reverse proxy: disable buffering.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	// If the in-memory ring is empty (server restart, etc.) try to rehydrate
	// from Postgres before subscribing so the first event the client sees
	// is the recent history bundle.
	h.Spectrum.LoadSnapshot(r.Context(), systemID)

	ch, unsubscribe := h.Spectrum.Subscribe(systemID)
	defer unsubscribe()

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
