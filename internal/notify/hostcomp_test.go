package notify

import (
	"context"
	"testing"
)

func TestHostCompPushCopy(t *testing.T) {
	for _, tc := range []struct {
		name, title, body string
		fn                func(string) (string, string)
	}{
		{"Budgewoi", "Pro is on us", `Thanks for hosting "Budgewoi". You've got RFSPager Pro free while it stays online.`, HostCompStartedPush},
		{"", "Pro is on us", "Thanks for hosting your receiver. You've got RFSPager Pro free while it stays online.", HostCompStartedPush},
		{"Budgewoi", "Free Pro paused", `"Budgewoi" hasn't been online enough lately. Pro comes back once it's been steadily online for about three days.`, HostCompLapsedPush},
		{"", "Free Pro paused", "Your receiver hasn't been online enough lately. Pro comes back once it's been steadily online for about three days.", HostCompLapsedPush},
		{"Hilltop Brigade", "Pro is on us", `Thanks for supporting "Hilltop Brigade". You've got RFSPager Pro free while it stays online.`, HostCompSupporterStartedPush},
		{"", "Pro is on us", "Thanks for supporting the receiver. You've got RFSPager Pro free while it stays online.", HostCompSupporterStartedPush},
	} {
		title, body := tc.fn(tc.name)
		if title != tc.title || body != tc.body {
			t.Errorf("%q: got %q / %q", tc.name, title, body)
		}
	}
}

// A notifier with no database (or a nil one) skips rather than panics.
func TestHostCompPushWithoutDatabaseIsANoop(t *testing.T) {
	var nilNotifier *Notifier
	nilNotifier.NotifyHostCompStarted(context.Background(), 1, "x")
	nilNotifier.NotifyHostCompSupporterStarted(context.Background(), 1, "x")
	(&Notifier{}).NotifyHostCompLapsed(context.Background(), 1, "x")
}
