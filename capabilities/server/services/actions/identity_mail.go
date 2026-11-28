package actions

import (
	"net/mail"
	"net/url"
	"os"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"slices"
	"strings"
)

const IdentityMailID = "host:identity-mail"

func IdentityMailEndpoint() (Endpoint, bool) {
	raw := os.Getenv("PLATFORM_IDENTITY_SMTP_URL")
	from := os.Getenv("PLATFORM_IDENTITY_SMTP_FROM")
	u, err := url.Parse(raw)
	address, addressErr := mail.ParseAddress(from)
	if err != nil || u.Scheme != "smtp" || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || addressErr != nil || address.Address != from {
		return Endpoint{}, false
	}
	if u.User != nil {
		if _, ok := u.User.Password(); ok || u.User.Username() == "" || os.Getenv("PLATFORM_IDENTITY_SMTP_PASSWORD") == "" {
			return Endpoint{}, false
		}
	}
	return Endpoint{ID: IdentityMailID, Kind: "email", URL: raw, From: from, Notifications: []string{"platform"}, AllowPrivate: os.Getenv("PLATFORM_IDENTITY_SMTP_ALLOW_PRIVATE") == "1", OperatorOwned: true}, true
}

func InvitationMail(app, key string) bool {
	return app == "platform" && strings.HasPrefix(key, "invite:")
}

func IdentityMailEndpoints(existing []Endpoint) []Endpoint {
	out := slices.DeleteFunc(slices.Clone(existing), func(ep Endpoint) bool { return ep.ID == IdentityMailID })
	if ep, ok := IdentityMailEndpoint(); ok {
		return append(out, ep)
	}
	return out
}

func invitationAddress(subjects []string) string {
	for _, subject := range subjects {
		if email, ok := strings.CutPrefix(subject, "user:"); ok {
			return email
		}
	}
	return ""
}

func IdentityMailReach(n platform.Notification, to protocol.Reach, owner protocol.IdentityProfile) protocol.Reach {
	if InvitationMail(n.App, n.Key) && owner != nil {
		to.Address = invitationAddress(owner.Account(n.Member).Subjects)
	}
	return to
}
