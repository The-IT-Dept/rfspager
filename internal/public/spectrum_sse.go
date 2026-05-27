package public

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"github.com/the-it-dept/rfspager/internal/recvhub"
)

// SpectrumSSE exposes the receiver hub's spectrum broker as a public,
// unauthenticated SSE stream keyed by system UUID rather than internal
// integer id. Anyone can subscribe — the spectrum data is a transparency
// feature, not sensitive information.
//
// Each frame is delivered identically to the admin SSE stream so the
// existing SpectrumWaterfall React component works against either endpoint
// just by changing the URL.
func SpectrumSSE(db *sqlx.DB, hub *recvhub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uuid := chi.URLParam(r, "uuid")
		if uuid == "" {
			http.Error(w, "bad uuid", http.StatusBadRequest)
			return
		}
		var systemID int64
		if err := db.GetContext(r.Context(), &systemID,
			`SELECT id FROM systems WHERE system_id = $1 AND deleted_at IS NULL AND approved_at IS NOT NULL`,
			uuid); err != nil {
			http.Error(w, "system not found", http.StatusNotFound)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		// Clear the per-request write deadline so the server-wide WriteTimeout
		// (30s for non-streaming routes) doesn't cut the stream off.
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

		hub.Spectrum.LoadSnapshot(r.Context(), systemID)

		ch, unsubscribe := hub.Spectrum.Subscribe(systemID)
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
}
