package notify

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsBadTokenError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		bad  bool
	}{
		{"FCM unregistered via sentinel", fmt.Errorf("sending FCM message: %w: %w", ErrBadToken, errors.New("Requested entity was not found.")), true},
		{"FCM NotRegistered text as logged in prod", errors.New("GCM: sending FCM message: NotRegistered"), true},
		{"APN unregistered", errors.New("APN: APN notification not sent: Unregistered (status 410)"), true},
		{"APN bad device token", errors.New("APN notification not sent: BadDeviceToken (status 400)"), true},
		{"FCM quota is transient", errors.New("sending FCM message: Quota exceeded"), false},
		{"network is transient", errors.New("sending FCM message: context deadline exceeded"), false},
	}
	for _, tc := range cases {
		if got := isBadTokenError(tc.err); got != tc.bad {
			t.Errorf("%s: isBadTokenError=%v want %v", tc.name, got, tc.bad)
		}
	}
}
