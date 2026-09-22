package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/the-it-dept/rfspager/internal/admin"
	"github.com/the-it-dept/rfspager/internal/config"
	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/dispatch"
	"github.com/the-it-dept/rfspager/internal/handler"
	"github.com/the-it-dept/rfspager/internal/iap"
	"github.com/the-it-dept/rfspager/internal/importer"
	resetmail "github.com/the-it-dept/rfspager/internal/mail"
	"github.com/the-it-dept/rfspager/internal/middleware"
	"github.com/the-it-dept/rfspager/internal/mobile"
	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/notify"
	"github.com/the-it-dept/rfspager/internal/public"
	"github.com/the-it-dept/rfspager/internal/recvhub"
	stripehandler "github.com/the-it-dept/rfspager/internal/stripe"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Database
	database, err := db.New(cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := database.Ping(context.Background()); err != nil {
		slog.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to database")

	// Run migrations
	if err := database.Migrate(context.Background()); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Seed default admin user
	adminStore := admin.NewStore(database.Conn())
	if cfg.AdminEmail != "" {
		if err := adminStore.SeedAdmin(context.Background(), cfg.AdminEmail, cfg.AdminPassword); err != nil {
			slog.Error("failed to bootstrap administrator", "error", err)
			os.Exit(1)
		}
	}

	// Push notification clients
	ctx := context.Background()
	fcmClient, err := notify.NewFCM(ctx, cfg.FirebaseCredentialsFile)
	if err != nil {
		slog.Error("failed to initialize FCM", "error", err)
	}

	apnClient, err := notify.NewAPN(cfg.APNKeyID, cfg.APNTeamID, cfg.APNBundleID, cfg.APNPrivateKey, cfg.APNProduction)
	if err != nil {
		slog.Error("failed to initialize APN", "error", err)
	}

	notifier := notify.New(database, fcmClient, apnClient)

	// IAP clients
	appleIAP, err := iap.NewAppleClient(cfg.AppleKeyID, cfg.AppleIssuerID, cfg.ApplePrivateKey, cfg.AppleBundleID, true)
	if err != nil {
		slog.Error("failed to initialize Apple IAP", "error", err)
	}

	googleIAP, err := iap.NewGoogleClient(ctx, cfg.GooglePlayCredentials, cfg.GooglePlayPackage)
	if err != nil {
		slog.Error("failed to initialize Google IAP", "error", err)
	}

	iapHandler := iap.NewHandler(database.Conn(), appleIAP, googleIAP, iap.Options{AllowedProductIDs: strings.Fields(cfg.IAPProductIDs), AppleEnvironment: cfg.AppleIAPEnvironment, GoogleEnvironment: cfg.GoogleIAPEnvironment})
	webhookHandler := iap.NewWebhookHandler(database.Conn(), appleIAP, googleIAP, iap.WebhookConfig{
		AppleBundleID:      cfg.AppleBundleID,
		GooglePackageName:  cfg.GooglePlayPackage,
		PubsubAudience:     cfg.GooglePubsubAudience,
		PubsubServiceEmail: cfg.GooglePubsubServiceEmail,
	}, iap.Options{AllowedProductIDs: strings.Fields(cfg.IAPProductIDs), AppleEnvironment: cfg.AppleIAPEnvironment, GoogleEnvironment: cfg.GoogleIAPEnvironment})

	// Stripe
	stripeHandler := stripehandler.NewHandler(database.Conn(),
		cfg.StripeSecretKey, cfg.StripePriceMonthly, cfg.StripePriceYearly,
		cfg.StripeWebhookSecret, cfg.BaseURL)

	// Optional password recovery delivery. Missing SMTP keeps recovery unavailable
	// rather than claiming to send mail.
	var resetMailer mobile.PasswordResetMailer
	if cfg.SMTPHost != "" {
		resetMailer, err = resetmail.NewSMTPPasswordResetMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom, cfg.SMTPMode)
		if err != nil {
			slog.Error("invalid SMTP configuration", "error", err)
			os.Exit(1)
		}
	}
	authOptions := mobile.AuthOptions{ResetMailer: resetMailer, ResetURL: strings.TrimRight(cfg.BaseURL, "/") + "/reset-password", FacebookAppID: cfg.FacebookAppID, FacebookAppSecret: cfg.FacebookAppSecret}

	// Handlers
	msgHandler := handler.NewMessageHandler(database)

	// LISTEN/NOTIFY pipeline. One dedicated connection per process; pq
	// reconnects automatically. Workers receive wake-ups on the channels we
	// subscribe to, then claim work via SKIP LOCKED.
	listener, err := db.NewListener(cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to start pg listener", "error", err)
		os.Exit(1)
	}
	defer listener.Close(context.Background())

	// One Listen() per consumer — the listener fans the same NOTIFY out to
	// every subscriber on that channel, so fan-out, mobile SSE and public SSE
	// all wake on the same rfs_message event.
	fanoutMessageCh, err := listener.Listen("rfs_message", 256)
	if err != nil {
		slog.Error("failed to listen rfs_message (fanout)", "error", err)
		os.Exit(1)
	}
	mobileSSEMessageCh, err := listener.Listen("rfs_message", 256)
	if err != nil {
		slog.Error("failed to listen rfs_message (mobile sse)", "error", err)
		os.Exit(1)
	}
	publicSSEMessageCh, err := listener.Listen("rfs_message", 256)
	if err != nil {
		slog.Error("failed to listen rfs_message (public sse)", "error", err)
		os.Exit(1)
	}
	dispatchCh, err := listener.Listen("rfs_dispatch", 16)
	if err != nil {
		slog.Error("failed to listen rfs_dispatch", "error", err)
		os.Exit(1)
	}

	fanoutWorker := dispatch.NewFanoutWorker(database.Conn(), fanoutMessageCh)
	dispatchWorker := dispatch.NewDispatchWorker(database.Conn(), notifier, dispatchCh)
	mobileSSE := mobile.NewSSEBroker(database.Conn())
	publicSSE := public.NewSSEBroker(database.Conn())

	// Receiver-agent websocket hub. PodID identifies this process in the
	// system_connections table so multi-pod takeovers Just Work.
	recvhub.PodID = podID()
	hub, err := recvhub.New(database, listener)
	if hub != nil {
		// Watchdog uses the notifier to push owners on offline/online
		// transitions. Optional — when nil the watchdog still records
		// events, it just skips the push.
		hub.SetNotifier(notifier)
	}
	if err != nil {
		slog.Error("failed to start receiver hub", "error", err)
		os.Exit(1)
	}

	bgCtx, cancelBg := context.WithCancel(context.Background())
	var bg sync.WaitGroup
	bg.Add(4)
	go func() { defer bg.Done(); fanoutWorker.Run(bgCtx) }()
	go func() { defer bg.Done(); dispatchWorker.Run(bgCtx) }()
	go func() { defer bg.Done(); mobileSSE.Run(bgCtx, mobileSSEMessageCh) }()
	go func() { defer bg.Done(); publicSSE.Run(bgCtx, publicSSEMessageCh) }()
	if err := hub.Start(bgCtx); err != nil {
		slog.Error("failed to start receiver hub workers", "error", err)
		os.Exit(1)
	}

	// Router
	r := chi.NewRouter()
	r.Use(middleware.RequestLog(slog.Default()))
	r.Use(chimw.Recoverer)
	r.Use(chimw.RequestID)
	r.Use(middleware.LimitBodies(1 << 20))
	authLimit, err := middleware.AuthRateLimit(30, time.Minute, cfg.TrustedProxyCIDRs)
	if err != nil {
		slog.Error("invalid proxy configuration", "error", err)
		os.Exit(1)
	}
	r.Use(authLimit)
	// NOTE: chimw.Timeout is applied per-group below — streaming routes
	// (websocket, SSE) deliberately stay off it because it cancels
	// r.Context() after 30s and would kill long-lived connections.

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})

	r.Route("/api", func(r chi.Router) {
		// --- Streaming endpoints (NO chi.Timeout) ---
		// chi.Timeout cancels r.Context() after 30s, which would kill any
		// long-lived connection. Websocket hijacks the conn so server
		// timeouts don't apply; the SSE handler clears its own write
		// deadline via http.NewResponseController internally.
		r.Group(func(r chi.Router) {
			r.Get("/receiver/ws", recvhub.Handler(database, hub, func(ctx context.Context, sys *models.System, p recvhub.PagePayload) (string, error) {
				return msgHandler.IngestPage(ctx, sys, p.Address, p.Message, time.Unix(p.Datetime, 0))
			}))
		})

		// --- Everything else with a 30s per-request timeout ---
		r.Group(func(r chi.Router) {
			r.Use(chimw.Timeout(30 * time.Second))

			handler.RegisterIngress(r, msgHandler)

			// Admin API
			r.Mount("/v1/admin", admin.Router(database.Conn(), cfg.JWTSecret, notifier))

			// Mobile API
			r.Mount("/v2/mobile", mobile.Router(database.Conn(), cfg.JWTSecret, cfg.AppleBundleID, mobileSSE, iapHandler, hub, authOptions))

			// Public API (no auth)
			publicHandlers := public.NewHandlers(database.Conn())
			r.Route("/public", func(r chi.Router) {
				r.Get("/stats", publicHandlers.Stats)
				r.Get("/messages", publicHandlers.ListMessages)
				r.Get("/messages/map", publicHandlers.MapMessages)
				r.Get("/agencies", publicHandlers.ListAgencies)
				r.Get("/major-incidents", publicHandlers.MajorIncidents)
				r.Get("/systems", publicHandlers.ListSystems)
				r.Get("/stations", publicHandlers.Stations)
				r.Get("/capcodes", publicHandlers.ListCapcodes)
				r.Get("/capcodes/unknown", publicHandlers.UnknownCapcodes)
				r.Get("/capcodes/messages", publicHandlers.CapcodeMessages)
				r.Get("/systems/{uuid}/uptime", publicHandlers.SystemUptime)
				r.Get("/events", publicSSE.Handler)
			})

			// Stripe (authenticated checkout/portal within mobile auth)
			if stripeHandler != nil {
				r.Get("/public/stripe/prices", stripeHandler.Prices)
				r.Route("/v2/mobile/stripe", func(r chi.Router) {
					r.Use(mobile.JWTUserAuth(database.Conn(), cfg.JWTSecret))
					r.Post("/create-checkout-session", mobile.WithStripeContext(stripeHandler.CreateCheckoutSession))
					r.Post("/create-portal-session", mobile.WithStripeContext(stripeHandler.CreatePortalSession))
				})
			}

			// Webhooks (public, signature-verified by the handlers themselves)
			r.Post("/webhooks/apple", webhookHandler.AppleNotification)
			r.Post("/webhooks/google", webhookHandler.GoogleNotification)
			if stripeHandler != nil {
				r.Post("/webhooks/stripe", stripeHandler.Webhook)
			}
		})
	})

	// Background importers (hazard reductions + major incidents every 5 min)
	imp := importer.New(database.Conn(), 5*time.Minute)
	imp.Start()
	defer imp.Stop()

	// Server
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Stop accepting new HTTP requests and let in-flight ones drain.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	// 2. Tell background workers to finish their current row and exit.
	cancelBg()

	// 3. Wait for them, then close the listener connection.
	done := make(chan struct{})
	go func() { bg.Wait(); close(done) }()
	select {
	case <-done:
		slog.Info("background workers drained")
	case <-shutdownCtx.Done():
		slog.Warn("shutdown deadline exceeded with workers still running")
	}
	slog.Info("server stopped")
}

// podID returns a stable identifier for this server process. Honors
// HOSTNAME (set by k8s + docker) or POD_ID, falling back to a fresh UUID
// so dev/local always gets *something* unique.
func podID() string {
	for _, k := range []string{"POD_ID", "HOSTNAME"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return fmt.Sprintf("pod-%s", uuid.NewString()[:8])
}
