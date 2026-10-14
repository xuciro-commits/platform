package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
)

// registerDefinitions indexes installed code declarations at composition. The
// entity/action owners still validate and execute them. This layer checks only
// cross-descriptor identity and references, without creating a second runtime.
func (t *Tenant) registerDefinitions() error {
	classes := map[string]bool{}
	versions := map[string]string{}
	for _, app := range t.apps {
		manifest := app.Manifest()
		versions[manifest.ID] = manifest.Version
		for _, declaration := range app.Declarations() {
			classes[declaration.GetDataClass()] = true
		}
	}
	objects := map[string]platform.AssetRef{}
	objectInfo := map[string]platform.EntityInfo{}
	for _, et := range t.records.sortedTypes() {
		info := et.info
		objects[info.Type] = platform.AssetRef{App: info.App, Kind: platform.AssetObject, Name: info.Type}
		objectInfo[info.Type] = info
	}
	seen := map[platform.AssetRef]bool{}
	add := func(def platform.Definition) error {
		if err := def.Ref.Check(); err != nil {
			return err
		}
		if seen[def.Ref] {
			return fmt.Errorf("asset %s is declared twice", def.Ref)
		}
		seen[def.Ref] = true
		t.definitions = append(t.definitions, def)
		return nil
	}
	for _, et := range t.records.sortedTypes() {
		info := et.info
		requires := append(platform.ConstraintDependencies(info), platform.RecordSetDependencies(info)...)
		for _, field := range info.Fields {
			if field.Property != nil {
				requires = append(requires, field.Property.Ref)
			}
			if field.Ref == "" {
				continue
			}
			if field.Type != "reference" && field.Type != "references" {
				return fmt.Errorf("asset %s field %s has reference %s with incompatible type %s", objects[info.Type], field.Name, field.Ref, field.Type)
			}
			// Enterprise elements belong to the host directory, not the record
			// store. The submission path checks availability and stereotype;
			// an optional reference may be declared without that directory loaded.
			if field.Ref == "enterprise.element" {
				continue
			}
			dep, ok := objects[field.Ref]
			if !ok {
				return fmt.Errorf("asset %s field %s requires missing object %s", objects[info.Type], field.Name, field.Ref)
			}
			requires = append(requires, dep)
		}
		if err := add(platform.Definition{Ref: objects[info.Type], Source: "code", Version: versions[info.App], ContractVersion: 1,
			Requires: uniqueRefs(requires), Entity: &info}); err != nil {
			return err
		}
	}
	actionByRef := map[platform.AssetRef]platform.Action{}
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, action := range manifest.Actions.All() {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetAction, Name: action.Schema}
			var requires []platform.AssetRef
			if dep, ok := objects[action.Target]; ok {
				requires = append(requires, dep)
			} else if !classes[action.Target] {
				return fmt.Errorf("asset %s targets missing data class %s", ref, action.Target)
			}
			for _, field := range action.Payload {
				if field.Ref == "" {
					continue
				}
				if field.Type != "string" {
					return fmt.Errorf("asset %s payload %s has reference %s with incompatible type %s", ref, field.Name, field.Ref, field.Type)
				}
				if field.Ref == "enterprise.element" {
					continue
				}
				dep, ok := objects[field.Ref]
				if !ok {
					return fmt.Errorf("asset %s payload %s requires missing object %s", ref, field.Name, field.Ref)
				}
				requires = append(requires, dep)
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Requires: uniqueRefs(requires), Action: &action}); err != nil {
				return err
			}
			actionByRef[ref] = action
		}
	}
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, page := range manifest.Pages {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetPage, Name: page.Name}
			if page.Layout != "list-detail" && page.Layout != "composed" {
				return fmt.Errorf("asset %s has unsupported layout %q", ref, page.Layout)
			}
			if page.Document != nil && page.Layout != "composed" {
				return fmt.Errorf("asset %s: a structured layout needs composed sections", ref)
			}
			if err := page.Document.Check(page.Sections); err != nil {
				return fmt.Errorf("asset %s: %w", ref, err)
			}
			if page.Object.Kind != platform.AssetObject || objects[page.Object.Name] != page.Object {
				return fmt.Errorf("asset %s requires missing object %s", ref, page.Object)
			}
			info := objectInfo[page.Object.Name]
			if len(page.Selections) != 0 && len(page.Sections) == 0 {
				return fmt.Errorf("asset %s: selections need composed sections", ref)
			}
			if len(page.Sections) != 0 {
				if err := t.checkSections(page, info); err != nil {
					return err
				}
			} else {
				for _, fields := range [][]string{page.ListFields, page.DetailFields} {
					if len(fields) == 0 {
						return fmt.Errorf("asset %s needs list and detail fields", ref)
					}
					seenFields := map[string]bool{}
					for _, field := range fields {
						if seenFields[field] {
							return fmt.Errorf("asset %s repeats field %s", ref, field)
						}
						seenFields[field] = true
						if _, ok := info.Field(field); !ok {
							return fmt.Errorf("asset %s requires missing field %s on %s", ref, field, page.Object)
						}
					}
				}
			}
			requires := []platform.AssetRef{page.Object}
			for _, actionRef := range page.Actions {
				action, ok := actionByRef[actionRef]
				if !ok {
					return fmt.Errorf("asset %s requires missing action %s", ref, actionRef)
				}
				if action.Target != page.Object.Name {
					return fmt.Errorf("asset %s action %s targets %s, not %s", ref, actionRef, action.Target, page.Object.Name)
				}
				requires = append(requires, actionRef)
			}
			if len(uniqueRefs(requires)) != len(requires) {
				return fmt.Errorf("asset %s repeats an action", ref)
			}
			asset, err := platform.PageReleaseAsset(manifest.ID, manifest.Version, page)
			if err != nil {
				return err
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Requires: asset.Requires, Page: &page}); err != nil {
				return err
			}
		}
	}
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, p := range manifest.PropertyTypes {
			if err := p.Check(); err != nil {
				return err
			}
			p := p
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetPropertyType, Name: p.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, PropertyType: &p}); err != nil {
				return err
			}
		}
		for _, l := range manifest.LinkTypes {
			parent, ok := t.entity(l.Parent.Name)
			if !ok {
				return fmt.Errorf("link parent missing")
			}
			child, ok := t.entity(l.Child.Name)
			if !ok || l.Child.App != manifest.ID {
				return fmt.Errorf("link child owner differs")
			}
			if err := l.CheckSchema(parent, child); err != nil {
				return err
			}
			if err := t.records.installLinkConstraint(l); err != nil {
				return err
			}
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetLinkType, Name: l.Name}, Source: "code", Version: manifest.Version, ContractVersion: l.Contract(), Requires: uniqueRefs([]platform.AssetRef{l.Parent, l.Child}), LinkType: &l}); err != nil {
				return err
			}
		}
		for _, query := range manifest.Queries {
			q := query
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetQuery, Name: q.Name}
			if q.Interface != "" {
				bound, err := t.bindInterfaceQuery(q)
				if err != nil {
					return fmt.Errorf("asset %s: %w", ref, err)
				}
				q = bound
				if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1, Requires: q.Dependencies(), Query: &q}); err != nil {
					return err
				}
				continue
			}
			object, ok := objects[q.Object]
			if !ok {
				return fmt.Errorf("asset %s reads missing object %s", ref, q.Object)
			}
			if q.By != "" {
				if f, ok := objectInfo[q.Object].Field(q.By); !ok || f.Type != "reference" {
					return fmt.Errorf("asset %s is run for %s, not a reference of %s", ref, q.By, q.Object)
				}
			}
			var terms []any
			if len(q.Domain) > 0 && json.Unmarshal(q.Domain, &terms) != nil {
				return fmt.Errorf("asset %s has a domain that is not a list of conditions", ref)
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Requires: []platform.AssetRef{object}, Query: &q}); err != nil {
				return err
			}
		}
		for _, function := range manifest.Functions {
			f := function
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetFunction, Name: f.Name}
			if err := f.Check(); err != nil {
				return err
			}
			object, ok := objects[f.Object]
			if !ok || object.App != manifest.ID {
				return fmt.Errorf("asset %s needs its owner's source object", ref)
			}
			for _, role := range f.Roles {
				if !slices.Contains(manifest.AllRoles(), role) {
					return fmt.Errorf("asset %s names an undeclared role", ref)
				}
			}
			for _, name := range f.Fields {
				field, ok := objectInfo[f.Object].Field(name)
				if !ok || !slices.Contains([]string{"text", "longtext", "choice", "integer", "decimal", "boolean"}, field.Type) ||
					slices.Contains(t.narrowable(t.records.types[f.Object]), name) {
					return fmt.Errorf("asset %s needs direct scalar source fields", ref)
				}
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Requires: []platform.AssetRef{object}, Function: &f}); err != nil {
				return err
			}
		}
		for _, agent := range manifest.Agents {
			cases := []map[string]any{}
			for _, test := range agent.Cases {
				cases = append(cases, map[string]any{"name": test.Name, "goal": test.Goal, "ref": test.Ref, "check": test.Check != nil})
			}
			raw, err := json.Marshal(cases)
			if err != nil {
				return err
			}
			descriptor := platform.AgentReleaseDescriptor{Name: agent.Name, Title: agent.Title, Description: agent.Description, Instructions: agent.Instructions, Tools: slices.Clone(agent.Tools), Budget: agent.Budget, Native: true, Guard: agent.Guard != nil, Handoff: agent.To != nil, Cases: raw}
			if err := descriptor.Check(); err != nil {
				return err
			}
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetAgent, Name: agent.Name}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1, Requires: descriptor.Dependencies(manifest.ID), Agent: &descriptor}); err != nil {
				return err
			}
		}
		for _, operation := range manifest.Operations {
			o := operation
			if err := o.Check(); err != nil {
				return fmt.Errorf("operation %s: %w", o.Name, err)
			}
			for _, role := range o.Roles {
				if !slices.Contains(manifest.AllRoles(), role) {
					return fmt.Errorf("operation %s names an undeclared role", o.Name)
				}
			}
			if o.Binding.Kind == "native" {
				if _, ok := app.(platform.OperationExecutor); !ok {
					return fmt.Errorf("operation %s has no native executor", o.Name)
				}
			}
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetCompute, Name: o.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, Requires: []platform.AssetRef{}, Operation: &o}); err != nil {
				return err
			}
		}
	}
	for _, app := range t.apps {
		manifest := app.Manifest()
		latest := map[string]platform.Flow{}
		for _, flow := range manifest.Flows {
			if old, ok := latest[flow.Name]; !ok || old.Version < flow.Version {
				latest[flow.Name] = flow
			}
		}
		for _, flow := range latest {
			asset, err := t.nativeFlowAsset(app, flow.Name, flow.Version)
			if err != nil {
				return err
			}
			var descriptor platform.FlowReleaseDescriptor
			if json.Unmarshal(asset.Body, &descriptor) != nil {
				return fmt.Errorf("native flow descriptor is invalid")
			}
			var definition platform.FlowDefinition
			if json.Unmarshal(descriptor.Definition, &definition) != nil {
				return fmt.Errorf("native flow definition is invalid")
			}
			if err := add(platform.Definition{Ref: asset.Ref, Source: "code", Version: asset.SourceVersion, ContractVersion: 1, Requires: asset.Requires, Flow: &definition}); err != nil {
				return err
			}
		}
	}
	for _, app := range t.apps {
		manifest := app.Manifest()
		pages, resources := []string{}, []platform.AssetRef{}
		for _, definition := range t.definitions {
			if definition.Ref.App != manifest.ID || definition.Source != "code" {
				continue
			}
			if definition.Ref.Kind == platform.AssetPage {
				pages = append(pages, definition.Ref.Name)
			} else if definition.Ref.Kind != platform.AssetApp {
				resources = append(resources, definition.Ref)
			}
		}
		if len(pages) == 0 && len(resources) == 0 {
			continue
		}
		declaration := platform.Application{Name: manifest.ID, Title: manifest.Title, Pages: pages, Resources: uniqueRefs(resources)}
		if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetApp, Name: manifest.ID}, Source: "code", Version: manifest.Version, ContractVersion: 1, Requires: declaration.Dependencies(manifest.ID), Application: &declaration}); err != nil {
			return err
		}
	}
	for _, et := range t.records.sortedTypes() {
		if err := t.checkObjectProperties(et.info); err != nil {
			return err
		}
	}
	slices.SortFunc(t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return nil
}

func uniqueRefs(refs []platform.AssetRef) []platform.AssetRef {
	out := []platform.AssetRef{}
	for _, ref := range refs {
		if !slices.Contains(out, ref) {
			out = append(out, ref)
		}
	}
	slices.SortFunc(out, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
	return out
}

// Definitions serves the installed assets the member can discover. Entity
// fields and actions are filtered through the existing read/catalog paths at
// request time, so role and field changes are never cached in the registry.
func (t *Tenant) Definitions(m platform.Member) []platform.Definition {
	definitions, err := t.definitionsFor(m)
	if err != nil {
		return []platform.Definition{}
	}
	return definitions
}

func (t *Tenant) definitionsFor(m platform.Member) ([]platform.Definition, error) {
	registered, err := t.ownedAssetDefinitions(t.definitions)
	if err != nil {
		return nil, err
	}
	return t.definitionsFrom(m, registered), nil
}

func ownedMetadataKind(kind platform.AssetKind) bool {
	return slices.Contains([]platform.AssetKind{platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetMigration, platform.AssetEvaluation}, kind)
}

// ownedAssetDefinition indexes only an owner's accepted descriptor identity.
// The original image remains the authority; no descriptor body is copied into
// discovery, and its metadata record is retained solely for read filtering.
func ownedAssetDefinition(plan platform.AssetInstallation) (platform.Definition, bool, error) {
	if !ownedMetadataKind(plan.Ref.Kind) {
		return platform.Definition{}, false, nil
	}
	var record struct {
		Published string `json:"published"`
	}
	if err := json.Unmarshal(plan.Image, &record); err != nil || record.Published == "" {
		return platform.Definition{}, false, fmt.Errorf("asset %s has no accepted definition", plan.Ref)
	}
	var asset platform.ReleaseAsset
	if err := json.Unmarshal([]byte(record.Published), &asset); err != nil || asset.Ref != plan.Ref || asset.SourceVersion == "" || asset.ContractVersion < 1 {
		return platform.Definition{}, false, fmt.Errorf("asset %s has an invalid accepted definition", plan.Ref)
	}
	definition := platform.Definition{Ref: plan.Ref, Source: "builder", Version: asset.SourceVersion, ContractVersion: asset.ContractVersion, Requires: slices.Clone(asset.Requires), OwnerRecord: plan.Record}
	if plan.Ref.Kind == platform.AssetEvaluation {
		descriptor, err := platform.ReadEvaluationSet(asset.Body)
		if err != nil {
			return definition, false, err
		}
		definition.Evaluation = &descriptor
	}
	return definition, true, nil
}

// ownedAssetDefinitions is a temporary read projection over stored owner
// publication images. Direct publication and recovery use the same index;
// reading it never installs definitions or changes the live registry.
func (t *Tenant) ownedAssetDefinitions(base []platform.Definition) ([]platform.Definition, error) {
	base = slices.Clone(base)
	base = slices.DeleteFunc(base, func(definition platform.Definition) bool {
		return definition.Ref.Kind == platform.AssetAgent && definition.Source != "code"
	})
	for _, owner := range t.apps {
		provider, ok := owner.(platform.AssetLifecycleOwner)
		if !ok {
			continue
		}
		assets, err := provider.ReleaseAssets()
		if err != nil {
			return nil, err
		}
		for _, asset := range assets {
			if asset.Ref.Kind != platform.AssetAgent {
				continue
			}
			var descriptor platform.AgentReleaseDescriptor
			if json.Unmarshal(asset.Body, &descriptor) != nil || descriptor.Check() != nil {
				return nil, fmt.Errorf("agent descriptor is unavailable")
			}
			ownerRecord := ""
			t.records.mu.Lock()
			if records := t.records.types[asset.Ref.App+".agent"]; records != nil {
				for id, row := range records.rows.all() {
					if businessField(records.info, row.value, "name") == asset.Ref.Name && !recordOf(row.value).Archived {
						ownerRecord = records.info.Type + "/" + id
						break
					}
				}
			}
			t.records.mu.Unlock()
			base = append(base, platform.Definition{Ref: asset.Ref, Source: "builder", Version: asset.SourceVersion, ContractVersion: asset.ContractVersion, Requires: slices.Clone(asset.Requires), Agent: &descriptor, OwnerRecord: ownerRecord})
		}
	}
	type image struct {
		app, schema, record string
		raw                 json.RawMessage
	}
	var images []image
	t.records.mu.Lock()
	for typ, et := range t.records.types {
		published, hasPublished := et.info.Go.FieldByName("Published")
		version, hasVersion := et.info.Go.FieldByName("DefinitionVersion")
		if !hasPublished || !hasVersion || published.Type.Kind() != reflect.String || version.Type.Kind() != reflect.Int {
			continue // other metadata retains its original installed index
		}
		if _, ok := t.app(et.info.App).(platform.AssetLifecycleOwner); !ok {
			continue
		}
		for id, row := range et.rows.all() {
			if recordOf(row.value).Archived {
				continue
			}
			ordinal := row.value.FieldByIndex(version.Index).Int()
			raw := row.value.FieldByIndex(published.Index).String()
			if ordinal == 0 && raw == "" {
				continue // an unpublished legacy/development configuration
			}
			if ordinal < 1 || raw == "" {
				t.records.mu.Unlock()
				return nil, fmt.Errorf("owned metadata %s/%s has no valid publication", typ, id)
			}
			body, err := row.image()
			if err != nil {
				t.records.mu.Unlock()
				return nil, err
			}
			images = append(images, image{et.info.App, typ + ".publish", typ + "/" + id, slices.Clone(body)})
		}
	}
	t.records.mu.Unlock()
	out := slices.DeleteFunc(slices.Clone(base), func(def platform.Definition) bool { return ownedMetadataKind(def.Ref.Kind) })
	if len(images) == 0 {
		slices.SortFunc(out, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
		return out, nil
	}
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return nil, err
	}
	seen := map[platform.AssetRef]bool{}
	for _, image := range images {
		plan, err := registry.DecodeAssetInstallation(image.app, image.schema, image.raw)
		if err != nil {
			return nil, fmt.Errorf("owned metadata %s: %w", image.record, err)
		}
		if plan.Record != image.record {
			return nil, fmt.Errorf("owned publication metadata identity differs")
		}
		def, indexed, err := ownedAssetDefinition(plan)
		if err != nil {
			return nil, err
		}
		if !indexed || seen[def.Ref] {
			return nil, fmt.Errorf("owned metadata %s has an unsupported or duplicate asset", image.record)
		}
		seen[def.Ref] = true
		out = append(out, def)
	}
	slices.SortFunc(out, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return out, nil
}

func (t *Tenant) definitionsFrom(m platform.Member, registeredDefinitions []platform.Definition) []platform.Definition {
	entities := map[string]platform.EntityInfo{}
	for _, info := range t.Entities(m) {
		entities[info.Type] = info
	}
	actions := map[string]platform.Action{}
	for _, action := range t.Catalog(m) {
		actions[action.Schema] = action
	}
	out := []platform.Definition{}
	for _, registered := range registeredDefinitions {
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
					if visible, ok := visibleInterfaceQuery(query, entities); ok {
						def.QueryVersions[version] = visible
					} else {
						delete(def.QueryVersions, version)
					}
				}
				if visible, ok := visibleInterfaceQuery(*def.Query, entities); ok {
					def.Query = &visible
				} else {
					def.Query = nil
				}
				if def.Query == nil && len(def.QueryVersions) == 0 {
					continue
				}
				def.Requires = nil
				if def.Query != nil {
					def.Requires = def.Query.Dependencies()
				}
				for _, retained := range def.QueryVersions {
					def.Requires = append(def.Requires, retained.Dependencies()...)
				}
				def.Requires = uniqueRefs(def.Requires)
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
			if def.Function == nil || !m.May(def.Ref.App, def.Function.Name, def.Function.Roles).Allow {
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
			if def.Agent == nil || !m.Active() || len(m.RolesIn(def.Ref.App)) == 0 && !m.Holds(AgentApp, AgentAdmin) {
				continue
			}
			if def.Source == "builder" && (def.OwnerRecord == "" || !t.Readable(m, def.OwnerRecord, time.Now())) {
				continue
			}
			if !slices.Contains(m.RolesIn(def.Ref.App), "builder") {
				projection := *def.Agent
				projection.Instructions, projection.Tools, projection.Checkpoints, projection.Cases = "", nil, nil, nil
				def.Agent = &projection
			}
		case platform.AssetCompute:
			if def.Operation == nil || !m.May(def.Ref.App, def.Operation.Name, def.Operation.Roles).Allow {
				continue
			}
		case platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetEvaluation:
			typ, _, _ := strings.Cut(def.OwnerRecord, "/")
			info, ok := entities[typ]
			if !ok || def.OwnerRecord == "" || !m.Active() || t.admits(m) != nil {
				continue
			}
			if _, visibleName := info.Field("name"); !visibleName || !t.Readable(m, def.OwnerRecord, time.Now()) {
				continue
			}
		case platform.AssetPage:
			visible, ok := t.visiblePage(def, m, entities, actions, registeredDefinitions)
			if !ok {
				continue
			}
			def = visible
		case platform.AssetApp:
			continue // after the pages, below: an application is offered with them
		}
		out = append(out, def)
	}
	out = filterPageNavigation(out)
	visible := map[platform.AssetRef]bool{}
	for _, def := range out {
		visible[def.Ref] = true
	}
	for i, def := range out {
		switch def.Ref.Kind {
		case platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetEvaluation:
			out[i].Requires = slices.DeleteFunc(slices.Clone(def.Requires), func(ref platform.AssetRef) bool { return !visible[ref] })
		}
	}
	// An application is offered to whoever may open one of its pages (ADR-0036).
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
	for _, registered := range registeredDefinitions {
		if def, ok := t.visibleApplication(registered, m, out, entities, opens, visibleResources); ok {
			out = append(out, def)
		}
	}
	out = filterExplorationAssets(out)
	slices.SortFunc(out, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	for i := range out {
		if out[i].Requires == nil {
			out[i].Requires = []platform.AssetRef{}
		}
	}
	return out
}

// visiblePage is a page definition as m sees it: absent unless they may open
// it, with the fields, actions, sections, buttons and services they may not
// see removed and its content version computed.
func (t *Tenant) visiblePage(def platform.Definition, m platform.Member, entities map[string]platform.EntityInfo, actions map[string]platform.Action, registeredDefinitions []platform.Definition) (platform.Definition, bool) {
	if m.Roles[def.Ref.App] == "" || def.Page == nil {
		return def, false
	}
	page := *def.Page
	digest, err := platform.PageContentVersion(page)
	if err != nil {
		return def, false
	}
	def.ContentVersion = digest
	info, ok := entities[page.Object.Name]
	if !ok {
		return def, false
	}
	visibleFields := map[string]bool{}
	page.Selections = slices.DeleteFunc(slices.Clone(page.Selections), func(selection platform.SelectionVariable) bool {
		_, visible := entities[selection.Object.Name]
		return !visible
	})
	selections := map[string]bool{}
	for _, selection := range page.Selections {
		selections[selection.Name] = true
	}
	for _, field := range info.Fields {
		visibleFields[field.Name] = true
	}
	page.ListFields = slices.DeleteFunc(slices.Clone(page.ListFields), func(name string) bool { return !visibleFields[name] })
	page.DetailFields = slices.DeleteFunc(slices.Clone(page.DetailFields), func(name string) bool { return !visibleFields[name] })
	page.Actions = slices.DeleteFunc(slices.Clone(page.Actions), func(ref platform.AssetRef) bool { _, ok := actions[ref.Name]; return !ok })
	// A composed page's widgets are trimmed the same way: a field or an
	// action this member may not have is not on the page (ADR-0035).
	if len(page.Sections) > 0 {
		sections := make([]platform.Section, 0, len(page.Sections))
		for _, section := range page.Sections {
			section = page.EffectiveModelSection(section, func(typ string) (platform.EntityInfo, bool) { info, ok := entities[typ]; return info, ok })
			if q, ok := page.InterfaceQueryForSection(section); ok {
				named := visiblePageInterfaceQuery(page, q, entities, registeredDefinitions)
				if named != nil && page.CheckInterfaceSection(section, named) == nil {
					sections = append(sections, section)
				}
				continue
			}
			if slices.ContainsFunc(section.ServiceDependencies(), func(ref platform.AssetRef) bool {
				if ref.Kind != platform.AssetObject {
					return false // write permission does not control discovery of a readable service
				}
				service, visible := entities[ref.Name]
				return !visible || service.App != ref.App
			}) {
				continue
			}
			if section.Selection != "" && !selections[section.Selection] || section.ParentSelection != "" && !selections[section.ParentSelection] {
				continue
			}
			shown := info
			if section.Object.Name != "" && section.Object.Name != page.Object.Name {
				other, ok := entities[section.Object.Name]
				if !ok {
					continue // its object is not this member's to read
				}
				shown = other
			}
			shown = platform.ModelFields(shown, section.Model, func(typ string) (platform.EntityInfo, bool) {
				value, ok := entities[typ]
				return platform.ConstraintEntity(value), ok
			})
			section.Fields = slices.DeleteFunc(slices.Clone(section.Fields), func(name string) bool {
				field, visible := shown.Field(name)
				return !visible || section.Widget == "form" && (field.ReadOnly || !field.WritesAny(m.RolesIn(shown.App)))
			})
			section.Actions = slices.DeleteFunc(slices.Clone(section.Actions), func(ref platform.AssetRef) bool { _, ok := actions[ref.Name]; return !ok })
			if (section.Widget == "chart" || section.Widget == "metric" || (section.Widget == "pivot" || section.Widget == "heatmap")) && !checkAggregateSection(section, shown) {
				continue
			}
			if section.Widget == "record-list" && section.CardLabel != "id" {
				if _, ok := shown.Field(section.CardLabel); !ok {
					continue
				}
			}
			if section.CheckLeaderboard(shown) != nil {
				continue
			}
			if section.CheckHistogram(shown) != nil {
				continue
			}
			if section.Avatar != nil {
				avatar := *section.Avatar
				avatar.DetailFields = slices.DeleteFunc(slices.Clone(avatar.DetailFields), func(name string) bool { _, ok := shown.Field(name); return name != "id" && !ok })
				section.Avatar = &avatar
			}
			if section.CheckResourceList(shown) != nil {
				continue
			}
			projected, ok := page.ProjectExploration(section, func(ref platform.AssetRef) (platform.EntityInfo, bool) {
				e, ok := entities[ref.Name]
				return e, ok && e.App == ref.App
			}, func(b platform.AssetBinding) (platform.LinkType, bool) {
				for _, d := range registeredDefinitions {
					if d.Ref == b.Ref {
						if selected := d.LinkVersion(b.SourceVersion); selected != nil && selected.LinkType != nil {
							return *selected.LinkType, true
						}
					}
				}
				return platform.LinkType{}, false
			})
			if !ok {
				continue
			}
			section = projected
			if section.CheckContextViews(shown) != nil {
				continue
			}
			if section.CheckRecordComparison(shown) != nil {
				continue
			}
			if section.CheckRecordCard(shown) != nil {
				continue
			}
			if section.CheckSparkline(shown) != nil {
				continue
			}
			if section.CheckTerms(shown) != nil {
				continue
			}
			if section.Widget == "action-table" {
				if len(section.Actions) != 1 {
					continue
				}
				a, offered := actions[section.Actions[0].Name]
				if !offered || section.CheckActionTable(shown, a) != nil {
					continue
				}
			}
			if page.CheckObservation(section, func(ref platform.AssetRef) (platform.EntityInfo, bool) {
				e, ok := entities[ref.Name]
				return e, ok && e.App == ref.App
			}) != nil {
				continue
			}
			if section.CheckCollectionAnalysis(shown) != nil {
				if section.Analysis != nil && section.Analysis.Kind == "record-axes" && page.Document != nil {
					doc := *page.Document
					doc.Variables = maps.Clone(doc.Variables)
					delete(doc.Variables, section.AnalysisXVariable)
					delete(doc.Variables, section.AnalysisYVariable)
					page.Document = &doc
				}
				continue
			}
			if section.CheckSummary(shown) != nil {
				continue
			}
			if section.CheckRecordGantt(shown) != nil {
				continue
			}
			if section.CheckRecordPicker(shown) != nil {
				continue
			}
			if section.CheckRecordCalendar(shown) != nil {
				continue
			}
			if section.CheckRecordEvents(shown) != nil {
				continue
			}
			if section.CheckCollectionBuilderFields(shown) != nil || section.CheckMap(shown) != nil || page.CheckSceneBinding(section, func(ref platform.AssetRef) (platform.EntityInfo, bool) {
				actual, ok := entities[ref.Name]
				return actual, ok && actual.App == ref.App
			}) != nil {
				continue
			}
			if section.CheckScatter(shown) != nil {
				continue
			}
			if section.CheckRecordChart(shown) != nil {
				continue
			}
			if section.CheckTimeline(shown) != nil {
				continue
			}
			if section.CheckStatusTracker(shown) != nil {
				continue
			}
			if page.CheckKanban(section, shown) != nil {
				continue
			}
			parentType := page.Object.Name
			if section.ParentSelection != "" {
				for _, selection := range page.Selections {
					if selection.Name == section.ParentSelection {
						parentType = selection.Object.Name
					}
				}
			}
			if section.Relation != "" && !slices.ContainsFunc(shown.Fields, func(f platform.FieldInfo) bool {
				return f.Type == "reference" && f.Ref == parentType && f.Inverse == section.Relation
			}) {
				continue // the member cannot follow this parent reference
			}
			if section.Widget == "form" {
				if _, offered := actions[shown.Type+".create"]; !offered {
					continue
				}
			}
			if section.Widget == "form" && slices.ContainsFunc(slices.Collect(maps.Keys(section.Inputs)), func(name string) bool {
				field, writable := shown.Field(name)
				if !writable || !field.WritesAny(m.RolesIn(shown.App)) {
					return true
				}
				binding := section.Inputs[name]
				if binding.Source != "subject" {
					return false
				}
				_, err := platform.RecordPathField(parentType, binding.Path, func(typ string) (platform.EntityInfo, bool) {
					info, visible := entities[typ]
					return info, visible
				})
				return err != nil
			}) {
				continue // no manual-input fallback for a hidden bound source
			}
			if section.Widget == "record-links" {
				section.RecordLinks = slices.DeleteFunc(slices.Clone(section.RecordLinks), func(group platform.PageRecordLink) bool {
					probe := section
					probe.RecordLinks = []platform.PageRecordLink{group}
					return probe.CheckRecordLinks(shown, func(typ string) (platform.EntityInfo, bool) { info, ok := entities[typ]; return info, ok }) != nil
				})
				if len(section.RecordLinks) == 0 {
					continue
				}
			}
			section.TableColumns = slices.DeleteFunc(slices.Clone(section.TableColumns), func(c platform.PageTableColumn) bool {
				return c.Field != "id" && !slices.Contains(section.Fields, c.Field)
			})
			if len(section.Facets) > 0 && slices.ContainsFunc(section.Facets, func(f platform.PageFacet) bool { _, ok := shown.Field(f.Field); return !ok }) {
				continue
			}
			if section.Widget == "filter" && len(section.Fields) == 0 && len(section.Facets) == 0 && section.FilterSearchVariable == "" {
				continue
			}
			if section.InlineEdit != nil {
				edit := *section.InlineEdit
				edit.Fields = slices.DeleteFunc(slices.Clone(edit.Fields), func(name string) bool {
					f, visible := shown.Field(name)
					return !visible || f.ReadOnly || !f.WritesAny(m.RolesIn(shown.App))
				})
				if _, offered := actions[edit.Action.Name]; !offered || len(edit.Fields) == 0 {
					section.InlineEdit = nil
				} else {
					section.InlineEdit = &edit
				}
			}
			if section.Widget == "inline-action" && len(section.Actions) == 1 && len(section.ActionDefaults) > 0 {
				a, ok := actions[section.Actions[0].Name]
				if !ok || section.CheckInlineAction(shown, a) != nil {
					continue
				}
			}
			if section.Widget == "inline-action" && len(section.Actions) != 1 {
				continue
			}
			if section.Function != nil {
				owner, ok := t.app(section.Function.Ref.App).(platform.FunctionDefinitions)
				if !ok {
					continue
				}
				versionText, ok := strings.CutPrefix(section.Function.SourceVersion, t.app(section.Function.Ref.App).Manifest().Version+".function-")
				version, err := strconv.Atoi(versionText)
				if !ok || err != nil {
					continue
				}
				function, _, exists := owner.FunctionDefinition(section.Function.Ref.Name, version)
				if !exists || !m.May(section.Function.Ref.App, function.Name, function.Roles).Allow || slices.ContainsFunc(function.Fields, func(name string) bool {
					field, found := shown.Field(name)
					return !found || !field.ReadsAny(m.RolesIn(section.Function.Ref.App))
				}) {
					continue
				}
			}
			if _, creates := actions[shown.Type+".create"]; section.Widget == "form" && !creates {
				continue // a form this member could not submit is not on their page
			}
			if section.Operation != nil {
				op, _, err := t.pageOperation(section.Operation)
				if err != nil || !m.May(section.Operation.Ref.App, op.Name, op.Roles).Allow || slices.ContainsFunc(slices.Collect(maps.Values(section.Inputs)), func(binding platform.Binding) bool {
					if binding.Source != "subject" || len(binding.Path) == 0 {
						return false
					}
					_, ok := shown.Field(binding.Path[0])
					return !ok
				}) {
					continue
				}
			}
			sections = append(sections, platform.CanonicalModelSection(section))
		}
		page.Sections = sections
		if page.Document != nil {
			doc := *page.Document
			doc.Queries = map[string]platform.PageQuery{}
			doc.Variables = maps.Clone(doc.Variables)
			for id, v := range doc.Variables {
				if c := v.Source; c != nil && c.Compute != nil {
					ref := page.RecordResourceObject(c.Compute.RecordVariable)
					object, ok := entities[ref.Name]
					op, _, err := t.pageOperation(&c.Compute.Operation)
					if !ok || err != nil || !m.May(c.Compute.Operation.Ref.App, op.Name, op.Roles).Allow || page.CheckComputeResource(*c.Compute, object, op, v.Type, func(typ string) (platform.EntityInfo, bool) { v, ok := entities[typ]; return v, ok }) != nil {
						delete(doc.Variables, id)
						continue
					}
				}
				if v.Mode == "aggregate" && v.Source != nil {
					q := doc.Queries[v.Source.Query]
					if q.Object.Name == "" {
						q = page.Document.Queries[v.Source.Query]
					}
					info, ok := entities[q.Object.Name]
					if !ok || doc.CheckAggregateScalar(v, info) != nil {
						delete(doc.Variables, id)
					}
				}
				if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
					info, ok := entities[v.Source.Object.Name]
					if !ok || page.CheckPropertySchema(v, info) != nil {
						delete(doc.Variables, id)
					}
				}
				if v.Mode == "shared" && (v.Type == "record" || v.Type == "filter" || v.Type == "record-set") && v.Source != nil && v.Source.Object != nil {
					if _, ok := entities[v.Source.Object.Name]; !ok {
						delete(doc.Variables, id)
					}
				}
			}
			for id, q := range page.Document.Queries {
				if q.Interface != "" {
					if visiblePageInterfaceQuery(page, q, entities, registeredDefinitions) != nil {
						doc.Queries[id] = q
					}
					continue
				}
				info, ok := entities[q.Object.Name]
				if !ok {
					continue
				}
				var named *platform.Definition
				if q.Query != nil {
					for i := range registeredDefinitions {
						if registeredDefinitions[i].Ref == q.Query.Ref {
							named = registeredDefinitions[i].QuerySourceVersion(q.Query.SourceVersion)
							break
						}
					}
				}
				if named != nil && named.LinkType != nil {
					l := named.LinkType
					parent, pok := entities[l.Parent.Name]
					child, cok := entities[l.Child.Name]
					if !pok || !cok || l.CheckSchema(parent, child) != nil {
						named = nil
					}
				}
				if page.CheckResourceListQuery(id, named) == nil && page.CheckAvatarQuery(id, named, info) == nil && page.CheckQuerySchema(q, info, named) == nil && (named == nil || named.LinkType != nil || named.Query != nil && checkNamedQuery(*named.Query, info) == nil) {
					doc.Queries[id] = q
				}
			}
			page.Document = &doc
		}
		page.Document = page.Document.Visible(sections)
		// Removing a resource producer also removes dependent visibility
		// branches. Repeat to closure without exposing orphan inputs.
		if page.Document != nil {
			for {
				visible := map[string]bool{}
				for _, node := range page.Document.Nodes {
					if node.Kind == "widget" {
						visible[node.Section] = true
					}
				}
				before := len(page.Sections)
				page.Sections = slices.DeleteFunc(page.Sections, func(section platform.Section) bool { return !visible[section.ID] })
				if len(page.Sections) == before {
					break
				}
				page.Document = page.Document.Visible(page.Sections)
			}
		}
	}
	if page.Document != nil {
		for i, s := range page.Sections {
			if s.Widget == "button-group" {
				page.Sections[i].Buttons = slices.DeleteFunc(slices.Clone(s.Buttons), func(b platform.PageButton) bool {
					return !slices.ContainsFunc(page.Document.Events, func(e platform.PageEventBinding) bool { return e.Source == s.ID && e.Control == b.ID })
				})
			}
		}
	}
	def.Requires = append([]platform.AssetRef{page.Object}, page.Actions...)
	def.Page = &page
	return def, true
}

// visibleApplication is an application definition as m sees it: absent unless
// they may open one of its pages (ADR-0036); its pages, groups, resources,
// queries and variables are narrowed to the definitions visible so far.
func (t *Tenant) visibleApplication(registered platform.Definition, m platform.Member, visible []platform.Definition, entities map[string]platform.EntityInfo, opens, visibleResources map[platform.AssetRef]bool) (platform.Definition, bool) {
	if registered.Ref.Kind != platform.AssetApp || registered.Application == nil || m.Roles[registered.Ref.App] == "" {
		return platform.Definition{}, false
	}
	def, application := registered, *registered.Application
	closed := func(name string) bool {
		return !opens[platform.AssetRef{App: def.Ref.App, Kind: platform.AssetPage, Name: name}]
	}
	application.Pages = slices.DeleteFunc(slices.Clone(application.Pages), closed)
	if len(application.Pages) == 0 && len(application.Resources) == 0 {
		return platform.Definition{}, false
	}
	// A heading over none of this member's pages is not in their navigation.
	groups := make([]platform.AppGroup, 0, len(application.Groups))
	for _, g := range application.Groups {
		if g.Pages = slices.DeleteFunc(slices.Clone(g.Pages), closed); len(g.Pages) > 0 {
			groups = append(groups, g)
		}
	}
	application.Groups = groups
	application.Header = application.VisibleHeader()
	application.Resources = slices.DeleteFunc(slices.Clone(application.Resources), func(ref platform.AssetRef) bool {
		if ref.Kind == platform.AssetFlow {
			return !m.Holds("build", "builder") || t.procs == nil || !t.procs.HasPublishedFlow(ref.Name)
		}
		return !visibleResources[ref]
	})
	if len(application.Pages) == 0 && len(application.Resources) == 0 {
		return platform.Definition{}, false
	}
	application.Queries = maps.Clone(application.Queries)
	application.Variables = maps.Clone(application.Variables)
	for id, q := range application.Queries {
		info, ok := entities[q.Object.Name]
		var named *platform.Definition
		if q.Query != nil {
			for _, d := range visible {
				if d.Ref == q.Query.Ref {
					named = d.QuerySourceVersion(q.Query.SourceVersion)
					break
				}
			}
		}
		if named != nil && named.LinkType != nil {
			l := named.LinkType
			parent, pok := entities[l.Parent.Name]
			child, cok := entities[l.Child.Name]
			if !pok || !cok || l.CheckSchema(parent, child) != nil {
				named = nil
			}
		}
		if !ok || application.QueryPage().CheckQuerySchema(q, info, named) != nil {
			delete(application.Queries, id)
		}
	}
	for changed := true; changed; {
		changed = false
		for id, q := range application.Queries {
			missing := q.MissingSetInput(application.Queries)
			for _, param := range q.Variables() {
				if _, ok := application.Variables[param]; !ok {
					missing = true
				}
			}
			if missing {
				delete(application.Queries, id)
				changed = true
			}
		}
		for id, v := range application.Variables {
			missing := false
			if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
				info, ok := entities[v.Source.Object.Name]
				_, parent := application.Variables[v.Source.Variable]
				missing = !ok || !parent || application.QueryPage().CheckPropertySchema(v, info) != nil
			}
			if v.Mode == "aggregate" && v.Source != nil {
				_, ok := application.Queries[v.Source.Query]
				missing = !ok
			}
			if v.Mode == "resource" && v.Source != nil {
				if (v.Source.Kind == "record" || v.Source.Kind == "filter" || v.Source.Kind == "record-set") && v.Source.Object != nil {
					_, ok := entities[v.Source.Object.Name]
					missing = !ok
					if ok && v.Type == "filter" {
						source := *v.Source
						info := entities[source.Object.Name]
						source.Fields = slices.DeleteFunc(slices.Clone(source.Fields), func(name string) bool {
							field, exists := info.Field(name)
							return !exists || (field.Type != "choice" && field.Type != "boolean" && field.Type != "reference")
						})
						v.Source = &source
						application.Variables[id] = v
						missing = len(source.Fields) == 0
					}
				} else {
					_, ok := application.Queries[v.Source.Query]
					missing = !ok
				}
			}
			if v.Expression != nil {
				for _, arg := range v.Expression.Args {
					if arg.Variable != "" {
						if _, ok := application.Variables[arg.Variable]; !ok {
							missing = true
						}
					}
				}
			}
			if missing {
				delete(application.Variables, id)
				changed = true
			}
		}
	}
	def.Requires = application.Dependencies(def.Ref.App)
	def.Application = &application
	return def, true
}
