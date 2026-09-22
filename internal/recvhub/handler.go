package recvhub

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/models"
)

// PageHandler is called once per decoded page received from any agent. It
// returns the ack status ("ok" / "dup" / "err") and an optional error.
type PageHandler func(ctx context.Context, sys *models.System, p PagePayload) (string, error)

// Handler builds the HTTP handler for GET /api/receiver/ws. apikey auth,
// websocket upgrade, then off to Conn.run.
func Handler(database *db.DB, hub *Hub, onPage PageHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("apikey")
		if apiKey == "" {
			apiKey = r.Header.Get("X-Api-Key")
		}

		if apiKey == "" {
			http.Error(w, `{"error":"apikey required"}`, http.StatusUnauthorized)
			return
		}

		sys, err := database.GetSystemByAPIKey(r.Context(), apiKey)
		if err != nil {
			http.Error(w, `{"error":"unknown api key"}`, http.StatusUnauthorized)
			return
		}

		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// Receivers run on the same trust boundary as the server; same-
			// origin isn't meaningful and the client is a native Go binary.
			InsecureSkipVerify: true,
		})
		if err != nil {
			slog.Warn("websocket accept failed", "err", err)
			return
		}

		// Detach the WS lifecycle from the HTTP request context: chi's
		// Timeout middleware would otherwise cancel it after 30s and kill
		// every long-lived connection. The conn closes on its own when
		// the socket fails, the hub shuts down, or a takeover evicts it.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		c := newConn(hub, ws, sys)
		c.OnPage = onPage
		c.run(ctx, database)
	}
}
