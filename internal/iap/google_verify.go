package iap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/api/idtoken"
)

// verifyGooglePubsubOIDC validates the Pub/Sub push OIDC token attached to a
// webhook request. The audience must match what was configured on the Pub/Sub
// push subscription. If serviceAccountEmail is non-empty, the token's email
// claim must match it (extra defense against a stranger forwarding a token).
//
// audience is typically the webhook URL (e.g. https://api.example.com/api/webhooks/google),
// matching the push subscription's "Authentication audience" setting.
func verifyGooglePubsubOIDC(ctx context.Context, r *http.Request, audience, serviceAccountEmail string) error {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return errors.New("missing or malformed Authorization header")
	}
	token := strings.TrimPrefix(auth, "Bearer ")

	payload, err := idtoken.Validate(ctx, token, audience)
	if err != nil {
		return fmt.Errorf("OIDC token validation: %w", err)
	}

	if iss, _ := payload.Claims["iss"].(string); iss != "https://accounts.google.com" && iss != "accounts.google.com" {
		return fmt.Errorf("unexpected issuer: %s", iss)
	}

	if serviceAccountEmail != "" {
		email, _ := payload.Claims["email"].(string)
		if email != serviceAccountEmail {
			return fmt.Errorf("unexpected service account email: %s", email)
		}
		if verified, _ := payload.Claims["email_verified"].(bool); !verified {
			return errors.New("service account email not verified")
		}
	}

	return nil
}
