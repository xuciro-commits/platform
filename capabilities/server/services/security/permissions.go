package security

import (
	"maps"
	"reflect"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
	controlowner "platformserver/services/security/control"
)

type RolePermissions struct {
	Role    string             `json:"role"`
	Holders int                `json:"holders"`
	Actions []PermissionAction `json:"actions"`
}

type PermissionAction struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Scope string `json:"scope"`
}

type FieldPermission struct {
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}

type ObjectPermissions struct {
	Type         string            `json:"type"`
	Title        string            `json:"title"`
	DefaultScope string            `json:"defaultScope,omitempty"`
	RoleScopes   map[string]string `json:"roleScopes,omitempty"`
	OwnerField   string            `json:"ownerField,omitempty"`
	UnitField    string            `json:"unitField,omitempty"`
	Structure    string            `json:"structure,omitempty"`
	Participants bool              `json:"participants,omitempty"`
	Through      bool              `json:"through,omitempty"`
	Fields       []FieldPermission `json:"fields,omitempty"`
}

type AppPermissions struct {
	App      string              `json:"app"`
	Title    string              `json:"title"`
	Reads    []string            `json:"reads"`
	Everyone []string            `json:"everyone"`
	Roles    []RolePermissions   `json:"roles"`
	Objects  []ObjectPermissions `json:"objects,omitempty"`
}

type Explanation struct {
	Member     string                 `json:"member"`
	Permission string                 `json:"permission"`
	App        string                 `json:"app"`
	Roles      []string               `json:"roles"`
	Allowed    []string               `json:"allowed"`
	Grants     []platform.Grant       `json:"grants,omitempty"`
	Markings   []string               `json:"markings,omitempty"`
	Verdict    platform.AuthzDecision `json:"verdict"`
}

type PermissionsReport struct {
	Apps    func() []platform.Service
	Holders func(app, role string) int
}

func spaceDisplayTitle(space string) string {
	switch space {
	case "crm":
		return "CRM"
	case "hr", "hr-core":
		return "HR"
	case "uom":
		return "Units of measure"
	case "geo":
		return "Places"
	case "party":
		return "Business partners"
	}
	if space == "" {
		return ""
	}
	return strings.ToUpper(space[:1]) + space[1:]
}

func describeObjectPermissions(appID string, entity platform.RecordType) ObjectPermissions {
	roleScopes := map[string]string{}
	for role, scope := range entity.Scope.Levels {
		if role != "builder" || appID == "build" && !strings.Contains(entity.Type, ".") {
			roleScopes[role] = scope
		}
	}
	if space, _, ok := strings.Cut(entity.Type, "."); ok && space != appID {
		delete(roleScopes, "builder")
	}
	item := ObjectPermissions{
		Type:         entity.Type,
		Title:        entity.Title,
		DefaultScope: entity.Scope.Default,
		OwnerField:   entity.Scope.Owner,
		UnitField:    entity.Scope.Unit,
		Structure:    entity.Scope.Structure,
		Participants: entity.Scope.Participants != nil,
		Through:      entity.Scope.Through != nil || entity.Scope.Share != nil,
	}
	if item.Title == "" {
		item.Title = entity.Type
	}
	if len(roleScopes) > 0 {
		item.RoleScopes = roleScopes
	}
	if info, err := records.Describe(appID, entity, func(reflect.Type) string { return "?" }); err == nil {
		if info.Title != "" {
			item.Title = info.Title
		}
		for _, field := range info.Fields {
			if field.Internal || len(field.Read) == 0 && len(field.Write) == 0 {
				continue
			}
			read := slices.DeleteFunc(slices.Clone(field.Read), func(r string) bool { return r == "builder" && len(field.Read) > 1 })
			write := slices.DeleteFunc(slices.Clone(field.Write), func(r string) bool { return r == "builder" && len(field.Write) > 1 })
			item.Fields = append(item.Fields, FieldPermission{Name: field.Name, Title: field.Title, Read: read, Write: write})
		}
	}
	return item
}

func (p PermissionsReport) Permissions() []AppPermissions {
	out := []AppPermissions{}
	seen := map[string]bool{}
	spaceRoles := map[string][]string{}
	spaceActions := map[string][]platform.Action{}
	spaceObjects := map[string][]ObjectPermissions{}
	spaceOwners := map[string]platform.Manifest{}
	for _, a := range p.Apps() {
		m := a.Manifest()
		seen[m.ID] = true
		ap := AppPermissions{App: m.ID, Title: m.Title, Reads: append([]string{}, m.Reads...), Everyone: append([]string{}, m.Everyone...), Roles: []RolePermissions{}}
		if ap.Title == "" {
			ap.Title = m.ID
		}
		for _, role := range m.AllRoles() {
			if m.Actions.Custom(role) {
				ownApp := false
				for _, act := range m.Actions.ForRoles([]string{role}) {
					if slices.Contains(act.Roles, platform.AnyMember) {
						continue
					}
					if space, _, ok := strings.Cut(act.Target, "."); !ok || space == "" || space == m.ID {
						ownApp = true
					} else if !slices.Contains(spaceRoles[space], role) {
						spaceRoles[space] = append(spaceRoles[space], role)
						spaceOwners[space] = m
					}
				}
				if !ownApp {
					continue
				}
			}
			rp := RolePermissions{Role: role, Holders: p.Holders(m.ID, role), Actions: []PermissionAction{}}
			for _, act := range m.Actions.ForRoles([]string{role}) {
				if space, _, ok := strings.Cut(act.Target, "."); ok && space != "" && space != m.ID && m.ID == "build" {
					continue
				}
				rp.Actions = append(rp.Actions, PermissionAction{ID: act.Schema, Title: act.Title, Scope: act.Target})
			}
			ap.Roles = append(ap.Roles, rp)
		}
		for _, act := range m.Actions.All() {
			space, _, ok := strings.Cut(act.Target, ".")
			if !ok || space == "" || space == m.ID {
				continue
			}
			spaceOwners[space] = m
			spaceActions[space] = append(spaceActions[space], act)
			for _, role := range act.Roles {
				if role != "builder" && role != platform.AnyMember && !slices.Contains(spaceRoles[space], role) {
					spaceRoles[space] = append(spaceRoles[space], role)
				}
			}
		}
		for _, entity := range m.Entities {
			obj := describeObjectPermissions(m.ID, entity)
			space, _, ok := strings.Cut(entity.Type, ".")
			if !ok || space == "" || space == m.ID {
				ap.Objects = append(ap.Objects, obj)
				continue
			}
			spaceOwners[space] = m
			spaceObjects[space] = append(spaceObjects[space], obj)
			for role := range entity.Scope.Levels {
				if role != "builder" && !slices.Contains(spaceRoles[space], role) {
					spaceRoles[space] = append(spaceRoles[space], role)
				}
			}
		}
		slices.SortFunc(ap.Objects, func(a, b ObjectPermissions) int { return strings.Compare(a.Type, b.Type) })
		out = append(out, ap)
	}
	spaces := map[string]bool{}
	for space := range spaceRoles {
		spaces[space] = true
	}
	for space := range spaceObjects {
		spaces[space] = true
	}
	for _, space := range slices.Sorted(maps.Keys(spaces)) {
		if seen[space] {
			continue
		}
		roles := slices.Clone(spaceRoles[space])
		slices.Sort(roles)
		if len(roles) == 0 && len(spaceObjects[space]) == 0 {
			continue
		}
		ap := AppPermissions{
			App: space, Title: spaceDisplayTitle(space),
			Reads: []string{}, Everyone: []string{},
			Roles:   make([]RolePermissions, 0, len(roles)),
			Objects: slices.Clone(spaceObjects[space]),
		}
		slices.SortFunc(ap.Objects, func(a, b ObjectPermissions) int { return strings.Compare(a.Type, b.Type) })
		ownerManifest, hasOwner := spaceOwners[space]
		for _, role := range roles {
			rp := RolePermissions{Role: role, Holders: p.Holders(space, role), Actions: []PermissionAction{}}
			if hasOwner && ownerManifest.Actions != nil && ownerManifest.Actions.Custom(role) {
				for _, act := range ownerManifest.Actions.ForRoles([]string{role}) {
					if targetSpace, _, ok := strings.Cut(act.Target, "."); ok && targetSpace == space {
						rp.Actions = append(rp.Actions, PermissionAction{ID: act.Schema, Title: act.Title, Scope: act.Target})
					}
				}
			} else {
				for _, act := range spaceActions[space] {
					if slices.Contains(act.Roles, role) || slices.Contains(act.Roles, "user") || slices.Contains(act.Roles, platform.AnyMember) {
						rp.Actions = append(rp.Actions, PermissionAction{ID: act.Schema, Title: act.Title, Scope: act.Target})
					}
				}
			}
			slices.SortFunc(rp.Actions, func(a, b PermissionAction) int { return strings.Compare(a.ID, b.ID) })
			ap.Roles = append(ap.Roles, rp)
		}
		out = append(out, ap)
	}
	key := func(a AppPermissions) string {
		if a.App == controlowner.PlatformApp {
			return " "
		}
		return a.App
	}
	slices.SortFunc(out, func(a, b AppPermissions) int { return strings.Compare(key(a), key(b)) })
	return out
}

type PermissionExplainer struct {
	Member func(string) (platform.Member, bool)
	App    func(string) platform.Service
	Apps   func() []platform.Service
	Entry  func(m platform.Member, app, name string) platform.AuthzDecision
	Caller func(m platform.Member, app string) platform.Caller
}

func (e PermissionExplainer) Explain(member, permission string) (Explanation, *platform.Error) {
	m, ok := e.Member(member)
	if !ok {
		return Explanation{}, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	out := Explanation{
		Member: member, Permission: permission,
		Roles: []string{}, Allowed: []string{},
		Grants: slices.Clone(m.Grants), Markings: slices.Clone(m.Markings),
	}
	if app, name, isRead := strings.Cut(permission, ":read:"); isRead {
		a := e.App(app)
		if a == nil {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No app {app}", app)
		}
		mf := a.Manifest()
		out.App, out.Roles = app, nonNil(m.RolesIn(app))
		switch {
		case slices.Contains(mf.Everyone, name):
			out.Allowed = []string{platform.AnyMember}
		case slices.Contains(mf.Reads, name):
			out.Allowed = mf.AllRoles()
		default:
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No read {name} in {app}", name, app)
		}
		out.Verdict = e.Entry(m, app, name)
		return out, nil
	}
	for _, a := range e.Apps() {
		mf := a.Manifest()
		if action, own := mf.Actions.Action(permission); own {
			caller := e.Caller(m, mf.ID)
			allowed := action.Roles
			if req, _, ok := mf.Actions.AuthorizationRequest(caller, permission, ""); ok {
				allowed = req.Allowed
			}
			appID := mf.ID
			if space, _, ok := strings.Cut(action.Target, "."); ok && space != "" && space != mf.ID && e.App(space) == nil {
				appID = space
			}
			out.App, out.Roles, out.Allowed = appID, nonNil(m.RolesFor(mf.ID, permission)), nonNil(allowed)
			out.Verdict = mf.Actions.Decide(caller, permission)
			return out, nil
		}
	}
	return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No action {action}", permission)
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
