package config

import (
	"fmt"
	"os"
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
	StripeSecretKey    string
	StripePriceMonthly string
	StripePriceYearly  string
	StripeWebhookSecret string
	BaseURL            string

	// APN
	APNKeyID      string
	APNTeamID     string
	APNBundleID   string
	APNPrivateKey string
	APNProduction bool
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                    getEnv("PORT", "8080"),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		JWTSecret:               getEnv("JWT_SECRET", "change-me-in-production"),
		AdminEmail:              getEnv("ADMIN_EMAIL", "admin@rfspager.local"),
		AdminPassword:           getEnv("ADMIN_PASSWORD", "admin1234"),
		AppleKeyID:              os.Getenv("APPLE_KEY_ID"),
		AppleIssuerID:           os.Getenv("APPLE_ISSUER_ID"),
		ApplePrivateKey:         os.Getenv("APPLE_PRIVATE_KEY"),
		AppleBundleID:           getEnv("APPLE_BUNDLE_ID", "io.devlan.rfspager"),
		GooglePlayCredentials:    os.Getenv("GOOGLE_PLAY_CREDENTIALS"),
		GooglePlayPackage:        getEnv("GOOGLE_PLAY_PACKAGE", "io.devlan.rfspager"),
		GooglePubsubAudience:     os.Getenv("GOOGLE_PUBSUB_AUDIENCE"),
		GooglePubsubServiceEmail: os.Getenv("GOOGLE_PUBSUB_SERVICE_EMAIL"),
		StripeSecretKey:         os.Getenv("STRIPE_SECRET_KEY"),
		StripePriceMonthly:     os.Getenv("STRIPE_PRICE_MONTHLY"),
		StripePriceYearly:      os.Getenv("STRIPE_PRICE_YEARLY"),
		StripeWebhookSecret:    os.Getenv("STRIPE_WEBHOOK_SECRET"),
		BaseURL:                getEnv("BASE_URL", "http://localhost:5173"),
		FirebaseCredentialsFile: os.Getenv("FIREBASE_CREDENTIALS"),
		APNKeyID:                os.Getenv("APN_KEY_ID"),
		APNTeamID:               os.Getenv("APN_TEAM_ID"),
		APNBundleID:             os.Getenv("APN_BUNDLE_ID"),
		APNPrivateKey:           os.Getenv("APN_PRIVATE_KEY"),
		APNProduction:           getEnv("APN_PRODUCTION", "true") == "true",
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
