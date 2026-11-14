package idp

import (
	"strings"
	"time"

	controlowner "platformserver/services/security/control"

	"platformserver/platform"
)

type Console[T any] struct {
	Tenant   T
	ID       string
	Sessions platform.IdentitySessions
	Join     func(subject string, now time.Time) bool
}

type Members[T any] struct {
	Authenticate Authenticate
	SecondFactor Attest
	Now          func() time.Time
	Consoles     func() []Console[T]
}

func (m Members[T]) Resolve(authorization, want, agent string) (platform.Member, T, bool) {
	var none T
	credential := strings.TrimPrefix(authorization, "Bearer ")
	now := m.Now()
	if strings.HasPrefix(credential, controlowner.TokenPrefix) {
		for _, c := range m.Consoles() {
			if d := c.Sessions; d != nil && (want == "" || c.ID == want) {
				if member, ok := d.MemberByToken(credential, now); ok && d.NoticeSession(member.ID, credential, agent, "token", now) {
					return member, c.Tenant, true
				}
			}
		}
		return platform.Member{}, none, false
	}
	subject, ok := m.Authenticate(credential)
	if !ok {
		return platform.Member{}, none, false
	}
	for _, c := range m.Consoles() {
		if d := c.Sessions; d == nil || want != "" && c.ID != want {
			continue
		} else if member, ok := d.MemberBySubject(subject); ok {
			if d.SecondFactorRequired() && (m.SecondFactor == nil || !m.SecondFactor(credential)) {
				return platform.Member{}, none, false
			}
			if !d.NoticeSession(member.ID, credential, agent, "sign-in", now) {
				return platform.Member{}, none, false
			}
			d.SeenMember(member.ID, now)
			return member, c.Tenant, true
		}
	}
	for _, c := range m.Consoles() {
		if d := c.Sessions; d != nil && (want == "" || c.ID == want) && (!d.SecondFactorRequired() || m.SecondFactor != nil && m.SecondFactor(credential)) && d.JoinsSubject(subject) && c.Join(subject, now) {
			return m.Resolve(authorization, want, agent)
		}
	}
	return platform.Member{}, none, false
}
