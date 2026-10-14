package coverage

import (
	"sort"
	"strings"
)

// Capcode is the slice of a capcode the coverage map needs.
type Capcode struct {
	ID       int64  `db:"id"`
	Address  string `db:"address"`
	Alias    string `db:"alias"`
	AgencyID *int64 `db:"agency_id"`
	Ignore   bool   `db:"ignore"`
}

// prefixLen is how many leading digits of an address identify the district
// an unknown capcode most likely belongs to (1370xxx = Northern Beaches).
const prefixLen = 4

// prefixShare is the share of known capcodes under a prefix that must agree
// on one agency before an unknown address under it is given that agency.
const prefixShare = 0.8

// Resolver maps a page address to its capcode and agency the same way ingest
// does (exact address, else the most specific "_" wildcard), falling back to
// the 4-digit prefix map for addresses with no capcode. Built per request
// from the current capcodes, so capcode edits apply retroactively.
type Resolver struct {
	exact     map[string]Capcode
	wildcards []Capcode
	byID      map[int64]Capcode
	prefixes  map[string]int64
}

// Resolution is what an address resolved to. CapcodeID is 0 when no capcode
// matched; AgencyID is 0 when neither a capcode nor the prefix map placed it.
type Resolution struct {
	CapcodeID int64
	AgencyID  int64
	ByPrefix  bool
}

// NewResolver indexes the non-deleted capcodes. Ignored capcodes resolve
// like any other (ingest drops their pages before they are recorded) but do
// not vote in the prefix map.
func NewResolver(capcodes []Capcode) *Resolver {
	r := &Resolver{
		exact: make(map[string]Capcode, len(capcodes)),
		byID:  make(map[int64]Capcode, len(capcodes)),
	}
	for _, c := range capcodes {
		r.byID[c.ID] = c
		if strings.ContainsAny(c.Address, "_%") {
			r.wildcards = append(r.wildcards, c)
			continue
		}
		// Several capcodes can share an address; ingest takes the first by
		// its ORDER BY, which for equal addresses is arbitrary. Lowest id is
		// at least deterministic.
		if prev, ok := r.exact[c.Address]; !ok || c.ID < prev.ID {
			r.exact[c.Address] = c
		}
	}
	// Same order as FindCapcodeByAddress: REPLACE(address, '_', '%') DESC,
	// which puts literal digits ahead of wildcards position by position.
	sort.SliceStable(r.wildcards, func(i, j int) bool {
		return strings.ReplaceAll(r.wildcards[i].Address, "_", "%") > strings.ReplaceAll(r.wildcards[j].Address, "_", "%")
	})
	r.prefixes = buildPrefixMap(capcodes)
	return r
}

// buildPrefixMap returns prefix -> agency for every 4-digit prefix whose
// known capcodes agree (>= prefixShare) on one agency.
func buildPrefixMap(capcodes []Capcode) map[string]int64 {
	counts := map[string]map[int64]int{}
	for _, c := range capcodes {
		if c.Ignore || c.AgencyID == nil || len(c.Address) < prefixLen {
			continue
		}
		p := c.Address[:prefixLen]
		if strings.ContainsAny(p, "_%") {
			continue
		}
		if counts[p] == nil {
			counts[p] = map[int64]int{}
		}
		counts[p][*c.AgencyID]++
	}
	out := make(map[string]int64, len(counts))
	for p, byAgency := range counts {
		total, best, bestN := 0, int64(0), 0
		for id, n := range byAgency {
			total += n
			if n > bestN || (n == bestN && id < best) {
				best, bestN = id, n
			}
		}
		if total > 0 && float64(bestN)/float64(total) >= prefixShare {
			out[p] = best
		}
	}
	return out
}

// Resolve places an address. storedCapcodeID is the capcode ingest matched
// at the time (0 if none); it is used only when the address no longer
// matches any capcode, e.g. after the capcode's address was edited.
func (r *Resolver) Resolve(address string, storedCapcodeID int64) Resolution {
	if c, ok := r.match(address); ok {
		return Resolution{CapcodeID: c.ID, AgencyID: agencyOf(c)}
	}
	if c, ok := r.byID[storedCapcodeID]; ok && storedCapcodeID != 0 {
		return Resolution{CapcodeID: c.ID, AgencyID: agencyOf(c)}
	}
	if len(address) >= prefixLen {
		if id, ok := r.prefixes[address[:prefixLen]]; ok {
			return Resolution{AgencyID: id, ByPrefix: true}
		}
	}
	return Resolution{}
}

func (r *Resolver) match(address string) (Capcode, bool) {
	if c, ok := r.exact[address]; ok {
		return c, true
	}
	for _, c := range r.wildcards {
		if likeMatch(address, c.Address) {
			return c, true
		}
	}
	return Capcode{}, false
}

func agencyOf(c Capcode) int64 {
	if c.AgencyID == nil {
		return 0
	}
	return *c.AgencyID
}

// likeMatch reports whether s matches the SQL LIKE pattern p ("_" one
// character, "%" any run; no escapes, as capcode addresses never use them).
func likeMatch(s, p string) bool {
	if p == "" {
		return s == ""
	}
	switch p[0] {
	case '%':
		for i := 0; i <= len(s); i++ {
			if likeMatch(s[i:], p[1:]) {
				return true
			}
		}
		return false
	case '_':
		return s != "" && likeMatch(s[1:], p[1:])
	default:
		return s != "" && s[0] == p[0] && likeMatch(s[1:], p[1:])
	}
}
