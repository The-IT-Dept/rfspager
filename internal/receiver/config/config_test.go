package config

import "testing"

// Legacy backfeed is supported again and must stay so until prod is cut over to
// the Go backend; this previously asserted the opposite. A legacy URL without
// its system uuid is still a misconfiguration and must not start silently.
func TestLoadAcceptsLegacyBackfeed(t *testing.T) {
	t.Setenv("RECEIVER_ENDPOINTS_FILE", "")
	t.Setenv("RECEIVER_API_KEY", "test-key")
	t.Setenv("RECEIVER_SERVER_URL", "https://go.example")
	t.Setenv("RECEIVER_LEGACY_URL", "https://v3.rfspager.app")
	t.Setenv("RECEIVER_LEGACY_SYSTEM_UUID", "3eceecaf-1c47-49b8-955d-50d3c8b23ee6")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("legacy backfeed rejected: %v", err)
	}
	var sawLegacy bool
	for _, e := range cfg.Endpoints {
		if e.Mode == "legacy" && e.SystemUUID != "" {
			sawLegacy = true
		}
	}
	if !sawLegacy {
		t.Fatalf("no legacy endpoint built from the environment: %+v", cfg.Endpoints)
	}
}

func TestLoadRejectsLegacyURLWithoutSystemUUID(t *testing.T) {
	t.Setenv("RECEIVER_ENDPOINTS_FILE", "")
	t.Setenv("RECEIVER_API_KEY", "test-key")
	t.Setenv("RECEIVER_LEGACY_URL", "https://v3.rfspager.app")
	t.Setenv("RECEIVER_LEGACY_SYSTEM_UUID", "")
	if _, err := Load(); err == nil {
		t.Fatal("legacy url without a system uuid was accepted")
	}
}
func TestLoadRequiresAuthenticatedTransport(t *testing.T) {
	t.Setenv("RECEIVER_API_KEY", "")
	t.Setenv("RECEIVER_LEGACY_URL", "")
	t.Setenv("RECEIVER_LEGACY_SYSTEM_UUID", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing key accepted")
	}
}
func TestLoadRejectsPlaintextRemoteServer(t *testing.T) {
	t.Setenv("RECEIVER_API_KEY", "test-key")
	t.Setenv("RECEIVER_LEGACY_URL", "")
	t.Setenv("RECEIVER_LEGACY_SYSTEM_UUID", "")
	t.Setenv("RECEIVER_SERVER_URL", "http://receiver.example.invalid")
	if _, err := Load(); err == nil {
		t.Fatal("remote plaintext transport accepted")
	}
}
