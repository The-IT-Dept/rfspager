package iap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

// signSelfSignedJWS produces a valid ES256 JWS whose x5c leaf is a freshly
// generated certificate that does NOT chain to Apple's root. It isolates the
// two things verifyAppleJWS must enforce: the signature, and the chain.
func signSelfSignedJWS(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Attacker Signer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	header, _ := json.Marshal(map[string]any{
		"alg": "ES256",
		"x5c": []string{base64.StdEncoding.EncodeToString(der)},
	})
	payload, _ := json.Marshal(map[string]any{
		"transactionId":         "t",
		"originalTransactionId": "o",
		"bundleId":              "io.devlan.rfspager",
		"productId":             "io.devlan.rfspager.monthly",
		"expiresDate":           time.Now().Add(time.Hour).UnixMilli(),
		"environment":           "Production",
	})
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	hash := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// A correctly signed JWS whose certificate is unknown must be rejected: proves
// the x5c chain is actually verified and we are not merely trusting a
// self-asserted certificate embedded in the token.
func TestVerifyAppleJWSRejectsUntrustedCertificate(t *testing.T) {
	if _, err := verifyAppleJWS(signSelfSignedJWS(t)); err == nil || !strings.Contains(err.Error(), "chain") {
		t.Fatalf("untrusted cert accepted, err = %v", err)
	}
}
