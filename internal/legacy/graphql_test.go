package legacy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOperation(t *testing.T) {
	cases := map[string]string{
		"query me { me { id } }": "me",
		"query messages($first: Int!, $after: String) { messages(first: $first) { edges { node { id } } } }": "messages",
		"mutation deleteInstallation($id: ID!) { deleteInstallation(id: $id) { id } }":                       "deleteInstallation",
		"\n  mutation login($input: LoginInput) { login(input: $input) { token } }":                          "login",
		"{ me { id } }":      "me",
		"":                   "",
		"not graphql at all": "",
	}
	for q, want := range cases {
		if got := Operation(q); got != want {
			t.Errorf("Operation(%q) = %q, want %q", q, got, want)
		}
	}
}

func TestFeedNotice(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	d := Data("messages", now)
	conn := d["messages"].(map[string]any)
	edges := conn["edges"].([]any)
	if len(edges) != 1 {
		t.Fatalf("want exactly one edge, got %d", len(edges))
	}
	node := edges[0].(map[string]any)["node"].(map[string]any)
	if node["id"] != NoticeID || node["message"] != NoticeText || node["created_at"] != "2026-10-14T12:00:00Z" {
		t.Errorf("unexpected node: %v", node)
	}
	capcode := node["capcode"].(map[string]any)
	if capcode["alias"] != "Update required" || capcode["agency"] != "RFSPager" {
		t.Errorf("unexpected capcode: %v", capcode)
	}
	if conn["pageInfo"].(map[string]any)["hasNextPage"] != false {
		t.Errorf("notice must not paginate")
	}
	if Data("message", now)["message"].(map[string]any)["id"] != NoticeID {
		t.Errorf("message detail should return the notice")
	}
}

func TestOtherOperations(t *testing.T) {
	now := time.Now()
	me := Data("me", now)["me"].(map[string]any)
	if me["id"] != NoticeID || me["subscription_valid"] != "1" || me["notificationsCount"] != 0 {
		t.Errorf("unexpected me: %v", me)
	}
	if tok := Data("login", now)["login"].(map[string]any)["token"]; tok != NoticeID {
		t.Errorf("login token = %v", tok)
	}
	if v, ok := Data("deleteInstallation", now)["deleteInstallation"]; !ok || v != nil {
		t.Errorf("deleteInstallation must be a present null field, got %v ok=%v", v, ok)
	}
	if len(Data("", now)) != 0 {
		t.Errorf("unknown document should give empty data")
	}
}

func TestHandler(t *testing.T) {
	h := GraphQL(func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) })

	body := `{"query":"query messages($first: Int!) { messages(first: $first) { edges { node { id message } } } }","variables":{"first":25}}`
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type %q: the app only parses GraphQL JSON content types", ct)
	}
	var resp struct {
		Data   map[string]any `json:"data"`
		Errors []any          `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Errors != nil {
		t.Fatalf("must never return errors: %v", resp.Errors)
	}
	node := resp.Data["messages"].(map[string]any)["edges"].([]any)[0].(map[string]any)["node"].(map[string]any)
	if node["id"] != NoticeID {
		t.Errorf("node id = %v", node["id"])
	}

	// Malformed body: still 200 with empty data, never an error.
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader("{not json")))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"data":{}}` {
		t.Errorf("malformed body: %d %s", rec.Code, rec.Body.String())
	}

	// GET with ?query= like Lighthouse supported.
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/graphql?query=query+me+%7B+me+%7B+id+%7D+%7D", nil))
	if !strings.Contains(rec.Body.String(), `"me":{`) {
		t.Errorf("GET: %s", rec.Body.String())
	}
}
