package idp

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/security/control"
	"slices"
	"strings"
	"time"
)

type WorkspaceIdentity struct {
	ID      string
	Owner   control.IdentityOwner
	Stopped bool
}

type Entrance struct {
	Authenticate           Authenticate
	SecondFactor           Attest
	Now                    func() time.Time
	HostAdmins             map[string]bool
	SelfService, CanCreate bool
	Issuer                 string
	Workspaces             func() []WorkspaceIdentity
}

func (h Entrance) Entry(authorization string) (protocol.IdentityEntry, bool) {
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || token == "" || strings.HasPrefix(token, "pat_") || strings.HasPrefix(token, "dmcp_") || h.Authenticate == nil {
		return protocol.IdentityEntry{}, false
	}
	subject, ok := h.Authenticate(token)
	if !ok || subject == "" {
		return protocol.IdentityEntry{}, false
	}
	result := protocol.IdentityEntry{Subject: subject, Workspaces: []protocol.MeTenant{}, Invitations: []protocol.PendingWorkspace{}, CanCreate: h.CanCreate && (h.HostAdmins[subject] || h.SelfService && strings.HasPrefix(subject, "user:"))}
	for _, tenant := range h.Workspaces() {
		if tenant.Stopped {
			continue
		}
		d := tenant.Owner
		if d == nil || d.SecondFactorRequired() && (h.SecondFactor == nil || !h.SecondFactor(token)) {
			continue
		}
		member, known := d.MemberBySubject(subject)
		if known && !member.Active() {
			if member.Status == platform.MemberInvited && member.Invitation != nil && member.Invitation.State == "pending" && h.Now().Before(member.Invitation.Expires) {
				result.Invitations = append(result.Invitations, protocol.PendingWorkspace{MeTenant: protocol.MeTenant{ID: tenant.ID, Name: d.TenantRecord().Name}, Expires: member.Invitation.Expires})
			}
			continue
		}
		if !known && (!d.JoinableSubject(subject) || !d.JoinsSubject(subject)) {
			continue
		}
		result.Workspaces = append(result.Workspaces, protocol.MeTenant{ID: tenant.ID, Name: d.TenantRecord().Name})
	}
	slices.SortFunc(result.Workspaces, func(a, b protocol.MeTenant) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(result.Invitations, func(a, b protocol.PendingWorkspace) int { return strings.Compare(a.ID, b.ID) })
	return result, true
}

func WorkspaceSpec(b protocol.WorkspaceCreate, subject string) (protocol.CreateTenantRequest, bool) {
	if !control.ValidTenantID(b.ID) || strings.TrimSpace(b.Name) == "" || len(b.Name) > 160 {
		return protocol.CreateTenantRequest{}, false
	}
	if b.Timezone != "" {
		if _, err := time.LoadLocation(b.Timezone); err != nil {
			return protocol.CreateTenantRequest{}, false
		}
	}
	settings := map[string]string{}
	if b.Language != "" {
		settings[control.SettingLanguage] = b.Language
	}
	if b.Timezone != "" {
		settings[control.SettingTimezone] = b.Timezone
	}
	return protocol.CreateTenantRequest{ID: b.ID, Name: strings.TrimSpace(b.Name), Admin: subject, Settings: settings}, true
}

func WorkspaceIdentities[T any](tenants []T, scope func(T) WorkspaceIdentity) []WorkspaceIdentity {
	out := make([]WorkspaceIdentity, 0, len(tenants))
	for _, t := range tenants {
		out = append(out, scope(t))
	}
	return out
}
func MemberWorkspaces(scopes []WorkspaceIdentity, subject string) []protocol.MeTenant {
	out := []protocol.MeTenant{}
	for _, s := range scopes {
		if s.Owner != nil && !s.Stopped {
			if m, ok := s.Owner.MemberBySubject(subject); ok && m.Active() {
				out = append(out, protocol.MeTenant{ID: s.ID, Name: s.Owner.TenantRecord().Name})
			}
		}
	}
	return out
}
func LoginIdentities(scopes []WorkspaceIdentity) []protocol.LoginIdentity {
	var out []protocol.LoginIdentity
	for _, s := range scopes {
		if s.Owner != nil {
			out = append(out, s.Owner.Identities()...)
		}
	}
	return out
}
