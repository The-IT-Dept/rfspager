package config

import (
	"strings"
	"testing"
)

func cleanConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"JWT_SECRET", "ADMIN_EMAIL", "ADMIN_PASSWORD", "STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET", "STRIPE_PRICE_MONTHLY", "STRIPE_PRICE_YEARLY", "SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM", "SMTP_MODE", "FACEBOOK_APP_ID", "FACEBOOK_APP_SECRET", "GOOGLE_PUBSUB_AUDIENCE", "GOOGLE_PUBSUB_SERVICE_EMAIL", "TRUSTED_PROXY_CIDRS", "IAP_PRODUCT_IDS", "APPLE_IAP_ENVIRONMENT", "GOOGLE_IAP_ENVIRONMENT"} {
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
