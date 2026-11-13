package host

import records "platformserver/kernel/records"

import (
	"platformserver/services/actions"
	pageowner "platformserver/services/apps/pages"
	"platformserver/services/resources/authoring"
	"time"

	"platformserver/platform"
)

func (t *Tenant) definitionsFrom(m platform.Member, registeredDefinitions []platform.Definition) []platform.Definition {
	m = t.authorizationMember(m)
	entities := map[string]platform.EntityInfo{}
	for _, info := range t.Entities(m) {
		entities[info.Type] = info
	}
	actionsByName := map[string]platform.Action{}
	for _, action := range t.Catalog(m) {
		actionsByName[action.Schema] = action
	}
	pages := pageowner.DefinitionProjection{Member: m, Entities: entities, Actions: actionsByName, Definitions: registeredDefinitions, Criteria: t.modelCriteria, Function: func(binding platform.AssetBinding) (platform.AIFunction, bool) {
		return pageowner.RetainedFunction(t.App(binding.Ref.App), binding)
	}, Operation: func(binding *platform.AssetBinding) (platform.Operation, bool) {
		value, _, err := t.pageOperation(binding)
		return value, err == nil
	},
		CheckQuery: func(q platform.NamedQuery, info platform.EntityInfo) error {
			return checkNamedQuery(q, info, t.services.Rules)
		}}
	caller := func(app string) platform.Caller {
		return platform.NewCaller(runtime{t: t, scoped: true}, m, app, false, false)
	}
	return build.DefinitionVisibility{Tenant: t.ID, Member: m, Entities: entities, Actions: actionsByName, Registered: registeredDefinitions,
		OwnerActive:  t.OwnerActive,
		Denied:       func() bool { return t.Admits(m) != nil },
		ResourceRole: func(project string) string { return t.ResourceRole(m, project) },
		ResourceOf:   t.resourceRow,
		ProjectTitle: t.projectTitle,
		Capability: func(app, name string, roles []string) bool {
			return t.authorizeCapability(caller(app), app, name, roles, true, authorizationDetails{}).Allow
		},
		NativeRead: func(app, name string, roles []string) bool {
			return t.authorizeNative(caller(app), name, "read", roles, true).Allow
		},
		ProtocolRead: func(def platform.Definition, name string) bool {
			for _, provision := range t.App(def.Ref.App).Manifest().Provides {
				if provision.Protocol.Name == def.Protocol.Name {
					return t.authorizeEntry(m, def.Ref.App, provision.Reads[name], "read", Now()).Allow
				}
			}
			return false
		},
		Readable:                func(ref string) bool { return t.readable(m, ref, time.Now()) },
		ObservationSourceActive: func(id string) bool { return t.Projection.ObservationSourceActive(id) },
		CheckQuery: func(q platform.NamedQuery, info platform.EntityInfo) error {
			return checkNamedQuery(q, info, t.services.Rules)
		},
		Page:                      pages.Page,
		VisibleQuery:              pageowner.VisibleQuery,
		FilterNavigation:          pageowner.FilterNavigation,
		FilterExplorationAssets:   pageowner.FilterExplorationAssets,
		ProjectProtocol:           actions.ProjectProtocol,
		ApplicationEntryWithdrawn: t.applicationEntryWithdrawn,
		HasPublishedFlow:          func(name string) bool { return t.procs != nil && t.procs.HasPublishedFlow(name) },
		Describe: func(def platform.Definition) (platform.Definition, error) {
			return t.describer().DescribeDefinition(def)
		},
	}.Project()
}

func (t *Tenant) resourceRow(ref platform.AssetRef) (build.Resource, bool) {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	resources := t.Projection.Type(build.ResourceType)
	if resources == nil {
		return build.Resource{}, false
	}
	row := resources.Rows().Get(platform.ResourceID(t.ID, ref))
	if row == nil {
		return build.Resource{}, false
	}
	return row.Value.Interface().(build.Resource), true
}

func (t *Tenant) projectTitle(project string) string {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	if apps := t.Projection.Type(build.AppType); apps != nil {
		if row := apps.Rows().Get(project); row != nil {
			return row.Value.Interface().(build.Application).Title
		}
	}
	return ""
}

func (t *Tenant) applicationEntryWithdrawn(ref platform.AssetRef) bool {
	if ref.App != build.ID || ref.Kind != platform.AssetApp {
		return false
	}
	t.Projection.Lock()
	defer t.Projection.Unlock()
	entity := t.Projection.Type(build.AppType)
	if entity == nil {
		return false
	}
	for _, row := range entity.Rows().All() {
		if !row.Value.FieldByName("Record").Interface().(platform.Record).Archived && records.BusinessField(entity.Info(), row.Value, "name") == ref.Name {
			return records.BusinessField(entity.Info(), row.Value, "state") == "withdrawn"
		}
	}
	return false
}
