package handler

import "github.com/go-chi/chi/v5"

// RegisterIngress exposes only authenticated HTTP receiver routes.
// The UUID-only and Laravel Sanctum routes have deliberately been retired.
func RegisterIngress(r chi.Router, h *MessageHandler) {
	r.Post("/messages", h.ProcessPagermon)
	r.Get("/receiver/me", h.ReceiverMe)
}
