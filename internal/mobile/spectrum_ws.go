package mobile

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"github.com/the-it-dept/rfspager/internal/recvhub"
)

// SpectrumWS serves GET /api/v2/mobile/systems/{uuid}/spectrum as a
// WebSocket stream of spectrum frames + an initial history bundle. Auth
// is the standard mobile JWT middleware (Bearer token); React Native's
// native WebSocket supports custom headers so we don't need a query-token
// fallback like the admin SSE does.
func SpectrumWS(db *sqlx.DB, hub *recvhub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r.Context())
		if user == nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		uuid := chi.URLParam(r, "uuid")
		if uuid == "" {
			http.Error(w, "bad uuid", http.StatusBadRequest)
			return
		}
		var systemID int64
		if err := db.GetContext(r.Context(), &systemID,
			`SELECT id FROM systems WHERE system_id = $1 AND deleted_at IS NULL`, uuid); err != nil {
			http.Error(w, "system not found", http.StatusNotFound)
			return
		}

		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true, // same trust boundary as the rest of the mobile API
		})
		if err != nil {
			slog.Warn("mobile spectrum WS accept failed", "err", err)
			return
		}

		// Detach from r.Context() so streaming isn't cut by chi.Timeout.
		// Client disconnect is detected via ws.Write / ws.Ping returning err.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		defer ws.Close(websocket.StatusNormalClosure, "client disconnected")

		hub.Spectrum.LoadSnapshot(ctx, systemID)
		ch, unsubscribe := hub.Spectrum.Subscribe(systemID)
		defer unsubscribe()

		// Drain incoming messages in a goroutine so a client-side close is
		// noticed promptly (otherwise we'd only see it on the next Write).
		go func() {
			for {
				if _, _, err := ws.Read(ctx); err != nil {
					cancel()
					return
				}
			}
		}()

		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-ch:
				if !ok {
					return
				}
				if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
					return
				}
			case <-keepalive.C:
				if err := ws.Ping(ctx); err != nil {
					return
				}
			}
		}
	}
}
