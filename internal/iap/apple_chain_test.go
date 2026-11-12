package iap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// testChain is a stand-in for Apple's App Store signing chain: a root, a
// WWDR-style intermediate and a signing leaf, each optionally carrying the
// Apple marker extension the verifier requires.
type testChain struct {
	root, intermediate, leaf *x509.Certificate
	leafKey                  *ecdsa.PrivateKey
}

type chainOpts struct {
	intermediateOID bool
	leafOID         bool
}

var markerValue = []byte{0x05, 0x00} // ASN.1 NULL, as Apple uses

func newTestCert(t *testing.T, cn string, isCA bool, oid asn1.ObjectIdentifier, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}
	if isCA {
		tmpl.KeyUsage = x509.KeyUsageCertSign
	}
	if oid != nil {
		tmpl.ExtraExtensions = []pkix.Extension{{Id: oid, Value: markerValue}}
	}
	if parent == nil {
		parent, parentKey = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func newTestChain(t *testing.T, o chainOpts) testChain {
	t.Helper()
	root, rootKey := newTestCert(t, "Test Root CA", true, nil, nil, nil)
	var iOID, lOID asn1.ObjectIdentifier
	if o.intermediateOID {
		iOID = oidAppleWWDRIntermediate
	}
	if o.leafOID {
		lOID = oidAppleStoreSigningLeaf
	}
	inter, interKey := newTestCert(t, "Test WWDR CA", true, iOID, root, rootKey)
	leaf, leafKey := newTestCert(t, "Test App Store Signing", false, lOID, inter, interKey)
	return testChain{root: root, intermediate: inter, leaf: leaf, leafKey: leafKey}
}

func signJWS(t *testing.T, key *ecdsa.PrivateKey, x5c []*x509.Certificate, payload any) string {
	t.Helper()
	chain := make([]string, len(x5c))
	for i, c := range x5c {
		chain[i] = base64.StdEncoding.EncodeToString(c.Raw)
	}
	header, _ := json.Marshal(map[string]any{"alg": "ES256", "x5c": chain})
	body, _ := json.Marshal(payload)
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
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

func testTransaction(otx string) map[string]any {
	return map[string]any{
		"transactionId":         otx,
		"originalTransactionId": otx,
		"bundleId":              "io.devlan.rfspager",
		"productId":             "io.devlan.rfspager.monthly",
		"expiresDate":           time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
		"environment":           "Production",
	}
}

func TestVerifyAppleJWSAcceptsAppleShapedChain(t *testing.T) {
	c := newTestChain(t, chainOpts{intermediateOID: true, leafOID: true})
	tok := signJWS(t, c.leafKey, []*x509.Certificate{c.leaf, c.intermediate, c.root}, testTransaction("1"))
	if _, err := verifyAppleJWSWith(tok, c.root, time.Now()); err != nil {
		t.Fatalf("well-formed chain rejected: %v", err)
	}
}

// The forgery this guards against: a certificate that genuinely chains to the
// trusted root but was issued for something other than App Store signing
// (e.g. an Apple Pay certificate whose key a developer holds).
func TestVerifyAppleJWSRejectsNonStoreCertificates(t *testing.T) {
	cases := map[string]chainOpts{
		"leaf without App Store marker":       {intermediateOID: true},
		"intermediate without WWDR marker":    {leafOID: true},
		"neither certificate carries markers": {},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			c := newTestChain(t, o)
			tok := signJWS(t, c.leafKey, []*x509.Certificate{c.leaf, c.intermediate, c.root}, testTransaction("1"))
			if _, err := verifyAppleJWSWith(tok, c.root, time.Now()); err == nil || !strings.Contains(err.Error(), "chain") {
				t.Fatalf("accepted, err = %v", err)
			}
		})
	}
}

func TestVerifyAppleJWSRejectsMalformedChains(t *testing.T) {
	c := newTestChain(t, chainOpts{intermediateOID: true, leafOID: true})
	other := newTestChain(t, chainOpts{intermediateOID: true, leafOID: true})
	cases := map[string][]*x509.Certificate{
		"two certificates":         {c.leaf, c.intermediate},
		"four certificates":        {c.leaf, c.intermediate, c.intermediate, c.root},
		"root is not trusted root": {c.leaf, c.intermediate, other.root},
		"intermediate swapped":     {c.leaf, other.intermediate, c.root},
	}
	for name, x5c := range cases {
		t.Run(name, func(t *testing.T) {
			tok := signJWS(t, c.leafKey, x5c, testTransaction("1"))
			if _, err := verifyAppleJWSWith(tok, c.root, time.Now()); err == nil || !strings.Contains(err.Error(), "chain") {
				t.Fatalf("accepted, err = %v", err)
			}
		})
	}
}

func TestVerifyAppleJWSRejectsExpiredChain(t *testing.T) {
	c := newTestChain(t, chainOpts{intermediateOID: true, leafOID: true})
	tok := signJWS(t, c.leafKey, []*x509.Certificate{c.leaf, c.intermediate, c.root}, testTransaction("1"))
	if _, err := verifyAppleJWSWith(tok, c.root, time.Now().Add(48*time.Hour)); err == nil {
		t.Fatal("expired chain accepted")
	}
}

// Production verification must only ever trust the embedded Apple root.
func TestVerifyAppleJWSUsesEmbeddedAppleRoot(t *testing.T) {
	if got := appleRoot().Subject.CommonName; got != "Apple Root CA - G3" {
		t.Fatalf("embedded root = %q", got)
	}
	c := newTestChain(t, chainOpts{intermediateOID: true, leafOID: true})
	tok := signJWS(t, c.leafKey, []*x509.Certificate{c.leaf, c.intermediate, c.root}, testTransaction("1"))
	if _, err := verifyAppleJWS(tok); err == nil || !strings.Contains(err.Error(), "Apple root") {
		t.Fatalf("chain to a non-Apple root accepted, err = %v", err)
	}
}

// A verified JWS for a transaction Apple has never heard of is a forgery
// (or a fabricated ID), not an outage: it must be rejected rather than
// trusted on its own.
func TestValidateAppleJWSRejectsTransactionAppleDoesNotKnow(t *testing.T) {
	c := newTestChain(t, chainOpts{intermediateOID: true, leafOID: true})
	restore := trustedAppleRoot
	trustedAppleRoot = func() *x509.Certificate { return c.root }
	t.Cleanup(func() { trustedAppleRoot = restore })

	status := http.StatusNotFound
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"errorCode":4040010,"errorMessage":"Transaction id not found."}`))
	}))
	defer srv.Close()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	h := &Handler{apple: &AppleClient{keyID: "k", issuerID: "i", privateKey: key, bundleID: "io.devlan.rfspager", production: true, apiBase: srv.URL}}
	tok := signJWS(t, c.leafKey, []*x509.Certificate{c.leaf, c.intermediate, c.root}, testTransaction("999"))

	if _, err := h.validateAppleJWS(tok); err == nil || !strings.Contains(err.Error(), "no record") {
		t.Fatalf("404 from Apple: err = %v, want rejection", err)
	}

	// An Apple outage still falls back to the (now strictly verified) JWS.
	status = http.StatusInternalServerError
	res, err := h.validateAppleJWS(tok)
	if err != nil || !res.Valid {
		t.Fatalf("500 from Apple: res = %+v, err = %v, want JWS fallback", res, err)
	}
}

// A genuine App Store Server Notification (type TEST, which carries no
// customer data) captured from production with
// `apple-notify-test -history 720h -print TEST`. It must pass the strict
// chain checks against the embedded Apple root; it is checked at its own
// signedDate so the fixture does not age out when Apple's leaf expires.
func TestVerifyAppleJWSAcceptsRealAppleNotification(t *testing.T) {
	raw, err := os.ReadFile("testdata/apple_test_notification.jws")
	if err != nil {
		t.Fatal(err)
	}
	tok := strings.TrimSpace(string(raw))
	payload, err := verifyAppleJWSWith(tok, appleRoot(), time.UnixMilli(1792007589418))
	if err != nil {
		t.Fatalf("real Apple notification rejected: %v", err)
	}
	var notif AppleNotificationPayload
	if err := json.Unmarshal(payload, &notif); err != nil || notif.NotificationType != "TEST" {
		t.Fatalf("payload = %s, err = %v", payload, err)
	}

	// Tampering with the payload must break the signature.
	parts := strings.Split(tok, ".")
	forged := strings.Replace(string(payload), `"TEST"`, `"DID_RENEW"`, 1)
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(forged))
	if _, err := verifyAppleJWSWith(strings.Join(parts, "."), appleRoot(), time.UnixMilli(1792007589418)); err == nil {
		t.Fatal("tampered payload accepted")
	}
}
