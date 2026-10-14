package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const libredeskTimeout = 15 * time.Second

// libredeskClient is the HTTP plumbing the Libredesk mailers share: URL and
// credential validation, the API token header, and outbound conversations
// from one inbox, created by one agent (the bot).
type libredeskClient struct {
	baseURL   string
	authToken string
	inboxID   int
	agentID   int
	client    *http.Client
}

func newLibredeskClient(baseURL, apiKey, apiSecret string, inboxID, agentID int) (*libredeskClient, error) {
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
	return &libredeskClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		authToken: "token " + apiKey + ":" + apiSecret,
		inboxID:   inboxID,
		agentID:   agentID,
		client:    &http.Client{Timeout: libredeskTimeout},
	}, nil
}

// createConversation starts an outbound conversation to recipient (already
// validated) with content as the first message, and returns its uuid.
// content is rendered by Libredesk through text/template, so any
// user-controlled text in it must go through templateSafe.
func (c *libredeskClient) createConversation(ctx context.Context, recipient, name, subject, content string) (string, error) {
	var created struct {
		Data struct {
			UUID string `json:"uuid"`
		} `json:"data"`
	}
	err := c.do(ctx, http.MethodPost, "/api/v1/conversations", map[string]any{
		"inbox_id":      c.inboxID,
		"initiator":     "agent",
		"agent_id":      c.agentID,
		"contact_email": recipient,
		"first_name":    contactFirstName(name, recipient),
		"subject":       subject,
		"content":       content,
	}, &created)
	if err != nil {
		return "", fmt.Errorf("creating Libredesk conversation: %w", err)
	}
	if created.Data.UUID == "" {
		return "", errors.New("creating Libredesk conversation: response had no conversation id")
	}
	return created.Data.UUID, nil
}

// addTag adds an existing Libredesk tag to a conversation. Unknown tag names
// are silently ignored there rather than created.
func (c *libredeskClient) addTag(ctx context.Context, conversation, tag string) error {
	return c.do(ctx, http.MethodPost, conversationPath(conversation)+"/tags", map[string]any{"tags": []string{tag}, "action": "add_tags"}, nil)
}

func conversationPath(conversation string) string {
	return "/api/v1/conversations/" + url.PathEscape(conversation)
}

func (c *libredeskClient) do(ctx context.Context, method, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
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

// templateSafe makes user-controlled text safe to embed in a Libredesk
// message body. Libredesk runs the body through Go's text/template before
// sending it, so beyond HTML escaping every brace is replaced by its
// character reference: "{{ .Foo }}" can then never parse as a template
// action, and the email still shows the braces the person typed.
func templateSafe(s string) string {
	s = html.EscapeString(s)
	s = strings.ReplaceAll(s, "{", "&#123;")
	return strings.ReplaceAll(s, "}", "&#125;")
}
