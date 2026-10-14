package iap

import "testing"

func TestAppleNotificationHasTransaction(t *testing.T) {
	var test AppleNotificationPayload
	test.NotificationType = "TEST"
	if appleNotificationHasTransaction(&test) {
		t.Error("TEST notifications carry no transaction and must be acknowledged, not rejected")
	}
	if appleNotificationHasTransaction(nil) {
		t.Error("nil must not count as a transaction")
	}
	var renew AppleNotificationPayload
	renew.NotificationType = "DID_RENEW"
	renew.Data.SignedTransactionInfo = "eyJhbGciOiJFUzI1NiJ9.e30.sig"
	if !appleNotificationHasTransaction(&renew) {
		t.Error("a renewal carries a transaction")
	}
}
