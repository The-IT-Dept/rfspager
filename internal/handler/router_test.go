package handler

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacyIngressRoutesAreGone(t *testing.T) {
	r := chi.NewRouter()
	RegisterIngress(r, NewMessageHandler(nil))
	for _, path := range []string{"/messages/example-uuid", "/v2/system/ping", "/v2/system/multimon_message"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{}`)))
		if w.Code != 404 {
			t.Errorf("legacy path %s remains: %d", path, w.Code)
		}
	}
}
func TestSupportedIngressNeedsCredential(t *testing.T) {
	r := chi.NewRouter()
	RegisterIngress(r, NewMessageHandler(nil))
	for _, tt := range []struct{ method, path string }{{"POST", "/messages"}, {"GET", "/receiver/me"}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{"address":"1","message":"test"}`)))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s returned %d", tt.path, w.Code)
		}
	}
}
