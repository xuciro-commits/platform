package platformserver

import (
	"context"
	"slices"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDC verifies access tokens signed by an OpenID provider (docs/ADR/0007) and
// returns the caller's subject. The provider proves who is calling; the tenant's
// directory says what that caller is there (K6, ADR-0010). Subjects are
// "user:<verified email>" for people and "client:<client id>" for machines
// (client-credentials tokens carry no user). keys is the provider's JWKS URL,
// given apart from the issuer so a server can reach the provider on an internal
// address while tokens name the public one.
func OIDC(issuer, keys string) Authenticate {
	authenticate, _ := OIDCProvider(issuer, keys)
	return authenticate
}

// OIDCProvider is OIDC with the provider's word on the second factor: the
// token's amr (RFC 8176) names mfa, otp, hwk, sms or a passkey (ADR-0078 §2).
func OIDCProvider(issuer, keys string) (Authenticate, Attest) {
	verifier := oidc.NewVerifier(issuer, oidc.NewRemoteKeySet(context.Background(), keys),
		&oidc.Config{SkipClientIDCheck: true, SupportedSigningAlgs: []string{oidc.EdDSA, oidc.RS256, oidc.ES256}})
	type claims struct {
		Type          string   `json:"typ"`
		Party         string   `json:"azp"`
		Email         string   `json:"email"`
		EmailVerified bool     `json:"email_verified"`
		Methods       []string `json:"amr"`
	}
	verify := func(credential string) (claims, bool) {
		token, err := verifier.Verify(context.Background(), credential)
		if err != nil {
			return claims{}, false
		}
		var c claims
		if token.Claims(&c) != nil || c.Type != "Bearer" { // an ID token is not a credential
			return claims{}, false
		}
		if token.Subject == "" && c.Party != "" {
			c.Party = "client:" + c.Party
		} else if c.Email != "" && c.EmailVerified {
			c.Party = "user:" + c.Email
		} else {
			return claims{}, false
		}
		return c, true
	}
	authenticate := func(credential string) (string, bool) {
		c, ok := verify(credential)
		return c.Party, ok
	}
	attest := func(credential string) bool {
		c, ok := verify(credential)
		return ok && slices.ContainsFunc(c.Methods, func(m string) bool { return slices.Contains([]string{"mfa", "otp", "hwk", "sms", "swk", "user"}, m) })
	}
	return authenticate, attest
}
