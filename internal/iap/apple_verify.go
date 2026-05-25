package iap

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
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
	appleRootPool *x509.CertPool
	appleRootOnce sync.Once
)

func appleRoots() *x509.CertPool {
	appleRootOnce.Do(func() {
		appleRootPool = x509.NewCertPool()
		appleRootPool.AppendCertsFromPEM(appleRootCAPEM)
	})
	return appleRootPool
}

// verifyAppleJWS validates a JWS signed by Apple's App Store Server.
// It walks the x5c cert chain to the embedded Apple Root CA G3, then verifies
// the ES256 signature with the leaf certificate's public key.
// Returns the raw decoded JSON payload on success.
func verifyAppleJWS(jwsToken string) ([]byte, error) {
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
	if len(header.X5C) < 1 {
		return nil, errors.New("missing x5c cert chain")
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

	intermediates := x509.NewCertPool()
	for _, c := range certs[1:] {
		intermediates.AddCert(c)
	}

	if _, err := certs[0].Verify(x509.VerifyOptions{
		Roots:         appleRoots(),
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return nil, fmt.Errorf("x5c chain verification: %w", err)
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
