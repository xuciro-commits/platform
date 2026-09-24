package platformserver

import (
	"context"

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
	verifier := oidc.NewVerifier(issuer, oidc.NewRemoteKeySet(context.Background(), keys),
		&oidc.Config{SkipClientIDCheck: true, SupportedSigningAlgs: []string{oidc.EdDSA, oidc.RS256, oidc.ES256}})
	return func(credential string) (string, bool) {
		token, err := verifier.Verify(context.Background(), credential)
		if err != nil {
			return "", false
		}
		var claims struct {
			Type          string `json:"typ"`
			Party         string `json:"azp"`
			Email         string `json:"email"`
			EmailVerified bool   `json:"email_verified"`
		}
		if token.Claims(&claims) != nil || claims.Type != "Bearer" { // an ID token is not a credential
			return "", false
		}
		switch {
		case token.Subject == "" && claims.Party != "":
			return "client:" + claims.Party, true
		case claims.Email != "" && claims.EmailVerified:
			return "user:" + claims.Email, true
		}
		return "", false
	}
}
