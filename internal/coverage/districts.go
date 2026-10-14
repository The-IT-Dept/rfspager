package coverage

import (
	"math"
	"strings"
	"unicode"
)

// District assignment: each RFS-channel transmitter takes the district whose
// labelled points (brigade stations, geocoded incidents) dominate around it.

const (
	// stationWeight and incidentWeight are the votes of one brigade station
	// and one ~1 km incident cell.
	stationWeight  = 3.0
	incidentWeight = 1.0
	// voteRadiusKm is the neighbourhood searched first; voteFallbackKm is
	// used when nothing lies within it (remote western sites).
	voteRadiusKm   = 30.0
	voteFallbackKm = 60.0
)

// LabelPoint is a location known to belong to a district.
type LabelPoint struct {
	Lat, Lng float64
	AgencyID int64
	Weight   float64
}

// Station is an RFS brigade station from emergency_stations.
type Station struct {
	Name string  `db:"name"`
	Lat  float64 `db:"lat"`
	Lng  float64 `db:"lng"`
}

// NormalizeName upper-cases s, turns every run of punctuation and spaces
// into one space and spells "MOUNT" as "MT", so "Mt. Victoria" and "MOUNT
// VICTORIA" compare equal.
func NormalizeName(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToUpper(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
			continue
		}
		space = true
	}
	words := strings.Fields(b.String())
	for i, w := range words {
		if w == "MOUNT" {
			words[i] = "MT"
		}
	}
	return strings.Join(words, " ")
}

// aliasKeys returns the brigade names a capcode alias refers to: the text
// before " Brigade", normalised, and for "A/B Brigade" also "A" and "B".
// Aliases without " Brigade" (groups, officers, districts) return nil.
func aliasKeys(alias string) []string {
	i := strings.Index(strings.ToUpper(alias), " BRIGADE")
	if i <= 0 {
		return nil
	}
	return splitKeys(alias[:i], "/")
}

// stationKeys returns the names a station may be listed under: the whole
// name, and its parts when it is a combined "A / B" or "A - B" station.
func stationKeys(name string) []string {
	return splitKeys(name, "/", " - ")
}

func splitKeys(s string, seps ...string) []string {
	var keys []string
	seen := map[string]bool{}
	add := func(k string) {
		k = NormalizeName(k)
		if k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	add(s)
	parts := []string{s}
	for _, sep := range seps {
		var next []string
		for _, p := range parts {
			next = append(next, strings.Split(p, sep)...)
		}
		parts = next
	}
	if len(parts) > 1 {
		for _, p := range parts {
			add(p)
		}
	}
	return keys
}

// stationLabels turns brigade stations into labelled points. A station is
// used when one of its names matches the brigade aliases of exactly one
// district agency (several capcodes of one brigade are fine), trying the
// whole name before its parts. Station names that occur more than once are
// skipped: "North" or "Clarence" could be anywhere.
func stationLabels(stations []Station, capcodes []Capcode, isDistrict func(int64) bool) []LabelPoint {
	agenciesByKey := map[string]map[int64]bool{}
	for _, c := range capcodes {
		if c.AgencyID == nil || !isDistrict(*c.AgencyID) {
			continue
		}
		for _, k := range aliasKeys(c.Alias) {
			if agenciesByKey[k] == nil {
				agenciesByKey[k] = map[int64]bool{}
			}
			agenciesByKey[k][*c.AgencyID] = true
		}
	}

	nameCount := map[string]int{}
	for _, s := range stations {
		nameCount[NormalizeName(s.Name)]++
	}

	var out []LabelPoint
	for _, s := range stations {
		if nameCount[NormalizeName(s.Name)] > 1 {
			continue
		}
		for _, k := range stationKeys(s.Name) {
			agencies := agenciesByKey[k]
			if len(agencies) != 1 {
				continue
			}
			for id := range agencies {
				out = append(out, LabelPoint{Lat: s.Lat, Lng: s.Lng, AgencyID: id, Weight: stationWeight})
			}
			break
		}
	}
	return out
}

// voteDistrict picks the district for a transmitter at (lat, lng): each
// labelled point within voteRadiusKm (voteFallbackKm if none) votes
// weight/(1+d)^2 for its agency. confidence is the winner's share of the
// total. ok is false when no point is in range.
func voteDistrict(lat, lng float64, points []LabelPoint) (agencyID int64, confidence float64, ok bool) {
	for _, radius := range []float64{voteRadiusKm, voteFallbackKm} {
		votes := map[int64]float64{}
		total := 0.0
		for _, p := range points {
			// Cheap box test before the trigonometry: a degree of latitude
			// is ~111 km and a degree of longitude at NSW latitudes ~90 km.
			if math.Abs(p.Lat-lat) > radius/100 || math.Abs(p.Lng-lng) > radius/80 {
				continue
			}
			d := distanceKm(lat, lng, p.Lat, p.Lng)
			if d > radius {
				continue
			}
			v := p.Weight / ((1 + d) * (1 + d))
			votes[p.AgencyID] += v
			total += v
		}
		if total == 0 {
			continue
		}
		var best int64
		bestV := -1.0
		for id, v := range votes {
			if v > bestV || (v == bestV && id < best) {
				best, bestV = id, v
			}
		}
		return best, bestV / total, true
	}
	return 0, 0, false
}

// inNSW is a loose bounding box around NSW and the ACT, used to drop
// garbled coordinates from damaged pages.
func inNSW(lat, lng float64) bool {
	return lat >= -38.5 && lat <= -27.5 && lng >= 140.5 && lng <= 154
}
