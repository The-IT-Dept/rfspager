package config

import (
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port        string
	DatabaseURL string

	// Firebase
	FirebaseCredentialsFile string

	// Admin JWT
	JWTSecret string

	// Admin seed
	AdminEmail    string
	AdminPassword string

	// Apple IAP (App Store Server API)
	AppleKeyID      string
	AppleIssuerID   string
	ApplePrivateKey string
	AppleBundleID   string

	// Google Play IAP
	GooglePlayCredentials string
	GooglePlayPackage     string

	// Google Pub/Sub push (RTDN webhook auth)
	GooglePubsubAudience     string // Audience configured on the Pub/Sub push subscription
	GooglePubsubServiceEmail string // Optional: expected service-account email on the OIDC token

	// Stripe
	StripeSecretKey     string
	StripePriceMonthly  string
	StripePriceYearly   string
	StripeWebhookSecret string
	BaseURL             string

	// Optional password recovery delivery and social provider validation.
	SMTPHost             string
	SMTPPort             int
	SMTPUsername         string
	SMTPPassword         string
	SMTPFrom             string
	SMTPMode             string
	AdaptyWebhookAuth    string
	FacebookAppID        string
	FacebookAppSecret    string
	TrustedProxyCIDRs    []string
	IAPProductIDs        string
	AppleIAPEnvironment  string
	GoogleIAPEnvironment string

	// APN
	APNKeyID      string
	APNTeamID     string
	APNBundleID   string
	APNPrivateKey string
	APNProduction bool

	// Receiver distribution. The repository is private, so the server holds a
	// read-only GitHub token and trades a valid receiver API key for a binary.
	GitHubToken   string
	GitHubRepo    string
	PublicBaseURL string
}

func Load() (*Config, error) {
	cfg := &Config{
		SMTPHost:                 os.Getenv("SMTP_HOST"),
		SMTPUsername:             os.Getenv("SMTP_USERNAME"),
		SMTPPassword:             os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:                 os.Getenv("SMTP_FROM"),
		SMTPMode:                 getEnv("SMTP_MODE", "starttls"),
		TrustedProxyCIDRs:        parseTrustedProxyCIDRs(os.Getenv("TRUSTED_PROXY_CIDRS")),
		IAPProductIDs:            strings.Join(strings.Fields(strings.ReplaceAll(getEnv("IAP_PRODUCT_IDS", "io.devlan.rfspager.monthly io.devlan.rfspager.yearly"), ",", " ")), " "),
		AppleIAPEnvironment:      getEnv("APPLE_IAP_ENVIRONMENT", "Production"),
		GoogleIAPEnvironment:     getEnv("GOOGLE_IAP_ENVIRONMENT", "Production"),
		AdaptyWebhookAuth:        os.Getenv("ADAPTY_WEBHOOK_AUTH"),
		FacebookAppID:            os.Getenv("FACEBOOK_APP_ID"),
		FacebookAppSecret:        os.Getenv("FACEBOOK_APP_SECRET"),
		Port:                     getEnv("PORT", "8080"),
		DatabaseURL:              os.Getenv("DATABASE_URL"),
		JWTSecret:                os.Getenv("JWT_SECRET"),
		AdminEmail:               os.Getenv("ADMIN_EMAIL"),
		AdminPassword:            os.Getenv("ADMIN_PASSWORD"),
		AppleKeyID:               os.Getenv("APPLE_KEY_ID"),
		AppleIssuerID:            os.Getenv("APPLE_ISSUER_ID"),
		ApplePrivateKey:          os.Getenv("APPLE_PRIVATE_KEY"),
		AppleBundleID:            getEnv("APPLE_BUNDLE_ID", "io.devlan.rfspager"),
		GooglePlayCredentials:    os.Getenv("GOOGLE_PLAY_CREDENTIALS"),
		GooglePlayPackage:        getEnv("GOOGLE_PLAY_PACKAGE", "io.devlan.rfspager"),
		GooglePubsubAudience:     os.Getenv("GOOGLE_PUBSUB_AUDIENCE"),
		GooglePubsubServiceEmail: os.Getenv("GOOGLE_PUBSUB_SERVICE_EMAIL"),
		StripeSecretKey:          os.Getenv("STRIPE_SECRET_KEY"),
		StripePriceMonthly:       os.Getenv("STRIPE_PRICE_MONTHLY"),
		StripePriceYearly:        os.Getenv("STRIPE_PRICE_YEARLY"),
		StripeWebhookSecret:      os.Getenv("STRIPE_WEBHOOK_SECRET"),
		BaseURL:                  getEnv("BASE_URL", "http://localhost:5173"),
		FirebaseCredentialsFile:  os.Getenv("FIREBASE_CREDENTIALS"),
		APNKeyID:                 os.Getenv("APN_KEY_ID"),
		APNTeamID:                os.Getenv("APN_TEAM_ID"),
		APNBundleID:              os.Getenv("APN_BUNDLE_ID"),
		APNPrivateKey:            os.Getenv("APN_PRIVATE_KEY"),
		APNProduction:            getEnv("APN_PRODUCTION", "true") == "true",
		GitHubToken:              os.Getenv("GITHUB_TOKEN"),
		GitHubRepo:               getEnv("GITHUB_REPO", "the-it-dept/rfspager"),
		PublicBaseURL:            os.Getenv("PUBLIC_BASE_URL"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	if len(cfg.JWTSecret) < 32 || len(uniqueCharacters(cfg.JWTSecret)) < 8 || strings.Contains(strings.ToLower(cfg.JWTSecret), "change-me") || strings.Contains(strings.ToLower(cfg.JWTSecret), "your-secret") {
		return nil, fmt.Errorf("JWT_SECRET must be an independently generated random secret of at least 32 characters (for example: openssl rand -hex 32)")
	}
	if (cfg.AdminEmail == "") != (cfg.AdminPassword == "") {
		return nil, fmt.Errorf("ADMIN_EMAIL and ADMIN_PASSWORD must both be set for explicit bootstrap, or both omitted")
	}
	if cfg.AdminEmail != "" {
		address, err := mail.ParseAddress(cfg.AdminEmail)
		if err != nil || address.Address != cfg.AdminEmail {
			return nil, fmt.Errorf("ADMIN_EMAIL must be an email address")
		}
		if len(cfg.AdminPassword) < 12 || len(cfg.AdminPassword) > 72 {
			return nil, fmt.Errorf("ADMIN_PASSWORD must be 12 to 72 bytes")
		}
	}
	if cfg.StripeSecretKey != "" && (cfg.StripeWebhookSecret == "" || cfg.StripePriceMonthly == "" || cfg.StripePriceYearly == "") {
		return nil, fmt.Errorf("Stripe requires STRIPE_WEBHOOK_SECRET, STRIPE_PRICE_MONTHLY and STRIPE_PRICE_YEARLY")
	}
	if (cfg.FacebookAppID == "") != (cfg.FacebookAppSecret == "") {
		return nil, fmt.Errorf("FACEBOOK_APP_ID and FACEBOOK_APP_SECRET must both be set, or both omitted")
	}
	if cfg.GooglePubsubAudience != "" && cfg.GooglePubsubServiceEmail == "" {
		return nil, fmt.Errorf("GOOGLE_PUBSUB_SERVICE_EMAIL is required when Google RTDN is enabled")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname()))) {
		return nil, fmt.Errorf("BASE_URL must be an HTTPS URL (HTTP is allowed only on loopback for local development)")
	}
	if cfg.SMTPHost != "" || cfg.SMTPFrom != "" || cfg.SMTPUsername != "" || cfg.SMTPPassword != "" {
		if cfg.SMTPHost == "" || cfg.SMTPFrom == "" {
			return nil, fmt.Errorf("SMTP_HOST and SMTP_FROM are required for password recovery")
		}
		if _, err := mail.ParseAddress(cfg.SMTPFrom); err != nil || strings.ContainsAny(cfg.SMTPFrom, "\r\n") {
			return nil, fmt.Errorf("SMTP_FROM must be a valid email address")
		}
		if (cfg.SMTPUsername == "") != (cfg.SMTPPassword == "") {
			return nil, fmt.Errorf("SMTP_USERNAME and SMTP_PASSWORD must both be set, or both omitted")
		}
		if cfg.SMTPMode != "starttls" && cfg.SMTPMode != "tls" && !(cfg.SMTPMode == "plain" && isLoopback(cfg.SMTPHost)) {
			return nil, fmt.Errorf("SMTP_MODE must be starttls or tls; plain is only permitted on loopback")
		}
		defaultPort := "587"
		if cfg.SMTPMode == "tls" {
			defaultPort = "465"
		}
		cfg.SMTPPort, err = strconv.Atoi(getEnv("SMTP_PORT", defaultPort))
		if err != nil || cfg.SMTPPort < 1 || cfg.SMTPPort > 65535 {
			return nil, fmt.Errorf("invalid SMTP_PORT")
		}
	}
	for _, cidr := range cfg.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, fmt.Errorf("invalid TRUSTED_PROXY_CIDRS entry")
		}
	}
	if cfg.IAPProductIDs == "" {
		return nil, fmt.Errorf("IAP_PRODUCT_IDS is required")
	}
	if cfg.AppleIAPEnvironment != "Production" && cfg.AppleIAPEnvironment != "Sandbox" {
		return nil, fmt.Errorf("APPLE_IAP_ENVIRONMENT must be Production or Sandbox")
	}
	if cfg.GoogleIAPEnvironment != "Production" && cfg.GoogleIAPEnvironment != "Test" {
		return nil, fmt.Errorf("GOOGLE_IAP_ENVIRONMENT must be Production or Test")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseTrustedProxyCIDRs reads TRUSTED_PROXY_CIDRS, dropping empty entries so a
// trailing or doubled comma cannot make an empty string look like an entry.
func parseTrustedProxyCIDRs(raw string) []string {
	out := make([]string, 0, 4)
	for _, entry := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		out = append(out, entry)
	}
	return out
}

func uniqueCharacters(s string) map[rune]struct{} {
	out := make(map[rune]struct{})
	for _, c := range s {
		out[c] = struct{}{}
	}
	return out
}
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
