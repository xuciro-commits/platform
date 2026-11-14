package kernel

import (
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func OwnersOf[T any](ids []string, apps []platform.App) map[string]T {
	out := map[string]T{}
	for i, a := range apps {
		if x, ok := a.(T); ok {
			out[ids[i]] = x
		}
	}
	return out
}

func ClaimRole[T any](slot *T, a platform.App, role string) error {
	x, ok := a.(T)
	if !ok {
		return nil
	}
	if any(*slot) != nil {
		return fmt.Errorf("two %s owners", role)
	}
	*slot = x
	return nil
}

type OptionalRoles struct {
	Directory platform.Directory
	Linker    platform.Linker
	Tasks     platform.Tasks
}

func (o OptionalRoles) Calendar(party string, day platform.Date) platform.Calendar {
	if o.Directory != nil {
		return o.Directory.Calendar(party, day)
	}
	return platform.Calendar{}
}

func (o OptionalRoles) PartyUnits(party, structure string, day platform.Date) []string {
	if o.Directory != nil {
		return o.Directory.Units(party, structure, day)
	}
	return nil
}

func (o OptionalRoles) Holders(structure, unit, role string, day platform.Date) []string {
	if o.Directory != nil {
		return o.Directory.Holders(structure, unit, role, day)
	}
	return nil
}

func (o OptionalRoles) MemberUnits(c platform.Caller, structure string, now time.Time) []string {
	return o.PartyUnits("member:"+c.ID, structure, c.Today(now))
}

func (o OptionalRoles) Element(id string, now time.Time) (platform.ElementInfo, bool) {
	if o.Directory == nil {
		return platform.ElementInfo{}, false
	}
	return o.Directory.Element(id, now.UTC().Format(time.DateOnly))
}

func (o OptionalRoles) Related(element, stereotype string, outgoing bool, now time.Time) []string {
	if o.Directory == nil {
		return nil
	}
	return o.Directory.Related(element, stereotype, outgoing, now.UTC().Format(time.DateOnly))
}

func (o OptionalRoles) Links(c platform.Caller, entity string) []string {
	var out []string
	if o.Linker != nil {
		out = o.Linker.Links(c, entity)
	}
	return records.AppendRelationNeighbors(out, c.OwnedRelations(entity), entity)
}

func (o OptionalRoles) Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if o.Linker == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return o.Linker.Link(c, from, to, key, now)
}

func (o OptionalRoles) Assign(c platform.Caller, rec *pb.ChangeRecord, a platform.Assignment) *kernel.Error {
	if o.Tasks == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return o.Tasks.Assign(c, rec, a)
}

func ClaimNames(owner map[string]string, tenant string, m platform.Manifest) error {
	var names []string
	for _, r := range m.Reads {
		names = append(names, "read:"+r)
	}
	for _, action := range m.Actions.All() {
		names = append(names, "action:"+action.Schema)
	}
	for input := range m.Inputs {
		names = append(names, "input:"+input)
	}
	for _, n := range names {
		if other := owner[n]; other != "" {
			return fmt.Errorf("tenant %s: %q is declared by %s and %s", tenant, n, other, m.ID)
		}
		owner[n] = m.ID
	}
	return nil
}

func DeclareOwned[R any](tenant string, apps []platform.App, declared func(platform.Manifest) bool, role R, kind string, declare func(R, platform.App) error) error {
	for _, a := range apps {
		if !declared(a.Manifest()) {
			continue
		}
		if any(role) == nil {
			return fmt.Errorf("tenant %s: %s declares %ss, and the tenant runs no %s app", tenant, a.Manifest().ID, kind, kind)
		}
		if err := declare(role, a); err != nil {
			return fmt.Errorf("tenant %s: %v", tenant, err)
		}
	}
	return nil
}

func OwnerOfClass(apps []platform.App, dataClass string) (string, bool) {
	for _, a := range apps {
		if slices.ContainsFunc(a.Declarations(), func(d *pb.AuthorityDeclaration) bool { return d.GetDataClass() == dataClass }) {
			return a.Manifest().ID, true
		}
	}
	return "", false
}

func DeclarationsOf(apps []platform.App) []*pb.AuthorityDeclaration {
	var out []*pb.AuthorityDeclaration
	for _, a := range apps {
		out = append(out, a.Declarations()...)
	}
	return out
}
