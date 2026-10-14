// Package legacy answers the retired Laravel GraphQL API for the old 4.x
// mobile app.
//
// That app cannot show a server error: its GraphQL client swallows error
// responses, and a non-200 status surfaces as a raw dump on the login
// screen. What it does do is render whatever the server returns as feed
// data. So every operation gets a fixed, well-typed answer, and the only
// message it will ever see is a single "update required" notice at the top
// of the pager feed. Nothing here is authenticated and nothing reads the
// database: the endpoint can only ever return the notice.
package legacy

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"time"
)

const (
	// NoticeID is the id of the synthetic message, capcode and user.
	NoticeID = "legacy-update-notice"

	noticeAlias  = "Update required"
	noticeAgency = "RFSPager"
	noticeIcon   = "fas fa-exclamation-triangle"
	noticeColour = "#ff1003"

	// NoticeText is what the old app shows as the message body.
	NoticeText = "RFSPager has moved to a new app, and pages no longer appear in this version. " +
		"Open the App Store or Google Play, search for RFSPager and tap Update. " +
		"Your account and subscription carry over."
)

var (
	opNameRe     = regexp.MustCompile(`^\s*(?:query|mutation|subscription)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	firstFieldRe = regexp.MustCompile(`\{\s*([A-Za-z_][A-Za-z0-9_]*)`)
)

// Operation returns the root field an mst-gql document asks for. The old app
// names every operation after its root field ("query me { me { ... } }"), so
// the name is enough; anonymous documents fall back to the first field.
func Operation(query string) string {
	if m := opNameRe.FindStringSubmatch(query); m != nil {
		return m[1]
	}
	if m := firstFieldRe.FindStringSubmatch(query); m != nil {
		return m[1]
	}
	return ""
}

// GraphQL serves POST (JSON body) and GET (?query=) like Lighthouse did.
func GraphQL(now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var query string
		if r.Method == http.MethodGet {
			query = r.URL.Query().Get("query")
		} else {
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			var req struct {
				Query string `json:"query"`
			}
			// A body we cannot parse is answered with empty data rather than
			// an error, for the reasons in the package comment.
			_ = json.Unmarshal(body, &req)
			query = req.Query
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": Data(Operation(query), now())})
	}
}

// Data is the "data" object returned for an operation. Shapes follow the
// app's generated mobx-state-tree models: ids and user_id are strings,
// notification counts are integers, subscription_valid is the string "1".
func Data(op string, now time.Time) map[string]any {
	switch op {
	case "messages":
		return map[string]any{"messages": connection(now)}
	case "message":
		return map[string]any{"message": notice(now)}
	case "me":
		return map[string]any{"me": user()}
	case "login", "socialLogin":
		// A token keeps a logged-out copy of the old app moving to the feed,
		// where the notice is. It is not accepted anywhere.
		return map[string]any{op: map[string]any{"__typename": "AccessToken", "token": NoticeID}}
	case "register":
		return map[string]any{"register": map[string]any{"__typename": "RegisterResponse", "token": NoticeID}}
	case "":
		return map[string]any{}
	default:
		// Mutations and the incidents, map and photo queries: a null field,
		// never an error. The forced-logout path in the app only triggers on
		// an "Unauthenticated." error, and we never want that loop.
		return map[string]any{op: nil}
	}
}

func notice(now time.Time) map[string]any {
	return map[string]any{
		"__typename": "Message",
		"id":         NoticeID,
		"message":    NoticeText,
		"address":    noticeAgency,
		"created_at": now.UTC().Format(time.RFC3339),
		"capcode": map[string]any{
			"__typename": "Capcode",
			"id":         NoticeID,
			"alias":      noticeAlias,
			"agency":     noticeAgency,
			"icon":       noticeIcon,
			"colour":     noticeColour,
		},
		"responders": []any{},
	}
}

func connection(now time.Time) map[string]any {
	return map[string]any{
		"__typename": "MessageConnection",
		"pageInfo": map[string]any{
			"__typename":      "PageInfo",
			"hasNextPage":     false,
			"hasPreviousPage": false,
			"startCursor":     nil,
			"endCursor":       nil,
			"total":           1,
			"count":           1,
			"currentPage":     1,
			"lastPage":        1,
		},
		"edges": []any{
			map[string]any{"__typename": "MessageEdge", "cursor": NoticeID, "node": notice(now)},
		},
	}
}

func user() map[string]any {
	return map[string]any{
		"__typename":               "User",
		"id":                       NoticeID,
		"user_id":                  NoticeID,
		"name":                     noticeAlias,
		"email":                    "hello@rfspager.app",
		"notificationsCount":       0,
		"unreadNotificationsCount": 0,
		"subscription_product_id":  nil,
		"subscription_valid":       "1",
		"subscription_expiry":      nil,
		"will_renew":               false,
	}
}
