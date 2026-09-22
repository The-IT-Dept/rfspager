// Package install serves the receiver bootstrap: a shell script a system owner
// pipes to bash on their Pi, and an authenticated proxy in front of the GitHub
// release assets it downloads.
//
// The proxy exists because the repository is private. A Pi has no GitHub
// credential and should never be given one; it has a receiver API key, which it
// needs anyway. So the server holds a read-only GitHub token, and trades a valid
// receiver key for a binary.
package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// platforms we publish. Anything else is rejected rather than guessed at: a
// wrong binary on a Pi fails in ways that are tedious to diagnose remotely.
var platforms = map[string]bool{
	"linux-arm64": true,
	"linux-amd64": true,
	"linux-armv7": true,
}

// systemLookup is the subset of the database the proxy needs. Taking an
// interface keeps this package out of the db package's import graph.
type systemLookup interface {
	SystemNameForAPIKey(ctx context.Context, apiKey string) (string, error)
}

type Handlers struct {
	db       systemLookup
	ghToken  string
	ghRepo   string
	baseURL  string
	client   *http.Client
	mu       sync.Mutex
	cached   *release
	cachedAt time.Time
	cacheFor time.Duration
}

func New(db systemLookup, ghToken, ghRepo, baseURL string) *Handlers {
	return &Handlers{
		db:       db,
		ghToken:  ghToken,
		ghRepo:   ghRepo,
		baseURL:  strings.TrimRight(baseURL, "/"),
		client:   &http.Client{Timeout: 60 * time.Second},
		cacheFor: 5 * time.Minute,
	}
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// latest resolves the newest release, cached briefly. Without the cache every
// Pi boot loop would spend the server's GitHub rate limit.
//
// The repository releases as a single unit — one vX.Y.Z tag builds the server,
// web, receiver and mobile together — so /releases/latest is unambiguous and
// already excludes drafts and prereleases.
func (h *Handlers) latest(ctx context.Context) (*release, error) {
	h.mu.Lock()
	if h.cached != nil && time.Since(h.cachedAt) < h.cacheFor {
		defer h.mu.Unlock()
		return h.cached, nil
	}
	h.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", h.ghRepo), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+h.ghToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching github: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no published release yet")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("github releases/latest: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}

	h.mu.Lock()
	h.cached, h.cachedAt = &rel, time.Now()
	h.mu.Unlock()
	return &rel, nil
}

// apiKey accepts the same header spellings the rest of the receiver surface does.
func apiKey(r *http.Request) string {
	for _, k := range []string{"apikey", "apiKey", "X-Api-Key"} {
		if v := r.Header.Get(k); v != "" {
			return v
		}
	}
	if v := r.Header.Get("Authorization"); strings.HasPrefix(v, "Bearer ") {
		return strings.TrimPrefix(v, "Bearer ")
	}
	return ""
}

// Binary streams a release asset to an authenticated receiver.
// GET /api/binaries/receiver/{platform}        -> the binary
// GET /api/binaries/receiver/{platform}.sha256 -> its checksum
func (h *Handlers) Binary(w http.ResponseWriter, r *http.Request) {
	if h.ghToken == "" {
		writeErr(w, http.StatusServiceUnavailable, "binary distribution is not configured on this server")
		return
	}

	key := apiKey(r)
	if key == "" {
		writeErr(w, http.StatusUnauthorized, "apikey required")
		return
	}
	name, err := h.db.SystemNameForAPIKey(r.Context(), key)
	if err != nil {
		slog.Warn("binary proxy: unknown api key")
		writeErr(w, http.StatusUnauthorized, "unknown api key")
		return
	}

	platform := chi.URLParam(r, "platform")
	wantSum := strings.HasSuffix(platform, ".sha256")
	platform = strings.TrimSuffix(platform, ".sha256")
	if !platforms[platform] {
		writeErr(w, http.StatusNotFound, "unknown platform "+platform)
		return
	}

	rel, err := h.latest(r.Context())
	if err != nil {
		slog.Error("binary proxy: resolving latest release", "error", err)
		writeErr(w, http.StatusBadGateway, "could not resolve the latest release")
		return
	}

	want := "rfspager-receiver-" + platform
	if wantSum {
		want += ".sha256"
	}
	var assetURL string
	for _, a := range rel.Assets {
		if a.Name == want {
			assetURL = a.URL
			break
		}
	}
	if assetURL == "" {
		writeErr(w, http.StatusNotFound,
			fmt.Sprintf("release %s has no asset %q", rel.TagName, want))
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, assetURL, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "building upstream request")
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.ghToken)
	// Asset URLs return JSON metadata unless octet-stream is asked for explicitly.
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := h.client.Do(req)
	if err != nil {
		slog.Error("binary proxy: fetching asset", "error", err)
		writeErr(w, http.StatusBadGateway, "could not fetch the release asset")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeErr(w, http.StatusBadGateway, "upstream returned "+resp.Status)
		return
	}

	slog.Info("receiver binary served", "system", name, "platform", platform, "release", rel.TagName, "checksum", wantSum)

	if wantSum {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+want+`"`)
	}
	w.Header().Set("X-RFSPager-Release", rel.TagName)
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, resp.Body); err != nil {
		slog.Warn("binary proxy: copy interrupted", "error", err)
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
