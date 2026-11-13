package actions

import (
	"slices"

	"platformserver/platform"
)

type CatalogHooks struct {
	Apps          func() []platform.App
	Entities      func() []platform.EntityInfo
	Provider      func(used string) (platform.App, string, bool)
	SchemaVersion func(platform.App, platform.Action) (platform.SchemaVersion, error)
}

func (h CatalogHooks) Of(m platform.Member) []platform.Action {
	out := []platform.Action{}
	entities := map[string]platform.EntityInfo{}
	for _, info := range h.Entities() {
		entities[info.Type] = info
	}
	for _, a := range h.Apps() {
		for _, action := range a.Manifest().Actions.ForRoles(m.RolesIn(a.Manifest().ID)) {
			if !m.May(a.Manifest().ID, action.Schema, action.Roles).Allow {
				continue
			}
			if !slices.ContainsFunc(action.Uses, func(used string) bool {
				owner, schema, _ := h.Provider(used)
				return owner == nil || !owner.Manifest().Actions.PermitsAny(m.RolesIn(owner.Manifest().ID), schema)
			}) {
				if version, err := h.SchemaVersion(a, action); err == nil {
					action.SchemaVersion = &version
				}
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
