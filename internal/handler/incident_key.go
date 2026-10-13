package handler

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
)

// IncidentKey reduces a page to a key shared by every copy of the same
// dispatch, whichever capcode it was sent to, so push notifications can be
// suppressed to one per incident message per user.
//
//   - RFS dispatches carry an incident number ("26-131320"). The key is that
//     number plus the message body with the leading timestamp and unit
//     codes removed, so the initial call to three brigades collapses to one
//     key while the later "Stop Message" for the same incident keeps its
//     own.
//   - FRNSW turnouts carry "INC: 203357-13102026". One incident pages one
//     or more stations with the same type; the INC number alone is the key.
//   - Anything else keys on its body with the timestamp and unit codes
//     removed, which collapses identical broadcasts (training reminders,
//     weather alerts) sent to several capcodes.
//
// Unit codes are the uppercase alphanumeric tokens like LHCENTR1, CCGWANDP
// or HVDO. Removing every such token also strips uppercase words from the
// body, which is fine: the key only has to be identical across copies and
// distinct across different messages, and the incident number anchors it.
func IncidentKey(message string) string {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return ""
	}
	if m := frnswIncident.FindStringSubmatch(msg); m != nil {
		return "frinc:" + m[1]
	}
	body := rfsTimestamp.ReplaceAllString(msg, "")
	body = unitCode.ReplaceAllString(body, "")
	body = strings.Join(strings.Fields(body), " ")
	if m := rfsIncident.FindStringSubmatch(msg); m != nil {
		return "rfs:" + m[1] + ":" + shortHash(body)
	}
	return "msg:" + shortHash(body)
}

var (
	// "26-131320" style RFS incident numbers.
	rfsIncident = regexp.MustCompile(`\b(\d{2}-\d{6})\b`)
	// "INC: 203357-13102026" on FRNSW turnouts.
	frnswIncident = regexp.MustCompile(`\bINC:\s*(\d+-\d{8})\b`)
	// Leading "13 October 2026 22:47:41 " or a bare "16:55:01 " time.
	rfsTimestamp = regexp.MustCompile(`^\s*(?:\d{1,2} [A-Z][a-z]+ \d{4} )?\d{2}:\d{2}:\d{2}\s*`)
	// Unit codes and other uppercase alphanumeric tokens of 4+ characters.
	unitCode = regexp.MustCompile(`\b[A-Z][A-Z0-9]{3,}\b`)
)

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:8])
}
