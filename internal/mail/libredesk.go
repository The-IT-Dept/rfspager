package mail

import (
	"context"
	"errors"
	"html"
	"log/slog"
	"net/http"
)

const (
	// PasswordResetTag must already exist in Libredesk; unknown tag names are
	// silently ignored there rather than created.
	PasswordResetTag = "password-reset"
)

// LibredeskPasswordResetMailer sends password recovery links as outbound
// conversations from a Libredesk email inbox, so every reset is visible in
// the support desk alongside the customer's other tickets and replies land
// in the same thread. It implements mobile.PasswordResetMailer.
//
// The API agent needs conversations:write, conversations:read,
// conversations:read_assigned, conversations:update_status and
// conversations:update_tags. Each conversation is assigned to that agent
// (self-assignment sends no notification) so it can tag and resolve it.
//
// Libredesk has no way to hide message content: agents who can read the
// inbox can see the reset link until it expires or is used.
type LibredeskPasswordResetMailer struct {
	*libredeskClient
}

func NewLibredeskPasswordResetMailer(baseURL, apiKey, apiSecret string, inboxID, agentID int) (*LibredeskPasswordResetMailer, error) {
	c, err := newLibredeskClient(baseURL, apiKey, apiSecret, inboxID, agentID)
	if err != nil {
		return nil, err
	}
	return &LibredeskPasswordResetMailer{c}, nil
}

// SendPasswordReset queues the email in Libredesk, which delivers it through
// the inbox's mail server. Tagging and resolving are best-effort: once the
// conversation exists the email is on its way, so their failure is logged,
// not returned.
func (m *LibredeskPasswordResetMailer) SendPasswordReset(ctx context.Context, recipient, name, fullResetURL, token string) error {
	if m == nil {
		return errors.New("password reset mailer is not configured")
	}
	if token == "" {
		return errors.New("password reset token is required")
	}
	recipient, err := safeEmailAddress(recipient)
	if err != nil {
		return err
	}
	if err := safeResetURL(fullResetURL); err != nil {
		return err
	}

	conversation, err := m.createConversation(ctx, recipient, name, "Reset your RFSPager password", passwordResetHTML(fullResetURL))
	if err != nil {
		return err
	}

	if err := m.addTag(ctx, conversation, PasswordResetTag); err != nil {
		slog.Warn("password reset: tagging Libredesk conversation failed", "conversation", conversation, "error", err)
	}
	if err := m.do(ctx, http.MethodPut, conversationPath(conversation)+"/status", map[string]string{"status": "Resolved"}, nil); err != nil {
		slog.Warn("password reset: resolving Libredesk conversation failed", "conversation", conversation, "error", err)
	}
	return nil
}

// passwordResetHTML is the message body. Libredesk renders content through
// text/template before sending, so it must never contain user-controlled
// text; the URL is server-built and escaped.
func passwordResetHTML(resetURL string) string {
	link := html.EscapeString(resetURL)
	return `<p>Hi,</p>` +
		`<p>We received a request to reset the password for your RFSPager account. Use the link below to choose a new one:</p>` +
		`<p><a href="` + link + `">Reset your password</a></p>` +
		`<p>If the link doesn't open, copy this address into your browser:<br>` + link + `</p>` +
		`<p>The link expires in one hour and can only be used once. If you didn't ask to reset your password, you can ignore this email; your password won't change.</p>` +
		`<p>RFSPager</p>`
}
