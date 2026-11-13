package build

import (
	"maps"
	"slices"
	"strings"

	"platformserver/platform"
	appsauthoring "platformserver/services/apps/authoring"
	dataowner "platformserver/services/data"
	functionauthoring "platformserver/services/functions/authoring"
	ontology "platformserver/services/ontology"
)

type DefinitionVisibility struct {
	Tenant                    string
	Member                    platform.Member
	Entities                  map[string]platform.EntityInfo
	Actions                   map[string]platform.Action
	Registered                []platform.Definition
	OwnerActive               func(app string) bool
	Denied                    func() bool
	ResourceRole              func(project string) string
	ResourceOf                func(ref platform.AssetRef) (Resource, bool)
	ProjectTitle              func(project string) string
	Capability                func(app, name string, roles []string) bool
	NativeRead                func(app, name string, roles []string) bool
	ProtocolRead              func(def platform.Definition, name string) bool
	Readable                  func(ref string) bool
	ObservationSourceActive   func(id string) bool
	CheckQuery                func(q platform.NamedQuery, info platform.EntityInfo) error
	Page                      func(def platform.Definition) (platform.Definition, bool)
	VisibleQuery              func(q platform.NamedQuery, entities map[string]platform.EntityInfo) (platform.NamedQuery, bool)
	FilterNavigation          func([]platform.Definition) []platform.Definition
	FilterExplorationAssets   func([]platform.Definition) []platform.Definition
	ProjectProtocol           func(p platform.ProtocolReleaseDescriptor, actions map[string]platform.Action, readable func(name string) bool) (*platform.ProtocolReleaseDescriptor, bool)
	ApplicationEntryWithdrawn func(ref platform.AssetRef) bool
	HasPublishedFlow          func(name string) bool
	Describe                  func(def platform.Definition) (platform.Definition, error)
}

func (v DefinitionVisibility) Project() []platform.Definition {
	m, entities, actionsByName := v.Member, v.Entities, v.Actions
	out := []platform.Definition{}
	for _, registered := range v.Registered {
		if !v.OwnerActive(registered.Ref.App) {
			continue
		}
		if ProjectDelegated(m) {
			project := ""
			if resource, ok := v.ResourceOf(registered.Ref); ok {
				project = resource.Project
			}
			if role := v.ResourceRole(project); role == "" || role == platform.ProjectDiscovererRole {
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
			action, ok := actionsByName[def.Ref.Name]
			if !ok {
				continue
			}
			def.Action = &action
		case platform.AssetObjectTypeGroup:
			if def.ObjectTypeGroup == nil || !m.Active() || v.Denied() {
				continue
			}
		case platform.AssetPropertyType, platform.AssetValueType, platform.AssetLinkType:
			if !ontology.ProjectDefinition(&def, entities, m.Holds(def.Ref.App, "builder")) {
				continue
			}
		case platform.AssetQuery:
			if def.Query == nil {
				continue
			}
			if def.Query.Interface != "" {
				def.QueryVersions = maps.Clone(def.QueryVersions)
				for version, query := range def.QueryVersions {
					if visible, ok := v.VisibleQuery(query, entities); ok {
						def.QueryVersions[version] = visible
					} else {
						delete(def.QueryVersions, version)
					}
				}
				if visible, ok := v.VisibleQuery(*def.Query, entities); ok {
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
				if v.CheckQuery(query, info) != nil {
					delete(def.QueryVersions, version)
				}
			}
			if v.CheckQuery(*def.Query, info) != nil {
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
			if def.Function == nil || !v.Capability(def.Ref.App, def.Function.Name, def.Function.Roles) {
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
			if def.Source == "builder" && (def.OwnerRecord == "" || !v.Readable(def.OwnerRecord)) {
				continue
			}
			if !slices.Contains(m.RolesIn(def.Ref.App), "builder") {
				projection := *def.Agent
				projection.Instructions, projection.Tools, projection.Checkpoints, projection.Cases = "", nil, nil, nil
				def.Agent = &projection
			}
		case platform.AssetCompute:
			if def.Operation == nil || !v.Capability(def.Ref.App, def.Operation.Name, def.Operation.Roles) {
				continue
			}
		case platform.AssetObservation:
			if def.Observation == nil {
				continue
			}
			if def.OwnerRecord != "" && !v.ObservationSourceActive(def.OwnerRecord) {
				continue
			}
			declaration := def.Observation
			if !v.NativeRead(def.Ref.App, def.Ref.Name, declaration.Roles) {
				continue
			}
			visible, ok := dataowner.ProjectObservation(declaration, entities, actionsByName)
			if !ok {
				continue
			}
			def.Observation = visible
		case platform.AssetInterface:
			if !ontology.ProjectDefinition(&def, entities, m.Holds("build", "builder")) {
				continue
			}
		case platform.AssetProtocol:
			if def.Protocol == nil || !m.Active() {
				continue
			}
			projected, ok := v.ProjectProtocol(*def.Protocol, actionsByName, func(name string) bool { return v.ProtocolRead(def, name) })
			if !ok {
				continue
			}
			def.Protocol = projected
		default:
			typ, _, _ := strings.Cut(def.OwnerRecord, "/")
			info, ok := entities[typ]
			if !ok || def.OwnerRecord == "" || !m.Active() || v.Denied() {
				continue
			}
			if _, visibleName := info.Field("name"); !visibleName || !v.Readable(def.OwnerRecord) {
				continue
			}
		case platform.AssetPage:
			visible, ok := v.Page(def)
			if !ok {
				continue
			}
			def = visible
		case platform.AssetApp:
			if !m.Active() {
				continue
			}
		}
		out = append(out, def)
	}
	out = v.FilterNavigation(out)
	if !m.Holds("platform", "admin") {
		pages := map[platform.AssetRef]bool{}
		for _, definition := range out {
			if definition.Ref.Kind == platform.AssetPage {
				pages[definition.Ref] = true
			}
		}
		out = slices.DeleteFunc(out, func(definition platform.Definition) bool {
			if definition.Ref.Kind != platform.AssetApp {
				return false
			}
			return definition.Application == nil || !slices.ContainsFunc(definition.Application.Pages, func(name string) bool {
				return pages[platform.AssetRef{App: definition.Ref.App, Kind: platform.AssetPage, Name: name}]
			})
		})
	}

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
	if m.Holds("build", "builder") {
		for _, registered := range v.Registered {
			if registered.Application == nil {
				continue
			}
			for _, ref := range registered.Application.Resources {
				if ref.Kind == platform.AssetFlow {
					publishedFlows[ref] = v.HasPublishedFlow(ref.Name)
				}
			}
		}
	}
	applications := appsauthoring.ApplicationProjection{Member: m, Definitions: out, Entities: entities, Pages: opens, Resources: visibleResources, PublishedFlows: publishedFlows}
	for _, registered := range v.Registered {
		if !v.OwnerActive(registered.Ref.App) || v.ApplicationEntryWithdrawn(registered.Ref) {
			continue
		}
		if def, ok := appsauthoring.ProjectApplication(registered, applications); ok {
			if index := slices.IndexFunc(out, func(candidate platform.Definition) bool { return candidate.Ref == def.Ref }); index >= 0 {
				out[index] = def
			} else {
				out = append(out, def)
			}
		}
	}
	out = v.FilterExplorationAssets(out)
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
		out[i].Provides = v.visibleProvides(out[i])
		out[i].RID = platform.ResourceID(v.Tenant, out[i].Ref)
		out[i].OwningProject, out[i].OwningProjectTitle, out[i].ManagedBy = v.owner(out[i])
	}
	return out
}

func (v DefinitionVisibility) owner(def platform.Definition) (string, string, string) {
	if def.Application == nil || def.Source == "code" {
		return "", "", ""
	}
	resource, ok := v.ResourceOf(def.Ref)
	if !ok {
		return "", "", ""
	}
	return resource.Project, v.ProjectTitle(resource.Project), resource.ManagedBy
}

func (v DefinitionVisibility) visibleProvides(def platform.Definition) []platform.Capability {
	provided := def.Provides
	if def.Action != nil || def.Query != nil || def.Function != nil || def.Operation != nil || def.Entity != nil || def.Protocol != nil || def.Observation != nil {
		if projected, err := v.Describe(def); err == nil {
			provided = projected.Provides
		} else {
			return []platform.Capability{}
		}
	}
	out := []platform.Capability{}
	for _, capability := range provided {
		if len(capability.Roles) > 0 && !v.Member.May(def.Ref.App, capability.Name, capability.Roles).Allow {
			continue
		}
		if capability.Kind == "query" && def.Query != nil {
			input, output := functionauthoring.QueryCapabilitySchemas(*def.Query, v.Entities[def.Query.Object])
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
