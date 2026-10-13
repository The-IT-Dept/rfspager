package control

import (
	"context"
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
	"runtime"
	"strings"
	"time"
)

// Self-update.
//
// The update command downloads the published receiver binary for this
// platform from the server's authenticated binary proxy, verifies its
// sha256, swaps it into place (current -> receiver.prev, new -> current),
// writes an update marker and exits so systemd restarts the unit. On each
// boot the marker's boot count goes up; a new binary that cannot stay up
// is rolled back by renaming receiver.prev over it. The first welcome from
// the new binary clears the marker and reports the command as ok.

// Rollback policy.
const (
	MaxBootsBeforeRollback = 3
	RollbackWindow         = 10 * time.Minute
)

// UpdateMarker is <state>/update.json while an update is being confirmed.
type UpdateMarker struct {
	CommandID string    `json:"command_id"`
	From      string    `json:"from"`       // version that ran the update
	To        string    `json:"to"`         // release tag requested, e.g. "v5.5.0"
	StartedAt time.Time `json:"started_at"` // when the swap happened
	Boots     int       `json:"boots"`      // starts of the new binary so far
	// StableAt is set once the new binary has stayed up for StableAfter;
	// from then on restarts no longer count towards a rollback.
	StableAt time.Time `json:"stable_at,omitempty"`
}

// StableAfter is how long a freshly updated binary must stay up before its
// later restarts stop counting towards a rollback.
const StableAfter = 60 * time.Second

// MarkStable records that the updated binary ran long enough to be trusted.
// No-op when the marker is already gone (update confirmed) or belongs to a
// different update.
func (s *Store) MarkStable(m *UpdateMarker) error {
	if s == nil || m == nil {
		return nil
	}
	cur, err := s.LoadMarker()
	if err != nil || cur == nil || cur.CommandID != m.CommandID {
		return err
	}
	cur.StableAt = time.Now()
	return s.SaveMarker(cur)
}

// LoadMarker reads the update marker; nil when there is none.
func (s *Store) LoadMarker() (*UpdateMarker, error) {
	if !s.Enabled() {
		return nil, nil
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, markerFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m UpdateMarker
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", markerFile, err)
	}
	return &m, nil
}

// SaveMarker writes the update marker atomically.
func (s *Store) SaveMarker(m *UpdateMarker) error {
	if !s.Enabled() {
		return errNoStore
	}
	return s.writeJSON(markerFile, m)
}

// DeleteMarker removes the update marker if present.
func (s *Store) DeleteMarker() error {
	if !s.Enabled() {
		return nil
	}
	err := os.Remove(filepath.Join(s.dir, markerFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Updater performs the download-verify-swap sequence.
type Updater struct {
	ServerURL string // primary endpoint base URL
	APIKey    string
	HTTP      *http.Client
	Platform  string // e.g. linux-arm64; DetectPlatform() when empty
	// Executable is the path of the running binary (symlinks resolved).
	// Defaults to os.Executable.
	Executable string
	Store      *Store
	Version    string // running version, recorded as the marker's From
}

// DetectPlatform names the release asset for this machine the same way
// install.sh does: linux-arm64, linux-amd64 or linux-armv7.
func DetectPlatform() string {
	arch := runtime.GOARCH
	switch arch {
	case "arm":
		arch = "armv7"
	}
	return runtime.GOOS + "-" + arch
}

// ResolveExecutable returns the running binary's real path.
func ResolveExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}

// Run downloads tag, verifies it and swaps it into place. On success the
// marker is written and the caller is expected to exit 0; the receiver.new
// file is removed on any failure and the current binary is left in place.
func (u *Updater) Run(ctx context.Context, commandID, tag string, progress func(stage, msg string)) error {
	if u.Store == nil || !u.Store.Enabled() {
		return errors.New("state dir unavailable; refusing to update without a rollback marker")
	}
	exe := u.Executable
	if exe == "" {
		var err error
		if exe, err = ResolveExecutable(); err != nil {
			return fmt.Errorf("locating executable: %w", err)
		}
	}
	platform := u.Platform
	if platform == "" {
		platform = DetectPlatform()
	}
	httpc := u.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 5 * time.Minute}
	}
	dir := filepath.Dir(exe)
	newPath := filepath.Join(dir, "receiver.new")
	prevPath := filepath.Join(dir, "receiver.prev")

	progress("downloading", fmt.Sprintf("fetching %s for %s", tag, platform))
	sum, size, err := u.download(ctx, httpc, platform, tag, newPath)
	if err != nil {
		os.Remove(newPath)
		return fmt.Errorf("download: %w", err)
	}

	progress("verifying", fmt.Sprintf("%d bytes, checking sha256", size))
	want, err := u.checksum(ctx, httpc, platform, tag)
	if err != nil {
		os.Remove(newPath)
		return fmt.Errorf("checksum: %w", err)
	}
	if !strings.EqualFold(want, sum) {
		os.Remove(newPath)
		return fmt.Errorf("checksum mismatch: published %s, downloaded %s", want, sum)
	}

	progress("swapping", "installing the new binary")
	os.Remove(prevPath)
	if err := os.Rename(exe, prevPath); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("setting aside the current binary: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		// Put the old one back; nothing has changed from systemd's view.
		if rb := os.Rename(prevPath, exe); rb != nil {
			return fmt.Errorf("installing the new binary: %w (and restoring the old one failed: %v)", err, rb)
		}
		return fmt.Errorf("installing the new binary: %w", err)
	}
	marker := &UpdateMarker{CommandID: commandID, From: u.Version, To: tag, StartedAt: time.Now().UTC()}
	if err := u.Store.SaveMarker(marker); err != nil {
		// Without a marker a bad binary would not roll back, but the swap
		// is done; be loud rather than undo a verified install.
		slog.Error("update marker not written; automatic rollback disabled for this update", "err", err)
	}
	slog.Info("update installed", "from", u.Version, "to", tag, "sha256", sum, "prev", prevPath)
	progress("restarting", "exiting; systemd restarts the receiver on "+tag)
	return nil
}

func (u *Updater) binaryURL(platform, tag, suffix string) string {
	return strings.TrimRight(u.ServerURL, "/") + "/api/binaries/receiver/" + platform + suffix + "?version=" + url.QueryEscape(tag)
}

func (u *Updater) get(ctx context.Context, httpc *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", u.APIKey)
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

// download streams the asset to dst (mode 0755) and returns its sha256.
func (u *Updater) download(ctx context.Context, httpc *http.Client, platform, tag, dst string) (sum string, size int64, err error) {
	resp, err := u.get(ctx, httpc, u.binaryURL(platform, tag, ""))
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}
	if size == 0 {
		return "", 0, errors.New("empty download")
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// checksum fetches the published .sha256 and returns its hex digest (the
// first field; the file is in sha256sum format, "<hex>  <name>").
func (u *Updater) checksum(ctx context.Context, httpc *http.Client, platform, tag string) (string, error) {
	resp, err := u.get(ctx, httpc, u.binaryURL(platform, tag, ".sha256"))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", fmt.Errorf("malformed checksum file %q", strings.TrimSpace(string(raw)))
	}
	return fields[0], nil
}

// BaseVersion reduces "v5.5.0", "5.5.0-abc1234" or "v5.4.0-3-gabc-dirty"
// to "5.5.0" / "5.4.0" for comparing a running build with a release tag.
func BaseVersion(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	return v
}

// BootCheck runs before anything else at startup. With no marker it does
// nothing. A marker for another version is stale and removed. Otherwise
// the boot count goes up, and a binary that has already been started
// MaxBootsBeforeRollback times inside RollbackWindow is replaced by
// receiver.prev: rolledBack is then true and the caller must exit non-zero
// so systemd starts the restored binary. The marker is returned when the
// caller should confirm the update after the first welcome.
func BootCheck(store *Store, exe, version string) (marker *UpdateMarker, rolledBack bool, err error) {
	m, err := store.LoadMarker()
	if err != nil {
		slog.Warn("update marker unreadable; removing it", "err", err)
		return nil, false, store.DeleteMarker()
	}
	if m == nil {
		return nil, false, nil
	}
	if BaseVersion(version) != BaseVersion(m.To) || BaseVersion(m.To) == "" {
		slog.Warn("stale update marker removed", "marker_to", m.To, "running", version)
		return nil, false, store.DeleteMarker()
	}
	m.Boots++
	if m.Boots > MaxBootsBeforeRollback && m.StableAt.IsZero() && time.Since(m.StartedAt) < RollbackWindow {
		prev := filepath.Join(filepath.Dir(exe), "receiver.prev")
		if _, statErr := os.Stat(prev); statErr != nil {
			slog.Error("update crash loop detected but no receiver.prev to roll back to", "boots", m.Boots, "to", m.To, "err", statErr)
			return nil, false, store.DeleteMarker()
		}
		if err := os.Rename(prev, exe); err != nil {
			return nil, false, fmt.Errorf("rolling back to %s: %w", prev, err)
		}
		_ = store.DeleteMarker()
		slog.Error("UPDATE ROLLED BACK: the new receiver restarted too often; receiver.prev restored",
			"to", m.To, "from", m.From, "boots", m.Boots, "started_at", m.StartedAt, "command_id", m.CommandID)
		return nil, true, nil
	}
	if err := store.SaveMarker(m); err != nil {
		slog.Warn("could not update the boot count on the update marker", "err", err)
	}
	slog.Info("update pending confirmation", "to", m.To, "boots", m.Boots, "command_id", m.CommandID)
	return m, false, nil
}
