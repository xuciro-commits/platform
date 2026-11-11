package host

import records "platformserver/kernel/records"

import (
	"maps"
	pageowner "platformserver/services/apps/pages"
	"platformserver/services/functions/authoring"
	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
	appsauthoring "platformserver/services/apps/authoring"
	"platformserver/services/resources/authoring"
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
	pages := pageowner.DefinitionProjection{Member: m, Entities: entities, Actions: actions, Definitions: registeredDefinitions, Criteria: t.modelCriteria, Function: sources.function, Operation: sources.operation,
		CheckQuery: func(q platform.NamedQuery, info platform.EntityInfo) error {
			return checkNamedQuery(q, info, t.services.Rules)
		}}
	out := []platform.Definition{}
	for _, registered := range registeredDefinitions {
		if !t.ownerActive(registered.Ref.App) {
			continue
		}
		if build.ProjectDelegated(m) {
			project := ""
			t.Projection.Lock()
			if resources := t.Projection.Type(build.ResourceType); resources != nil {
				if row := resources.Rows().Get(platform.ResourceID(t.ID, registered.Ref)); row != nil {
					project = row.Value.Interface().(build.Resource).Project
				}
			}
			t.Projection.Unlock()
			if role := t.ResourceRole(m, project); role == "" || role == platform.ProjectDiscovererRole {
				continue
			}
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
		case platform.AssetObjectTypeGroup:
			if def.ObjectTypeGroup == nil || !m.Active() || t.Admits(m) != nil {
				continue
			}
		case platform.AssetPropertyType:
			if def.PropertyType == nil {
				continue
			}
			visible := func(version string) bool {
				if m.Holds(def.Ref.App, "builder") {
					return true
				}
				return definitionVersionVisible(entities, def.Ref, version, func(f platform.FieldInfo) *platform.AssetBinding { return f.Property })
			}
			versions, currentVisible := retainVisibleDefinitionVersions(def.PropertyVersions, def.Version, visible)
			def.PropertyVersions = versions
			if !currentVisible {
				def.PropertyType = nil
				if len(versions) == 0 {
					continue
				}
			}
		case platform.AssetValueType:
			if def.ValueType == nil {
				continue
			}
			visible := func(version string) bool {
				if m.Holds(def.Ref.App, "builder") {
					return true
				}
				return definitionVersionVisible(entities, def.Ref, version, func(f platform.FieldInfo) *platform.AssetBinding { return f.ValueType })
			}
			versions, currentVisible := retainVisibleDefinitionVersions(def.ValueVersions, def.Version, visible)
			def.ValueVersions = versions
			if !currentVisible {
				def.ValueType = nil
				if len(versions) == 0 {
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
				if checkNamedQuery(query, info, t.services.Rules) != nil {
					delete(def.QueryVersions, version)
				}
			}
			if checkNamedQuery(*def.Query, info, t.services.Rules) != nil {
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
			caller := platform.NewCaller(runtime{t: t, scoped: true}, m, def.Ref.App, false, false)
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
			caller := platform.NewCaller(runtime{t: t, scoped: true}, m, def.Ref.App, false, false)
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
			caller := platform.NewCaller(runtime{t: t, scoped: true}, m, def.Ref.App, false, false)
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
				for _, provision := range t.App(def.Ref.App).Manifest().Provides {
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
			typ, _, _ := strings.Cut(def.OwnerRecord, "/")
			info, ok := entities[typ]
			if !ok || def.OwnerRecord == "" || !m.Active() || t.Admits(m) != nil {
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
	applications := appsauthoring.ApplicationProjection{Member: m, Definitions: out, Entities: entities, Pages: opens, Resources: visibleResources, PublishedFlows: publishedFlows}
	for _, registered := range registeredDefinitions {
		if !t.ownerActive(registered.Ref.App) {
			continue
		}
		if t.applicationEntryWithdrawn(registered.Ref) {
			continue
		}
		if def, ok := appsauthoring.ProjectApplication(registered, applications); ok {
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
		out[i].OwningProject, out[i].OwningProjectTitle, out[i].ManagedBy = t.definitionOwner(out[i])
	}
	return out
}

func (t *Tenant) definitionOwner(def platform.Definition) (string, string, string) {
	if def.Application == nil || def.Source == "code" {
		return "", "", ""
	}
	t.Projection.Lock()
	defer t.Projection.Unlock()
	resources := t.Projection.Type(build.ResourceType)
	if resources == nil {
		return "", "", ""
	}
	row := resources.Rows().Get(platform.ResourceID(t.ID, def.Ref))
	if row == nil {
		return "", "", ""
	}
	resource := row.Value.Interface().(build.Resource)
	title := ""
	if apps := t.Projection.Type(build.AppType); apps != nil {
		if project := apps.Rows().Get(resource.Project); project != nil {
			title = project.Value.Interface().(build.Application).Title
		}
	}
	return resource.Project, title, resource.ManagedBy
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

func definitionVersionVisible(entities map[string]platform.EntityInfo, ref platform.AssetRef, version string, pick func(platform.FieldInfo) *platform.AssetBinding) bool {
	for _, entity := range entities {
		for _, field := range entity.Fields {
			if binding := pick(field); binding != nil && binding.Ref == ref && binding.SourceVersion == version {
				return true
			}
		}
	}
	return false
}

func retainVisibleDefinitionVersions[V any](versions map[string]V, current string, visible func(string) bool) (map[string]V, bool) {
	retained := maps.Clone(versions)
	for version := range retained {
		if !visible(version) {
			delete(retained, version)
		}
	}
	return retained, visible(current)
}

type retainedPageDefinitionSources struct{ tenant *Tenant }

func (s retainedPageDefinitionSources) function(binding platform.AssetBinding) (platform.AIFunction, bool) {
	app := s.tenant.App(binding.Ref.App)
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

func (t *Tenant) visibleProvides(def platform.Definition, m platform.Member, entities map[string]platform.EntityInfo) []platform.Capability {
	provided := def.Provides
	if def.Action != nil || def.Query != nil || def.Function != nil || def.Operation != nil || def.Entity != nil || def.Protocol != nil || def.Observation != nil {
		if projected, err := t.describer().DescribeDefinition(def); err == nil {
			provided = projected.Provides
		} else {
			return []platform.Capability{}
		}
	}
	out := []platform.Capability{}
	for _, capability := range provided {
		if len(capability.Roles) > 0 && !m.May(def.Ref.App, capability.Name, capability.Roles).Allow {
			continue
		}
		if capability.Kind == "query" && def.Query != nil {
			input, output := authoring.QueryCapabilitySchemas(*def.Query, entities[def.Query.Object])
			if input != nil {
				capability.Inputs = *input
			}
			if output != nil {
				capability.Outputs = *output
			}
		}
		out = append(out, capability)
	}
	return out
}
