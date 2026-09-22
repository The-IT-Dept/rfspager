package authjwt

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

func TestSecurityValidateRejectsWrongAlgorithm(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{
		"iss": Issuer, "aud": "rfspager-mobile", "typ": "mobile", "sub": int64(1), "ver": int64(0), "exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate([]byte("test-secret"), signed, "rfspager-mobile", "mobile"); err == nil {
		t.Fatal("HS384 token was accepted")
	}
}

func TestSecurityValidateRequiresAllSessionClaims(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": Issuer, "aud": "rfspager-mobile", "typ": "mobile", "sub": int64(1), "exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate([]byte("test-secret"), signed, "rfspager-mobile", "mobile"); err == nil {
		t.Fatal("token missing revocation version was accepted")
	}
}
