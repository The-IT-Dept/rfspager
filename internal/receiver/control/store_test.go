package control

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/the-it-dept/rfspager/internal/receiver/config"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

func envBootstrap(t *testing.T) rxproto.RuntimeConfig {
	t.Helper()
	t.Setenv("RECEIVER_API_KEY", "k")
	t.Setenv("RECEIVER_SERVER_URL", "https://go.example")
	t.Setenv("RECEIVER_ENDPOINTS_FILE", "")
	t.Setenv("RECEIVER_LEGACY_URL", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.RuntimeBootstrap()
}

// Precedence: server revision (cache) over env bootstrap over defaults.
func TestStartupPrecedence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	t.Setenv("RECEIVER_STATE_DIR", dir)
	t.Setenv("RECEIVER_TUNER_GAIN", "496")
	t.Setenv("RECEIVER_PPM", "-3")
	t.Setenv("RECEIVER_DONGLE_SERIAL", "RFS00005")
	t.Setenv("RECEIVER_POST_ALL", "1")

	if got := config.StateDir(); got != dir {
		t.Fatalf("StateDir %q, want %q", got, dir)
	}
	store := NewStore(config.StateDir())
	if !store.Enabled() {
		t.Fatal("store should have created the dir")
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o750 {
		t.Fatalf("state dir mode: %v %v", fi, err)
	}

	// No cache: the env bootstrap wins, at version 0.
	env := envBootstrap(t)
	v, cfg, source, problems := Startup(store, env)
	if v != 0 || source != "env" || problems != nil {
		t.Fatalf("no cache: v=%d source=%s problems=%v", v, source, problems)
	}
	if cfg.TunerGainTenthsDB != 496 || cfg.PPM != -3 || cfg.DongleSerial != "RFS00005" || !cfg.PostAll {
		t.Fatalf("env values not honoured: %+v", cfg)
	}
	if cfg.SampleRate != 1_920_000 || len(cfg.Channels) != 2 || cfg.LogLevel != "info" {
		t.Fatalf("env bootstrap should sit over the defaults: %+v", cfg)
	}

	// A server revision is cached: it beats the env on the next start.
	server := rxproto.Defaults()
	server.TunerGainTenthsDB = 297
	server.Channels = append(server.Channels, rxproto.ChannelConfig{Label: "TEST", FreqHz: 148_700_000, Enabled: false})
	if err := store.SaveConfig(7, server); err != nil {
		t.Fatal(err)
	}
	v, cfg, source, _ = Startup(NewStore(config.StateDir()), env)
	if v != 7 || cfg.TunerGainTenthsDB != 297 || len(cfg.Channels) != 3 || cfg.PPM != 0 {
		t.Fatalf("cache should win: v=%d cfg=%+v", v, cfg)
	}
	if source == "env" {
		t.Fatalf("source %q", source)
	}

	// A corrupt cache is ignored in favour of the env.
	if err := os.WriteFile(filepath.Join(dir, configFile), []byte("{not json"), 0o640); err != nil {
		t.Fatal(err)
	}
	if v, cfg, source, _ = Startup(store, env); v != 0 || source != "env" || cfg.TunerGainTenthsDB != 496 {
		t.Fatalf("corrupt cache: v=%d source=%s", v, source)
	}

	// An invalid cache (fails validation) is ignored too.
	bad := rxproto.Defaults()
	bad.SampleRate = 1234
	if err := store.SaveConfig(8, bad); err != nil {
		t.Fatal(err)
	}
	if v, _, source, _ = Startup(store, env); v != 0 || source != "env" {
		t.Fatalf("invalid cache: v=%d source=%s", v, source)
	}

	// An invalid env bootstrap falls back to the defaults, with the
	// problems reported.
	_ = store.DeleteMarker()
	os.Remove(filepath.Join(dir, configFile))
	env.SampleRate = 2_048_000 // not a multiple of the audio rate
	v, cfg, source, problems = Startup(store, env)
	if v != 0 || source != "defaults" || problems["sample_rate"] == "" || cfg.SampleRate != 1_920_000 {
		t.Fatalf("invalid env: v=%d source=%s problems=%v cfg=%+v", v, source, problems, cfg)
	}
}

func TestCachedConfigNormalisesMissingKeys(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := os.WriteFile(filepath.Join(dir, configFile), []byte(`{"version":2,"config":{"ppm":5}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	v, cfg, ok := store.LoadConfig()
	if !ok || v != 2 || cfg.PPM != 5 || cfg.SampleRate != 1_920_000 || len(cfg.Channels) != 2 {
		t.Fatalf("v=%d ok=%v cfg=%+v", v, ok, cfg)
	}
}

func TestStoreFailsSoft(t *testing.T) {
	// A file where the dir should be: MkdirAll fails.
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(blocked)
	if store.Enabled() {
		t.Fatal("store should be disabled")
	}
	if _, _, ok := store.LoadConfig(); ok {
		t.Fatal("disabled store returned a config")
	}
	if err := store.SaveConfig(1, rxproto.Defaults()); err == nil {
		t.Fatal("save on a disabled store should error")
	}
	if m, err := store.LoadMarker(); m != nil || err != nil {
		t.Fatalf("marker on a disabled store: %v %v", m, err)
	}
	if v, _, source, _ := Startup(store, rxproto.Defaults()); v != 0 || source != "env" {
		t.Fatalf("startup with a disabled store: v=%d source=%s", v, source)
	}
}
