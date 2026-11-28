package host

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	runtimeprotocol "platformserver/kernel/protocol"
	transport "platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/actions"
	agentowner "platformserver/services/ai/delegation"
	controlowner "platformserver/services/security/control"
	"platformserver/services/security/idp"
)

type hostMailCredentials struct{}

func (hostMailCredentials) CredentialValue(name, _, _ string) ([]byte, bool) {
	return controlowner.LookupSecret(nil, name)
}
func (hostMailCredentials) CredentialHeader(name, _, _ string) ([]byte, bool) {
	return controlowner.LookupSecret(nil, name)
}

func (h *Host) EnableLocalOnboarding(accounts *idp.Accounts) {
	h.Accounts = accounts
	h.authenticate = accounts.Authenticate(h.Development, func() time.Time { return h.Now() })
	controlowner.UseTokenKey(accounts.Key())
	agentowner.UseTokenKey(accounts.Key())
	h.Mint = func(subject string) string { return accounts.Mint(subject, h.Now()) }
}

func (h *Host) authWorkspaces(subject string) []runtimeprotocol.AuthWorkspace {
	return idp.MemberWorkspaces(h.workspaceIdentities(), subject)
}

func (h *Host) sendOnboardingMail(to, subject, text, id string, now time.Time) error {
	if h.MailEndpoint.URL == "" {
		return nil
	}
	raw, _ := json.Marshal(actions.Letter{To: to, Subject: subject, Text: text})
	ep := actions.Endpoint{ID: "idp-mail", Kind: "email", URL: h.MailEndpoint.URL, From: cmp.Or(h.MailEndpoint.From, "noreply@platform.local"), Secret: h.MailEndpoint.Secret, AllowPrivate: h.MailEndpoint.AllowPrivate}
	out := (actions.Transport{Credentials: hostMailCredentials{}, Send: guarded}).Mail(ep, platform.Effect{ID: id, Body: string(raw), At: now}, now)
	if out.Result != "delivered" {
		return fmt.Errorf("%s", cmp.Or(out.Detail, out.Result))
	}
	return nil
}

func (h *Host) routesOnboarding(rt *routes) {
	rt.public(transport.Route{Pattern: "POST /v1/auth/email/code", Summary: "Send a one-time email verification code for sign-up or sign-in", Body: runtimeprotocol.EmailCodeRequest{}, Answer: runtimeprotocol.EmailCodeAnswer{}}, func(w http.ResponseWriter, r *http.Request) {
		now := h.Now()
		h.Accounts.ServeCodeRequest(w, r, h.Accounts != nil && h.Issuer == "", now, func(to, sub, txt, id string) error { return h.sendOnboardingMail(to, sub, txt, id, now) }, nil)
	})
	rt.public(transport.Route{Pattern: "POST /v1/auth/email/verify", Summary: "Verify a one-time email code and issue a local identity session token", Body: runtimeprotocol.EmailVerifyRequest{}, Answer: runtimeprotocol.AuthSession{}}, func(w http.ResponseWriter, r *http.Request) {
		h.Accounts.ServeCodeVerify(w, r, h.Accounts != nil && h.Issuer == "", h.Now(), h.authWorkspaces, nil)
	})
	sessionRoute := transport.Route{Pattern: "GET /v1/auth/session", Summary: "The caller's authenticated account and active workspaces before or after joining a tenant", Answer: runtimeprotocol.AuthSession{}}
	h.routes = append(h.routes, sessionRoute)
	rt.mux.HandleFunc(sessionRoute.Pattern, func(w http.ResponseWriter, r *http.Request) {
		idp.ServeAuthSession(w, r, h.Accounts, h.authenticate, h.authWorkspaces)
	})
	if h.Development && h.Accounts != nil && h.Accounts.Captured() != nil {
		rt.mux.HandleFunc("GET /v1/dev/mailbox", h.Accounts.ServeDevMailbox)
	}
}
