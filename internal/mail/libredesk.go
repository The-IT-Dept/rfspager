package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	libredeskTimeout = 15 * time.Second
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
	baseURL   string
	authToken string
	inboxID   int
	agentID   int
	client    *http.Client
}

func NewLibredeskPasswordResetMailer(baseURL, apiKey, apiSecret string, inboxID, agentID int) (*LibredeskPasswordResetMailer, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname()))) {
		return nil, errors.New("Libredesk URL must be an HTTPS URL")
	}
	if apiKey == "" || apiSecret == "" || strings.ContainsAny(apiKey+apiSecret, ": \r\n") {
		return nil, errors.New("Libredesk API key and secret are required")
	}
	if inboxID <= 0 || agentID <= 0 {
		return nil, errors.New("Libredesk inbox and agent IDs are required")
	}
	return &LibredeskPasswordResetMailer{
		baseURL:   strings.TrimRight(baseURL, "/"),
		authToken: "token " + apiKey + ":" + apiSecret,
		inboxID:   inboxID,
		agentID:   agentID,
		client:    &http.Client{Timeout: libredeskTimeout},
	}, nil
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

	var created struct {
		Data struct {
			UUID string `json:"uuid"`
		} `json:"data"`
	}
	err = m.do(ctx, http.MethodPost, "/api/v1/conversations", map[string]any{
		"inbox_id":      m.inboxID,
		"initiator":     "agent",
		"agent_id":      m.agentID,
		"contact_email": recipient,
		"first_name":    contactFirstName(name, recipient),
		"subject":       "Reset your RFSPager password",
		"content":       passwordResetHTML(fullResetURL),
	}, &created)
	if err != nil {
		return fmt.Errorf("creating Libredesk conversation: %w", err)
	}
	if created.Data.UUID == "" {
		return errors.New("creating Libredesk conversation: response had no conversation id")
	}

	path := "/api/v1/conversations/" + url.PathEscape(created.Data.UUID)
	if err := m.do(ctx, http.MethodPost, path+"/tags", map[string]any{"tags": []string{PasswordResetTag}, "action": "add_tags"}, nil); err != nil {
		slog.Warn("password reset: tagging Libredesk conversation failed", "conversation", created.Data.UUID, "error", err)
	}
	if err := m.do(ctx, http.MethodPut, path+"/status", map[string]string{"status": "Resolved"}, nil); err != nil {
		slog.Warn("password reset: resolving Libredesk conversation failed", "conversation", created.Data.UUID, "error", err)
	}
	return nil
}

func (m *LibredeskPasswordResetMailer) do(ctx context.Context, method, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", m.authToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &apiErr)
		return fmt.Errorf("Libredesk %s %s: HTTP %d %s", method, path, resp.StatusCode, apiErr.Message)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decoding Libredesk response: %w", err)
		}
	}
	return nil
}

// contactFirstName is the name a new Libredesk contact is created with
// (existing contacts keep theirs). Libredesk requires one, so fall back to
// the mailbox name.
func contactFirstName(name, email string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return strings.SplitN(email, "@", 2)[0]
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
