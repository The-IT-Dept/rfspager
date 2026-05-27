package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// hazardReductionURL — RFS migrated their hazard-reduction map data from
// their own /funnelback proxy (which now returns a Squiz CMS login page) to
// Squiz's cloud-hosted Funnelback at nrfs-search.funnelback.squiz.cloud.
// The `!showall` query is what the live website uses to get every record.
const hazardReductionURL = "https://nrfs-search.funnelback.squiz.cloud/s/search.json?collection=nrfs~sp-hazard-reduction&profile=map&num_ranks=1000&query=%21showall"

// New Funnelback envelope. Results live at response.resultPacket.results[];
// each result's metadata sits under listMetadata as `{key: [values...]}`.
type hrResponse struct {
	Response struct {
		ResultPacket struct {
			Results []hrResult `json:"results"`
		} `json:"resultPacket"`
	} `json:"response"`
}

type hrResult struct {
	Title        string              `json:"title"`
	ListMetadata map[string][]string `json:"listMetadata"`
}

// first returns the first value of a multi-valued metadata field.
func (r hrResult) first(key string) string {
	v := r.ListMetadata[key]
	if len(v) == 0 {
		return ""
	}
	return strings.TrimSpace(v[0])
}

func importHazardReductions(ctx context.Context, db *sqlx.DB) error {
	req, err := http.NewRequestWithContext(ctx, "GET", hazardReductionURL, nil)
	if err != nil {
		return err
	}
	// Funnelback's Squiz-hosted endpoint is happy with a default User-Agent
	// but be explicit so we're identifiable in their logs.
	req.Header.Set("User-Agent", "rfspager-importer/1.0 (https://rfspager.app)")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching hazard reductions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("hazard reductions HTTP %d", resp.StatusCode)
	}

	var data hrResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("decoding hazard reductions: %w", err)
	}

	loc, _ := time.LoadLocation("Australia/Sydney")
	results := data.Response.ResultPacket.Results
	count := 0

	for _, r := range results {
		guar := r.first("guarReference")
		if guar == "" {
			continue
		}
		location := r.first("location")
		title := strings.TrimSpace(strings.ReplaceAll(location, "HAZARD REDUCTION", ""))
		size := parseSize(r.first("size"))
		startAt := parseAUDate(r.first("d"), loc)
		endAt := parseAUDate(r.first("DueDateTo"), loc)

		geometry := buildGeometry(r.first("geometryType"), r.ListMetadata["polygons"])
		geometryJSON, _ := json.Marshal(geometry)

		_, err := db.ExecContext(ctx, `
			INSERT INTO hazardreductions (upstream_id, title, "leadAgency", "supportingAgencies", lga, size, tenure, geometry, start_at, end_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
			ON CONFLICT (upstream_id) DO UPDATE SET
				title = EXCLUDED.title,
				"leadAgency" = EXCLUDED."leadAgency",
				"supportingAgencies" = EXCLUDED."supportingAgencies",
				lga = EXCLUDED.lga,
				size = EXCLUDED.size,
				tenure = EXCLUDED.tenure,
				geometry = EXCLUDED.geometry,
				start_at = EXCLUDED.start_at,
				end_at = EXCLUDED.end_at,
				updated_at = NOW()`,
			guar, title, r.first("leadAgency"), r.first("supportingAgencies"),
			r.first("lga"), size, r.first("tenure"), string(geometryJSON), startAt, endAt)
		if err != nil {
			slog.Error("upsert hazard reduction failed", "upstream_id", guar, "error", err)
			continue
		}
		count++
	}

	slog.Info("hazard reductions imported", "count", count, "total", len(results))
	return nil
}

func parseSize(s string) float64 {
	s = strings.ReplaceAll(s, " ha", "")
	s = strings.TrimSpace(s)
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseAUDate(s string, loc *time.Location) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2/01/2006", s, loc)
	if err != nil {
		// Try alternate format
		t, err = time.ParseInLocation("02/01/2006", s, loc)
		if err != nil {
			return nil
		}
	}
	t = t.Add(12 * time.Hour) // midday
	return &t
}

// buildGeometry turns the Funnelback `polygons` metadata array into a GeoJSON
// geometry. The new feed format is a flat list of "lat;lng" strings; ring
// boundaries are implicit — a ring closes when a point repeats one that was
// the start of the current open ring. For Polygon and MultiPolygon outputs
// we wrap accordingly.
func buildGeometry(geomType string, polygonPoints []string) map[string]any {
	rings := make([][][2]float64, 0)
	current := make([][2]float64, 0)

	for _, raw := range polygonPoints {
		parts := strings.Split(raw, ";")
		if len(parts) < 2 {
			continue
		}
		lat, errLat := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		lng, errLng := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if errLat != nil || errLng != nil {
			continue
		}
		pt := [2]float64{lng, lat} // GeoJSON is [lng, lat]
		current = append(current, pt)

		// Ring closes when we return to the starting point.
		if len(current) > 1 && current[0] == current[len(current)-1] {
			rings = append(rings, current)
			current = make([][2]float64, 0)
		}
	}
	// Trailing unclosed ring (the upstream should always close, but be safe).
	if len(current) > 0 {
		rings = append(rings, current)
	}

	switch strings.ToLower(geomType) {
	case "polygon":
		return map[string]any{"type": "Polygon", "coordinates": rings}
	case "multipolygon":
		// Each ring becomes its own polygon with no holes.
		multi := make([][][][2]float64, 0, len(rings))
		for _, r := range rings {
			multi = append(multi, [][][2]float64{r})
		}
		return map[string]any{"type": "MultiPolygon", "coordinates": multi}
	default:
		return map[string]any{"type": geomType, "coordinates": rings}
	}
}
