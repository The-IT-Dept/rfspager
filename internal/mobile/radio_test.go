package mobile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func TestRequireSubscription(t *testing.T) {
	cases := []struct {
		name string
		user *AuthUser
		want int
	}{
		{"no user", nil, http.StatusUnauthorized},
		{"free user", &AuthUser{ID: 1}, http.StatusPaymentRequired},
		{"subscriber", &AuthUser{ID: 2, SubscriptionValid: true}, http.StatusOK},
		{"superuser without subscription", &AuthUser{ID: 3, Superuser: true}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := RequireSubscription(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.user != nil {
				req = req.WithContext(context.WithValue(req.Context(), UserContextKey, tc.user))
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d", rr.Code, tc.want)
			}
			if rr.Code == http.StatusPaymentRequired {
				var body map[string]string
				if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || body["code"] != "subscription_required" {
					t.Fatalf("402 body = %q, want code subscription_required", rr.Body.String())
				}
			}
		})
	}
}

// cmd/server mounts RadioRouter outside the timeout group at
// /v2/mobile/radio while the rest of the mobile API is mounted at /v2/mobile
// inside it. The more specific mount has to win, or the SSE stream would be
// cut at 30s by the timeout middleware.
func TestRadioRouterMountTakesPrecedenceOverMobileMount(t *testing.T) {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Mount("/v2/mobile/radio", RadioRouter(nil, "test-secret", nil, nil))
	})
	r.Group(func(r chi.Router) {
		r.Use(chimw.Timeout(1))
		r.Mount("/v2/mobile", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}))
	})
	for _, path := range []string{"/v2/mobile/radio/events", "/v2/mobile/radio/calls", "/v2/mobile/radio/units/1234567"} {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		// No token: RadioRouter's JWTUserAuth answers 401. The mobile mount
		// would have answered 418.
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("GET %s = %d, want 401 from the radio router", path, rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v2/mobile/messages", nil))
	if rr.Code != http.StatusTeapot {
		t.Errorf("GET /v2/mobile/messages = %d, want the mobile mount", rr.Code)
	}
}
