package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEndpoints(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "endpoints.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECEIVER_ENDPOINTS_FILE", p)
	return p
}

// One dongle feeding the legacy Laravel ingress and the Go backend at once is
// the whole point of the file, so that shape has to load.
func TestLoadEndpointsAcceptsMixedModes(t *testing.T) {
	writeEndpoints(t, `{"endpoints":[
		{"name":"prod-legacy","mode":"legacy","url":"https://v3.rfspager.app/","system_uuid":"3eceecaf-1c47-49b8-955d-50d3c8b23ee6"},
		{"name":"dev","mode":"ws","url":"https://go.dev.example","api_key":"KEY"}
	]}`)
	eps, err := LoadEndpoints()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("got %d endpoints, want 2", len(eps))
	}
	if eps[0].Mode != "legacy" || eps[0].SystemUUID == "" {
		t.Errorf("legacy endpoint not parsed: %+v", eps[0])
	}
	if eps[0].URL != "https://v3.rfspager.app" {
		t.Errorf("trailing slash not trimmed: %q", eps[0].URL)
	}
	if eps[1].Mode != "ws" || eps[1].APIKey != "KEY" {
		t.Errorf("ws endpoint not parsed: %+v", eps[1])
	}
}

// A misconfigured endpoint must fail at startup rather than silently drop an
// upstream — a receiver quietly feeding one backend instead of two is the
// failure this file exists to avoid.
func TestLoadEndpointsRejectsIncomplete(t *testing.T) {
	for name, body := range map[string]string{
		"ws without key":          `{"endpoints":[{"mode":"ws","url":"https://a.example"}]}`,
		"legacy without uuid":     `{"endpoints":[{"mode":"legacy","url":"https://a.example"}]}`,
		"unknown mode":            `{"endpoints":[{"mode":"carrier-pigeon","url":"https://a.example"}]}`,
		"plain http off-loopback": `{"endpoints":[{"mode":"ws","url":"http://a.example","api_key":"K"}]}`,
		"no endpoints":            `{"endpoints":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			writeEndpoints(t, body)
			if _, err := LoadEndpoints(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// An explicitly configured endpoints file that is missing must fail loudly.
// Falling back to single-endpoint mode would silently feed one backend when the
// operator asked for two, which is the exact failure this file exists to stop.
func TestLoadEndpointsFailsWhenConfiguredFileIsMissing(t *testing.T) {
	t.Setenv("RECEIVER_ENDPOINTS_FILE", filepath.Join(t.TempDir(), "absent.json"))
	if _, err := LoadEndpoints(); err == nil {
		t.Fatal("a missing endpoints file was silently ignored")
	}
}

// With the variable unset, a receiver falls back to the environment — that is
// what the installer writes and what every existing receiver runs.
func TestLoadEndpointsUnsetMeansEnvMode(t *testing.T) {
	t.Setenv("RECEIVER_ENDPOINTS_FILE", "")
	if _, err := os.Stat(defaultEndpointsFile); err == nil {
		t.Skip("host has a real " + defaultEndpointsFile)
	}
	eps, err := LoadEndpoints()
	if err != nil || eps != nil {
		t.Fatalf("want (nil, nil) for env mode, got (%v, %v)", eps, err)
	}
}
