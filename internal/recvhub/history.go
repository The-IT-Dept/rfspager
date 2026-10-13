package recvhub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// SystemResolver maps a request to the system it addresses. It returns an
// HTTP status and error when it cannot: the owner resolver answers 403 for
// somebody else's system, the admin resolver 404 for an unknown id. The
// handlers below are shared by the owner (mobile) and admin routers, which
// differ only in how they resolve and authorise the system.
type SystemResolver func(r *http.Request) (systemID int64, status int, err error)

// historyLimit parses ?limit= with the shared default and cap.
func historyLimit(r *http.Request) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 500 {
		limit = 200
	}
	return limit
}

// SystemEvents returns the newest `limit` events for a system, newest first:
// {"data": [{id, type, severity, message, metadata, created_at}]}.
func SystemEvents(sqlDB *sqlx.DB, resolve SystemResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		systemID, status, err := resolve(r)
		if err != nil {
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		rows, err := sqlDB.QueryxContext(r.Context(), `
			SELECT id, type, severity, message, metadata, created_at
			  FROM system_events
			 WHERE system_id = $1
			 ORDER BY created_at DESC
			 LIMIT $2`, systemID, historyLimit(r))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		data := []map[string]any{}
		for rows.Next() {
			row := make(map[string]any)
			if err := rows.MapScan(row); err != nil {
				continue
			}
			cleanRow(row)
			data = append(data, row)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	}
}

// SystemLogsRecent returns the most recent persisted log lines, oldest
// first. Plain GET (no streaming) so clients can poll it without an SSE
// polyfill: {"data": [{ts, level, source, message, raw}]} with ts in unix
// milliseconds.
func SystemLogsRecent(hub *Hub, resolve SystemResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		systemID, status, err := resolve(r)
		if err != nil {
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		rows, err := hub.Logs.RecentLogs(r.Context(), systemID, historyLimit(r))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": rows})
	}
}

// SystemLogSSE streams persisted + live receiver log lines. The first
// events the client receives are the most recent 200 rows (oldest-first);
// subsequent events are live broadcasts from the broker. Same envelope
// shape in both cases.
func SystemLogSSE(hub *Hub, resolve SystemResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		systemID, status, err := resolve(r)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		streamSSE(w, r,
			func(ctx context.Context) ([][]byte, error) {
				rows, err := hub.Logs.RecentLogs(ctx, systemID, 200)
				if err != nil {
					return nil, err
				}
				out := make([][]byte, 0, len(rows))
				for _, row := range rows {
					envelope, _ := json.Marshal(row)
					out = append(out, envelope)
				}
				return out, nil
			},
			func() (<-chan []byte, func()) { return hub.Logs.Subscribe(systemID) },
		)
	}
}

// statsBackfill is how many stored reports a new stats stream subscriber
// gets before live ones: 60 reports at the default 15 s interval is the
// last quarter hour.
const statsBackfill = 60

// StatsSSE streams stats envelopes ({"ts": unix millis, "stats":
// rxproto.Stats}): the newest 60 stored reports oldest-first, then live.
func StatsSSE(hub *Hub, resolve SystemResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		systemID, status, err := resolve(r)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		streamSSE(w, r,
			func(ctx context.Context) ([][]byte, error) { return hub.Stats.Recent(ctx, systemID, statsBackfill) },
			func() (<-chan []byte, func()) { return hub.Stats.Subscribe(systemID) },
		)
	}
}

// SystemConfig returns the config the receiver should be running:
// {"version": n, "config": rxproto.RuntimeConfig}. Version 0 with the
// defaults when the system has no revision yet. Read-only; the admin API
// owns writes.
func SystemConfig(sqlDB *sqlx.DB, resolve SystemResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		systemID, status, err := resolve(r)
		if err != nil {
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		var (
			version int
			raw     []byte
		)
		err = sqlDB.QueryRowContext(r.Context(),
			`SELECT version, config FROM receiver_config_revisions
			  WHERE system_id = $1 ORDER BY version DESC LIMIT 1`, systemID).Scan(&version, &raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		cfg, err := rxproto.Normalize(raw)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rxproto.ConfigPayload{Version: version, Config: cfg})
	}
}

// streamSSE runs one server-sent-events response: headers, a backfill of
// stored envelopes, then live envelopes from subscribe until the client
// goes away. A comment line every 15 s keeps proxies from closing idle
// streams.
func streamSSE(w http.ResponseWriter, r *http.Request,
	backfill func(ctx context.Context) ([][]byte, error),
	subscribe func() (<-chan []byte, func()),
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	// Disable per-request write deadline — streaming.
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

	if envelopes, err := backfill(r.Context()); err == nil {
		for _, envelope := range envelopes {
			writeSSE(w, envelope)
		}
		flusher.Flush()
	}

	ch, cancel := subscribe()
	defer cancel()

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
			writeSSE(w, data)
			flusher.Flush()
		case <-keepalive.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, data []byte) {
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n\n"))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// cleanRow turns the []byte values MapScan produces for text and jsonb
// columns into strings and decoded JSON so they serialise naturally.
func cleanRow(m map[string]any) {
	for k, v := range m {
		if b, ok := v.([]byte); ok {
			var decoded any
			if err := json.Unmarshal(b, &decoded); err == nil {
				m[k] = decoded
			} else {
				m[k] = string(b)
			}
		}
	}
}
