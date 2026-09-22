package config

import "testing"

func TestLoadRejectsLegacyConfiguration(t *testing.T) {
	t.Setenv("RECEIVER_API_KEY", "test-key")
	t.Setenv("RECEIVER_LEGACY_URL", "http://localhost:8080")
	t.Setenv("RECEIVER_LEGACY_SYSTEM_UUID", "test-system")
	if _, err := Load(); err == nil {
		t.Fatal("legacy unauthenticated transport still accepted")
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
