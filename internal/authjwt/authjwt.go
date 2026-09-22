// Package authjwt contains the small, shared contract for first-party session
// JWTs. Callers still load authorization from the database; claims only name
// the authenticated session and its revocation version.
package authjwt

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const Issuer = "rfspager"

type Claims struct {
	Subject int64
	Version int64
}

func Issue(secret []byte, audience, tokenType string, subject, version int64, lifetime time.Duration, extras map[string]any) (string, error) {
	if subject <= 0 || version < 0 || len(secret) == 0 {
		return "", errors.New("invalid session token inputs")
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": Issuer,
		"aud": audience,
		"typ": tokenType,
		"sub": subject,
		"ver": version,
		"iat": now.Unix(),
		"exp": now.Add(lifetime).Unix(),
	}
	for key, value := range extras {
		claims[key] = value
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

func Validate(secret []byte, tokenString, audience, tokenType string) (Claims, error) {
	if len(secret) == 0 {
		return Claims{}, errors.New("missing session token key")
	}
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %q", token.Header["alg"])
		}
		return secret, nil
	})
	if err != nil || !token.Valid {
		return Claims{}, errors.New("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, errors.New("invalid token claims")
	}
	if stringClaim(claims, "iss") != Issuer || stringClaim(claims, "aud") != audience || stringClaim(claims, "typ") != tokenType {
		return Claims{}, errors.New("wrong token issuer, audience, or type")
	}
	exp, ok := integerClaim(claims, "exp")
	if !ok || exp <= time.Now().Unix() {
		return Claims{}, errors.New("missing or expired token expiry")
	}
	subject, ok := integerClaim(claims, "sub")
	if !ok || subject <= 0 {
		return Claims{}, errors.New("invalid token subject")
	}
	version, ok := integerClaim(claims, "ver")
	if !ok || version < 0 {
		return Claims{}, errors.New("invalid token version")
	}
	return Claims{Subject: subject, Version: version}, nil
}

func stringClaim(claims jwt.MapClaims, key string) string {
	value, _ := claims[key].(string)
	return value
}

func integerClaim(claims jwt.MapClaims, key string) (int64, bool) {
	value, ok := claims[key].(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value > math.MaxInt64 || value < math.MinInt64 {
		return 0, false
	}
	return int64(value), true
}
