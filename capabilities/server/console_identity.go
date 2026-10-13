package platformserver

import (
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
