package platformserver

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"platformserver/internal/host"
	"platformserver/platform"
)

// MemberByID preserves durable directory lookup, including retained offboarded
// members whose currentMember projection has no grants. Sign-in lookup below
// deliberately rejects those retained entries.
func (d *Console) MemberByID(id string) (platform.Member, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	member := d.members[id]
	if member == nil {
		return platform.Member{}, false
	}
	return d.currentMember(member), true
}

// AgentMembers is the current directory view of outside agent principals.
// It exposes neither authentication subjects nor token/session state.
func (d *Console) AgentMembers() []platform.Member {
	d.mu.Lock()
	defer d.mu.Unlock()
	var members []platform.Member
	for _, member := range d.members {
		if member.Agent && member.Status != platform.MemberLeft {
			members = append(members, d.currentMember(member))
		}
	}
	slices.SortFunc(members, func(a, b platform.Member) int { return strings.Compare(a.ID, b.ID) })
	return members
}

// Ready installs custom access only when the complete tenant is available.
// Restore continues applying accepted directory access at its existing point.
func (d *Console) Ready() error {
	d.applyAccess()
	return nil
}

var _ host.IdentityDirectory = (*Console)(nil)
var _ host.CompositionReady = (*Console)(nil)

func (d *Console) AttachConsole(h host.ConsoleHost) { d.host = h }

func (d *Console) BindPackageIndex(index *PackageIndex) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.index = index
}
func (d *Console) MemberCount() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.members) }

func (d *Console) NewSimulationIdentity(tenant string, members []platform.Member) platform.App {
	members = append(slices.Clone(members), platform.Member{ID: "app:candidate-test", Tenant: tenant, Roles: map[string]string{PlatformApp: Admin, platform.AIApp: platform.AIAdmin}})
	seats := make([]Seat, 0, len(members))
	for _, member := range members {
		member.Tenant = tenant
		member.Roles = maps.Clone(member.Roles)
		seats = append(seats, Seat{Subjects: []string{member.ID}, Member: member})
	}
	return NewConsole(tenant, seats...)
}

// UnitGrantUsage reads both current grants and their accepted history. Revoking
// access must not make a previously used organisation identity deletable.
func (d *Console) UnitGrantUsage(unit, until string) []host.UnitGrantUsage {
	uses := map[string]bool{}
	d.mu.Lock()
	for _, member := range d.members {
		grants := slices.Clone(member.Grants)
		if member.Active() {
			grants = append(grants, d.currentMember(member).Grants...)
		}
		for _, grant := range grants {
			if grant.Unit == unit {
				uses[member.ID] = uses[member.ID] || member.Active() && (grant.Until == "" || grant.Until > until)
			}
		}
	}
	for _, team := range d.teams {
		for _, grant := range team.Grants {
			if grant.Unit == unit {
				uses["team:"+team.ID] = uses["team:"+team.ID] || grant.Until == "" || grant.Until > until
			}
		}
	}
	d.mu.Unlock()
	for _, record := range d.ledger.RecordsFor(d.tenant) {
		submission := record.GetSubmission()
		if submission.GetSchema().GetName() == SchemaTeamSave {
			var team Team
			if json.Unmarshal(submission.GetPayload(), &team) == nil && slices.ContainsFunc(team.Grants, func(grant platform.Grant) bool { return grant.Unit == unit }) {
				id := "team:" + submission.GetTarget().GetId()
				if _, exists := uses[id]; !exists {
					uses[id] = false
				}
			}
		}
		if submission.GetSchema().GetName() != SchemaGrant {
			continue
		}
		var grant platform.Grant
		if json.Unmarshal(submission.GetPayload(), &grant) == nil && grant.Unit == unit {
			member := submission.GetTarget().GetId()
			if _, exists := uses[member]; !exists {
				uses[member] = false
			}
		}
	}
	result := make([]host.UnitGrantUsage, 0, len(uses))
	for member, current := range uses {
		result = append(result, host.UnitGrantUsage{Member: member, Current: current})
	}
	slices.SortFunc(result, func(a, b host.UnitGrantUsage) int { return strings.Compare(a.Member, b.Member) })
	return result
}
