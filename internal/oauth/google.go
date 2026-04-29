// Package oauth wraps the Google id_token verification used by the
// /sessions/login-complete handler. The interface is small + injection-
// friendly so handler tests can stub the verifier without round-tripping
// to Google's certs.
package oauth

import (
	"context"
	"fmt"

	"google.golang.org/api/idtoken"
)

// GoogleIDTokenClaims is the v2 sign-in's view of a verified Google
// id_token. Only the fields the auto-join flow needs are exposed —
// callers that need richer profile data should re-verify or look up
// from a separate path.
type GoogleIDTokenClaims struct {
	Subject     string // sub
	Email       string
	DisplayName string // name (optional)
	AvatarURL   string // picture (optional)
}

// GoogleIDTokenVerifier verifies a raw Google-issued id_token and
// returns the relevant claims. Implementations MUST validate signature,
// audience, and expiry; they SHOULD reject tokens whose `email_verified`
// claim is false.
type GoogleIDTokenVerifier interface {
	Verify(ctx context.Context, idToken string) (*GoogleIDTokenClaims, error)
}

// GoogleVerifier is the production verifier — wraps
// google.golang.org/api/idtoken.Validate, which fetches and caches
// Google's signing certs.
type GoogleVerifier struct {
	audience string
}

// NewGoogleVerifier constructs a GoogleVerifier bound to the given
// audience. The audience MUST match the OAuth client ID configured at
// the Google project that issued the id_token; mismatched audiences
// are a security-meaningful rejection (someone else's token shouldn't
// authenticate against this service).
func NewGoogleVerifier(audience string) *GoogleVerifier {
	return &GoogleVerifier{audience: audience}
}

// Verify validates the token and extracts claims.
func (v *GoogleVerifier) Verify(ctx context.Context, idToken string) (*GoogleIDTokenClaims, error) {
	if v.audience == "" {
		return nil, fmt.Errorf("oauth.Verify: audience is unset (set GOOGLE_OAUTH_AUDIENCE / GOOGLE_OAUTH_CLIENT_ID)")
	}
	if idToken == "" {
		return nil, fmt.Errorf("oauth.Verify: empty id_token")
	}

	payload, err := idtoken.Validate(ctx, idToken, v.audience)
	if err != nil {
		return nil, fmt.Errorf("oauth.Verify: validate: %w", err)
	}

	// `email_verified` MUST be true. Google sometimes returns the claim
	// as a string ("true") and sometimes as a bool, depending on the
	// signing path; handle both defensively.
	if !claimBool(payload.Claims, "email_verified") {
		return nil, fmt.Errorf("oauth.Verify: email_verified claim is missing or false")
	}

	out := &GoogleIDTokenClaims{
		Subject:     payload.Subject,
		Email:       claimString(payload.Claims, "email"),
		DisplayName: claimString(payload.Claims, "name"),
		AvatarURL:   claimString(payload.Claims, "picture"),
	}
	if out.Subject == "" {
		return nil, fmt.Errorf("oauth.Verify: missing sub claim")
	}
	if out.Email == "" {
		return nil, fmt.Errorf("oauth.Verify: missing email claim")
	}
	return out, nil
}

func claimString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func claimBool(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true"
	default:
		return false
	}
}
