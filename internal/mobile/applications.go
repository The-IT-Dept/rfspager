package mobile

import (
	"context"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/mail"
)

// applicationEmailTimeout bounds the thank-you email (Libredesk calls plus
// storing the conversation uuid).
const applicationEmailTimeout = 30 * time.Second

// ApplicationMailer thanks someone for applying to host a receiver and
// returns the support conversation it started.
// *mail.LibredeskApplicationMailer implements it.
type ApplicationMailer interface {
	SendApplicationReceived(ctx context.Context, to, name string, app mail.ReceiverApplication) (conversationUUID string, err error)
}

// sendApplicationReceived emails the applicant and stores the conversation
// on the system, so the approval can reply in the same thread. Best-effort:
// the application already exists, so failures are only logged.
func sendApplicationReceived(ctx context.Context, db *sqlx.DB, mailer ApplicationMailer, systemID int64, to, name string, app mail.ReceiverApplication) {
	conversation, err := mailer.SendApplicationReceived(ctx, to, name, app)
	if err != nil {
		slog.Warn("receiver application: thank-you email failed", "system_id", systemID, "error", err)
		return
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE systems SET support_conversation_uuid = $1 WHERE id = $2`, conversation, systemID); err != nil {
		slog.Warn("receiver application: storing support conversation failed", "system_id", systemID, "conversation", conversation, "error", err)
	}
}
