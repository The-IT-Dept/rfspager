package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/jmoiron/sqlx"
)

const majorIncidentsURL = "https://www.rfs.nsw.gov.au/feeds/majorIncidents.json"

type miResponse struct {
	Features []miFeature `json:"features"`
}

type miFeature struct {
	Properties miProperties   `json:"properties"`
	Geometry   map[string]any `json:"geometry"`
}

type miProperties struct {
	GUID        string `json:"guid"`
	Title       string `json:"title"`
	Category    string `json:"category"`
	PubDate     string `json:"pubDate"`
	Description string `json:"description"`
}

func importMajorIncidents(ctx context.Context, db *sqlx.DB) error {
	req, err := http.NewRequestWithContext(ctx, "GET", majorIncidentsURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching major incidents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("major incidents HTTP %d", resp.StatusCode)
	}

	var data miResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("decoding major incidents: %w", err)
	}

	count := 0

	for _, f := range data.Features {
		parsedData := parseDescription(f.Properties.Description)
		dataJSON, _ := json.Marshal(parsedData)
		geometryJSON, _ := json.Marshal(f.Geometry)

		createdAt := parsePubDate(f.Properties.PubDate)

		_, err := db.ExecContext(ctx, `
			INSERT INTO majorincidents (upstream_id, title, category, geometry, data, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
			ON CONFLICT (upstream_id) DO UPDATE SET
				title = EXCLUDED.title,
				category = EXCLUDED.category,
				geometry = EXCLUDED.geometry,
				data = EXCLUDED.data,
				updated_at = NOW()`,
			f.Properties.GUID, f.Properties.Title, f.Properties.Category,
			string(geometryJSON), string(dataJSON), createdAt)
		if err != nil {
			slog.Error("upsert major incident failed", "upstream_id", f.Properties.GUID, "error", err)
			continue
		}
		count++
	}

	slog.Info("major incidents imported", "count", count, "total", len(data.Features))
	return nil
}

// parseDescription parses the HTML description "KEY: value<br />KEY: value" into a map.
func parseDescription(desc string) map[string]string {
	result := make(map[string]string)
	parts := strings.Split(desc, "<br />")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.Index(part, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(part[:idx])
		val := strings.TrimSpace(part[idx+1:])
		result[toCamelCase(key)] = val
	}
	return result
}

// toCamelCase converts "SOME KEY" to "someKey".
func toCamelCase(s string) string {
	s = strings.ToLower(s)
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	result := words[0]
	for _, w := range words[1:] {
		if len(w) > 0 {
			runes := []rune(w)
			runes[0] = unicode.ToUpper(runes[0])
			result += string(runes)
		}
	}
	return result
}

// parsePubDate reads the feed's pubDate, "d/m/Y h:i:s A" (e.g.
// "12/11/2026 1:03:00 AM"). It is UTC, not Sydney time: the same incident's
// description says "UPDATED: 12 Nov 2026 12:03" (AEDT). Reading it as Sydney
// time put every incident 10-11 hours in the past (fixed by migration 040).
func parsePubDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2/01/2006 3:04:05 PM", s, time.UTC)
	if err != nil {
		t, err = time.ParseInLocation("02/01/2006 3:04:05 PM", s, time.UTC)
		if err != nil {
			return nil
		}
	}
	return &t
}
