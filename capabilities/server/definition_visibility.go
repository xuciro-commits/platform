package platformserver

import records "platformserver/kernel/records"

import (
	"maps"
	pageowner "platformserver/studio/pages"
	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
	"platformserver/studio/build"
)

func (t *Tenant) definitionsFrom(m platform.Member, registeredDefinitions []platform.Definition) []platform.Definition {
	m = t.authorizationMember(m)
	entities := map[string]platform.EntityInfo{}
	for _, info := range t.Entities(m) {
		entities[info.Type] = info
	}
	actions := map[string]platform.Action{}
	for _, action := range t.Catalog(m) {
		actions[action.Schema] = action
	}
	sources := retainedPageDefinitionSources{tenant: t}
	pages := pageowner.DefinitionProjection{Member: m, Entities: entities, Actions: actions, Definitions: registeredDefinitions, Criteria: t.modelCriteria, Function: sources.function, Operation: sources.operation, CheckQuery: checkNamedQuery}
	out := []platform.Definition{}
	for _, registered := range registeredDefinitions {
		if !t.ownerActive(registered.Ref.App) {
			continue
		}
		def := registered
		switch def.Ref.Kind {
		case platform.AssetObject:
			info, ok := entities[def.Ref.Name]
			if !ok {
				continue
			}
			def.Entity = &info
		case platform.AssetAction:
			action, ok := actions[def.Ref.Name]
			if !ok {
				continue
			}
			def.Action = &action
		case platform.AssetPropertyType:
			if def.PropertyType == nil {
				continue
			}
			def.PropertyVersions = maps.Clone(def.PropertyVersions)
			visible := func(version string) bool {
				if m.Holds(def.Ref.App, "builder") {
					return true
				}
				for _, e := range entities {
					for _, f := range e.Fields {
						if f.Property != nil && f.Property.Ref == def.Ref && f.Property.SourceVersion == version {
							return true
						}
					}
				}
				return false
			}
			for version := range def.PropertyVersions {
				if !visible(version) {
					delete(def.PropertyVersions, version)
				}
			}
			if !visible(def.Version) {
				def.PropertyType = nil
				if len(def.PropertyVersions) == 0 {
					continue
				}
			}
		case platform.AssetLinkType:
			if def.LinkType == nil {
				continue
			}
			def.LinkVersions = maps.Clone(def.LinkVersions)
			visible := func(l platform.LinkType) bool {
				parent, pok := entities[l.Parent.Name]
				child, cok := entities[l.Child.Name]
				return pok && cok && l.CheckSchema(parent, child) == nil
			}
			for version, l := range def.LinkVersions {
				if !visible(l) {
					delete(def.LinkVersions, version)
				}
			}
			if !visible(*def.LinkType) {
				def.LinkType = nil
				if len(def.LinkVersions) == 0 {
					continue
				}
			}
		case platform.AssetQuery:
			if def.Query == nil {
				continue
			}
			if def.Query.Interface != "" {
				def.QueryVersions = maps.Clone(def.QueryVersions)
				for version, query := range def.QueryVersions {
					if visible, ok := pageowner.VisibleQuery(query, entities); ok {
						def.QueryVersions[version] = visible
					} else {
						delete(def.QueryVersions, version)
					}
				}
				if visible, ok := pageowner.VisibleQuery(*def.Query, entities); ok {
					def.Query = &visible
				} else {
					def.Query = nil
				}
				if def.Query == nil && len(def.QueryVersions) == 0 {
					continue
				}

				break
			}
			info, ok := entities[def.Query.Object]
			if !ok {
				continue
			}
			def.QueryVersions = maps.Clone(def.QueryVersions)
			for version, query := range def.QueryVersions {
				if checkNamedQuery(query, info) != nil {
					delete(def.QueryVersions, version)
				}
			}
			if checkNamedQuery(*def.Query, info) != nil {
				def.Query = nil
				if len(def.QueryVersions) == 0 {
					continue
				}
			}
		case platform.AssetFlow:
			if def.Flow == nil || len(m.RolesIn(def.Ref.App)) == 0 {
				continue
			}
		case platform.AssetFunction:
			caller := platform.NewCaller(runtime{t}, m, def.Ref.App, false, false)
			if def.Function == nil || !t.authorizeCapability(caller, def.Ref.App, def.Function.Name, def.Function.Roles, true, authorizationDetails{}).Allow {
				continue
			}
			info, ok := entities[def.Function.Object]
			if !ok || slices.ContainsFunc(def.Function.Fields, func(name string) bool {
				_, found := info.Field(name)
				return !found
			}) {
				continue
			}
		case platform.AssetAgent:
			if def.Agent == nil || !m.Active() || len(m.RolesIn(def.Ref.App)) == 0 && !m.Holds(platform.AgentApp, platform.AgentAdmin) {
				continue
			}
			if def.Source == "builder" && (def.OwnerRecord == "" || !t.readable(m, def.OwnerRecord, time.Now())) {
				continue
			}
			if !slices.Contains(m.RolesIn(def.Ref.App), "builder") {
				projection := *def.Agent
				projection.Instructions, projection.Tools, projection.Checkpoints, projection.Cases = "", nil, nil, nil
				def.Agent = &projection
			}
		case platform.AssetCompute:
			caller := platform.NewCaller(runtime{t}, m, def.Ref.App, false, false)
			if def.Operation == nil || !t.authorizeCapability(caller, def.Ref.App, def.Operation.Name, def.Operation.Roles, true, authorizationDetails{}).Allow {
				continue
			}
		case platform.AssetObservation:
			if def.Observation == nil {
				continue
			}
			if def.OwnerRecord != "" && !t.observationSourceActive(def.OwnerRecord) {
				continue
			}
			declaration := def.Observation
			caller := platform.NewCaller(runtime{t}, m, def.Ref.App, false, false)
			if !t.authorizeNative(caller, def.Ref.Name, "read", declaration.Roles, true).Allow {
				continue
			}
			info, found := entities[declaration.Object.Name]
			if !found {
				continue
			}
			if _, found := info.Field(declaration.DeviceField); !found {
				continue
			}
			visible := *declaration
			if visible.Decision != nil {
				readable := true
				for _, name := range observationDecisionFields(*visible.Decision) {
					if _, found := info.Field(name); !found {
						readable = false
					}
				}
				if _, found := actions[visible.Decision.Action.Name]; !found || !readable {
					visible.Decision = nil
				}
			}
			def.Observation = &visible
		case platform.AssetInterface:
			if def.Interface == nil {
				continue
			}
			if !m.Holds("build", "builder") && !slices.ContainsFunc(slices.Collect(maps.Values(entities)), func(info platform.EntityInfo) bool { return slices.Contains(info.Implements, def.Ref.Name) }) {
				continue
			}
		case platform.AssetProtocol:
			if def.Protocol == nil || !m.Active() {
				continue
			}
			projected := *def.Protocol
			projected.ActionBindings = maps.Clone(projected.ActionBindings)
			projected.ActionRoles = maps.Clone(projected.ActionRoles)
			projected.Actions = slices.DeleteFunc(slices.Clone(projected.Actions), func(action platform.Action) bool {
				binding := projected.ActionBindings[action.Schema]
				_, ok := actions[binding.Name]
				if !ok {
					delete(projected.ActionBindings, action.Schema)
					delete(projected.ActionRoles, action.Schema)
				}
				return !ok
			})
			projected.Reads = slices.DeleteFunc(slices.Clone(projected.Reads), func(name string) bool {
				for _, provision := range t.app(def.Ref.App).Manifest().Provides {
					if provision.Protocol.Name == projected.Name {
						return !t.authorizeEntry(m, def.Ref.App, provision.Reads[name], "read", Now()).Allow
					}
				}
				return true
			})
			if len(projected.Actions) == 0 && len(projected.Reads) == 0 {
				continue
			}
			def.Protocol = &projected
		default:
			if def.Source == "system" {
				if !m.Active() || !m.Holds(def.Ref.App, build.Builder) {
					continue
				}
				break
			}
			typ, _, _ := strings.Cut(def.OwnerRecord, "/")
			info, ok := entities[typ]
			if !ok || def.OwnerRecord == "" || !m.Active() || t.admits(m) != nil {
				continue
			}
			if _, visibleName := info.Field("name"); !visibleName || !t.readable(m, def.OwnerRecord, time.Now()) {
				continue
			}
		case platform.AssetPage:
			visible, ok := pages.Page(def)
			if !ok {
				continue
			}
			def = visible
		case platform.AssetApp:
			continue
		}
		out = append(out, def)
	}
	out = pageowner.FilterNavigation(out)

	opens := map[platform.AssetRef]bool{}
	for _, def := range out {
		if def.Ref.Kind == platform.AssetPage {
			opens[def.Ref] = true
		}
	}
	visibleResources := map[platform.AssetRef]bool{}
	for _, def := range out {
		visibleResources[def.Ref] = true
	}
	publishedFlows := map[platform.AssetRef]bool{}
	if m.Holds("build", "builder") && t.procs != nil {
		for _, registered := range registeredDefinitions {
			if registered.Application == nil {
				continue
			}
			for _, ref := range registered.Application.Resources {
				if ref.Kind == platform.AssetFlow {
					publishedFlows[ref] = t.procs.HasPublishedFlow(ref.Name)
				}
			}
		}
	}
	applications := build.ApplicationProjection{Member: m, Definitions: out, Entities: entities, Pages: opens, Resources: visibleResources, PublishedFlows: publishedFlows}
	for _, registered := range registeredDefinitions {
		if !t.ownerActive(registered.Ref.App) {
			continue
		}
		if t.applicationEntryWithdrawn(registered.Ref) {
			continue
		}
		if def, ok := build.ProjectApplication(registered, applications); ok {
			out = append(out, def)
		}
	}
	out = pageowner.FilterExplorationAssets(out)
	slices.SortFunc(out, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	visible := map[platform.AssetRef]bool{}
	for _, def := range out {
		visible[def.Ref] = true
	}
	for i := range out {
		out[i].Requires = slices.DeleteFunc(slices.Clone(out[i].Requires), func(ref platform.AssetRef) bool { return !visible[ref] })
		if out[i].Requires == nil {
			out[i].Requires = []platform.AssetRef{}
		}
		out[i].Provides = t.visibleProvides(out[i], m, entities)
		out[i].RID = platform.ResourceID(t.ID, out[i].Ref)
	}
	return out
}

func (t *Tenant) applicationEntryWithdrawn(ref platform.AssetRef) bool {
	if ref.App != build.ID || ref.Kind != platform.AssetApp {
		return false
	}
	t.records.Lock()
	defer t.records.Unlock()
	entity := t.records.Type(build.AppType)
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

type retainedPageDefinitionSources struct{ tenant *Tenant }

func (s retainedPageDefinitionSources) function(binding platform.AssetBinding) (platform.AIFunction, bool) {
	app := s.tenant.app(binding.Ref.App)
	owner, ok := app.(platform.FunctionDefinitions)
	if !ok {
		return platform.AIFunction{}, false
	}
	text, ok := strings.CutPrefix(binding.SourceVersion, app.Manifest().Version+".function-")
	version, err := strconv.Atoi(text)
	if !ok || err != nil {
		return platform.AIFunction{}, false
	}
	value, _, exists := owner.FunctionDefinition(binding.Ref.Name, version)
	return value, exists
}
func (s retainedPageDefinitionSources) operation(binding *platform.AssetBinding) (platform.Operation, bool) {
	value, _, err := s.tenant.pageOperation(binding)
	return value, err == nil
}
