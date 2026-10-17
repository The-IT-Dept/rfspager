package mail

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"text/template"
)

func TestTemplateSafe(t *testing.T) {
	for in, want := range map[string]string{
		"Budgewoi":                  "Budgewoi",
		`<b>"Tom" & 'Jerry'</b>`:    "&lt;b&gt;&#34;Tom&#34; &amp; &#39;Jerry&#39;&lt;/b&gt;",
		"{{ .Foo }}":                "&#123;&#123; .Foo &#125;&#125;",
		`{{ printf "%s" "x" }}`:     "&#123;&#123; printf &#34;%s&#34; &#34;x&#34; &#125;&#125;",
		"{{- template \"a\" . -}}":  "&#123;&#123;- template &#34;a&#34; . -&#125;&#125;",
		"curl -fsSL https://x | sh": "curl -fsSL https://x | sh",
	} {
		got := templateSafe(in)
		if got != want {
			t.Errorf("templateSafe(%q) = %q, want %q", in, got, want)
		}
		if strings.ContainsAny(got, "{}<>\"'") {
			t.Errorf("templateSafe(%q) = %q still contains a brace, tag or quote", in, got)
		}
	}
}

// What Libredesk does with a message body: parse it as a text/template and
// execute it. User text that tried to inject an action must come out as
// literal text, not run.
func TestApplicationBodiesSurviveTemplateRendering(t *testing.T) {
	payload := "{{ .Foo }}{{ index . 0 }}"
	bodies := map[string]string{
		"received": applicationReceivedHTML(payload, ReceiverApplication{SystemName: payload, Suburb: payload, Notes: "line one\n" + payload}),
		"approved": applicationApprovedHTML(payload, payload, "curl -fsSL https://rfspager.app/api/install | sudo bash -s -- '{{.X}}'"),
	}
	for name, body := range bodies {
		tmpl, err := template.New(name).Parse(body)
		if err != nil {
			t.Fatalf("%s: body does not parse as a template: %v", name, err)
		}
		var out strings.Builder
		if err := tmpl.Execute(&out, map[string]string{"Foo": "INJECTED"}); err != nil {
			t.Fatalf("%s: executing body: %v", name, err)
		}
		if out.String() != body {
			t.Errorf("%s: template rendering changed the body", name)
		}
		if strings.Contains(out.String(), "INJECTED") || strings.Contains(out.String(), "{{") {
			t.Errorf("%s: template action survived: %s", name, out.String())
		}
		if !strings.Contains(body, "&#123;&#123; .Foo &#125;&#125;") {
			t.Errorf("%s: user text missing from the body: %s", name, body)
		}
	}
}

func TestApplicationReceivedBody(t *testing.T) {
	body := applicationReceivedHTML("Joseph", ReceiverApplication{SystemName: "Budgewoi", Suburb: "Budgewoi <NSW>", Notes: "Roof mount\r\nGood view north"})
	for _, want := range []string{
		"<p>Hi Joseph,</p>",
		"Receiver name: Budgewoi<br>",
		"Suburb: Budgewoi &lt;NSW&gt;<br>",
		"Hardware: Your own<br>",
		"Notes: Roof mount<br>Good view north</p>",
		`href="https://rfspager.app/host"`,
		`href="https://rfspager.app/coverage"`,
		"reply to this email",
		"The RFSPager team",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if body := applicationReceivedHTML("  ", ReceiverApplication{SystemName: "x", Suburb: "y"}); !strings.HasPrefix(body, "<p>Hi,</p>") || !strings.Contains(body, "Notes: None") {
		t.Errorf("nameless, note-less body = %s", body)
	}
	if strings.Contains(body, "help with the hardware") {
		t.Errorf("hardware-help paragraph in an own-hardware body:\n%s", body)
	}
	needs := applicationReceivedHTML("Joseph", ReceiverApplication{SystemName: "x", Suburb: "y", Hardware: HardwareNeeds})
	if !strings.Contains(needs, "Hardware: You'd need help with it<br>") || !strings.Contains(needs, "help with the hardware") {
		t.Errorf("needs-hardware body = %s", needs)
	}
}

func TestApplicationApprovedBody(t *testing.T) {
	cmd := "curl -fsSL https://rfspager.app/api/install | sudo bash -s -- ABC123"
	body := applicationApprovedHTML("Joseph", "Budgewoi", cmd)
	for _, want := range []string{
		"<p>Hi Joseph,</p>",
		`your receiver "Budgewoi" is approved`,
		">" + cmd + "</pre>",
		"Raspberry Pi 4 or 5",
		"RTL-SDR",
		`href="https://rfspager.app/host"`,
		"don't share it publicly",
		"reply to this email",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

func newTestApplicationMailer(t *testing.T, url string, team int) *LibredeskApplicationMailer {
	t.Helper()
	m, err := NewLibredeskApplicationMailer(url, "key", "secret", 8, 19, team)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func assertNeverResolved(t *testing.T, calls []libredeskCall) {
	t.Helper()
	for _, c := range calls {
		if strings.HasSuffix(c.path, "/status") {
			t.Fatalf("application conversation status was changed: %+v", c)
		}
	}
}

func TestApplicationReceivedCreatesTagsAssignsTeamAndStaysOpen(t *testing.T) {
	srv, calls := fakeLibredesk(t, "")
	m := newTestApplicationMailer(t, srv.URL, 4)
	uuid, err := m.SendApplicationReceived(context.Background(), "member@example.invalid", "Joseph",
		ReceiverApplication{SystemName: "{{ .Foo }}", Suburb: "Budgewoi", Notes: "Roof"})
	if err != nil {
		t.Fatal(err)
	}
	if uuid != "conv-1" {
		t.Fatalf("uuid = %q", uuid)
	}
	if len(*calls) != 3 {
		t.Fatalf("calls = %+v", *calls)
	}
	create := (*calls)[0]
	b := create.body
	if create.method != http.MethodPost || create.path != "/api/v1/conversations" || create.auth != "token key:secret" ||
		b["inbox_id"] != float64(8) || b["agent_id"] != float64(19) || b["initiator"] != "agent" ||
		b["contact_email"] != "member@example.invalid" || b["first_name"] != "Joseph" ||
		b["subject"] != "Thanks for offering to host an RFSPager receiver" {
		t.Fatalf("create call = %+v", create)
	}
	if content, _ := b["content"].(string); strings.Contains(content, "{{") || !strings.Contains(content, "&#123;&#123; .Foo &#125;&#125;") {
		t.Fatalf("content = %q", content)
	}
	if c := (*calls)[1]; c.method != http.MethodPost || c.path != "/api/v1/conversations/conv-1/tags" ||
		c.body["action"] != "add_tags" || len(c.body["tags"].([]any)) != 1 || c.body["tags"].([]any)[0] != "receiver-application" {
		t.Fatalf("tag call = %+v", c)
	}
	if c := (*calls)[2]; c.method != http.MethodPut || c.path != "/api/v1/conversations/conv-1/assignee/team" || c.body["assignee_id"] != float64(4) {
		t.Fatalf("team call = %+v", c)
	}
	assertNeverResolved(t, *calls)
}

func TestApplicationReceivedSkipsTeamWhenUnset(t *testing.T) {
	srv, calls := fakeLibredesk(t, "")
	m := newTestApplicationMailer(t, srv.URL, 0)
	if _, err := m.SendApplicationReceived(context.Background(), "member@example.invalid", "", ReceiverApplication{SystemName: "a", Suburb: "b"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %+v", *calls)
	}
	for _, c := range *calls {
		if strings.Contains(c.path, "assignee") {
			t.Fatalf("team assignment attempted with no team: %+v", c)
		}
	}
	if got := (*calls)[0].body["first_name"]; got != "member" {
		t.Fatalf("first_name = %v", got)
	}
	assertNeverResolved(t, *calls)
}

func TestApplicationReceivedToleratesTagAndTeamFailures(t *testing.T) {
	for _, fail := range []string{"/api/v1/conversations/conv-1/tags", "/api/v1/conversations/conv-1/assignee/team"} {
		srv, calls := fakeLibredesk(t, fail)
		m := newTestApplicationMailer(t, srv.URL, 4)
		uuid, err := m.SendApplicationReceived(context.Background(), "member@example.invalid", "", ReceiverApplication{SystemName: "a", Suburb: "b"})
		if err != nil || uuid != "conv-1" {
			t.Fatalf("%s failing: uuid %q, err %v", fail, uuid, err)
		}
		if len(*calls) != 3 {
			t.Fatalf("%s failing: calls = %+v", fail, *calls)
		}
	}
}

func TestApplicationReceivedReportsCreateFailure(t *testing.T) {
	srv, calls := fakeLibredesk(t, "/api/v1/conversations")
	m := newTestApplicationMailer(t, srv.URL, 4)
	uuid, err := m.SendApplicationReceived(context.Background(), "member@example.invalid", "", ReceiverApplication{SystemName: "a", Suburb: "b"})
	if err == nil || uuid != "" || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("uuid %q, err %v", uuid, err)
	}
	if len(*calls) != 1 {
		t.Fatalf("follow-up calls made after a failed create: %+v", *calls)
	}
}

func TestApplicationApprovedRepliesInExistingConversation(t *testing.T) {
	srv, calls := fakeLibredesk(t, "")
	m := newTestApplicationMailer(t, srv.URL, 4)
	cmd := "curl -fsSL https://rfspager.app/api/install | sudo bash -s -- ABC"
	uuid, err := m.SendApplicationApproved(context.Background(), "conv-9", "member@example.invalid", "Joseph", "Budgewoi", cmd)
	if err != nil {
		t.Fatal(err)
	}
	if uuid != "conv-9" {
		t.Fatalf("uuid = %q, want the existing conversation", uuid)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %+v", *calls)
	}
	c := (*calls)[0]
	if c.method != http.MethodPost || c.path != "/api/v1/conversations/conv-9/messages" ||
		c.body["private"] != false || c.body["sender_type"] != "agent" {
		t.Fatalf("reply call = %+v", c)
	}
	// Libredesk rejects an email reply without recipients (HTTP 500 "`to` empty").
	if to, _ := c.body["to"].([]any); len(to) != 1 || to[0] != "member@example.invalid" {
		t.Fatalf("reply to = %#v, want the applicant", c.body["to"])
	}
	if msg, _ := c.body["message"].(string); !strings.Contains(msg, cmd+"</pre>") || !strings.Contains(msg, `"Budgewoi" is approved`) {
		t.Fatalf("message = %q", msg)
	}
}

func TestApplicationApprovedStartsConversationWithoutOne(t *testing.T) {
	srv, calls := fakeLibredesk(t, "")
	m := newTestApplicationMailer(t, srv.URL, 4)
	uuid, err := m.SendApplicationApproved(context.Background(), "", "member@example.invalid", "Joseph", "Budgewoi", "curl x | sudo bash -s -- ABC")
	if err != nil {
		t.Fatal(err)
	}
	if uuid != "conv-1" {
		t.Fatalf("uuid = %q, want the new conversation", uuid)
	}
	if len(*calls) != 3 {
		t.Fatalf("calls = %+v", *calls)
	}
	create := (*calls)[0]
	if create.path != "/api/v1/conversations" || create.body["subject"] != "Your RFSPager receiver is approved" {
		t.Fatalf("create call = %+v", create)
	}
	if content, _ := create.body["content"].(string); !strings.Contains(content, "ABC</pre>") {
		t.Fatalf("content = %q", content)
	}
	if (*calls)[1].path != "/api/v1/conversations/conv-1/tags" || (*calls)[2].path != "/api/v1/conversations/conv-1/assignee/team" {
		t.Fatalf("calls = %+v", *calls)
	}
	for _, c := range *calls {
		if strings.HasSuffix(c.path, "/messages") {
			t.Fatalf("replied instead of creating: %+v", c)
		}
	}
	assertNeverResolved(t, *calls)
}

func TestApplicationApprovedReportsReplyFailure(t *testing.T) {
	srv, _ := fakeLibredesk(t, "/api/v1/conversations/conv-9/messages")
	m := newTestApplicationMailer(t, srv.URL, 0)
	uuid, err := m.SendApplicationApproved(context.Background(), "conv-9", "member@example.invalid", "", "a", "curl x")
	if err == nil || uuid != "" || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("uuid %q, err %v", uuid, err)
	}
}

func TestApplicationMailerRejectsUnsafeInput(t *testing.T) {
	if _, err := NewLibredeskApplicationMailer("http://support.example", "key", "secret", 8, 19, 0); err == nil {
		t.Fatal("accepted a non-HTTPS URL")
	}
	if _, err := NewLibredeskApplicationMailer("https://support.example", "key", "secret", 8, 19, -1); err == nil {
		t.Fatal("accepted a negative team id")
	}
	srv, calls := fakeLibredesk(t, "")
	m := newTestApplicationMailer(t, srv.URL, 0)
	if _, err := m.SendApplicationReceived(context.Background(), "member@example.invalid\r\nBcc: victim@example.invalid", "", ReceiverApplication{}); err == nil {
		t.Fatal("accepted a recipient with a header injection")
	}
	if _, err := m.SendApplicationApproved(context.Background(), "conv-9", "not an email", "", "a", "curl x"); err == nil {
		t.Fatal("accepted an invalid recipient")
	}
	if _, err := m.SendApplicationApproved(context.Background(), "conv-9", "member@example.invalid", "", "a", " "); err == nil {
		t.Fatal("accepted an empty install command")
	}
	var nilMailer *LibredeskApplicationMailer
	if _, err := nilMailer.SendApplicationReceived(context.Background(), "member@example.invalid", "", ReceiverApplication{}); err == nil {
		t.Fatal("nil mailer claimed to send")
	}
	if len(*calls) != 0 {
		t.Fatalf("unsafe input reached Libredesk: %+v", *calls)
	}
}
