package recvhub

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

// Event types — keep these in sync with the frontend type-badge code.
const (
	EvtReceiverConnected     = "receiver_connected"
	EvtReceiverDisconnected  = "receiver_disconnected"
	EvtReceiverOffline       = "receiver_offline"
	EvtReceiverOnline        = "receiver_online"
	EvtReceiverVersionChange = "receiver_version_change"
	EvtReceiverDongleChange  = "receiver_dongle_change"
	EvtReceiverError         = "receiver_error"
	EvtReceiverCrash         = "receiver_crash"

	// Remote management: config push outcomes, command completion and
	// capture uploads. Metadata carries the version / command id so the
	// admin UI can link an event back to the revision or command row.
	EvtReceiverConfigApplied  = "receiver_config_applied"
	EvtReceiverConfigRejected = "receiver_config_rejected"
	EvtReceiverCommand        = "receiver_command"
	EvtReceiverCaptureReady   = "receiver_capture_ready"
)

const (
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// EmitEvent writes a row into system_events. Best-effort: failures are
// logged but never propagated so they can't break the WS hot path.
func EmitEvent(ctx context.Context, db *sqlx.DB, systemID int64, evtType, severity, message string, metadata map[string]any) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		slog.Warn("event metadata marshal failed", "system_id", systemID, "type", evtType, "err", err)
		metaJSON = []byte("{}")
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO system_events (system_id, type, severity, message, metadata)
		 VALUES ($1, $2, $3, $4, $5)`,
		systemID, evtType, severity, message, metaJSON); err != nil {
		slog.Warn("emit event failed", "system_id", systemID, "type", evtType, "err", err)
	}
}

// LookupLastConnectedAt returns the timestamp of the most recent
// receiver_connected event for the system, or zero if there isn't one.
// Used to compute downtime on disconnect.
func LookupLastConnectedAt(ctx context.Context, db *sqlx.DB, systemID int64) time.Time {
	var t time.Time
	_ = db.GetContext(ctx, &t,
		`SELECT created_at FROM system_events
		  WHERE system_id = $1
		    AND type IN ($2, $3)
		  ORDER BY created_at DESC LIMIT 1`,
		systemID, EvtReceiverConnected, EvtReceiverOnline)
	return t
}
