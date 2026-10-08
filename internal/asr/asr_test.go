package asr

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(url string) *Client {
	return New(Config{
		URL: url, Model: "Qwen/Qwen3-ASR-1.7B", APIKey: "test-key", Language: "en",
		AttemptTimeout: 200 * time.Millisecond, MaxAttempts: 3, Backoff: time.Millisecond,
	})
}

func TestNewWithoutURLOrKeyIsOff(t *testing.T) {
	if New(Config{URL: "http://x"}) != nil || New(Config{APIKey: "k"}) != nil {
		t.Fatal("New returned a client without both URL and key")
	}
}

func TestTranscribeSendsOpenAIRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("model") != "Qwen/Qwen3-ASR-1.7B" || r.FormValue("response_format") != "json" || r.FormValue("language") != "en" {
			t.Errorf("form = %v", r.MultipartForm.Value)
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(f)
		if hdr.Filename != "call.wav" || hdr.Header.Get("Content-Type") != "audio/wav" || string(data) != "RIFFdata" {
			t.Errorf("file %q %q %q", hdr.Filename, hdr.Header.Get("Content-Type"), data)
		}
		// The shape the production endpoint returned for a test clip.
		io.WriteString(w, `{"text":"Central Coast A Gosford Two Five Responding.","usage":{"type":"duration","seconds":4}}`)
	}))
	defer srv.Close()
	got, err := testClient(srv.URL).Transcribe(context.Background(), "call.wav", []byte("RIFFdata"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "Central Coast A Gosford Two Five Responding." {
		t.Errorf("transcript = %q", got)
	}
}

func TestTranscribeRetriesThen503Succeeds(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			http.Error(w, `{"error":"overloaded"}`, http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, `{"text":"ok"}`)
	}))
	defer srv.Close()
	got, err := testClient(srv.URL).Transcribe(context.Background(), "a.wav", []byte("x"))
	if err != nil || got != "ok" || n.Load() != 3 {
		t.Fatalf("got %q, %v after %d requests; want ok after 3", got, err, n.Load())
	}
}

func TestTranscribeGivesUpAfterMaxAttempts(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := testClient(srv.URL).Transcribe(context.Background(), "a.wav", []byte("x"))
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 429 || n.Load() != 3 {
		t.Fatalf("err = %v after %d requests; want a 429 StatusError after 3", err, n.Load())
	}
}

func TestTranscribeDoesNotRetryClientErrors(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.Error(w, `{"error":{"code":"unauthorized","message":"login required"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := testClient(srv.URL).Transcribe(context.Background(), "a.wav", []byte("x"))
	if err == nil || n.Load() != 1 {
		t.Fatalf("err = %v after %d requests; want an error after 1", err, n.Load())
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Error("error message contains the API key")
	}
}

func TestTranscribeTimesOutEachAttempt(t *testing.T) {
	release := make(chan struct{})
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		io.Copy(io.Discard, r.Body)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release) // before Close, which waits for the handlers
	start := time.Now()
	_, err := testClient(srv.URL).Transcribe(context.Background(), "a.wav", []byte("x"))
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a deadline exceeded", err)
	}
	if n.Load() != 3 {
		t.Errorf("%d attempts, want 3", n.Load())
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestTranscribeStopsOnContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL, APIKey: "k", Backoff: time.Hour})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Transcribe(ctx, "a.wav", []byte("x")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context's error during backoff", err)
	}
}

func TestTranscribeRejectsUnexpectedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"result":"?"}`)
	}))
	defer srv.Close()
	if _, err := testClient(srv.URL).Transcribe(context.Background(), "a.wav", []byte("x")); err == nil {
		t.Fatal("accepted a response without text")
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"Central Coast A Gosford Two Five Responding.": "Central Coast A Gosford Two Five Responding.",
		"  ":                                 "",
		"language English<asr_text>Hello":    "Hello",
		"language None<asr_text>":            "",
		" two\n  lines\tof  text ":           "two lines of text",
		"<asr_text>tagged</asr_text>":        "tagged",
		"language english <asr_text> spaced": "spaced",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
