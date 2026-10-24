package platformserver

// The tenant's read-only facade: what a member may call, the authority's
// declarations, the app list and the member directory.

import (
	"cmp"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"slices"
)

// Catalog is what m may call: each app's actions for m's role there, and an
// action that uses protocol actions only when m may call the bound provider's.
func (t *Tenant) Catalog(m platform.Member) []platform.Action {
	m = m.WithAuthorization(t.services.Authorization)
	out := []platform.Action{}
	entities := map[string]platform.EntityInfo{}
	for _, info := range t.Entities(m) {
		entities[info.Type] = info
	}
	for _, a := range t.liveApps() {
		for _, action := range a.Manifest().Actions.ForRoles(m.RolesIn(a.Manifest().ID)) {
			if !m.May(a.Manifest().ID, action.Schema, action.Roles).Allow {
				continue
			}
			if !slices.ContainsFunc(action.Uses, func(used string) bool {
				owner, schema, _ := t.provider(used)
				return owner == nil || !owner.Manifest().Actions.PermitsAny(m.RolesIn(owner.Manifest().ID), schema)
			}) {
				if version, err := t.actionSchemaVersion(a, action); err == nil {
					action.SchemaVersion = &version
				}
				// Generated create/edit forms use this member's writable fields.
				// Custom actions keep their own declared inputs and policy.
				if info, ok := entities[action.Target]; ok && info.App == a.Manifest().ID &&
					slices.Contains(info.Standard, action.Schema) && (action.Schema == info.Type+".create" || action.Schema == info.Type+".edit") {
					action.Payload = slices.DeleteFunc(slices.Clone(action.Payload), func(input platform.Field) bool {
						field, visible := info.Field(input.Name)
						return !visible || field.ReadOnly || !field.Writes(m.Roles[info.App])
					})
				}
				out = append(out, action)
			}
		}
	}
	return out
}

func (t *Tenant) Declarations() []*pb.AuthorityDeclaration {
	var out []*pb.AuthorityDeclaration
	for _, a := range t.apps {
		out = append(out, a.Declarations()...)
	}
	return out
}

// Apps describes the enabled apps from their manifests: discovery,
// the protocol graph and the capability matrix read this.
func (t *Tenant) Apps() []AppInfo {
	out := []AppInfo{}
	for _, a := range t.liveApps() {
		m := a.Manifest()
		info := AppInfo{ID: m.ID, Title: m.Title, Version: m.Version, Reads: append([]string{}, m.Reads...), Provides: []string{}, Consumes: []string{},
			Roles: m.AllRoles(), Capabilities: m.Actions.Capabilities(), Inputs: []string{}, Uses: []string{}, Subscribes: append([]string{}, m.Subscribes...), Emits: append([]platform.EffectKind{}, m.Emits...), Interfaces: append([]platform.Interface{}, m.Interfaces...)}
		for input, journaled := range m.Inputs {
			info.Inputs = append(info.Inputs, input+map[bool]string{true: "", false: " (not journaled)"}[journaled])
		}
		slices.Sort(info.Inputs)
		for _, pv := range m.Provides {
			info.Provides = append(info.Provides, pv.Protocol.ID())
		}
		for _, c := range m.Consumes {
			info.Consumes = append(info.Consumes, c.Protocol+map[bool]string{true: " (optional)", false: ""}[c.Optional])
		}
		for _, action := range m.Actions.All() {
			for _, used := range action.Uses {
				info.Uses = append(info.Uses, action.Schema+" → "+used)
			}
		}
		out = append(out, info)
	}
	return out
}

// AppEntry is an app a member may open in the workspace: the
// tenant runs it and the member holds a role in it.
type AppEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Role  string `json:"role"`
}

// AppsOf are the apps the member holds a role in, in the order the tenant runs them.
func (t *Tenant) AppsOf(m platform.Member) []AppEntry {
	out := []AppEntry{}
	for _, a := range t.liveApps() {
		man := a.Manifest()
		if role := m.Roles[man.ID]; role != "" {
			out = append(out, AppEntry{ID: man.ID, Title: cmp.Or(man.Title, man.ID), Role: role})
		}
	}
	return out
}

type AppInfo struct {
	ID           string                    `json:"id"`
	Title        string                    `json:"title"` // the manifest's, in the caller's language
	Version      string                    `json:"version"`
	Reads        []string                  `json:"reads"`
	Roles        []string                  `json:"roles"`
	Capabilities []platform.CapabilityInfo `json:"capabilities"`
	Inputs       []string                  `json:"inputs"`
	Uses         []string                  `json:"uses"`
	Subscribes   []string                  `json:"subscribes"`
	Provides     []string                  `json:"provides"`
	Consumes     []string                  `json:"consumes"`
	Emits        []platform.EffectKind     `json:"emits"`
	Interfaces   []platform.Interface      `json:"interfaces"` // the shapes this app declares (A2)
}

// Member is a member of the tenant's directory, as the console holds it now:
// for tests and tools that act as a member.
func (t *Tenant) Member(id string) (platform.Member, bool) {
	if t.identity == nil {
		return platform.Member{}, false
	}
	member, found := t.identity.MemberByID(id)
	return member.WithAuthorization(t.services.Authorization), found
}
