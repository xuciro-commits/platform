package build

import (
	"fmt"
	"platformserver/platform"
	"slices"
)

var readLevels = map[string]string{"all": platform.ScopeTenant, "below": platform.ScopeBelow, "unit": platform.ScopeUnit, "own": platform.ScopeOwn, "none": platform.ScopeNone}

func checkScope(o Object) error {
	field := func(name string) *Field {
		if i := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == name }); i >= 0 {
			return &o.Fields[i]
		}
		return nil
	}
	sc := o.Scope
	if sc == nil {
		sc = &ObjectScope{}
	}
	if sc.Owner != "" {
		if f := field(sc.Owner); f == nil || f.Type != "text" && f.Type != "reference" {
			return fmt.Errorf("the owner field %q is not a text or reference field of this object", sc.Owner)
		}
	}
	if sc.Unit != "" {
		if f := field(sc.Unit); f == nil || f.Type != "text" && f.Type != "reference" {
			return fmt.Errorf("the unit field %q is not a text or reference field of this object", sc.Unit)
		}
	}
	for _, a := range o.Access {
		if (a.Read == "unit" || a.Read == "below") && (sc.Unit == "" || sc.Structure == "") {
			return fmt.Errorf("the role %q reads by unit, so the object needs a unit field and a structure under Scope", a.Role)
		}
	}
	return nil
}

func checkAccess(o Object) error {
	roles := map[string]bool{Builder: true}
	for _, a := range o.Access {
		switch {
		case !Named(a.Role):
			return fmt.Errorf("the role %q is not lower-case letters and digits", a.Role)
		case a.Role == Builder:
			return fmt.Errorf("the role %q always does everything with what it builds", Builder)
		case roles[a.Role]:
			return fmt.Errorf("the role %q is given access twice", a.Role)
		case readLevels[a.Read] == "":
			return fmt.Errorf("the role %q reads %q; it reads all, below, unit, own or none", a.Role, a.Read)
		case a.Read == "none" && (a.Create || a.Edit || a.Archive):
			return fmt.Errorf("the role %q may not read the object, so it may not create, edit or archive it either", a.Role)
		}
		roles[a.Role] = true
	}
	known := func(role string) bool { return roles[role] || len(o.Access) == 0 && role == User }
	for _, act := range o.Actions {
		for _, r := range act.Roles {
			if !known(r) {
				return fmt.Errorf("the action %q is for %q, which is not a role of this object", act.Name, r)
			}
		}
	}
	for _, f := range o.Fields {
		for _, r := range append(slices.Clone(f.Read), f.Write...) {
			if !known(r) {
				return fmt.Errorf("the field %q names %q, which is not a role of this object", f.Name, r)
			}
		}
		if len(f.Read) > 0 {
			for _, r := range f.Write {
				if !slices.Contains(f.Read, r) {
					return fmt.Errorf("the field %q is set by %q, which may not read it", f.Name, r)
				}
			}
		}
	}
	return nil
}

func access(o Object) (platform.Standard, platform.Scope, []string) {
	std := platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, User}, Capability: o.Name}
	if len(o.Access) == 0 {
		return std, platform.Scope{}, []string{Builder, User}
	}
	std.Roles = []string{Builder}
	std.CreateRoles, std.EditRoles, std.ArchiveRoles = []string{Builder}, []string{Builder}, []string{Builder}
	scope := platform.Scope{Owner: platform.OwnerCreated, Levels: map[string]string{}, Default: platform.ScopeNone}
	if o.Scope != nil {
		if o.Scope.Owner != "" {
			scope.Owner = o.Scope.Owner
		}
		scope.Unit, scope.Structure = o.Scope.Unit, o.Scope.Structure
	}
	scope.Levels[Builder] = platform.ScopeTenant
	roles := []string{Builder}
	for _, a := range o.Access {
		roles = append(roles, a.Role)
		scope.Levels[a.Role] = readLevels[a.Read]
		if a.Create {
			std.CreateRoles = append(std.CreateRoles, a.Role)
		}
		if a.Edit {
			std.EditRoles = append(std.EditRoles, a.Role)
		}
		if a.Archive {
			std.ArchiveRoles = append(std.ArchiveRoles, a.Role)
		}
	}
	return std, scope, roles
}
