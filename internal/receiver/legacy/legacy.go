// Package legacy provides a thin HTTP poster that delivers decoded pages
// to the original Laravel-era rfspager backend. Used when the receiver
// needs to backfeed the old platform while it's still in production.
//
// Endpoint shape (from laravel-old/routes/api.php + MessageController):
//
//	POST {base_url}/api/messages/{system_uuid}
//	Content-Type: application/json
//	Body: {"address": "0521839", "message": "...", "timestamp": <unix-seconds>}
//
// The system is resolved by `system_id` UUID — no API key, no bearer token.
// The Laravel side runs its own dedupe (address + md5 within ±5 min) so
// idempotent retries are safe.
package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type Client struct {
	BaseURL    string
	SystemUUID string
	HTTP       *http.Client

	mu     sync.Mutex
	status Status
}

// Status is the endpoint's delivery record for the local UI. Times are unix
// ms, 0 when never.
type Status struct {
	LastOK      bool // the newest post succeeded
	PagesSent   int64
	PagesFailed int64
	LastPageAt  int64
	LastError   string
	LastErrorAt int64
}

// Status reports the delivery counters.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Client) record(err error) {
	now := time.Now().UnixMilli()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.LastOK = err == nil
	if err != nil {
		c.status.PagesFailed++
		c.status.LastError, c.status.LastErrorAt = err.Error(), now
		return
	}
	c.status.PagesSent++
	c.status.LastPageAt = now
}

func New(baseURL, systemUUID string) *Client {
	return &Client{
		BaseURL:    baseURL,
		SystemUUID: systemUUID,
		HTTP:       &http.Client{Timeout: 15 * time.Second},
	}
}

// SendPage matches the wsclient.Client.SendPage signature so it can stand
// in as the Forwarder's PageSender. The function and baud arguments are
// accepted for interface parity but the legacy endpoint ignores them.
func (c *Client) SendPage(addr, msg string, when time.Time, fn, baud int) {
	_ = fn
	_ = baud
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := c.post(ctx, addr, msg, when)
		c.record(err)
		if err != nil {
			// Previously discarded. With one upstream a silent drop was merely
			// invisible; fanning out to several, it makes "did this page reach
			// prod?" unanswerable — which is exactly the question a cutover
			// needs to answer.
			slog.Warn("legacy post failed",
				"url", c.BaseURL,
				"system_uuid", c.SystemUUID,
				"address", addr,
				"err", err,
			)
		}
	}()
}

func (c *Client) post(ctx context.Context, addr, msg string, when time.Time) error {
	body, _ := json.Marshal(map[string]any{
		"address":   addr,
		"message":   msg,
		"timestamp": when.Unix(),
	})
	url := fmt.Sprintf("%s/api/messages/%s", c.BaseURL, c.SystemUUID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("legacy server returned %d: %s", resp.StatusCode, string(rb))
	}
	return nil
}
