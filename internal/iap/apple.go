package iap

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// AppleClient validates purchases against Apple's App Store Server API v2.
type AppleClient struct {
	keyID      string
	issuerID   string
	privateKey *ecdsa.PrivateKey
	bundleID   string
	production bool
	apiBase    string // tests only: replaces Apple's hosts
}

func NewAppleClient(keyID, issuerID, privateKeyPEM, bundleID string, production bool) (*AppleClient, error) {
	if keyID == "" || issuerID == "" || privateKeyPEM == "" {
		slog.Warn("Apple IAP credentials not configured")
		return nil, nil
	}

	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode Apple private key PEM")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsing Apple private key: %w", err)
		}
	}

	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("Apple private key is not ECDSA")
	}

	return &AppleClient{
		keyID:      keyID,
		issuerID:   issuerID,
		privateKey: ecKey,
		bundleID:   bundleID,
		production: production,
	}, nil
}

// generateAuthToken creates a JWT for authenticating with Apple's App Store Server API.
func (c *AppleClient) generateAuthToken() (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": c.issuerID,
		"iat": now.Unix(),
		"exp": now.Add(30 * time.Minute).Unix(),
		"aud": "appstoreconnect-v1",
		"bid": c.bundleID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = c.keyID
	return token.SignedString(c.privateKey)
}

// GetSubscriptionStatus returns the current state of the subscription that
// transactionID belongs to, verified against Apple's signatures.
//
// It asks the Get All Subscription Statuses endpoint rather than the
// transaction-info one: looking a subscription up by its original transaction
// ID returns the *first* purchase (long expired for any renewing subscriber),
// and only the status endpoint carries the renewal info that says whether it
// will auto-renew. environment is the transaction's own ("Production" or
// "Sandbox"); when it is unknown, production is tried first and sandbox on a
// not-found, as Apple recommends.
func (c *AppleClient) GetSubscriptionStatus(transactionID, environment string) (*ValidationResult, error) {
	switch environment {
	case "Sandbox":
		return c.subscriptionStatus(appleSandboxURL, transactionID)
	case "Production":
		return c.subscriptionStatus(appleProductionURL, transactionID)
	}
	first, second := appleProductionURL, appleSandboxURL
	if !c.production {
		first, second = second, first
	}
	result, err := c.subscriptionStatus(first, transactionID)
	var nf errAppleNotFound
	if errors.As(err, &nf) {
		return c.subscriptionStatus(second, transactionID)
	}
	return result, err
}

const (
	appleProductionURL = "https://api.storekit.itunes.apple.com"
	appleSandboxURL    = "https://api.storekit-sandbox.itunes.apple.com"
)

// Subscription status values from the App Store Server API.
const (
	appleStatusActive       = 1
	appleStatusExpired      = 2
	appleStatusBillingRetry = 3
	appleStatusGracePeriod  = 4
	appleStatusRevoked      = 5
)

type errAppleNotFound struct{ body string }

func (e errAppleNotFound) Error() string { return "Apple API returned 404: " + e.body }

type appleStatusResponse struct {
	Data []struct {
		LastTransactions []struct {
			OriginalTransactionID string `json:"originalTransactionId"`
			Status                int    `json:"status"`
			SignedTransactionInfo string `json:"signedTransactionInfo"`
			SignedRenewalInfo     string `json:"signedRenewalInfo"`
		} `json:"lastTransactions"`
	} `json:"data"`
}

func (c *AppleClient) subscriptionStatus(base, transactionID string) (*ValidationResult, error) {
	authToken, err := c.generateAuthToken()
	if err != nil {
		return nil, fmt.Errorf("generating auth token: %w", err)
	}

	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/inApps/v1/subscriptions/%s", base, url.PathEscape(transactionID)), nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Apple API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, errAppleNotFound{body: string(body)}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("Apple API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp appleStatusResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decoding Apple response: %w", err)
	}

	now := time.Now()
	var best, matched *ValidationResult
	for _, group := range apiResp.Data {
		for _, last := range group.LastTransactions {
			txn, err := DecodeAppleJWS(last.SignedTransactionInfo)
			if err != nil {
				return nil, fmt.Errorf("decoding transaction JWS: %w", err)
			}
			if txn.BundleID != c.bundleID {
				return nil, fmt.Errorf("bundle ID mismatch: got %s, want %s", txn.BundleID, c.bundleID)
			}
			var renewal *AppleRenewalInfoPayload
			if last.SignedRenewalInfo != "" {
				if renewal, err = DecodeRenewalInfo(last.SignedRenewalInfo); err != nil {
					return nil, fmt.Errorf("decoding renewal info JWS: %w", err)
				}
			}
			r := appleStatusResult(last.Status, txn, renewal, now)
			if txn.OriginalTransactionID == transactionID || txn.TransactionID == transactionID {
				matched = r
			}
			if best == nil || preferAppleResult(r, best) {
				best = r
			}
		}
	}
	if matched != nil {
		return matched, nil
	}
	if best == nil {
		return nil, fmt.Errorf("Apple returned no subscriptions for this transaction")
	}
	return best, nil
}

// appleStatusResult maps one subscription's status, latest transaction and
// renewal info onto a ValidationResult.
func appleStatusResult(status int, txn *AppleJWSTransactionPayload, renewal *AppleRenewalInfoPayload, now time.Time) *ValidationResult {
	expiresAt := time.UnixMilli(txn.ExpiresDate)
	valid := false
	switch status {
	case appleStatusActive:
		valid = true
	case appleStatusGracePeriod:
		// Billing failed but Apple still grants access until the grace
		// period ends; report that as the expiry.
		valid = true
		if renewal != nil && renewal.GracePeriodExpiresDate > 0 {
			expiresAt = time.UnixMilli(renewal.GracePeriodExpiresDate)
		}
	case appleStatusExpired, appleStatusBillingRetry, appleStatusRevoked:
		valid = false
	default:
		valid = expiresAt.After(now)
	}
	if txn.RevocationDate != nil {
		valid = false
	}
	return &ValidationResult{
		Valid:                 valid,
		ProductID:             txn.ProductID,
		Platform:              "apple",
		Environment:           txn.Environment,
		ExpiresAt:             &expiresAt,
		TransactionID:         txn.TransactionID,
		OriginalTransactionID: txn.OriginalTransactionID,
		WillRenew:             renewal != nil && renewal.AutoRenewStatus == 1,
	}
}

// preferAppleResult reports whether a beats b: an entitling subscription
// first, then the later expiry.
func preferAppleResult(a, b *ValidationResult) bool {
	if a.Valid != b.Valid {
		return a.Valid
	}
	return laterThan(a, b)
}

// DecodeAppleJWS verifies the JWS x5c cert chain against Apple's root CA,
// verifies the ES256 signature, and returns the decoded transaction payload.
func DecodeAppleJWS(jwsToken string) (*AppleJWSTransactionPayload, error) {
	payload, err := verifyAppleJWS(jwsToken)
	if err != nil {
		return nil, fmt.Errorf("verifying transaction JWS: %w", err)
	}
	var txn AppleJWSTransactionPayload
	if err := json.Unmarshal(payload, &txn); err != nil {
		return nil, fmt.Errorf("unmarshaling transaction: %w", err)
	}
	return &txn, nil
}

// DecodeAppleNotification verifies and decodes Apple's S2S notification signedPayload.
func DecodeAppleNotification(signedPayload string) (*AppleNotificationPayload, error) {
	payload, err := verifyAppleJWS(signedPayload)
	if err != nil {
		return nil, fmt.Errorf("verifying notification JWS: %w", err)
	}
	var notif AppleNotificationPayload
	if err := json.Unmarshal(payload, &notif); err != nil {
		return nil, fmt.Errorf("unmarshaling notification: %w", err)
	}
	return &notif, nil
}

// DecodeRenewalInfo verifies and decodes a signed renewal info JWS.
func DecodeRenewalInfo(signedRenewalInfo string) (*AppleRenewalInfoPayload, error) {
	payload, err := verifyAppleJWS(signedRenewalInfo)
	if err != nil {
		return nil, fmt.Errorf("verifying renewal info JWS: %w", err)
	}
	var info AppleRenewalInfoPayload
	if err := json.Unmarshal(payload, &info); err != nil {
		return nil, fmt.Errorf("unmarshaling renewal info: %w", err)
	}
	return &info, nil
}

// AppleSendAttempt is one delivery attempt of a notification to the URL
// configured in App Store Connect.
type AppleSendAttempt struct {
	AttemptDate       int64  `json:"attemptDate"`
	SendAttemptResult string `json:"sendAttemptResult"`
}

// AppleTestNotification is Apple's record of a TEST notification: the payload
// it sent and each delivery attempt.
type AppleTestNotification struct {
	SignedPayload string             `json:"signedPayload"`
	SendAttempts  []AppleSendAttempt `json:"sendAttempts"`
}

// AppleNotificationHistoryItem is one notification from Apple's history: the
// signed payload exactly as it was (or will be) delivered, with the attempts.
type AppleNotificationHistoryItem struct {
	SignedPayload string             `json:"signedPayload"`
	SendAttempts  []AppleSendAttempt `json:"sendAttempts"`
}

// NotificationHistory returns every notification Apple produced for the app
// between start and end (at most 180 days back), following pagination. The
// payloads are real Apple-signed notifications, so they can be run through
// the same verification as the webhook, or replayed to it.
func (c *AppleClient) NotificationHistory(environment string, start, end time.Time) ([]AppleNotificationHistoryItem, error) {
	var items []AppleNotificationHistoryItem
	token := ""
	for {
		endpoint := c.base(environment) + "/inApps/v1/notifications/history"
		if token != "" {
			endpoint += "?paginationToken=" + url.QueryEscape(token)
		}
		var page struct {
			NotificationHistory []AppleNotificationHistoryItem `json:"notificationHistory"`
			HasMore             bool                           `json:"hasMore"`
			PaginationToken     string                         `json:"paginationToken"`
		}
		body := map[string]any{"startDate": start.UnixMilli(), "endDate": end.UnixMilli()}
		if err := c.call(http.MethodPost, endpoint, body, &page); err != nil {
			return items, err
		}
		items = append(items, page.NotificationHistory...)
		if !page.HasMore || page.PaginationToken == "" {
			return items, nil
		}
		token = page.PaginationToken
	}
}

func (c *AppleClient) base(environment string) string {
	if c.apiBase != "" {
		return c.apiBase
	}
	if environment == "Sandbox" {
		return appleSandboxURL
	}
	return appleProductionURL
}

// RequestTestNotification asks Apple to send a TEST App Store Server
// Notification to the URL configured in App Store Connect for the
// environment ("Production" or "Sandbox") and returns the token that
// identifies it for TestNotificationStatus.
func (c *AppleClient) RequestTestNotification(environment string) (string, error) {
	var out struct {
		Token string `json:"testNotificationToken"`
	}
	if err := c.call(http.MethodPost, c.base(environment)+"/inApps/v1/notifications/test", nil, &out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", errors.New("Apple returned no testNotificationToken")
	}
	return out.Token, nil
}

// TestNotificationStatus reports Apple's delivery attempts for a test
// notification. Apple answers 404 until it has processed the request.
func (c *AppleClient) TestNotificationStatus(environment, token string) (*AppleTestNotification, error) {
	var out AppleTestNotification
	if err := c.call(http.MethodGet, c.base(environment)+"/inApps/v1/notifications/test/"+url.PathEscape(token), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AppleClient) call(method, endpoint string, body any, out any) error {
	authToken, err := c.generateAuthToken()
	if err != nil {
		return fmt.Errorf("generating auth token: %w", err)
	}
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, endpoint, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+authToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling Apple API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Apple API returned %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
