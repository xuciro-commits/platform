package idp

import (
	"context"
	"slices"

	"github.com/coreos/go-oidc/v3/oidc"
)

func OIDC(issuer, keys string) Authenticate {
	authenticate, _ := OIDCProvider(issuer, keys)
	return authenticate
}

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
		if token.Claims(&c) != nil || c.Type != "Bearer" {
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
