package idp

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
// verified amr asserts mfa or methods from two distinct factor categories
// (RFC 8176). User presence and a single possession method are not MFA.
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
		return ok && multipleFactors(c.Methods)
	}
	return authenticate, attest
}

// RFC 8176 distinguishes knowledge, possession and biometric methods. Unknown
// methods and user presence do not prove another factor.
func multipleFactors(methods []string) bool {
	if slices.Contains(methods, "mfa") {
		return true
	}
	knowledge, possession, biometric := false, false, false
	for _, method := range methods {
		switch method {
		case "pwd", "pin":
			knowledge = true
		case "otp", "hwk", "swk", "sms", "tel", "sc":
			possession = true
		case "fpt", "face", "iris", "retina", "vbm":
			biometric = true
		}
	}
	return knowledge && possession || knowledge && biometric || possession && biometric
}
