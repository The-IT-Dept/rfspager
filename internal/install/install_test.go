package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
)

type stubLookup struct{}

func (stubLookup) SystemNameForAPIKey(_ context.Context, key string) (string, error) {
	if key == "good-key" {
		return "Budgewoi", nil
	}
	return "", errors.New("unknown api key")
}

// githubStub stands in for api.github.com: a latest release, one older tag,
// and the asset bodies behind them. hits counts release lookups so the
// tests can prove the per-tag cache works.
func githubStub(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	releaseFor := func(r *http.Request, tag string) []byte {
		base := "http://" + r.Host + "/asset/" + tag
		rel := map[string]any{
			"tag_name": tag,
			"assets": []map[string]any{
				{"name": "rfspager-receiver-linux-arm64", "url": base + "/bin"},
				{"name": "rfspager-receiver-linux-arm64.sha256", "url": base + "/sum"},
			},
		}
		b, _ := json.Marshal(rel)
		return b
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/the-it-dept/rfspager/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer gh-token" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(releaseFor(r, "v5.4.0"))
	})
	mux.HandleFunc("/repos/the-it-dept/rfspager/releases/tags/", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		tag := r.URL.Path[len("/repos/the-it-dept/rfspager/releases/tags/"):]
		if tag != "v5.3.0" {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		_, _ = w.Write(releaseFor(r, tag))
	})
	mux.HandleFunc("/asset/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/octet-stream" {
			http.Error(w, "metadata requested", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "asset:"+r.URL.Path[len("/asset/"):])
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &hits
}

func newTestHandlers(t *testing.T, token string) (*Handlers, *atomic.Int32, http.Handler) {
	t.Helper()
	gh, hits := githubStub(t)
	h := New(stubLookup{}, token, "the-it-dept/rfspager", "https://rfspager.app")
	h.apiBase = gh.URL
	r := chi.NewRouter()
	r.Get("/api/binaries/receiver/{platform}", h.Binary)
	return h, hits, r
}

func getBinary(t *testing.T, router http.Handler, path string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("apikey", "good-key")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	body, _ := io.ReadAll(rr.Body)
	return rr, string(body)
}

func TestBinaryServesLatestReleaseByDefault(t *testing.T) {
	_, hits, router := newTestHandlers(t, "gh-token")
	rr, body := getBinary(t, router, "/api/binaries/receiver/linux-arm64")
	if rr.Code != http.StatusOK || body != "asset:v5.4.0/bin" {
		t.Fatalf("status=%d body=%q", rr.Code, body)
	}
	if got := rr.Header().Get("X-RFSPager-Release"); got != "v5.4.0" {
		t.Fatalf("release header = %q", got)
	}
	// The checksum comes from the same cached release.
	rr, body = getBinary(t, router, "/api/binaries/receiver/linux-arm64.sha256")
	if rr.Code != http.StatusOK || body != "asset:v5.4.0/sum" {
		t.Fatalf("checksum status=%d body=%q", rr.Code, body)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("release lookups = %d, want 1 (cached)", n)
	}
}

func TestBinaryServesRequestedVersion(t *testing.T) {
	_, hits, router := newTestHandlers(t, "gh-token")
	for range 2 {
		rr, body := getBinary(t, router, "/api/binaries/receiver/linux-arm64?version=v5.3.0")
		if rr.Code != http.StatusOK || body != "asset:v5.3.0/bin" {
			t.Fatalf("status=%d body=%q", rr.Code, body)
		}
		if got := rr.Header().Get("X-RFSPager-Release"); got != "v5.3.0" {
			t.Fatalf("release header = %q", got)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("tag lookups = %d, want 1 (cached per tag)", n)
	}
	// A pinned version and the latest are cached independently.
	rr, body := getBinary(t, router, "/api/binaries/receiver/linux-arm64")
	if rr.Code != http.StatusOK || body != "asset:v5.4.0/bin" {
		t.Fatalf("latest after pinned: status=%d body=%q", rr.Code, body)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("lookups after latest = %d, want 2", n)
	}
}

func TestBinaryRejectsVersionsThatAreNotReleaseTags(t *testing.T) {
	_, hits, router := newTestHandlers(t, "gh-token")
	for _, v := range []string{"5.3.0", "v5.3", "v5.3.0-rc1", "latest", "main", "v5.3.0;rm", "../v5.3.0"} {
		rr, body := getBinary(t, router, "/api/binaries/receiver/linux-arm64?version="+url.QueryEscape(v))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("version %q: status=%d body=%q", v, rr.Code, body)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("malformed versions reached github %d times", n)
	}
}

func TestBinaryUnknownVersionIsNotFound(t *testing.T) {
	_, _, router := newTestHandlers(t, "gh-token")
	rr, body := getBinary(t, router, "/api/binaries/receiver/linux-arm64?version=v9.9.9")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%q", rr.Code, body)
	}
}

func TestBinaryStillRequiresAnAPIKey(t *testing.T) {
	_, _, router := newTestHandlers(t, "gh-token")
	req := httptest.NewRequest(http.MethodGet, "/api/binaries/receiver/linux-arm64?version=v5.3.0", nil)
	req.Header.Set("apikey", "bad-key")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestLatestVersionIsCached(t *testing.T) {
	h, hits, _ := newTestHandlers(t, "gh-token")
	for range 3 {
		v, err := h.LatestVersion(context.Background())
		if err != nil || v != "v5.4.0" {
			t.Fatalf("LatestVersion = %q, %v", v, err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("lookups = %d, want 1", n)
	}
}

func TestLatestVersionWithoutTokenIsNotConfigured(t *testing.T) {
	h, hits, _ := newTestHandlers(t, "")
	if _, err := h.LatestVersion(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("github was contacted without a token")
	}
}
