package iap

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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

func (c *AppleClient) baseURL() string {
	if c.production {
		return "https://api.storekit.itunes.apple.com"
	}
	return "https://api.storekit-sandbox.itunes.apple.com"
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

// GetTransactionInfo fetches and validates a transaction from Apple.
func (c *AppleClient) GetTransactionInfo(transactionID string) (*ValidationResult, error) {
	authToken, err := c.generateAuthToken()
	if err != nil {
		return nil, fmt.Errorf("generating auth token: %w", err)
	}

	url := fmt.Sprintf("%s/inApps/v1/transactions/%s", c.baseURL(), transactionID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Apple API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Apple API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		SignedTransactionInfo string `json:"signedTransactionInfo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decoding Apple response: %w", err)
	}

	txn, err := DecodeAppleJWS(apiResp.SignedTransactionInfo)
	if err != nil {
		return nil, fmt.Errorf("decoding transaction JWS: %w", err)
	}

	// Check bundle ID
	if txn.BundleID != c.bundleID {
		return nil, fmt.Errorf("bundle ID mismatch: got %s, want %s", txn.BundleID, c.bundleID)
	}

	// Check revocation
	if txn.RevocationDate != nil {
		return &ValidationResult{Valid: false, Platform: "apple"}, nil
	}

	expiresAt := time.UnixMilli(txn.ExpiresDate)
	return &ValidationResult{
		Valid:                 expiresAt.After(time.Now()),
		ProductID:             txn.ProductID,
		Platform:              "apple",
		Environment:           txn.Environment,
		ExpiresAt:             &expiresAt,
		TransactionID:         txn.TransactionID,
		OriginalTransactionID: txn.OriginalTransactionID,
		WillRenew:             true, // Will be updated by renewal info if available
	}, nil
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
