package admin

import (
	"context"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

// approvalEmailTimeout bounds the approval email (owner lookup, Libredesk
// calls and storing the conversation uuid).
const approvalEmailTimeout = 30 * time.Second

// ApplicationMailer emails a receiver's owner the install command once it is
// approved: a reply in the application's support conversation when there is
// one, otherwise a new conversation, whose uuid is returned.
// *mail.LibredeskApplicationMailer implements it.
type ApplicationMailer interface {
	SendApplicationApproved(ctx context.Context, conversationUUID, to, name, systemName, installCommand string) (string, error)
}

// sendApplicationApproved looks up the system's owner and support
// conversation, sends the approval, and stores the conversation when a new
// one was started. Best-effort: the system is already approved and the
// command is in the app, so failures are only logged.
func sendApplicationApproved(ctx context.Context, db *sqlx.DB, mailer ApplicationMailer, systemID int64, installCommand string) {
	var owner struct {
		Email        string `db:"email"`
		Name         string `db:"name"`
		SystemName   string `db:"system_name"`
		Conversation string `db:"conversation"`
	}
	if err := db.GetContext(ctx, &owner, `
		SELECT u.email, COALESCE(u.name, '') AS name, s.name AS system_name,
		       COALESCE(s.support_conversation_uuid, '') AS conversation
		  FROM systems s
		  JOIN users u ON u.id = s.user_id
		 WHERE s.id = $1`, systemID); err != nil {
		slog.Warn("receiver approval: owner lookup for email failed", "system_id", systemID, "error", err)
		return
	}
	conversation, err := mailer.SendApplicationApproved(ctx, owner.Conversation, owner.Email, owner.Name, owner.SystemName, installCommand)
	if err != nil {
		slog.Warn("receiver approval: email failed", "system_id", systemID, "conversation", owner.Conversation, "error", err)
		return
	}
	if conversation == owner.Conversation {
		return
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE systems SET support_conversation_uuid = $1 WHERE id = $2`, conversation, systemID); err != nil {
		slog.Warn("receiver approval: storing support conversation failed", "system_id", systemID, "conversation", conversation, "error", err)
	}
}
