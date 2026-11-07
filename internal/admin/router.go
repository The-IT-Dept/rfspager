package admin

import (
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/coverage"
	"github.com/the-it-dept/rfspager/internal/notify"
)

// Router creates the admin API router mounted at /api/v1/admin. receivers
// wires the receiver remote-management routes; nil leaves them out. The
// two receiver SSE streams are not here: cmd/server mounts them outside
// the request-timeout group (see ReceiverStatsStream). coverageSvc wires the
// coverage map routes; nil leaves them out. applications emails owners the
// install command on approval; nil sends no email. extenders are the store
// clients behind "Give free time" on a user.
func Router(db *sqlx.DB, jwtSecret string, notifier *notify.Notifier, receivers *ReceiverDeps, coverageSvc *coverage.Service, applications ApplicationMailer, extenders Extenders) chi.Router {
	store := NewStore(db)
	auth := NewAuthHandler(store, jwtSecret)
	h := NewHandlers(store, notifier)
	h.applications = applications
	ext := newExtensionHandlers(db, extenders)
	sup := newSupporterHandlers(db)
	var rx *receiverHandlers
	if receivers != nil && receivers.Store != nil {
		rx = newReceiverHandlers(db, *receivers)
	}

	r := chi.NewRouter()

	// Public auth routes
	r.Post("/auth/login", auth.Login)

	// Protected routes (header-only — most admin API endpoints)
	r.Group(func(r chi.Router) {
		r.Use(auth.JWTAuth)

		r.Get("/auth/me", auth.Me)
		r.Post("/auth/logout", auth.Logout)
		r.Get("/dashboard", h.Dashboard)
		r.Get("/dashboard/app-versions", h.AppVersions)

		// Users
		r.Route("/users", func(r chi.Router) {
			r.Use(auth.RequireSuperadmin)
			r.Get("/", h.ListUsers)
			r.Post("/", h.CreateUser)
			r.Get("/{id}", h.GetUser)
			r.Put("/{id}", h.UpdateUser)
			r.Delete("/{id}", h.DeleteUser)
			r.Get("/{id}/installations", h.GetUserInstallations)
			r.Get("/{id}/iap-transactions", h.GetUserIAPTransactions)
			r.Get("/{id}/extensions", ext.List)
			r.Post("/{id}/extensions", ext.Grant)
		})

		// Billing / IAP
		r.Route("/billing", func(r chi.Router) {
			r.Get("/stats", h.BillingStats)
			r.Get("/transactions", h.ListIAPTransactions)
			r.Get("/transactions/{id}", h.GetIAPTransaction)
		})

		// Capcode, agency and system edits change what the coverage map
		// shows (districts, receiver locations); drop its cached reports.
		invalidate := invalidateCoverage(coverageSvc)

		// Capcodes
		r.Route("/capcodes", func(r chi.Router) {
			r.Use(invalidate)
			r.Get("/", h.ListCapcodes)
			r.Post("/", h.CreateCapcode)
			r.Get("/{id}", h.GetCapcode)
			r.Put("/{id}", h.UpdateCapcode)
			r.Delete("/{id}", h.DeleteCapcode)
		})

		// Agencies
		r.Route("/agencies", func(r chi.Router) {
			r.Use(invalidate)
			r.Get("/", h.ListAgencies)
			r.Post("/", h.CreateAgency)
			r.Get("/{id}", h.GetAgency)
			r.Put("/{id}", h.UpdateAgency)
			r.Delete("/{id}", h.DeleteAgency)
		})

		// Systems
		r.Route("/systems", func(r chi.Router) {
			r.Use(invalidate)
			r.Get("/", h.ListSystems)
			r.Post("/", h.CreateSystem)
			r.Get("/{id}", h.GetSystem)
			r.Put("/{id}", h.UpdateSystem)
			r.Delete("/{id}", h.DeleteSystem)
			r.With(auth.RequireSuperadmin).Post("/{id}/approve", h.ApproveSystem)
			mountSupporterRoutes(r, sup, auth.RequireSuperadmin)
			if rx != nil {
				// Receiver remote management: config, stats, events, logs,
				// commands, captures (receivers.go).
				mountReceiverRoutes(r, rx)
			}
		})
		if rx != nil {
			r.Get("/receivers/latest-version", rx.LatestVersion)
		}

		// Coverage map (coverage.go)
		if coverageSvc != nil {
			mountCoverageRoutes(r, coverageSvc)
		}

		// Messages
		r.Route("/messages", func(r chi.Router) {
			r.Get("/", h.ListMessages)
			r.Get("/{id}", h.GetMessage)
			r.Get("/{id}/notifications", h.ListMessageNotifications)
			r.Get("/{id}/copies", h.ListMessageCopies)
			r.Get("/{id}/incident", h.ListIncidentMessages)
			r.Put("/{id}", h.UpdateMessage)
			r.Delete("/{id}", h.DeleteMessage)
		})

		// Installations
		r.Route("/installations", func(r chi.Router) {
			r.Get("/", h.ListInstallations)
			r.Get("/{id}", h.GetInstallation)
			r.Delete("/{id}", h.DeleteInstallation)
		})

		// Subscriptions
		r.Route("/subscriptions", func(r chi.Router) {
			r.Get("/", h.ListSubscriptions)
			r.Post("/", h.CreateSubscription)
			r.Delete("/{id}", h.DeleteSubscription)
		})

		// Alertzones
		r.Route("/alertzones", func(r chi.Router) {
			r.Get("/", h.ListAlertzones)
			r.Post("/", h.CreateAlertzone)
			r.Get("/{id}", h.GetAlertzone)
			r.Put("/{id}", h.UpdateAlertzone)
			r.Delete("/{id}", h.DeleteAlertzone)
		})

		// Notifications
		r.Route("/notifications", func(r chi.Router) {
			r.Get("/", h.ListNotifications)
			r.Post("/{id}/read", h.MarkNotificationRead)
			r.Delete("/{id}", h.DeleteNotification)
		})

		// Responders
		r.Route("/responders", func(r chi.Router) {
			r.Get("/", h.ListResponders)
			r.Delete("/{id}", h.DeleteResponder)
		})

		// Capcode Suggestions
		r.Route("/capcode-suggestions", func(r chi.Router) {
			r.Get("/", h.ListCapcodeSuggestions)
			r.Post("/{id}/approve", h.ApproveCapcodeSuggestion)
			r.Delete("/{id}", h.DeleteCapcodeSuggestion)
		})

		// Major Incidents
		r.Route("/major-incidents", func(r chi.Router) {
			r.Get("/", h.ListMajorIncidents)
			r.Post("/", h.CreateMajorIncident)
			r.Get("/{id}", h.GetMajorIncident)
			r.Put("/{id}", h.UpdateMajorIncident)
			r.Delete("/{id}", h.DeleteMajorIncident)
		})

		// Hazard Reductions
		r.Route("/hazard-reductions", func(r chi.Router) {
			r.Get("/", h.ListHazardReductions)
			r.Post("/", h.CreateHazardReduction)
			r.Get("/{id}", h.GetHazardReduction)
			r.Put("/{id}", h.UpdateHazardReduction)
			r.Delete("/{id}", h.DeleteHazardReduction)
		})

		// Admin Users (self-management)
		r.Route("/admin-users", func(r chi.Router) {
			r.Use(auth.RequireSuperadmin)
			r.Get("/", h.ListAdminUsers)
			r.Get("/{id}", h.GetAdminUser)
			r.Post("/", h.CreateAdminUser)
			r.Put("/{id}", h.UpdateAdminUser)
			r.With(auth.PreventSelfAdminDelete).Delete("/{id}", h.DeleteAdminUser)
		})
	})

	return r
}
