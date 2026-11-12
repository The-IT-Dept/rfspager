package iap

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

//go:embed apple_root_ca_g3.pem
var appleRootCAPEM []byte

var (
	appleRootCert *x509.Certificate
	appleRootOnce sync.Once
)

// appleRoot is the bundled Apple Root CA - G3, the only root an App Store
// signed payload may chain to.
func appleRoot() *x509.Certificate {
	appleRootOnce.Do(func() {
		block, _ := pem.Decode(appleRootCAPEM)
		if block == nil {
			panic("iap: embedded Apple root CA is not PEM")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			panic("iap: embedded Apple root CA: " + err.Error())
		}
		appleRootCert = cert
	})
	return appleRootCert
}

// Marker extensions Apple puts on the App Store signing chain. Chaining to the
// Apple root is not enough on its own: Apple issues many other certificates
// under the same root (Apple Pay, Wallet, developer certificates), some with
// keys a third party holds. Apple's own server library requires both of these.
var (
	// Apple Worldwide Developer Relations intermediate.
	oidAppleWWDRIntermediate = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 2, 1}
	// App Store receipt / StoreKit signing leaf.
	oidAppleStoreSigningLeaf = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 11, 1}
)

// verifyAppleJWS validates a JWS signed by Apple's App Store Server and
// returns the raw decoded JSON payload on success.
func verifyAppleJWS(jwsToken string) ([]byte, error) {
	return verifyAppleJWSWith(jwsToken, trustedAppleRoot(), time.Now())
}

// trustedAppleRoot is swapped only by tests, which cannot obtain certificates
// under the real Apple root.
var trustedAppleRoot = appleRoot

// verifyAppleJWSWith checks that the x5c header is exactly [leaf, intermediate,
// root] with root byte-identical to the given trusted root, each certificate
// signed by the next and valid at the given time, both Apple marker extensions
// present, and the ES256 signature made by the leaf's key.
func verifyAppleJWSWith(jwsToken string, root *x509.Certificate, at time.Time) ([]byte, error) {
	parts := strings.Split(jwsToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWS format")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decoding JWS header: %w", err)
	}

	var header struct {
		Alg string   `json:"alg"`
		X5C []string `json:"x5c"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("parsing JWS header: %w", err)
	}

	if header.Alg != "ES256" {
		return nil, fmt.Errorf("unexpected JWS algorithm: %s", header.Alg)
	}
	if len(header.X5C) != 3 {
		return nil, fmt.Errorf("x5c chain verification: want 3 certificates, got %d", len(header.X5C))
	}

	certs := make([]*x509.Certificate, len(header.X5C))
	for i, b64 := range header.X5C {
		der, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("decoding x5c cert %d: %w", i, err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("parsing x5c cert %d: %w", i, err)
		}
		certs[i] = cert
	}
	leaf, intermediate := certs[0], certs[1]

	if !bytes.Equal(certs[2].Raw, root.Raw) {
		return nil, errors.New("x5c chain verification: root is not the Apple root CA")
	}
	if !hasExtension(intermediate, oidAppleWWDRIntermediate) {
		return nil, errors.New("x5c chain verification: intermediate is not Apple's WWDR CA")
	}
	if !hasExtension(leaf, oidAppleStoreSigningLeaf) {
		return nil, errors.New("x5c chain verification: leaf is not an App Store signing certificate")
	}

	roots := x509.NewCertPool()
	roots.AddCert(root)
	intermediates := x509.NewCertPool()
	intermediates.AddCert(intermediate)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   at,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return nil, fmt.Errorf("x5c chain verification: %w", err)
	}
	// Verify may build a shorter path; insist on the one in the header.
	if err := leaf.CheckSignatureFrom(intermediate); err != nil {
		return nil, fmt.Errorf("x5c chain verification: leaf not issued by intermediate: %w", err)
	}

	pubKey, ok := certs[0].PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("leaf cert public key is not ECDSA")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decoding JWS signature: %w", err)
	}
	if len(sig) != 64 {
		return nil, fmt.Errorf("invalid ES256 signature length: %d", len(sig))
	}

	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])

	signingInput := []byte(parts[0] + "." + parts[1])
	hash := sha256.Sum256(signingInput)

	if !ecdsa.Verify(pubKey, hash[:], r, s) {
		return nil, errors.New("JWS signature invalid")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decoding JWS payload: %w", err)
	}
	return payload, nil
}

func hasExtension(cert *x509.Certificate, oid asn1.ObjectIdentifier) bool {
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oid) {
			return true
		}
	}
	return false
}
