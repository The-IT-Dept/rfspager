package public

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/the-it-dept/rfspager/internal/coverage"
)

// CoverageReporter builds the public coverage map; *coverage.Service.
type CoverageReporter interface {
	PublicReport(ctx context.Context, days int) (*coverage.PublicResponse, error)
}

// Coverage serves GET /api/public/coverage?days=30|90 (anything else is
// 90): receivers, RFS paging sites, districts and the incident heat map,
// reduced from the admin coverage report. The service caches each window
// for 15 minutes; the header lets browsers and the edge do the same.
func Coverage(svc CoverageReporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		report, err := svc.PublicReport(r.Context(), days)
		if err != nil {
			slog.Error("public coverage report failed", "days", days, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "coverage is unavailable"})
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=900")
		writeJSON(w, http.StatusOK, report)
	}
}
