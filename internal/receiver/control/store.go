// Package control is the receiver's management plane: it owns the running
// RuntimeConfig, applies revisions the server pushes (live where it can,
// by restarting the dongle loop otherwise), persists them, and executes
// the server's typed commands (restart, dongle_reset, update, capture).
package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// File names under the state dir.
const (
	configFile = "config.json" // cached server config: {version, config, saved_at}
	markerFile = "update.json" // self-update in progress: see UpdateMarker
)

// Store persists what the receiver learns at runtime under the state dir
// (/var/lib/rfspager by default). Every method fails soft when the
// directory could not be created: the receiver then runs without
// persistence rather than not at all.
type Store struct {
	dir string
}

// NewStore creates dir (0750) if needed. On failure it logs and returns a
// store with persistence disabled.
func NewStore(dir string) *Store {
	if dir == "" {
		return &Store{}
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		slog.Warn("state dir unavailable; running without persistence", "dir", dir, "err", err)
		return &Store{}
	}
	return &Store{dir: dir}
}

// Enabled reports whether the store has a usable directory.
func (s *Store) Enabled() bool { return s != nil && s.dir != "" }

// Dir is the state directory, or "" when persistence is disabled.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

var errNoStore = errors.New("state dir unavailable")

type cachedConfig struct {
	Version int                   `json:"version"`
	Config  rxproto.RuntimeConfig `json:"config"`
	SavedAt time.Time             `json:"saved_at"`
}

// LoadConfig reads the cached server config. ok is false when there is no
// cache, it cannot be parsed, or it does not validate.
func (s *Store) LoadConfig() (version int, cfg rxproto.RuntimeConfig, ok bool) {
	if !s.Enabled() {
		return 0, rxproto.RuntimeConfig{}, false
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, configFile))
	if err != nil {
		return 0, rxproto.RuntimeConfig{}, false
	}
	c := cachedConfig{Config: rxproto.Defaults()}
	if err := json.Unmarshal(raw, &c); err != nil {
		slog.Warn("cached config unreadable; ignoring", "err", err)
		return 0, rxproto.RuntimeConfig{}, false
	}
	if errs := c.Config.Validate(); errs != nil {
		slog.Warn("cached config invalid; ignoring", "problems", errs)
		return 0, rxproto.RuntimeConfig{}, false
	}
	return c.Version, c.Config, true
}

// SaveConfig writes the cache atomically (temp file + rename).
func (s *Store) SaveConfig(version int, cfg rxproto.RuntimeConfig) error {
	if !s.Enabled() {
		return errNoStore
	}
	return s.writeJSON(configFile, cachedConfig{Version: version, Config: cfg, SavedAt: time.Now().UTC()})
}

func (s *Store) writeJSON(name string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(s.dir, name)
	tmp, err := os.CreateTemp(s.dir, name+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o640); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// Startup chooses the config to run with, in precedence order: the cached
// server revision if present and valid, else the env bootstrap at version
// 0, else (when the env values do not validate) the built-in defaults.
// source names the winner for the log; problems lists why the env
// bootstrap was rejected, if it was.
func Startup(store *Store, env rxproto.RuntimeConfig) (version int, cfg rxproto.RuntimeConfig, source string, problems map[string]string) {
	if v, c, ok := store.LoadConfig(); ok {
		return v, c, fmt.Sprintf("cache (%s)", filepath.Join(store.Dir(), configFile)), nil
	}
	if errs := env.Validate(); errs != nil {
		return 0, rxproto.Defaults(), "defaults", errs
	}
	return 0, env, "env", nil
}
