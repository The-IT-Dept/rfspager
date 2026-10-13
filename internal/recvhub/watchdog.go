package recvhub

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// offlineThreshold is how long a receiver can be unreachable before we
// declare it offline + push the owner. Matches the policy chosen in the
// product brief (5-minute debounce so brief WS flaps don't spam owners).
const offlineThreshold = 5 * time.Minute

// watchdogInterval is how often the loop runs. One minute is plenty given
// the 5-minute threshold and the cost-of-running.
const watchdogInterval = 60 * time.Second

// runWatchdog periodically looks for state transitions and emits events +
// owner-targeted push notifications. Two transitions matter:
//
//  1. Online → Offline: system has no live system_connections row AND its
//     last ping is older than offlineThreshold AND we haven't already
//     emitted a `receiver_offline` event more recent than the last online
//     event.
//
//  2. Offline → Online: system has a recent ping AND there's an unresolved
//     `receiver_offline` event (no matching `receiver_online` after it).
//
// The sweeper (PruneStaleSystemConnections) clears stale rows on its own
// schedule; this loop reads from system_connections + last_message_at so
// it doesn't matter whether the row's been pruned yet.
func (h *Hub) runWatchdog(ctx context.Context) {
	// Stagger first run by 15s so it doesn't fight the boot import storm.
	t := time.NewTimer(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.watchdogTick(ctx)
			t.Reset(watchdogInterval)
		}
	}
}

func (h *Hub) watchdogTick(ctx context.Context) {
	db := h.db.Conn()

	// Find systems we believe are offline but haven't told the owner about.
	// A system is offline when its last_ping_at is older than
	// offlineThreshold AND there's no unresolved receiver_offline event
	// already. A disconnect deletes the system_connections row, so without
	// one fall back to the latest of its last message and its last
	// connect/disconnect event: a receiver that drops for a server deploy or
	// proxy reload was alive moments ago, however quiet the radio has been.
	const offlineQuery = `
		WITH last_status AS (
		    SELECT s.id   AS system_id,
		           s.name AS system_name,
		           COALESCE(sc.last_ping_at,
		                    GREATEST(
		                        (SELECT MAX(m.created_at) FROM messages m WHERE m.system_id = s.id),
		                        (SELECT MAX(e.created_at) FROM system_events e
		                          WHERE e.system_id = s.id
		                            AND e.type IN ('receiver_connected', 'receiver_disconnected'))
		                    )
		                   ) AS last_seen_at,
		           (
		               SELECT type FROM system_events
		                WHERE system_id = s.id
		                  AND type IN ('receiver_offline', 'receiver_online', 'receiver_connected')
		                ORDER BY created_at DESC LIMIT 1
		           ) AS last_event_type
		      FROM systems s
		      LEFT JOIN system_connections sc ON sc.system_id = s.id
		     WHERE s.deleted_at IS NULL
		       AND s.approved_at IS NOT NULL
		)
		SELECT system_id, system_name, last_seen_at, last_event_type
		  FROM last_status
		 WHERE last_seen_at IS NOT NULL
		   AND last_seen_at < NOW() - $1::interval
		   AND (last_event_type IS NULL OR last_event_type != 'receiver_offline')
	`
	type row struct {
		SystemID      int64          `db:"system_id"`
		SystemName    string         `db:"system_name"`
		LastSeenAt    time.Time      `db:"last_seen_at"`
		LastEventType sql.NullString `db:"last_event_type"`
	}
	var toOffline []row
	if err := db.SelectContext(ctx, &toOffline, offlineQuery, offlineThreshold.String()); err != nil {
		slog.Warn("watchdog offline scan failed", "err", err)
	}
	for _, r := range toOffline {
		downFor := time.Since(r.LastSeenAt).Round(time.Second)
		EmitEvent(ctx, db, r.SystemID, EvtReceiverOffline, SeverityWarning,
			fmt.Sprintf("Receiver hasn't checked in for %s", downFor),
			map[string]any{"last_seen_at": r.LastSeenAt, "down_for_seconds": int(downFor.Seconds())})
		h.notifyOwnerOffline(ctx, r.SystemID, r.SystemName, downFor)
	}

	// Online recovery — system that previously had a `receiver_offline`
	// event but has a fresh ping again.
	const onlineQuery = `
		WITH last_status AS (
		    SELECT s.id   AS system_id,
		           s.name AS system_name,
		           sc.last_ping_at,
		           (
		               SELECT type FROM system_events
		                WHERE system_id = s.id
		                  AND type IN ('receiver_offline', 'receiver_online', 'receiver_connected')
		                ORDER BY created_at DESC LIMIT 1
		           ) AS last_event_type
		      FROM systems s
		      JOIN system_connections sc ON sc.system_id = s.id
		     WHERE s.deleted_at IS NULL
		       AND s.approved_at IS NOT NULL
		)
		SELECT system_id, system_name, last_ping_at
		  FROM last_status
		 WHERE last_ping_at > NOW() - $1::interval
		   AND last_event_type = 'receiver_offline'
	`
	type rrow struct {
		SystemID   int64     `db:"system_id"`
		SystemName string    `db:"system_name"`
		LastPingAt time.Time `db:"last_ping_at"`
	}
	var toOnline []rrow
	// Use a fresh-ping window of 2× the ping interval so we don't reverse
	// the transition on the very first ping that races through.
	if err := db.SelectContext(ctx, &toOnline, onlineQuery, (2 * PingInterval).String()); err != nil {
		slog.Warn("watchdog online scan failed", "err", err)
	}
	for _, r := range toOnline {
		EmitEvent(ctx, db, r.SystemID, EvtReceiverOnline, SeverityInfo,
			"Receiver back online", map[string]any{"recovered_at": r.LastPingAt})
		h.notifyOwnerOnline(ctx, r.SystemID, r.SystemName)
	}
}

// notifyOwnerOffline pushes the owner (if any) with a "your receiver is
// down" message and a restart hint. Owner is taken from systems.owner_id.
func (h *Hub) notifyOwnerOffline(ctx context.Context, systemID int64, name string, downFor time.Duration) {
	if h.notifier == nil {
		return
	}
	ownerID, ok := h.ownerID(ctx, systemID)
	if !ok {
		return
	}
	title := fmt.Sprintf("%s is offline", name)
	body := fmt.Sprintf("Your receiver hasn't checked in for %s. Try a power-cycle.", downFor)
	h.notifier.SendDirect(ctx, ownerID, title, body, map[string]string{
		"type":      "system_offline",
		"system_id": fmt.Sprint(systemID),
	})
}

func (h *Hub) notifyOwnerOnline(ctx context.Context, systemID int64, name string) {
	if h.notifier == nil {
		return
	}
	ownerID, ok := h.ownerID(ctx, systemID)
	if !ok {
		return
	}
	h.notifier.SendDirect(ctx, ownerID,
		fmt.Sprintf("%s is back online", name),
		"Receiver reconnected — pages are flowing again.",
		map[string]string{
			"type":      "system_online",
			"system_id": fmt.Sprint(systemID),
		})
}

// ownerID returns the owner of the system, if one is set. systems.user_id
// is the owner column (Laravel naming).
func (h *Hub) ownerID(ctx context.Context, systemID int64) (int64, bool) {
	var id sql.NullInt64
	if err := h.db.Conn().GetContext(ctx, &id,
		`SELECT user_id FROM systems WHERE id = $1`, systemID); err != nil {
		return 0, false
	}
	if !id.Valid {
		return 0, false
	}
	return id.Int64, true
}
