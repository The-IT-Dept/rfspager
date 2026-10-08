package radio

import (
	"fmt"
	"regexp"
	"strings"
)

// Subscribers only get talkgroups belonging to these agencies (by
// radio_talkgroups.agency). Everything else — police, ambulance, transport,
// and talkgroups nobody has named yet — is treated like a hidden talkgroup:
// left out of the feed, the live stream and the event stream, and its audio
// is neither kept nor transcribed. Call metadata is still recorded.
var publicAgencies = []string{"RFS", "FRNSW"}

// hiddenTG is the SQL test (over talkgroup alias tg) for "not shown to
// subscribers". Queries are written with COALESCE(tg.hidden, false) and run
// through vis, which swaps this in.
var hiddenTG = buildHiddenTG(publicAgencies)

var agencyCode = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)

// SetPublicAgencies replaces the allowlist (from RADIO_AGENCIES). Call it at
// startup, before serving.
func SetPublicAgencies(agencies []string) error {
	var clean []string
	for _, a := range agencies {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if !agencyCode.MatchString(a) {
			return fmt.Errorf("radio: bad agency code %q", a)
		}
		clean = append(clean, a)
	}
	if len(clean) == 0 {
		return fmt.Errorf("radio: no public agencies")
	}
	publicAgencies, hiddenTG = clean, buildHiddenTG(clean)
	return nil
}

func buildHiddenTG(agencies []string) string {
	quoted := make([]string, len(agencies))
	for i, a := range agencies {
		quoted[i] = "'" + a + "'" // agencyCode guarantees no quotes
	}
	return "(tg.hidden IS TRUE OR COALESCE(tg.agency, '') NOT IN (" + strings.Join(quoted, ", ") + "))"
}

// vis applies the subscriber visibility rule to a query.
func vis(query string) string {
	return strings.ReplaceAll(query, "COALESCE(tg.hidden, false)", hiddenTG)
}
