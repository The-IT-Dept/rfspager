package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

const (
	// ReceiverApplicationTag must already exist in Libredesk; unknown tag
	// names are silently ignored there rather than created.
	ReceiverApplicationTag = "receiver-application"

	applicationReceivedSubject = "Thanks for offering to host an RFSPager receiver"
	applicationApprovedSubject = "Your RFSPager receiver is approved"
	hostCompStartedSubject     = "Thanks for hosting: RFSPager Pro is on us"
	hostCompLapsedSubject      = "Your free RFSPager Pro has paused"

	hostURL     = "https://rfspager.app/host"
	coverageURL = "https://rfspager.app/coverage"
)

// Hardware answers on a receiver application (systems.hardware).
const (
	HardwareOwn   = "own"   // has a Pi, a dongle and an antenna
	HardwareNeeds = "needs" // would host one if we help with the gear
)

// ReceiverApplication is what someone submitted when they offered to host a
// receiver. Every field is user-controlled.
type ReceiverApplication struct {
	SystemName string
	Suburb     string
	Notes      string
	Hardware   string // HardwareOwn or HardwareNeeds
}

// LibredeskApplicationMailer emails people who apply to host a receiver,
// through the same Libredesk inbox as password resets. The thank-you starts
// a conversation that stays Open, so it lands in the support queue and the
// approval (with the install command) is a reply in the same thread.
//
// Beyond the reset mailer's permissions, the API agent needs messages:write
// (the approval reply) and conversations:update_team_assignee (when a team
// is configured). Assigning a team makes Libredesk drop the user assignee,
// so the conversation lands unassigned in the team queue and the agent
// loses conversations:read_assigned access to it. To reply later the agent
// must be a member of that team with conversations:read_team_all, and its
// availability set to away_manual so round-robin never hands it customer
// conversations. Without that, replies fail with 403 and are logged.
type LibredeskApplicationMailer struct {
	*libredeskClient
	teamID int
}

// NewLibredeskApplicationMailer takes the reset mailer's Libredesk settings
// plus an optional team (0 = leave the conversation unassigned to a team).
func NewLibredeskApplicationMailer(baseURL, apiKey, apiSecret string, inboxID, agentID, teamID int) (*LibredeskApplicationMailer, error) {
	if teamID < 0 {
		return nil, errors.New("Libredesk team ID must not be negative")
	}
	c, err := newLibredeskClient(baseURL, apiKey, apiSecret, inboxID, agentID)
	if err != nil {
		return nil, err
	}
	return &LibredeskApplicationMailer{libredeskClient: c, teamID: teamID}, nil
}

// SendApplicationReceived thanks the applicant in a new Open conversation
// and returns its uuid, to be stored against the system so the approval can
// reply in the same thread.
func (m *LibredeskApplicationMailer) SendApplicationReceived(ctx context.Context, to, name string, app ReceiverApplication) (string, error) {
	if m == nil {
		return "", errors.New("application mailer is not configured")
	}
	to, err := safeEmailAddress(to)
	if err != nil {
		return "", err
	}
	return m.startConversation(ctx, to, name, applicationReceivedSubject, applicationReceivedHTML(name, app))
}

// SendApplicationApproved sends the install command. With a conversation
// uuid it replies in that thread and returns the same uuid; without one
// (applied before these emails existed, or the thank-you failed) it starts
// a new conversation and returns its uuid.
func (m *LibredeskApplicationMailer) SendApplicationApproved(ctx context.Context, conversationUUID, to, name, systemName, installCommand string) (string, error) {
	if m == nil {
		return "", errors.New("application mailer is not configured")
	}
	if strings.TrimSpace(installCommand) == "" {
		return "", errors.New("install command is required")
	}
	to, err := safeEmailAddress(to)
	if err != nil {
		return "", err
	}
	return m.replyOrStart(ctx, conversationUUID, to, name, applicationApprovedSubject, applicationApprovedHTML(name, systemName, installCommand))
}

// SendHostCompStarted tells a receiver host they have Pro free while the
// receiver stays online: a reply in the receiver's support conversation, or
// a new conversation (whose uuid is returned) when it has none.
func (m *LibredeskApplicationMailer) SendHostCompStarted(ctx context.Context, conversationUUID, to, name, systemName string) (string, error) {
	if m == nil {
		return "", errors.New("application mailer is not configured")
	}
	to, err := safeEmailAddress(to)
	if err != nil {
		return "", err
	}
	return m.replyOrStart(ctx, conversationUUID, to, name, hostCompStartedSubject, hostCompStartedHTML(name, systemName))
}

// SendHostCompLapsed tells a receiver host their free Pro has paused because
// the receiver hasn't been online enough. Same threading as
// SendHostCompStarted.
func (m *LibredeskApplicationMailer) SendHostCompLapsed(ctx context.Context, conversationUUID, to, name, systemName string) (string, error) {
	if m == nil {
		return "", errors.New("application mailer is not configured")
	}
	to, err := safeEmailAddress(to)
	if err != nil {
		return "", err
	}
	return m.replyOrStart(ctx, conversationUUID, to, name, hostCompLapsedSubject, hostCompLapsedHTML(name, systemName))
}

// replyOrStart replies in conversationUUID and returns it, or, when it is
// empty, starts a new conversation (tagged, team-assigned, left Open) and
// returns that one's uuid. to must already be validated.
func (m *LibredeskApplicationMailer) replyOrStart(ctx context.Context, conversationUUID, to, name, subject, body string) (string, error) {
	if conversationUUID == "" {
		return m.startConversation(ctx, to, name, subject, body)
	}
	if err := m.do(ctx, http.MethodPost, conversationPath(conversationUUID)+"/messages", map[string]any{
		"message":     body,
		"private":     false,
		"sender_type": "agent",
	}, nil); err != nil {
		return "", fmt.Errorf("replying in Libredesk conversation: %w", err)
	}
	return conversationUUID, nil
}

// startConversation creates the conversation, then tags it and assigns the
// team. Those two are best-effort: the email is already on its way. The
// status is deliberately left Open so the application shows in the queue.
func (m *LibredeskApplicationMailer) startConversation(ctx context.Context, to, name, subject, body string) (string, error) {
	conversation, err := m.createConversation(ctx, to, name, subject, body)
	if err != nil {
		return "", err
	}
	if err := m.addTag(ctx, conversation, ReceiverApplicationTag); err != nil {
		slog.Warn("receiver application: tagging Libredesk conversation failed", "conversation", conversation, "error", err)
	}
	if m.teamID > 0 {
		if err := m.do(ctx, http.MethodPut, conversationPath(conversation)+"/assignee/team", map[string]int{"assignee_id": m.teamID}, nil); err != nil {
			slog.Warn("receiver application: assigning Libredesk conversation to team failed", "conversation", conversation, "team", m.teamID, "error", err)
		}
	}
	return conversation, nil
}

// greeting is "Hi Name," or "Hi," when there is no name.
func greeting(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return `<p>Hi ` + templateSafe(name) + `,</p>`
	}
	return `<p>Hi,</p>`
}

// multiline escapes user text and keeps its line breaks.
func multiline(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n")
	return strings.ReplaceAll(templateSafe(s), "\n", "<br>")
}

// applicationReceivedHTML is the thank-you body. Every user-controlled value
// goes through templateSafe (Libredesk renders the body as a template).
func applicationReceivedHTML(name string, app ReceiverApplication) string {
	notes := "None"
	if strings.TrimSpace(app.Notes) != "" {
		notes = multiline(app.Notes)
	}
	hardware, hardwareNext := "Your own", ""
	if app.Hardware == HardwareNeeds {
		hardware = "You'd need help with it"
		hardwareNext = `<p>You mentioned you'd need help with the hardware. We sometimes lend, donate or buy the gear for places we really need coverage, so we'll let you know what we can do when we look at your application.</p>`
	}
	return greeting(name) +
		`<p>Thanks for offering to host an RFSPager receiver. Every receiver lets us hear pages from more of NSW, so it really does make a difference.</p>` +
		`<p>Here's what you sent us:</p>` +
		`<p>Receiver name: ` + templateSafe(strings.TrimSpace(app.SystemName)) + `<br>` +
		`Suburb: ` + templateSafe(strings.TrimSpace(app.Suburb)) + `<br>` +
		`Hardware: ` + hardware + `<br>` +
		`Notes: ` + notes + `</p>` +
		hardwareNext +
		`<p>What happens next: we'll review your application, usually within a few days. Once it's approved, we'll reply in this email thread with the command that sets up the receiver on your Raspberry Pi.</p>` +
		`<p>If you have any questions in the meantime, just reply to this email.</p>` +
		`<p>You can check on your application at <a href="` + hostURL + `">rfspager.app/host</a>, and see where we can already hear pages at <a href="` + coverageURL + `">rfspager.app/coverage</a>.</p>` +
		`<p>Thanks,<br>The RFSPager team</p>`
}

// applicationApprovedHTML is the approval body with the install command.
// The command is server-built but escaped the same way as user text.
func applicationApprovedHTML(name, systemName, installCommand string) string {
	return greeting(name) +
		`<p>Good news: your receiver "` + templateSafe(strings.TrimSpace(systemName)) + `" is approved.</p>` +
		`<p>To set it up, run this command on your Raspberry Pi:</p>` +
		`<pre style="white-space:pre-wrap;word-break:break-all">` + templateSafe(installCommand) + `</pre>` +
		`<p>You'll need a Raspberry Pi 4 or 5 running Raspberry Pi OS (64-bit), with the RTL-SDR plugged in. The command installs the receiver as a service and connects it to RFSPager. It takes a few minutes.</p>` +
		`<p>Once it's online, your receiver shows up at <a href="` + hostURL + `">rfspager.app/host</a> and in the app.</p>` +
		`<p>The command contains your receiver's private key, so please don't share it publicly.</p>` +
		`<p>If you get stuck, just reply to this email and we'll help.</p>` +
		`<p>Thanks for helping us cover more of NSW,<br>The RFSPager team</p>`
}

// receiverName is `your receiver "Name"`, or `your receiver` without a name.
func receiverName(systemName string) string {
	if systemName = strings.TrimSpace(systemName); systemName != "" {
		return `your receiver "` + templateSafe(systemName) + `"`
	}
	return "your receiver"
}

// hostCompStartedHTML is the email when free Pro starts for a host.
func hostCompStartedHTML(name, systemName string) string {
	return greeting(name) +
		`<p>Thanks for hosting ` + receiverName(systemName) + `. It's online and picking up pages, so RFSPager Pro is on us for as long as it stays that way. It's already switched on for your account; there's nothing you need to do.</p>` +
		`<p>If you already pay for Pro through the App Store, Google Play or by card on our website, you can cancel that subscription and you'll keep Pro while you're hosting. We can't cancel it for you: do it in your App Store or Google Play subscription settings, or from your account at rfspager.app.</p>` +
		`<p>If the receiver goes offline for a while, free Pro pauses about a week later, and comes back by itself once it's steadily online again. You can see how it's going at <a href="` + hostURL + `">rfspager.app/host</a>.</p>` +
		`<p>If you have any questions, just reply to this email.</p>` +
		`<p>Thanks for helping us cover more of NSW,<br>The RFSPager team</p>`
}

// hostCompLapsedHTML is the email when a host's free Pro pauses.
func hostCompLapsedHTML(name, systemName string) string {
	return greeting(name) +
		`<p>` + upperFirst(receiverName(systemName)) + ` hasn't been online enough lately, so your free RFSPager Pro has paused. To keep it, a receiver needs to be online about 90% of the time over three days.</p>` +
		`<p>It comes back automatically once the receiver has been steadily online for about three days, so there's nothing else to do once it's back up. It's worth checking that the Raspberry Pi has power and internet and the RTL-SDR is plugged in. You can see its status at <a href="` + hostURL + `">rfspager.app/host</a>.</p>` +
		`<p>If you need a hand, just reply to this email and we'll help.</p>` +
		`<p>Thanks,<br>The RFSPager team</p>`
}

func upperFirst(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}
