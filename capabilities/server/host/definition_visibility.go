package host

import (
	"platformserver/services/actions"
	pageowner "platformserver/services/apps/pages"
	"platformserver/services/objects"
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
			return objects.CheckNamedQuery(q, info, t.services.Rules)
		}}
	caller := func(app string) platform.Caller {
		return platform.NewCaller(runtime{t: t, scoped: true}, m, app, false, false)
	}
	return build.DefinitionVisibility{Tenant: t.ID, Member: m, Entities: entities, Actions: actionsByName, Registered: registeredDefinitions,
		OwnerActive:  t.OwnerActive,
		Denied:       func() bool { return t.Admits(m) != nil },
		ResourceRole: func(project string) string { return t.ResourceRole(m, project) },
		ResourceOf: func(ref platform.AssetRef) (build.Resource, bool) {
			return build.StoreResourceRow(t.Projection, t.ID, ref)
		},
		ProjectTitle: func(project string) string { return build.StoreProjectTitle(t.Projection, project) },
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
			return objects.CheckNamedQuery(q, info, t.services.Rules)
		},
		Page:                      pages.Page,
		VisibleQuery:              pageowner.VisibleQuery,
		FilterNavigation:          pageowner.FilterNavigation,
		FilterExplorationAssets:   pageowner.FilterExplorationAssets,
		ProjectProtocol:           actions.ProjectProtocol,
		ApplicationEntryWithdrawn: func(ref platform.AssetRef) bool { return build.StoreApplicationEntryWithdrawn(t.Projection, ref) },
		HasPublishedFlow:          func(name string) bool { return t.procs != nil && t.procs.HasPublishedFlow(name) },
		Describe: func(def platform.Definition) (platform.Definition, error) {
			return t.describer().DescribeDefinition(def)
		},
	}.Project()
}
