package records

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"slices"
	"time"
)

type ScopeDirectory interface {
	Units(party, structure string, day platform.Date) []string
	Below(units []string, structure string, day platform.Date) []string
}

// FieldView is a type as m may see it (ADR-0038): without the fields m's role
// in the app may not read, so search, filters, sort, grouping and forms never
// reach them; the rows are shared. hidden are those fields.
func FieldView(m platform.Member, source platform.EntityInfo) (view platform.EntityInfo, hidden []platform.FieldInfo) {
	roles := m.RolesIn(source.App)
	readable := func(f platform.FieldInfo) bool {
		if !f.ReadsAny(roles) || m.AIContext && f.AIInput != nil && !*f.AIInput {
			return false
		}
		if f.AliasOf != "" {
			target, found := source.Field(f.AliasOf)
			return found && target.ReadsAny(roles) && !(m.AIContext && target.AIInput != nil && !*target.AIInput)
		}
		return true
	}
	for _, f := range source.Fields {
		if !readable(f) {
			hidden = append(hidden, f)
		}
	}
	if len(hidden) == 0 {
		return source, nil
	}
	info := source
	info.Fields = slices.DeleteFunc(slices.Clone(info.Fields), func(f platform.FieldInfo) bool { return !readable(f) })
	return info, hidden
}

func ScopeVisibility(info platform.EntityInfo, m platform.Member, directory ScopeDirectory, now time.Time) (func(reflect.Value) bool, *kernel.Error) {
	roles, scope := m.RolesIn(info.App), info.Scope
	participant := func(v reflect.Value) bool {
		return scope.Participants != nil && slices.Contains(scope.Participants(v.Interface()), m.ID)
	}
	if len(roles) == 0 {
		if scope.Participants == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return participant, nil
	}
	base, err := scoped(directory, m, info, roles, now)
	if base == nil || err != nil || scope.Participants == nil {
		return base, err
	}
	return func(v reflect.Value) bool { return base(v) || participant(v) }, nil
}

// scoped is the widest scope m's roles give over a type's records (nil: all of them).
func scoped(directory ScopeDirectory, m platform.Member, info platform.EntityInfo, roles []string, now time.Time) (func(reflect.Value) bool, *kernel.Error) {
	// Each role contributes its own predicate; a wider level in one unit must
	// not discard a narrower role's independent unit.
	if len(roles) > 1 {
		var predicates []func(reflect.Value) bool
		for _, role := range roles {
			predicate, err := scoped(directory, m, info, []string{role}, now)
			if err != nil {
				return nil, err
			}
			if predicate == nil {
				return nil, nil
			}
			predicates = append(predicates, predicate)
		}
		return func(v reflect.Value) bool {
			return slices.ContainsFunc(predicates, func(p func(reflect.Value) bool) bool { return p(v) })
		}, nil
	}
	scope := info.Scope
	level := scope.LevelFor(roles)
	field := func(name string) func(reflect.Value) string {
		f, _ := info.Field(name)
		return func(v reflect.Value) string { return v.FieldByIndex(f.Index).String() }
	}
	switch level {
	case platform.ScopeNone:
		return func(reflect.Value) bool { return false }, nil
	case platform.ScopeOwn:
		if scope.Owner == platform.OwnerCreated { // whoever's decision created it (ADR-0044)
			return func(v reflect.Value) bool { return Header(v).Created.By == m.ID }, nil
		}
		owner := field(scope.Owner)
		return func(v reflect.Value) bool { return owner(v) == m.ID }, nil
	case platform.ScopeUnit, platform.ScopeBelow:
		var units []string
		if directory != nil {
			structure := scope.Structure
			if level == platform.ScopeUnit {
				structure = ""
			}
			// Combine membership-based grants and explicit unit grants of this role.
			held, unbound := false, false
			for _, g := range m.Grants {
				if g.App != info.App || !slices.Contains(roles, g.Role) {
					continue
				}
				held = true
				if g.Unit == "" {
					unbound = true
					continue
				}
				if level == platform.ScopeBelow && g.Structure != "" && g.Structure != scope.Structure {
					continue
				}
				units = append(units, directory.Below([]string{g.Unit}, structure, m.Today(now))...)
			}
			if !held || unbound {
				units = append(units, directory.Units("member:"+m.ID, structure, m.Today(now))...)
			}
		}
		unit := field(scope.Unit)
		return func(v reflect.Value) bool { return slices.Contains(units, unit(v)) }, nil
	}
	return nil, nil
}
