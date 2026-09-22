package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestLogOmitsCredentials(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	h := RequestLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	r := httptest.NewRequest("GET", "/stream?token=secret-token&apikey=secret-key", nil)
	h.ServeHTTP(httptest.NewRecorder(), r)
	if strings.Contains(out.String(), "secret-") {
		t.Fatal("credentials leaked into request log")
	}
	if !strings.Contains(out.String(), "/stream") {
		t.Fatal("request path not logged")
	}
}
func TestBodyLimitRejectsOversizedInput(t *testing.T) {
	h := LimitBodies(4)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(413)
		}
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader("12345")))
	if w.Code != 413 {
		t.Fatalf("oversized body got %d", w.Code)
	}
}
func TestAuthRateLimitCannotBeBypassedWithForwardedHeader(t *testing.T) {
	limit, err := AuthRateLimit(2, time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest("POST", "/api/v2/mobile/auth/login", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("X-Forwarded-For", []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}[i])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if i == 2 && w.Code != 429 {
			t.Fatalf("spoofed IP bypassed rate limit: %d", w.Code)
		}
	}
}
