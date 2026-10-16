// Package config loads the receiver agent's runtime config from the environment.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// DefaultStateDir is where the receiver keeps what it learns at runtime:
// the cached server config and the self-update marker. Override with
// RECEIVER_STATE_DIR (tests and dev checkouts).
const DefaultStateDir = "/var/lib/rfspager"

// StateDir returns the runtime state directory.
func StateDir() string {
	if v := strings.TrimSpace(os.Getenv("RECEIVER_STATE_DIR")); v != "" {
		return v
	}
	return DefaultStateDir
}

// DefaultUIAddr is where the local web UI listens (148 for the band).
const DefaultUIAddr = ":8148"

// UIAddr returns the local UI listen address from RECEIVER_UI_ADDR, or ""
// when it is set to "off" (or "0", "false", "disabled").
func UIAddr() string {
	v := strings.TrimSpace(os.Getenv("RECEIVER_UI_ADDR"))
	switch strings.ToLower(v) {
	case "":
		return DefaultUIAddr
	case "off", "0", "false", "disabled", "none":
		return ""
	}
	if !strings.Contains(v, ":") {
		v = ":" + v // a bare port
	}
	return v
}

// RuntimeBootstrap maps the tuning environment variables (RECEIVER_TUNER_GAIN
// and friends) onto the shared RuntimeConfig, over the defaults. It is only
// honoured when there is neither a server revision nor a cached one; see
// control.Startup.
func (c *Config) RuntimeBootstrap() rxproto.RuntimeConfig {
	rt := rxproto.Defaults()
	rt.TunerGainTenthsDB = c.TunerGain
	rt.PPM = c.CorrectionPPM
	if c.SampleRate != 0 {
		rt.SampleRate = c.SampleRate
	}
	rt.DongleIndex = c.DongleIndex
	rt.DongleSerial = c.DongleSerial
	rt.PostAll = c.PostAll
	return rt
}

type Config struct {
	// ServerURL is the rfspager base URL, e.g. https://rfspager.example.com
	ServerURL string

	// APIKey identifies this receiver against the rfspager server. Every
	// decoded page is posted under this key — frequency is no longer a
	// per-system property; the receiver owns the freq list.
	APIKey string

	// SampleRate is the RTL-SDR ADC rate. 1.92 MS/s is a clean rate that
	// divides nicely down to 38.4 kHz per channel (POCSAG baud-rate friendly).
	SampleRate uint32

	// TunerGain in tenths of a dB. 0 means use automatic gain.
	TunerGain int

	// CorrectionPPM is the dongle's frequency error correction.
	CorrectionPPM int

	// DongleIndex pins to a specific dongle by enumeration index. -1 means
	// "no pin, use index 0". Indices renumber when you re-plug dongles, so
	// prefer DongleSerial for anything you want stable across reboots.
	DongleIndex int

	// DongleSerial pins to a specific dongle by USB serial number (case-
	// insensitive substring match). Takes precedence over DongleIndex when
	// set. Examples: "RFS00005", "00000001".
	DongleSerial string

	// PostAll bypasses the receiver-side shouldPost content filter so
	// every decoded page (including beacons / numeric / noise-shaped
	// alpha) gets sent upstream. Useful for end-to-end pipeline tests
	// where you want guaranteed traffic without waiting for a real
	// dispatch.
	PostAll bool

	// Endpoints is the fan-out set. One receiver can feed several backends at
	// once — typically prod on the legacy Laravel ingress while the Go stack is
	// proven on dev, so a single dongle serves both during the cutover rather
	// than needing a second Pi on the same antenna.
	//
	// Empty means single-endpoint mode built from the environment, which is
	// what the installer writes and what every existing receiver runs.
	Endpoints []Endpoint
}

// Endpoint is one upstream. Mode decides the transport:
//
//	ws      authenticated websocket to the Go backend, keyed by api_key
//	legacy  plain POST to {url}/api/messages/{system_uuid} on Laravel
type Endpoint struct {
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	URL        string `json:"url"`
	APIKey     string `json:"api_key,omitempty"`
	SystemUUID string `json:"system_uuid,omitempty"`
}

type endpointsFile struct {
	Endpoints []Endpoint `json:"endpoints"`
}

// LoadEndpoints reads the fan-out config. Returns nil when no file is
// configured, leaving the caller in single-endpoint mode.
func LoadEndpoints() ([]Endpoint, error) {
	path := strings.TrimSpace(os.Getenv("RECEIVER_ENDPOINTS_FILE"))
	if path == "" {
		path = defaultEndpointsFile
		if _, err := os.Stat(path); err != nil {
			return nil, nil // not configured; env-var mode
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var f endpointsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(f.Endpoints) == 0 {
		return nil, fmt.Errorf("%s defines no endpoints", path)
	}
	for i := range f.Endpoints {
		e := &f.Endpoints[i]
		e.Mode = strings.ToLower(strings.TrimSpace(e.Mode))
		e.URL = strings.TrimRight(strings.TrimSpace(e.URL), "/")
		if e.Name == "" {
			e.Name = fmt.Sprintf("endpoint-%d", i+1)
		}
		if err := validateEndpoint(*e); err != nil {
			return nil, fmt.Errorf("%s: endpoint %q: %w", path, e.Name, err)
		}
	}
	return f.Endpoints, nil
}

const defaultEndpointsFile = "/etc/rfspager/endpoints.json"

func validateEndpoint(e Endpoint) error {
	if err := validateServerURL(e.URL); err != nil {
		return err
	}
	switch e.Mode {
	case "ws":
		if strings.TrimSpace(e.APIKey) == "" {
			return fmt.Errorf("mode ws needs an api_key")
		}
	case "legacy":
		if strings.TrimSpace(e.SystemUUID) == "" {
			return fmt.Errorf("mode legacy needs a system_uuid")
		}
	default:
		return fmt.Errorf("unknown mode %q (want ws or legacy)", e.Mode)
	}
	return nil
}

// validateServerURL keeps the same rule for every upstream: HTTPS, except on
// loopback where a plain-HTTP dev server is allowed.
func validateServerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid url %q", raw)
	}
	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return fmt.Errorf("must use HTTPS; HTTP is allowed only on loopback")
	}
	return nil
}

func Load() (*Config, error) {
	cfg := &Config{
		ServerURL:     getEnv("RECEIVER_SERVER_URL", "http://localhost:8080"),
		APIKey:        strings.Trim(strings.TrimSpace(os.Getenv("RECEIVER_API_KEY")), `"'`),
		SampleRate:    uint32(getInt("RECEIVER_SAMPLE_RATE", 1_920_000)),
		TunerGain:     getInt("RECEIVER_TUNER_GAIN", 0),
		CorrectionPPM: getInt("RECEIVER_PPM", 0),
		DongleIndex:   getInt("RECEIVER_DONGLE_INDEX", -1),
		DongleSerial:  strings.TrimSpace(os.Getenv("RECEIVER_DONGLE_SERIAL")),
		PostAll:       os.Getenv("RECEIVER_POST_ALL") == "1",
	}
	eps, err := LoadEndpoints()
	if err != nil {
		return nil, err
	}
	cfg.Endpoints = eps

	// A single legacy backfeed can still be configured purely from the
	// environment, which is how the Laravel-era receivers are set up. It stays
	// until prod is cut over to the Go backend.
	//
	// The file wins outright when present. Appending the environment's legacy
	// target on top would double-post to a backend the file already lists —
	// which is exactly the situation on a receiver being migrated from env-only
	// legacy to a fan-out file.
	if legacyURL := strings.TrimSpace(os.Getenv("RECEIVER_LEGACY_URL")); legacyURL != "" && len(cfg.Endpoints) == 0 {
		uuid := strings.TrimSpace(os.Getenv("RECEIVER_LEGACY_SYSTEM_UUID"))
		if uuid == "" {
			return nil, fmt.Errorf("RECEIVER_LEGACY_URL is set but RECEIVER_LEGACY_SYSTEM_UUID is not")
		}
		cfg.Endpoints = append(cfg.Endpoints, Endpoint{
			Name:       "legacy",
			Mode:       "legacy",
			URL:        strings.TrimRight(legacyURL, "/"),
			SystemUUID: uuid,
		})
	}

	if len(cfg.Endpoints) > 0 {
		// Fan-out config is authoritative; the single-endpoint env vars below
		// are only a fallback for receivers that have neither.
		return cfg, nil
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("RECEIVER_API_KEY is required (or configure %s)", defaultEndpointsFile)
	}
	if err := validateServerURL(cfg.ServerURL); err != nil {
		return nil, fmt.Errorf("RECEIVER_SERVER_URL %w", err)
	}
	cfg.Endpoints = []Endpoint{{
		Name:   "default",
		Mode:   "ws",
		URL:    strings.TrimRight(cfg.ServerURL, "/"),
		APIKey: cfg.APIKey,
	}}
	return cfg, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
