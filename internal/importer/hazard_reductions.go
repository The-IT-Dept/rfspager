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

const hazardReductionURL = "https://www.rfs.nsw.gov.au/funnelback/hr-map-data?collection=nsw-rfs-hazard-xml-new"

type hrResponse struct {
	Results []hrResult `json:"results"`
}

type hrResult struct {
	GuarReference      string      `json:"guarReference"`
	LeadAgency         string      `json:"leadAgency"`
	SupportingAgencies string      `json:"supportingAgencies"`
	LGA                string      `json:"lga"`
	Size               string      `json:"size"`
	Location           string      `json:"location"`
	Tenure             string      `json:"tenure"`
	StartDate          string      `json:"startDate"`
	EndDate            string      `json:"endDate"`
	GeometryType       string      `json:"geometryType"`
	Polygons           []hrPolygon `json:"polygons"`
}

type hrPolygon struct {
	Polygon string `json:"polygon"`
}

func importHazardReductions(ctx context.Context, db *sqlx.DB) error {
	req, err := http.NewRequestWithContext(ctx, "GET", hazardReductionURL, nil)
	if err != nil {
		return err
	}

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
	count := 0

	for _, r := range data.Results {
		size := parseSize(r.Size)
		title := strings.ReplaceAll(r.Location, "HAZARD REDUCTION", "")
		title = strings.TrimSpace(title)

		startAt := parseAUDate(r.StartDate, loc)
		endAt := parseAUDate(r.EndDate, loc)

		geometry := buildGeometry(r.GeometryType, r.Polygons)
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
			r.GuarReference, title, r.LeadAgency, r.SupportingAgencies,
			r.LGA, size, r.Tenure, string(geometryJSON), startAt, endAt)
		if err != nil {
			slog.Error("upsert hazard reduction failed", "upstream_id", r.GuarReference, "error", err)
			continue
		}
		count++
	}

	slog.Info("hazard reductions imported", "count", count, "total", len(data.Results))
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

func buildGeometry(geomType string, polygons []hrPolygon) map[string]any {
	coords := make([][][2]float64, 0)
	for _, p := range polygons {
		ring := make([][2]float64, 0)
		pairs := strings.Split(p.Polygon, "|")
		for _, pair := range pairs {
			parts := strings.Split(pair, ";")
			if len(parts) < 2 {
				continue
			}
			lat, _ := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			lng, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			ring = append(ring, [2]float64{lng, lat}) // GeoJSON is [lng, lat]
		}
		if len(ring) > 0 {
			coords = append(coords, ring)
		}
	}

	return map[string]any{
		"type":        geomType,
		"coordinates": coords,
	}
}
