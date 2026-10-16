package localui

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	webhooksFile    = "webhooks.json"
	maxWebhooks     = 20
	webhookQueueLen = 256
	webhookTimeout  = 10 * time.Second
)

// defaultRetryDelays are the waits before the 2nd, 3rd and 4th attempt.
var defaultRetryDelays = []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second}

// Webhook is a host-configured page target as stored in webhooks.json.
type Webhook struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Secret    string `json:"secret,omitempty"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
}

// WebhookView is a webhook as the API shows it: no secret, plus delivery state.
type WebhookView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Enabled     bool   `json:"enabled"`
	HasSecret   bool   `json:"has_secret"`
	CreatedAt   int64  `json:"created_at"`
	State       string `json:"state"` // ok | failing | idle | disabled
	Sent        int64  `json:"sent"`
	Failed      int64  `json:"failed"`
	LastStatus  int    `json:"last_status"`
	LastOKAt    *int64 `json:"last_ok_at"`
	LastError   string `json:"last_error"`
	LastErrorAt *int64 `json:"last_error_at"`
}

// WebhookInput is the body of a create or update.
type WebhookInput struct {
	Name    string  `json:"name"`
	URL     string  `json:"url"`
	Secret  *string `json:"secret"` // nil on update = keep
	Enabled *bool   `json:"enabled"`
}

// Identity is the receiver block of every webhook payload.
type Identity struct {
	Hostname string `json:"hostname"`
	System   string `json:"system"`
	Version  string `json:"version"`
}

// ValidationError carries per-field messages for a 422.
type ValidationError map[string]string

func (v ValidationError) Error() string { return fmt.Sprintf("invalid webhook: %v", map[string]string(v)) }

// ErrNotFound is returned for an unknown webhook id.
var ErrNotFound = errors.New("webhook not found")

type hook struct {
	cfg    Webhook // guarded by Webhooks.mu
	queue  chan []byte
	cancel context.CancelFunc

	// delivery state, guarded by Webhooks.mu
	sent, failed int64
	lastStatus   int
	lastOK       int64
	lastErr      string
	lastErrAt    int64
	failing      bool
}

// Webhooks owns the host's webhooks: persistence, one delivery worker per
// webhook, and the counters the Connections tab shows.
type Webhooks struct {
	mu       sync.Mutex
	hooks    map[string]*hook
	order    []string
	path     string
	client   *http.Client
	identity func() Identity
	ctx      context.Context
	onChange func()
	retries  []time.Duration // set before any webhook exists; read-only after
}

// NewWebhooks loads webhooks.json from dir and starts a worker for each.
// Workers stop when ctx is cancelled.
func NewWebhooks(ctx context.Context, dir string, identity func() Identity) *Webhooks {
	w := &Webhooks{
		hooks:    map[string]*hook{},
		client:   &http.Client{Timeout: webhookTimeout},
		identity: identity,
		ctx:      ctx,
		retries:  defaultRetryDelays,
	}
	if dir != "" {
		w.path = filepath.Join(dir, webhooksFile)
	}
	if w.path != "" {
		if b, err := os.ReadFile(w.path); err == nil {
			var f struct {
				Webhooks []Webhook `json:"webhooks"`
			}
			if err := json.Unmarshal(b, &f); err != nil {
				slog.Error("webhooks.json unreadable; starting with no webhooks", "path", w.path, "err", err)
			}
			for _, c := range f.Webhooks {
				w.startLocked(c)
			}
		}
	}
	return w
}

func (w *Webhooks) startLocked(c Webhook) {
	ctx, cancel := context.WithCancel(w.ctx)
	h := &hook{cfg: c, queue: make(chan []byte, webhookQueueLen), cancel: cancel}
	w.hooks[c.ID] = h
	w.order = append(w.order, c.ID)
	go w.worker(ctx, h)
}

// Deliver queues a posted page for every enabled webhook. Never blocks: a
// full queue drops its oldest page.
func (w *Webhooks) Deliver(p Page) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.hooks) == 0 {
		return
	}
	body := w.payload("page", p)
	for _, id := range w.order {
		h := w.hooks[id]
		if !h.cfg.Enabled {
			continue
		}
		for {
			select {
			case h.queue <- body:
			default:
				select {
				case <-h.queue:
					h.failed++
				default:
				}
				continue
			}
			break
		}
	}
}

type payloadPage struct {
	ID        int64  `json:"id"`
	Address   string `json:"address"`
	Message   string `json:"message"`
	Time      string `json:"time"`
	Timestamp int64  `json:"timestamp"`
	Channel   string `json:"channel"`
	FreqHz    uint32 `json:"freq_hz"`
	Baud      int    `json:"baud"`
	Function  int    `json:"function"`
	Quality   any    `json:"quality,omitempty"`
}

func (w *Webhooks) payload(event string, p Page) []byte {
	t := time.UnixMilli(p.TS)
	pp := payloadPage{
		ID: p.ID, Address: p.Address, Message: p.Message,
		Time: t.Format(time.RFC3339), Timestamp: t.Unix(),
		Channel: p.Channel, FreqHz: p.FreqHz, Baud: p.Baud, Function: p.Function,
	}
	if p.Quality != nil {
		pp.Quality = p.Quality
	}
	b, _ := json.Marshal(map[string]any{"event": event, "receiver": w.identity(), "page": pp})
	return b
}

func (w *Webhooks) worker(ctx context.Context, h *hook) {
	for {
		select {
		case <-ctx.Done():
			return
		case body := <-h.queue:
			w.deliver(ctx, h, body)
		}
	}
}

// deliver posts one page with retries and records the outcome.
func (w *Webhooks) deliver(ctx context.Context, h *hook, body []byte) {
	delivery := uuid.NewString()
	var status int
	var err error
	for attempt := 0; ; attempt++ {
		w.mu.Lock()
		cfg := h.cfg
		w.mu.Unlock()
		if !cfg.Enabled {
			return
		}
		status, err = w.post(ctx, cfg, "page", delivery, body)
		if err == nil || ctx.Err() != nil {
			break
		}
		// Show the failure straight away; the page only counts as failed
		// once the retries are used up.
		w.record(h, status, err, attempt == len(w.retries))
		if attempt == len(w.retries) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.retries[attempt]):
		}
	}
	if err == nil {
		w.record(h, status, nil, false)
	}
}

// record notes a delivery attempt; final marks the last failed attempt of
// a delivery, which counts the page as failed.
func (w *Webhooks) record(h *hook, status int, err error, final bool) {
	w.mu.Lock()
	now := time.Now().UnixMilli()
	h.lastStatus = status
	changed := h.failing != (err != nil)
	if err != nil {
		if final {
			h.failed++
		}
		h.lastErr, h.lastErrAt, h.failing = err.Error(), now, true
	} else {
		h.sent++
		h.lastOK, h.failing = now, false
	}
	name, url := h.cfg.Name, h.cfg.URL
	fn := w.onChange
	w.mu.Unlock()
	if err != nil && changed {
		slog.Warn("webhook failing", "webhook", name, "url", url, "err", err)
	} else if changed {
		slog.Info("webhook recovered", "webhook", name, "url", url)
	}
	if changed && fn != nil {
		fn()
	}
}

// post sends one request; any 2xx is success.
func (w *Webhooks) post(ctx context.Context, cfg Webhook, event, delivery string, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	id := w.identity()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "rfspager-receiver/"+id.Version)
	req.Header.Set("X-RFSPager-Event", event)
	req.Header.Set("X-RFSPager-Delivery", delivery)
	if cfg.Secret != "" {
		req.Header.Set("X-RFSPager-Signature", Sign(cfg.Secret, body))
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// Sign is the X-RFSPager-Signature value for body.
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// TestResult is the outcome of a Send test.
type TestResult struct {
	OK         bool   `json:"ok"`
	Status     int    `json:"status"`
	Error      string `json:"error"`
	DurationMs int64  `json:"duration_ms"`
}

// Test sends one "test" event with a sample page, no retries.
func (w *Webhooks) Test(ctx context.Context, id string, sample Page) (TestResult, error) {
	w.mu.Lock()
	h := w.hooks[id]
	var cfg Webhook
	if h != nil {
		cfg = h.cfg
	}
	w.mu.Unlock()
	if h == nil {
		return TestResult{}, ErrNotFound
	}
	start := time.Now()
	status, err := w.post(ctx, cfg, "test", uuid.NewString(), w.payload("test", sample))
	r := TestResult{OK: err == nil, Status: status, DurationMs: time.Since(start).Milliseconds()}
	if err != nil {
		r.Error = err.Error()
	}
	return r, nil
}

// List returns every webhook in creation order.
func (w *Webhooks) List() []WebhookView {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]WebhookView, 0, len(w.order))
	for _, id := range w.order {
		out = append(out, w.viewLocked(w.hooks[id]))
	}
	return out
}

// Summary counts webhooks for the status endpoint.
func (w *Webhooks) Summary() (total, enabled, failing int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, h := range w.hooks {
		total++
		if h.cfg.Enabled {
			enabled++
			if h.failing {
				failing++
			}
		}
	}
	return
}

func (w *Webhooks) viewLocked(h *hook) WebhookView {
	v := WebhookView{
		ID: h.cfg.ID, Name: h.cfg.Name, URL: h.cfg.URL, Enabled: h.cfg.Enabled,
		HasSecret: h.cfg.Secret != "", CreatedAt: h.cfg.CreatedAt,
		Sent: h.sent, Failed: h.failed, LastStatus: h.lastStatus, LastError: h.lastErr,
	}
	if h.lastOK != 0 {
		v.LastOKAt = &h.lastOK
	}
	if h.lastErrAt != 0 {
		v.LastErrorAt = &h.lastErrAt
	}
	switch {
	case !h.cfg.Enabled:
		v.State = "disabled"
	case h.failing:
		v.State = "failing"
	case h.lastOK != 0:
		v.State = "ok"
	default:
		v.State = "idle"
	}
	return v
}

func validate(name, rawURL string) ValidationError {
	errs := ValidationError{}
	if len([]rune(name)) > 60 {
		errs["name"] = "must be 60 characters or fewer"
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	switch {
	case strings.TrimSpace(rawURL) == "":
		errs["url"] = "is required"
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "":
		errs["url"] = "must be an http:// or https:// URL"
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// Create adds a webhook and starts delivering to it.
func (w *Webhooks) Create(in WebhookInput) (WebhookView, error) {
	if errs := validate(in.Name, in.URL); errs != nil {
		return WebhookView{}, errs
	}
	c := Webhook{
		ID:        uuid.NewString(),
		Name:      strings.TrimSpace(in.Name),
		URL:       strings.TrimSpace(in.URL),
		Enabled:   in.Enabled == nil || *in.Enabled,
		CreatedAt: time.Now().UnixMilli(),
	}
	if in.Secret != nil {
		c.Secret = *in.Secret
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.hooks) >= maxWebhooks {
		return WebhookView{}, ValidationError{"url": fmt.Sprintf("at most %d webhooks", maxWebhooks)}
	}
	w.startLocked(c)
	if err := w.saveLocked(); err != nil {
		w.removeLocked(c.ID)
		return WebhookView{}, err
	}
	slog.Info("webhook added", "webhook", c.Name, "url", c.URL)
	return w.viewLocked(w.hooks[c.ID]), nil
}

// Update changes a webhook in place; queued pages go to the new URL.
func (w *Webhooks) Update(id string, in WebhookInput) (WebhookView, error) {
	if errs := validate(in.Name, in.URL); errs != nil {
		return WebhookView{}, errs
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	h := w.hooks[id]
	if h == nil {
		return WebhookView{}, ErrNotFound
	}
	prev := h.cfg
	h.cfg.Name = strings.TrimSpace(in.Name)
	h.cfg.URL = strings.TrimSpace(in.URL)
	if in.Enabled != nil {
		h.cfg.Enabled = *in.Enabled
	}
	if in.Secret != nil {
		h.cfg.Secret = *in.Secret
	}
	if err := w.saveLocked(); err != nil {
		h.cfg = prev
		return WebhookView{}, err
	}
	if h.cfg.URL != prev.URL || !h.cfg.Enabled {
		h.failing = false
	}
	slog.Info("webhook updated", "webhook", h.cfg.Name, "url", h.cfg.URL, "enabled", h.cfg.Enabled)
	return w.viewLocked(h), nil
}

// Delete removes a webhook and stops its worker; queued pages are dropped.
func (w *Webhooks) Delete(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	h := w.hooks[id]
	if h == nil {
		return ErrNotFound
	}
	w.removeLocked(id)
	if err := w.saveLocked(); err != nil {
		return err
	}
	slog.Info("webhook deleted", "webhook", h.cfg.Name, "url", h.cfg.URL)
	return nil
}

func (w *Webhooks) removeLocked(id string) {
	if h := w.hooks[id]; h != nil {
		h.cancel()
	}
	delete(w.hooks, id)
	for i, o := range w.order {
		if o == id {
			w.order = append(w.order[:i], w.order[i+1:]...)
			break
		}
	}
}

// saveLocked writes webhooks.json (0600, it holds the secrets) atomically.
func (w *Webhooks) saveLocked() error {
	if w.path == "" {
		return errors.New("no state dir; webhooks cannot be saved")
	}
	list := make([]Webhook, 0, len(w.order))
	for _, id := range w.order {
		list = append(list, w.hooks[id].cfg)
	}
	b, err := json.MarshalIndent(map[string]any{"webhooks": list}, "", "  ")
	if err != nil {
		return err
	}
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, w.path)
}
