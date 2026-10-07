package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type libredeskCall struct {
	method, path, auth string
	body               map[string]any
}

func fakeLibredesk(t *testing.T, failPath string) (*httptest.Server, *[]libredeskCall) {
	t.Helper()
	var mu sync.Mutex
	calls := []libredeskCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		calls = append(calls, libredeskCall{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
		mu.Unlock()
		if r.URL.Path == failPath {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"status":"error","message":"permission denied"}`))
			return
		}
		if r.URL.Path == "/api/v1/conversations" {
			_, _ = w.Write([]byte(`{"status":"success","data":{"uuid":"conv-1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestLibredeskPasswordResetCreatesTagsAndResolves(t *testing.T) {
	srv, calls := fakeLibredesk(t, "")
	m, err := NewLibredeskPasswordResetMailer(srv.URL, "key", "secret", 8, 19)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SendPasswordReset(context.Background(), "member@example.invalid", "Joseph", "https://app.example/reset-password?token=abc_-1", "abc_-1"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 3 {
		t.Fatalf("calls = %+v", *calls)
	}
	create := (*calls)[0]
	if create.method != http.MethodPost || create.path != "/api/v1/conversations" || create.auth != "token key:secret" {
		t.Fatalf("create call = %+v", create)
	}
	b := create.body
	if b["inbox_id"] != float64(8) || b["agent_id"] != float64(19) || b["initiator"] != "agent" ||
		b["contact_email"] != "member@example.invalid" || b["first_name"] != "Joseph" {
		t.Fatalf("create body = %+v", b)
	}
	if content, _ := b["content"].(string); !strings.Contains(content, `href="https://app.example/reset-password?token=abc_-1"`) || strings.Contains(content, "{{") {
		t.Fatalf("content = %q", content)
	}
	if c := (*calls)[1]; c.method != http.MethodPost || c.path != "/api/v1/conversations/conv-1/tags" || c.body["action"] != "add_tags" {
		t.Fatalf("tag call = %+v", c)
	}
	if c := (*calls)[2]; c.method != http.MethodPut || c.path != "/api/v1/conversations/conv-1/status" || c.body["status"] != "Resolved" {
		t.Fatalf("status call = %+v", c)
	}
}

func TestLibredeskPasswordResetFallsBackToMailboxName(t *testing.T) {
	srv, calls := fakeLibredesk(t, "")
	m, _ := NewLibredeskPasswordResetMailer(srv.URL, "key", "secret", 8, 19)
	if err := m.SendPasswordReset(context.Background(), "member@example.invalid", "  ", "https://app.example/r?token=x", "x"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0].body["first_name"]; got != "member" {
		t.Fatalf("first_name = %v", got)
	}
}

func TestLibredeskPasswordResetReportsCreateFailure(t *testing.T) {
	srv, calls := fakeLibredesk(t, "/api/v1/conversations")
	m, _ := NewLibredeskPasswordResetMailer(srv.URL, "key", "secret", 8, 19)
	err := m.SendPasswordReset(context.Background(), "member@example.invalid", "", "https://app.example/r?token=x", "x")
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("follow-up calls made after a failed create: %+v", *calls)
	}
}

func TestLibredeskPasswordResetToleratesTagFailure(t *testing.T) {
	srv, calls := fakeLibredesk(t, "/api/v1/conversations/conv-1/tags")
	m, _ := NewLibredeskPasswordResetMailer(srv.URL, "key", "secret", 8, 19)
	if err := m.SendPasswordReset(context.Background(), "member@example.invalid", "", "https://app.example/r?token=x", "x"); err != nil {
		t.Fatalf("a tagging failure must not fail the reset: %v", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("resolve was skipped: %+v", *calls)
	}
}

func TestLibredeskPasswordResetRejectsUnsafeInput(t *testing.T) {
	for _, tc := range []struct{ url, key string }{
		{"http://support.example", "key"}, {"https://user:pw@support.example", "key"}, {"https://support.example", "k:ey"}, {"https://support.example", ""},
	} {
		if _, err := NewLibredeskPasswordResetMailer(tc.url, tc.key, "secret", 8, 19); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
	if _, err := NewLibredeskPasswordResetMailer("https://support.example", "key", "secret", 0, 19); err == nil {
		t.Fatal("accepted a missing inbox id")
	}
	srv, calls := fakeLibredesk(t, "")
	m, _ := NewLibredeskPasswordResetMailer(srv.URL, "key", "secret", 8, 19)
	if err := m.SendPasswordReset(context.Background(), "member@example.invalid\r\nBcc: victim@example.invalid", "", "https://app.example/r?token=x", "x"); err == nil {
		t.Fatal("accepted a recipient with a header injection")
	}
	if err := m.SendPasswordReset(context.Background(), "member@example.invalid", "", "javascript:alert(1)", "x"); err == nil {
		t.Fatal("accepted a non-HTTPS reset URL")
	}
	if len(*calls) != 0 {
		t.Fatalf("unsafe input reached Libredesk: %+v", *calls)
	}
}
