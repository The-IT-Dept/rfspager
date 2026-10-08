// Package asr transcribes audio through an OpenAI-compatible
// /v1/audio/transcriptions endpoint (vLLM serving Qwen3-ASR in production).
package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config for a transcription endpoint. URL and APIKey are both required;
// New returns nil without them so callers can treat ASR as switched off.
type Config struct {
	URL      string // full endpoint, e.g. https://host/v1/audio/transcriptions
	Model    string
	APIKey   string
	Language string // optional ISO-639-1 hint, e.g. "en"
	// Prompt is vocabulary and context sent with every request (the
	// OpenAI-style "prompt" field; Qwen3-ASR on vLLM honours it). Each
	// call can add its own context after it.
	Prompt string

	// Optional; zero means the default.
	AttemptTimeout time.Duration // per request, default 60s
	MaxAttempts    int           // including the first, default 3
	Backoff        time.Duration // before the 2nd attempt, doubling; default 2s
	HTTPClient     *http.Client
}

type Client struct {
	cfg  Config
	http *http.Client
}

// New returns nil when URL or APIKey is empty.
func New(cfg Config) *Client {
	if cfg.URL == "" || cfg.APIKey == "" {
		return nil
	}
	if cfg.AttemptTimeout <= 0 {
		cfg.AttemptTimeout = 60 * time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = 2 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &Client{cfg: cfg, http: client}
}

// maxRetryAfter caps how long a Retry-After header can make us wait.
const maxRetryAfter = 30 * time.Second

// StatusError is a non-2xx response from the endpoint.
type StatusError struct {
	Status int
	Body   string // start of the response body
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("asr: HTTP %d: %s", e.Status, e.Body)
}

func (e *StatusError) retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// Transcribe sends one audio file (filename's extension tells the server the
// format, e.g. "call.wav") and returns the normalised transcript, which is ""
// when no speech was recognised. 429s, 5xxs, network errors and attempt
// timeouts are retried with backoff up to MaxAttempts; other failures and
// ctx cancellation return at once.
func (c *Client) Transcribe(ctx context.Context, filename string, audio []byte, context string) (string, error) {
	prompt := strings.TrimSpace(c.cfg.Prompt + " " + context)
	var lastErr error
	wait := c.cfg.Backoff
	for attempt := 1; attempt <= c.cfg.MaxAttempts; attempt++ {
		text, retryAfter, err := c.attempt(ctx, filename, audio, prompt)
		if err == nil {
			return text, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		var se *StatusError
		if errors.As(err, &se) && !se.retryable() {
			return "", err
		}
		if attempt == c.cfg.MaxAttempts {
			break
		}
		delay := wait
		if retryAfter > delay {
			delay = min(retryAfter, maxRetryAfter)
		}
		wait *= 2
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return "", ctx.Err()
		case <-t.C:
		}
	}
	return "", fmt.Errorf("asr: gave up after %d attempts: %w", c.cfg.MaxAttempts, lastErr)
}

func (c *Client) attempt(ctx context.Context, filename string, audio []byte, prompt string) (string, time.Duration, error) {
	body, contentType, err := c.form(filename, audio, prompt)
	if err != nil {
		return "", 0, err
	}
	actx, cancel := context.WithTimeout(ctx, c.cfg.AttemptTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodPost, c.cfg.URL, body)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		// The URL is in *url.Error but the key is only in a header, so the
		// error is safe to log.
		return "", 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, err
	}
	if resp.StatusCode/100 != 2 {
		return "", retryAfter(resp.Header.Get("Retry-After")), &StatusError{Status: resp.StatusCode, Body: snippet(raw)}
	}
	var out struct {
		Text *string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Text == nil {
		return "", 0, &StatusError{Status: resp.StatusCode, Body: "unexpected response: " + snippet(raw)}
	}
	return Normalize(*out.Text), 0, nil
}

func (c *Client) form(filename string, audio []byte, prompt string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	h.Set("Content-Type", fileContentType(filename))
	fw, err := mw.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	if _, err := fw.Write(audio); err != nil {
		return nil, "", err
	}
	fields := [][2]string{{"model", c.cfg.Model}, {"response_format", "json"}}
	if c.cfg.Language != "" {
		fields = append(fields, [2]string{"language", c.cfg.Language})
	}
	if prompt != "" {
		fields = append(fields, [2]string{"prompt", prompt})
	}
	for _, f := range fields {
		if f[1] == "" {
			continue
		}
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return &buf, mw.FormDataContentType(), nil
}

func fileContentType(filename string) string {
	switch {
	case strings.HasSuffix(filename, ".wav"):
		return "audio/wav"
	case strings.HasSuffix(filename, ".ogg"), strings.HasSuffix(filename, ".opus"):
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}

// Qwen3-ASR's raw output is "language English<asr_text>the words"; vLLM's
// transcription endpoint strips that today (observed responses are plain
// {"text": "..."}), but a different serving stack or version might not.
var qwenPrefix = regexp.MustCompile(`^\s*language\s+[A-Za-z_-]+\s*<asr_text>`)

// Normalize cleans a transcript: drops a Qwen3-ASR "language X<asr_text>"
// prefix and stray <asr_text> tags, and collapses whitespace.
func Normalize(text string) string {
	text = qwenPrefix.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "<asr_text>", " ")
	text = strings.ReplaceAll(text, "</asr_text>", " ")
	return strings.Join(strings.Fields(text), " ")
}

func retryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
