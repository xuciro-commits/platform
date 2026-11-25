package actions

import (
	"slices"
	"strings"

	"platformserver/platform"
)

type CatalogHooks struct {
	Apps          func() []platform.Service
	Entities      func() []platform.RecordTypeInfo
	Provider      func(used string) (platform.Service, string, bool)
	SchemaVersion func(platform.Service, platform.Action) (platform.SchemaVersion, error)
}

func (h CatalogHooks) Of(m platform.Member) []platform.Action {
	out := []platform.Action{}
	entities := map[string]platform.RecordTypeInfo{}
	for _, info := range h.Entities() {
		entities[info.Type] = info
	}
	for _, a := range h.Apps() {
		permitted := map[string][]platform.Action{}
		for _, action := range a.Manifest().Actions.All() {
			roles := m.RolesFor(a.Manifest().ID, action.Schema)
			key := strings.Join(roles, ",")
			if _, known := permitted[key]; !known {
				permitted[key] = a.Manifest().Actions.ForRoles(roles)
			}
			if !slices.ContainsFunc(permitted[key], func(p platform.Action) bool { return p.Schema == action.Schema }) || !m.May(a.Manifest().ID, action.Schema, action.Roles).Allow {
				continue
			}
			if !slices.ContainsFunc(action.Uses, func(used string) bool {
				owner, schema, _ := h.Provider(used)
				return owner == nil || !owner.Manifest().Actions.PermitsAny(m.RolesFor(owner.Manifest().ID, schema), schema)
			}) {
				if version, err := h.SchemaVersion(a, action); err == nil {
					action.SchemaVersion = &version
				}
				if info, ok := entities[action.Target]; ok && info.App == a.Manifest().ID &&
					slices.Contains(info.Standard, action.Schema) && (action.Schema == info.Type+".create" || action.Schema == info.Type+".edit") {
					action.Payload = slices.DeleteFunc(slices.Clone(action.Payload), func(input platform.Field) bool {
						field, visible := info.Field(input.Name)
						return !visible || field.ReadOnly || !slices.ContainsFunc(m.RolesIn(info.Type), field.Writes)
					})
				}
				out = append(out, action)
			}
		}
	}
	visible := make(map[string]bool, len(out))
	for _, action := range out {
		visible[action.Schema] = true
	}
	for i := range out {
		if !visible[out[i].Reverse] {
			out[i].Reverse = ""
		}
	}
	return out
}
