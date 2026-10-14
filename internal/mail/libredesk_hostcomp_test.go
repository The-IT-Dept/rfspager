package mail

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"text/template"
)

func TestHostCompBodies(t *testing.T) {
	started := hostCompStartedHTML("Joseph", "Budgewoi")
	for _, want := range []string{
		"<p>Hi Joseph,</p>",
		`Thanks for hosting your receiver "Budgewoi".`,
		"RFSPager Pro is on us for as long as it stays that way",
		"App Store, Google Play or by card",
		"you can cancel that subscription and you'll keep Pro while you're hosting",
		`href="https://rfspager.app/host"`,
		"reply to this email",
	} {
		if !strings.Contains(started, want) {
			t.Errorf("started body missing %q:\n%s", want, started)
		}
	}
	lapsed := hostCompLapsedHTML("Joseph", "Budgewoi")
	for _, want := range []string{
		"<p>Hi Joseph,</p>",
		`<p>Your receiver "Budgewoi" hasn't been online enough lately, so your free RFSPager Pro has paused.`,
		"online about 90% of the time over three days",
		"comes back automatically once the receiver has been steadily online for about three days",
		`href="https://rfspager.app/host"`,
		"reply to this email",
	} {
		if !strings.Contains(lapsed, want) {
			t.Errorf("lapsed body missing %q:\n%s", want, lapsed)
		}
	}
	if b := hostCompStartedHTML("", " "); !strings.HasPrefix(b, "<p>Hi,</p>") || !strings.Contains(b, "Thanks for hosting your receiver.") {
		t.Errorf("nameless started body = %s", b)
	}
	if b := hostCompLapsedHTML("", ""); !strings.Contains(b, "<p>Your receiver hasn't been online enough lately") {
		t.Errorf("nameless lapsed body = %s", b)
	}
}

// Names are user-controlled and Libredesk renders bodies as templates.
func TestHostCompBodiesSurviveTemplateRendering(t *testing.T) {
	payload := `{{ .Foo }}<script>"x"</script>`
	for kind, body := range map[string]string{
		"started": hostCompStartedHTML(payload, payload),
		"lapsed":  hostCompLapsedHTML(payload, payload),
	} {
		tmpl, err := template.New(kind).Parse(body)
		if err != nil {
			t.Fatalf("%s: body does not parse as a template: %v", kind, err)
		}
		var out strings.Builder
		if err := tmpl.Execute(&out, map[string]string{"Foo": "INJECTED"}); err != nil {
			t.Fatalf("%s: executing body: %v", kind, err)
		}
		if out.String() != body || strings.Contains(body, "{{") || strings.Contains(body, "<script>") {
			t.Errorf("%s: unsafe body: %s", kind, body)
		}
		if !strings.Contains(body, "&#123;&#123; .Foo &#125;&#125;&lt;script&gt;&#34;x&#34;&lt;/script&gt;") {
			t.Errorf("%s: escaped name missing: %s", kind, body)
		}
	}
}

func TestHostCompEmailsReplyInExistingConversation(t *testing.T) {
	for kind, send := range map[string]func(*LibredeskApplicationMailer) (string, error){
		"started": func(m *LibredeskApplicationMailer) (string, error) {
			return m.SendHostCompStarted(context.Background(), "conv-9", "member@example.invalid", "Joseph", "Budgewoi")
		},
		"lapsed": func(m *LibredeskApplicationMailer) (string, error) {
			return m.SendHostCompLapsed(context.Background(), "conv-9", "member@example.invalid", "Joseph", "Budgewoi")
		},
	} {
		srv, calls := fakeLibredesk(t, "")
		uuid, err := send(newTestApplicationMailer(t, srv.URL, 4))
		if err != nil || uuid != "conv-9" {
			t.Fatalf("%s: uuid %q, err %v", kind, uuid, err)
		}
		if len(*calls) != 1 {
			t.Fatalf("%s: calls = %+v", kind, *calls)
		}
		c := (*calls)[0]
		if c.method != http.MethodPost || c.path != "/api/v1/conversations/conv-9/messages" || c.body["private"] != false || c.body["sender_type"] != "agent" {
			t.Fatalf("%s: reply call = %+v", kind, c)
		}
		if msg, _ := c.body["message"].(string); !strings.Contains(msg, `"Budgewoi"`) {
			t.Fatalf("%s: message = %q", kind, msg)
		}
	}
}

func TestHostCompEmailsStartConversationWithoutOne(t *testing.T) {
	for kind, tc := range map[string]struct {
		send    func(*LibredeskApplicationMailer) (string, error)
		subject string
	}{
		"started": {func(m *LibredeskApplicationMailer) (string, error) {
			return m.SendHostCompStarted(context.Background(), "", "member@example.invalid", "Joseph", "Budgewoi")
		}, "Thanks for hosting: RFSPager Pro is on us"},
		"lapsed": {func(m *LibredeskApplicationMailer) (string, error) {
			return m.SendHostCompLapsed(context.Background(), "", "member@example.invalid", "Joseph", "Budgewoi")
		}, "Your free RFSPager Pro has paused"},
	} {
		srv, calls := fakeLibredesk(t, "")
		uuid, err := tc.send(newTestApplicationMailer(t, srv.URL, 4))
		if err != nil || uuid != "conv-1" {
			t.Fatalf("%s: uuid %q, err %v", kind, uuid, err)
		}
		if len(*calls) != 3 {
			t.Fatalf("%s: calls = %+v", kind, *calls)
		}
		create := (*calls)[0]
		if create.path != "/api/v1/conversations" || create.body["subject"] != tc.subject || create.body["contact_email"] != "member@example.invalid" {
			t.Fatalf("%s: create call = %+v", kind, create)
		}
		if (*calls)[1].path != "/api/v1/conversations/conv-1/tags" || (*calls)[2].path != "/api/v1/conversations/conv-1/assignee/team" {
			t.Fatalf("%s: calls = %+v", kind, *calls)
		}
		assertNeverResolved(t, *calls)
	}
}

func TestHostCompEmailsReportFailuresAndRejectUnsafeInput(t *testing.T) {
	srv, _ := fakeLibredesk(t, "/api/v1/conversations/conv-9/messages")
	m := newTestApplicationMailer(t, srv.URL, 0)
	if uuid, err := m.SendHostCompLapsed(context.Background(), "conv-9", "member@example.invalid", "", "a"); err == nil || uuid != "" {
		t.Fatalf("reply failure: uuid %q, err %v", uuid, err)
	}

	srv, calls := fakeLibredesk(t, "")
	m = newTestApplicationMailer(t, srv.URL, 0)
	if _, err := m.SendHostCompStarted(context.Background(), "", "member@example.invalid\r\nBcc: victim@example.invalid", "", "a"); err == nil {
		t.Fatal("accepted a recipient with a header injection")
	}
	if _, err := m.SendHostCompLapsed(context.Background(), "conv-9", "not an email", "", "a"); err == nil {
		t.Fatal("accepted an invalid recipient")
	}
	var nilMailer *LibredeskApplicationMailer
	if _, err := nilMailer.SendHostCompStarted(context.Background(), "", "member@example.invalid", "", "a"); err == nil {
		t.Fatal("nil mailer claimed to send")
	}
	if _, err := nilMailer.SendHostCompLapsed(context.Background(), "", "member@example.invalid", "", "a"); err == nil {
		t.Fatal("nil mailer claimed to send")
	}
	if len(*calls) != 0 {
		t.Fatalf("unsafe input reached Libredesk: %+v", *calls)
	}
}
