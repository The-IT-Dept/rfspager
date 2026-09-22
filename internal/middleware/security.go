package middleware

import (
	"fmt"
	chimw "github.com/go-chi/chi/v5/middleware"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RequestLog deliberately excludes query strings, headers and request bodies.
// EventSource tokens must never be copied into application access logs.
func RequestLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				logger.Info("http request", "method", r.Method, "path", r.URL.Path, "status", wrapped.Status(), "duration", time.Since(start))
			}()
			next.ServeHTTP(wrapped, r)
		})
	}
}
func LimitBodies(size int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, size)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(w, r)
		})
	}
}

// AuthRateLimit is a per-process safety net. Deployments with multiple replicas
// should additionally enforce a shared limit at their trusted ingress proxy.
func AuthRateLimit(limit int, window time.Duration, trustedCIDRs []string) (func(http.Handler) http.Handler, error) {
	var trusted []*net.IPNet
	for _, cidr := range trustedCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR")
		}
		trusted = append(trusted, network)
	}
	isTrusted := func(ip net.IP) bool {
		for _, network := range trusted {
			if network.Contains(ip) {
				return true
			}
		}
		return false
	}
	clientIP := func(r *http.Request) string {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return "unknown"
		}
		if !isTrusted(ip) {
			return ip.String()
		}
		chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(chain) - 1; i >= 0; i-- {
			candidate := net.ParseIP(strings.TrimSpace(chain[i]))
			if candidate == nil {
				return ip.String()
			}
			if !isTrusted(candidate) {
				return candidate.String()
			}
		}
		return ip.String()
	}
	type bucket struct {
		count int
		until time.Time
	}
	entries := map[string]bucket{}
	var mu sync.Mutex
	lastSweep := time.Now()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || !(strings.HasPrefix(r.URL.Path, "/api/v2/mobile/auth/") || strings.HasPrefix(r.URL.Path, "/api/v1/admin/auth/")) {
				next.ServeHTTP(w, r)
				return
			}
			now := time.Now()
			key := clientIP(r)
			mu.Lock()
			if now.Sub(lastSweep) >= window {
				for k, b := range entries {
					if !now.Before(b.until) {
						delete(entries, k)
					}
				}
				lastSweep = now
			}
			b, exists := entries[key]
			if !exists || !now.Before(b.until) {
				b = bucket{until: now.Add(window)}
			}
			allowed := b.count < limit && (exists || len(entries) < 10000)
			if allowed {
				b.count++
				entries[key] = b
			}
			mu.Unlock()
			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())+1))
				http.Error(w, "too many authentication attempts", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}
