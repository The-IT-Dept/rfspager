package config

import (
	"strings"
	"testing"
)

func cleanConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"JWT_SECRET", "ADMIN_EMAIL", "ADMIN_PASSWORD", "STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET", "STRIPE_PRICE_MONTHLY", "STRIPE_PRICE_YEARLY", "SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM", "SMTP_MODE", "LIBREDESK_URL", "LIBREDESK_API_KEY", "LIBREDESK_API_SECRET", "LIBREDESK_INBOX_ID", "LIBREDESK_AGENT_ID", "LIBREDESK_TEAM_ID", "FACEBOOK_APP_ID", "FACEBOOK_APP_SECRET", "GOOGLE_PUBSUB_AUDIENCE", "GOOGLE_PUBSUB_SERVICE_EMAIL", "TRUSTED_PROXY_CIDRS", "IAP_PRODUCT_IDS", "APPLE_IAP_ENVIRONMENT", "GOOGLE_IAP_ENVIRONMENT"} {
		t.Setenv(key, "")
	}
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("BASE_URL", "http://localhost:5173")
}

func TestLoadRejectsMissingAndExampleSecrets(t *testing.T) {
	for _, secret := range []string{"", "change-me-in-production", "your-secret-key-here", strings.Repeat("a", 32)} {
		t.Run(secret, func(t *testing.T) {
			cleanConfigEnv(t)
			t.Setenv("JWT_SECRET", secret)
			if _, err := Load(); err == nil {
				t.Fatal("accepted missing or insecure JWT secret")
			}
		})
	}
}
func TestLoadDoesNotCreateDefaultAdministrator(t *testing.T) {
	cleanConfigEnv(t)
	t.Setenv("JWT_SECRET", "test-only-jwt-secret-with-enough-variety-123456789")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminEmail != "" || cfg.AdminPassword != "" {
		t.Fatal("implicit administrator configured")
	}
}
func TestLoadRejectsPartialSecurityConfiguration(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"admin missing password", map[string]string{"ADMIN_EMAIL": "owner@example.invalid"}},
		{"admin missing email", map[string]string{"ADMIN_PASSWORD": "different-long-password"}},
		{"admin weak password", map[string]string{"ADMIN_EMAIL": "owner@example.invalid", "ADMIN_PASSWORD": "admin1234"}},
		{"unsigned stripe", map[string]string{"STRIPE_SECRET_KEY": "test-stripe-key"}},
		{"incomplete smtp", map[string]string{"SMTP_HOST": "smtp.example.invalid"}},
		{"libredesk no secret", map[string]string{"LIBREDESK_URL": "https://support.example.invalid", "LIBREDESK_API_KEY": "key", "LIBREDESK_INBOX_ID": "8", "LIBREDESK_AGENT_ID": "19"}},
		{"libredesk no inbox", map[string]string{"LIBREDESK_URL": "https://support.example.invalid", "LIBREDESK_API_KEY": "key", "LIBREDESK_API_SECRET": "secret", "LIBREDESK_AGENT_ID": "19"}},
		{"libredesk team only", map[string]string{"LIBREDESK_TEAM_ID": "4"}},
		{"libredesk bad team", map[string]string{"LIBREDESK_URL": "https://support.example.invalid", "LIBREDESK_API_KEY": "key", "LIBREDESK_API_SECRET": "secret", "LIBREDESK_INBOX_ID": "8", "LIBREDESK_AGENT_ID": "19", "LIBREDESK_TEAM_ID": "-1"}},
		{"facebook no app secret", map[string]string{"FACEBOOK_APP_ID": "test-app"}},
		{"google missing sender", map[string]string{"GOOGLE_PUBSUB_AUDIENCE": "https://example.invalid/webhook"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanConfigEnv(t)
			t.Setenv("JWT_SECRET", "test-only-jwt-secret-with-enough-variety-123456789")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatal("accepted incomplete security configuration")
			}
		})
	}
}

func TestLoadLibredeskTeamIsOptional(t *testing.T) {
	for team, want := range map[string]int{"": 0, "4": 4} {
		cleanConfigEnv(t)
		t.Setenv("JWT_SECRET", "test-only-jwt-secret-with-enough-variety-123456789")
		for k, v := range map[string]string{"LIBREDESK_URL": "https://support.example.invalid", "LIBREDESK_API_KEY": "key", "LIBREDESK_API_SECRET": "secret", "LIBREDESK_INBOX_ID": "8", "LIBREDESK_AGENT_ID": "19", "LIBREDESK_TEAM_ID": team} {
			t.Setenv(k, v)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatalf("team %q: %v", team, err)
		}
		if cfg.LibredeskTeamID != want {
			t.Fatalf("team %q: LibredeskTeamID = %d, want %d", team, cfg.LibredeskTeamID, want)
		}
	}
}
