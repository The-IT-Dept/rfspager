package mobile

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/iap"
)

func Router(db *sqlx.DB, jwtSecret, appleBundleID string, sseBroker *SSEBroker, iapHandler *iap.Handler) chi.Router {
	auth := NewAuthHandler(db, jwtSecret, appleBundleID)
	h := NewHandlers(db)

	r := chi.NewRouter()

	// Public auth routes
	r.Post("/auth/login", auth.Login)
	r.Post("/auth/register", auth.Register)
	r.Post("/auth/social-login", auth.SocialLogin)
	r.Post("/auth/forgot-password", auth.ForgotPassword)
	r.Post("/auth/reset-password", auth.ResetPassword)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(JWTUserAuth(db, jwtSecret))

		// Auth
		r.Post("/auth/logout", auth.Logout)
		r.Post("/auth/update-password", auth.UpdatePassword)

		// User
		r.Get("/me", h.Me)
		r.Patch("/me", auth.UpdateProfile)
		r.Delete("/me", auth.DeleteAccount)

		// Messages
		r.Get("/messages", h.ListMessages)
		r.Get("/messages/map", h.MapMessages)
		r.Get("/messages/{id}", h.GetMessage)
		r.Post("/messages/{id}/respond", h.Respond)

		// Agencies & Capcodes
		r.Get("/agencies", h.ListAgencies)
		r.Get("/capcodes", h.ListCapcodes)

		// Capcode subscriptions
		r.Get("/subscriptions", h.ListSubscriptions)
		r.Post("/subscriptions", h.CreateSubscription)
		r.Delete("/subscriptions/{capcode_id}", h.DeleteSubscription)

		// Capcode filters
		r.Get("/filters", h.ListFilters)
		r.Post("/filters", h.CreateFilter)
		r.Delete("/filters/{capcode_id}", h.DeleteFilter)

		// Alertzones
		r.Get("/alertzones", h.ListAlertzones)
		r.Post("/alertzones", h.CreateAlertzone)
		r.Delete("/alertzones/{id}", h.DeleteAlertzone)

		// Installations (push tokens)
		r.Put("/installations/{id}", h.UpsertInstallation)
		r.Delete("/installations/{id}", h.DeleteInstallation)

		// Notifications
		r.Post("/notifications/mark-read", h.MarkNotificationsRead)

		// Incidents
		r.Get("/major-incidents", h.ListMajorIncidents)
		r.Get("/hazard-reductions", h.ListHazardReductions)

		// Systems
		r.Get("/systems", h.ListSystems)
		r.Post("/systems", h.CreateSystem)
		r.Get("/my-systems", h.MySystems)

		// Photos
		r.Get("/photos", h.ListPhotos)

		// IAP validation (inject user ID into iap context)
		r.Post("/iap/apple/validate", withIAPUserID(iapHandler.ValidateApple))
		r.Post("/iap/google/validate", withIAPUserID(iapHandler.ValidateGoogle))
		r.Post("/iap/restore", withIAPUserID(iapHandler.Restore))

		// SSE (real-time messages)
		r.Get("/events", sseBroker.Handler)
	})

	return r
}

// withIAPUserID wraps a handler to inject the mobile user's ID into the IAP context.
func withIAPUserID(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r.Context())
		if user == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		ctx := iap.WithUserID(r.Context(), user.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
