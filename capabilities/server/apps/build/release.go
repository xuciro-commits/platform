package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

// ReleaseAssets returns the builder's installed, unfiltered definitions. The
// member-facing Definitions projection must never be used to calculate a
// release identity: it omits rules and fields the reader cannot see.
func (b *Build) ReleaseAssets() ([]platform.ReleaseAsset, error) {
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, err
	}
	assets, err := releaseAssets(objects, pages, apps, processes, b.Manifest().Version)
	if err != nil {
		return nil, err
	}
	functions, err := b.functionAssets()
	if err != nil {
		return nil, err
	}
	assets = append(assets, functions...)
	queries, err := b.queryAssets()
	if err != nil {
		return nil, err
	}
	assets = append(assets, queries...)
	links, err := b.linkTypeAssets()
	if err != nil {
		return nil, err
	}
	assets = append(assets, links...)
	props, err := b.propertyTypeAssets()
	if err != nil {
		return nil, err
	}
	assets = append(assets, props...)
	code, err := b.CodeReleaseAssets()
	if err != nil {
		return nil, err
	}
	return append(assets, code...), nil
}

// FlowReleaseAsset reads a retained native version, not the current draft or
// latest installed version. The host closes its bindings through all owners.
func (b *Build) FlowReleaseAsset(name string, version int) (platform.ReleaseAsset, error) {
	processes, err := b.processInventory()
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range processes {
		if record.Published == "" {
			continue
		}
		image, _ := json.Marshal(record)
		if _, err := processImage(image); err != nil {
			return platform.ReleaseAsset{}, err
		}
		for _, raw := range record.Versions {
			p, ok := wasPublished[Process](raw)
			if ok && p.Name == name && p.Version == version {
				return processReleaseAsset(p, b.Manifest().Version)
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("process %s version %d is not retained", name, version)
}

func (b *Build) releaseInventory() ([]Object, []Page, []Application, error) {
	if b.host == nil {
		return nil, nil, nil, fmt.Errorf("builder has no host")
	}
	c := b.host.Automation(platform.Caller{}, ID)
	objects, err := readDefinitionInventory[Object](c)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read builder objects: %w", err)
	}
	pages, err := readDefinitionInventory[Page](c)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read builder pages: %w", err)
	}
	apps, err := readDefinitionInventory[Application](c)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read builder applications: %w", err)
	}
	return objects, pages, apps, nil
}

// The host caps one read page at 500; the existing release bound is 1000.
// Read the complete bounded inventory or fail, never silently omit assets.
func readDefinitionInventory[T any](c platform.Caller) ([]T, error) {
	var all []T
	for {
		rows, count, err := platform.Find[T](c, platform.Query{Limit: 500, Offset: len(all), Sort: []string{"id"}})
		if err != nil {
			return nil, err
		}
		if count > 1000 {
			return nil, fmt.Errorf("builder release inventory exceeds 1000 definitions of one kind")
		}
		all = append(all, rows...)
		if len(all) >= count {
			return all, nil
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("builder definition inventory is incomplete")
		}
	}
}

// DraftReleaseAssets substitutes precisely one saved draft in the owner's
// inventory. It does not install or publish it. Both snapshots are returned
// so the host can compare identical code dependencies under one tenant lock.
// Only the authenticated builder-facing host path may call this method.
// DraftReleaseAssets substitutes precisely one saved draft: the owner's own
// comparison for an asset kind, or one joint record substitution. The multi
// path is DraftReleaseAssetsMulti.
func (b *Build) DraftReleaseAssets(kind platform.AssetKind, id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return b.singleDraftAssets(kind, id)
}

// PublishedPage reports whether the generated page of an object was
// intentionally replaced by an explicit installed page of the same name.
func (b *Build) PublishedPage(name string) (bool, error) {
	_, pages, _, err := b.releaseInventory()
	if err != nil {
		return false, err
	}
	for _, record := range pages {
		if record.Archived || record.Published == "" {
			continue
		}
		saved, ok := wasPublished[Page](record.Published)
		if !ok {
			return false, fmt.Errorf("invalid published page %s", record.ID)
		}
		if saved.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (b *Build) validateObjectInstallation(record Object) error {
	if processes := b.host.Processes(); processes != nil {
		current, exists := platform.Get[Object](b.host.Automation(platform.Caller{}, ID), record.ID)
		if previous, ok := wasPublished[Object](current.Published); exists && ok {
			oldBody, err := definitionBody(previous, TypeOf(previous.Name))
			if err != nil {
				return err
			}
			newBody, err := definitionBody(record, TypeOf(record.Name))
			if err != nil {
				return err
			}
			if string(oldBody) != string(newBody) {
				running, err := processes.HasRunningDependency(TypeOf(previous.Name))
				if err != nil {
					return err
				}
				if running && !CompatibleObjectExpansion(previous, record) {
					return fmt.Errorf("finish the running processes before changing their object definition")
				}
			}
		}
	}
	entity := Entity(record)
	processes, err := b.processInventory()
	if err != nil {
		return err
	}
	current, _ := platform.Get[Object](b.host.Automation(platform.Caller{}, ID), record.ID)
	previous, _ := wasPublished[Object](current.Published)
	for _, process := range processes {
		if process.Published == "" {
			continue
		}
		raw, _ := json.Marshal(process)
		installed, err := processImage(raw)
		if err != nil {
			return err
		}
		if installed.Object != entity.Type && installed.Object != TypeOf(previous.Name) {
			continue
		}
		if problem := b.checkFlowOn(installed, entity); problem != nil {
			return fmt.Errorf("%s", problem.Message)
		}
	}
	pages, generated, err := b.objectInstallationPages(record)
	if err != nil {
		return err
	}
	return b.host.ValidateInstallDependents(entity, platform.EntityActions(entity), generated, pages...)
}

// Adding an optional scalar cannot change the meaning of an existing process:
// every old field, state, access rule and action remains exactly as declared.
// New actions may consume the new field; host activation still needs a plan
// when the object already stores rows.
func CompatibleObjectExpansion(previous, next Object) bool {
	if previous.Name != next.Name || len(next.Fields) <= len(previous.Fields) || len(previous.States) != len(next.States) || len(previous.Access) != len(next.Access) {
		return false
	}
	for i, state := range previous.States {
		if !reflect.DeepEqual(state, next.States[i]) {
			return false
		}
	}
	for i, access := range previous.Access {
		if !reflect.DeepEqual(access, next.Access[i]) {
			return false
		}
	}
	equal := func(a, b any) bool {
		x, xe := json.Marshal(a)
		y, ye := json.Marshal(b)
		return xe == nil && ye == nil && string(x) == string(y)
	}
	for _, field := range previous.Fields {
		index := slices.IndexFunc(next.Fields, func(other Field) bool { return other.Name == field.Name })
		if index < 0 || !equal(field, next.Fields[index]) {
			return false
		}
	}
	for _, action := range previous.Actions {
		index := slices.IndexFunc(next.Actions, func(other Action) bool { return other.Name == action.Name })
		if index < 0 || !equal(action, next.Actions[index]) {
			return false
		}
	}
	for _, field := range next.Fields {
		if slices.ContainsFunc(previous.Fields, func(old Field) bool { return old.Name == field.Name }) {
			continue
		}
		if field.Required || field.Ref != "" || field.Property != nil || field.Choices != "" || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "boolean", "date", "datetime"}, field.Type) {
			return false
		}
	}
	return true
}

// An explicitly published page owns its name after it replaces an object's
// generated page. Updating the object must not silently overwrite that page.
func (b *Build) objectInstallationPages(record Object) ([]platform.Page, string, error) {
	explicit, err := b.PublishedPage(record.Name)
	if err != nil {
		return nil, "", err
	}
	if explicit {
		return nil, "", nil
	}
	return []platform.Page{page(record)}, record.Name, nil
}

// releaseAssets uses the saved publication, never a later mutable draft. The
// object body retains every declarative field (including access, conditions
// and approvals), excluding only record identity/stamps and editor state.
// An action depends on that complete object, so changing a rule changes its
// closed release even when the action's display metadata stays the same.
func releaseAssets(objects []Object, pages []Page, apps []Application, processes []Process, sourceVersion string) ([]platform.ReleaseAsset, error) {
	var assets []platform.ReleaseAsset
	index := map[platform.AssetRef]int{}
	add := func(asset platform.ReleaseAsset, replacesGeneratedPage bool) error {
		if i, exists := index[asset.Ref]; exists {
			if replacesGeneratedPage && asset.Ref.Kind == platform.AssetPage {
				assets[i] = asset
				return nil
			}
			return fmt.Errorf("builder release asset %s is declared twice", asset.Ref)
		}
		index[asset.Ref] = len(assets)
		assets = append(assets, asset)
		return nil
	}
	for _, record := range objects {
		if record.Archived || record.Published == "" {
			continue
		}
		var saved Object
		if err := json.Unmarshal([]byte(record.Published), &saved); err != nil {
			return nil, fmt.Errorf("invalid published object %s: %w", record.ID, err)
		}
		if saved.ID != record.ID {
			return nil, fmt.Errorf("published object %s belongs to another record", record.ID)
		}
		ref := platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: TypeOf(saved.Name)}
		body, err := definitionBody(saved, ref.Name)
		if err != nil {
			return nil, err
		}
		var requires []platform.AssetRef
		for _, field := range saved.Fields {
			if field.Property != nil {
				requires = append(requires, field.Property.Ref)
			}
			if field.Ref != "" && field.Ref != ref.Name {
				app, _, ok := strings.Cut(field.Ref, ".")
				if !ok {
					return nil, fmt.Errorf("object %s field %s has invalid reference %s", ref, field.Name, field.Ref)
				}
				requires = append(requires, platform.AssetRef{App: app, Kind: platform.AssetObject, Name: field.Ref})
			}
		}
		for _, action := range saved.Actions {
			for _, create := range action.Creates {
				requires = append(requires, platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: create.Object})
			}
		}
		slices.SortFunc(requires, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
		requires = slices.Compact(requires)
		if err := add(platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: sourceVersion,
			Requires: requires, Body: body}, false); err != nil {
			return nil, err
		}
		for _, action := range platform.EntityActions(Entity(saved)) {
			actionBody, err := json.Marshal(struct {
				Schema string          `json:"schema"`
				Target string          `json:"target"`
				Roles  []string        `json:"roles"`
				Action platform.Action `json:"action"`
			}{Schema: action.Schema, Target: action.Target, Roles: action.Roles, Action: action})
			if err != nil {
				return nil, fmt.Errorf("encode action %s: %w", action.Schema, err)
			}
			if err := add(platform.ReleaseAsset{
				Ref:             platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: action.Schema},
				ContractVersion: 1, SourceVersion: sourceVersion, Requires: []platform.AssetRef{ref}, Body: actionBody,
			}, false); err != nil {
				return nil, err
			}
		}
		pageAsset, err := platform.PageReleaseAsset(ID, sourceVersion, page(saved))
		if err != nil {
			return nil, err
		}
		if err := add(pageAsset, false); err != nil {
			return nil, err
		}
	}
	for _, record := range pages {
		if record.Archived || record.Published == "" {
			continue
		}
		var saved Page
		if err := json.Unmarshal([]byte(record.Published), &saved); err != nil {
			return nil, fmt.Errorf("invalid published page %s: %w", record.ID, err)
		}
		if saved.ID != record.ID {
			return nil, fmt.Errorf("published page %s belongs to another record", record.ID)
		}
		asset, err := platform.PageReleaseAsset(ID, sourceVersion, descriptor(saved))
		if err != nil {
			return nil, err
		}
		if err := add(asset, true); err != nil {
			return nil, err
		}
	}
	for _, record := range apps {
		if record.Archived || record.Published == "" {
			continue
		}
		var saved Application
		if err := json.Unmarshal([]byte(record.Published), &saved); err != nil {
			return nil, fmt.Errorf("invalid published application %s: %w", record.ID, err)
		}
		if saved.ID != record.ID {
			return nil, fmt.Errorf("published application %s belongs to another record", record.ID)
		}
		asset, err := platform.ApplicationReleaseAsset(ID, sourceVersion, applicationDescriptor(saved))
		if err != nil {
			return nil, err
		}
		if err := add(asset, false); err != nil {
			return nil, err
		}
	}
	for _, record := range processes {
		if record.Published == "" {
			continue
		}
		image, _ := json.Marshal(record)
		saved, err := processImage(image)
		if err != nil {
			return nil, err
		}
		asset, err := processReleaseAsset(saved, sourceVersion)
		if err != nil {
			return nil, err
		}
		if err := add(asset, false); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(assets, func(a, b platform.ReleaseAsset) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return assets, nil
}

func processReleaseAsset(saved Process, sourceVersion string) (platform.ReleaseAsset, error) {
	fl := platform.Flow{Name: saved.Name, Subject: saved.Object}
	ref := platform.AssetRef{App: ID, Kind: platform.AssetFlow, Name: TypeOf(fl.Name)}
	var subject platform.AssetRef
	if fl.Subject != "" {
		owner, _, _ := strings.Cut(fl.Subject, ".")
		subject = platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: fl.Subject}
	}
	var actions []platform.AssetRef
	var functions []platform.AssetBinding
	var operations []platform.AssetBinding
	var queries []platform.AssetBinding
	var dependencies []platform.AssetRef
	for _, step := range saved.Steps {
		if step.Act != "" {
			name := step.Act
			if !strings.Contains(name, ".") {
				name = saved.Object + "." + name
			}
			owner := step.App
			if owner == "" {
				owner, _, _ = strings.Cut(name, ".")
			}
			actions = append(actions, platform.AssetRef{App: owner, Kind: platform.AssetAction, Name: name})
		}
		if step.Function != nil {
			owner := step.Function.App
			if owner == "" {
				owner = ID
			}
			ref := platform.AssetRef{App: owner, Kind: platform.AssetFunction, Name: step.Function.Name}
			if owner != ID || step.Function.Version == 0 {
				dependencies = append(dependencies, ref)
				continue
			}
			binding := platform.AssetBinding{Ref: ref, SourceVersion: sourceVersion + ".function-" + strconv.Itoa(step.Function.Version)}
			if !slices.Contains(functions, binding) {
				functions = append(functions, binding)
			}
		}
		if step.Operation != nil {
			owner := step.Operation.App
			if owner == "" {
				owner = ID
			}
			ref := platform.AssetRef{App: owner, Kind: platform.AssetCompute, Name: step.Operation.Name}
			if owner == ID && step.Operation.Version > 0 {
				operations = append(operations, platform.AssetBinding{Ref: ref, SourceVersion: sourceVersion + ".compute-" + strconv.Itoa(step.Operation.Version)})
			} else {
				dependencies = append(dependencies, ref)
			}
		}
		if step.Query != "" {
			ref := platform.AssetRef{App: step.App, Kind: platform.AssetQuery, Name: step.Query}
			if step.App == ID && step.QueryVersion > 0 {
				queries = append(queries, platform.AssetBinding{Ref: ref, SourceVersion: sourceVersion + ".query-" + strconv.Itoa(step.QueryVersion)})
			} else {
				dependencies = append(dependencies, ref)
			}
		}
		if step.Flow != "" {
			name := step.Flow
			if !strings.HasPrefix(name, ID+".") {
				name = TypeOf(name)
			}
			dependencies = append(dependencies, platform.AssetRef{App: ID, Kind: platform.AssetFlow, Name: name})
		}
	}
	slices.SortFunc(operations, func(a, b platform.AssetBinding) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	operations = slices.Compact(operations)
	slices.SortFunc(queries, func(a, b platform.AssetBinding) int {
		return strings.Compare(a.Ref.String()+"@"+a.SourceVersion, b.Ref.String()+"@"+b.SourceVersion)
	})
	queries = slices.Compact(queries)
	slices.SortFunc(dependencies, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
	dependencies = slices.Compact(dependencies)
	slices.SortFunc(functions, func(a, b platform.AssetBinding) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	slices.SortFunc(actions, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
	actions = slices.Compact(actions)
	definition := json.RawMessage(published(saved))
	if len(saved.originalDefinition) > 0 {
		definition = slices.Clone(saved.originalDefinition)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(definition, &fields); err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, key := range []string{"id", "revision", "created", "changed", "archived", "state", "version", "layout"} {
		delete(fields, key)
	}
	definition, err := json.Marshal(fields)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	body, err := json.Marshal(platform.FlowReleaseDescriptor{Name: ref.Name, Subject: subject, Actions: actions, Definition: definition, Functions: functions, Operations: operations, Queries: queries, Dependencies: dependencies})
	requires := append(slices.Clone(dependencies), actions...)
	if subject.Name != "" {
		requires = append(requires, subject)
	}
	for _, binding := range functions {
		requires = append(requires, binding.Ref)
	}
	for _, binding := range operations {
		requires = append(requires, binding.Ref)
	}
	for _, binding := range queries {
		requires = append(requires, binding.Ref)
	}
	return platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: sourceVersion,
		Requires: requires, Body: body}, err
}

// ProcessFromReleaseAsset reconstructs the bounded owner definition, checking
// that its compiled bindings are exactly the ones in the release envelope.
// A sandbox assigns its own native version ordinal; that counter is not the
// semantic content identity or a dependency version.
func ProcessFromReleaseAsset(asset platform.ReleaseAsset) (Process, error) {
	var envelope platform.FlowReleaseDescriptor
	var p Process
	if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetFlow || asset.ContractVersion != 1 {
		return p, fmt.Errorf("unsupported process release asset %s", asset.Ref)
	}
	if err := json.Unmarshal(asset.Body, &envelope); err != nil {
		return p, err
	}
	if err := json.Unmarshal(envelope.Definition, &p); err != nil {
		return p, err
	}
	expected, err := processReleaseAsset(p, asset.SourceVersion)
	if err != nil {
		return p, err
	}
	var got, want any
	if json.Unmarshal(asset.Body, &got) != nil || json.Unmarshal(expected.Body, &want) != nil || expected.Ref != asset.Ref || !reflect.DeepEqual(got, want) {
		return p, fmt.Errorf("process %s definition and compiled bindings differ", asset.Ref)
	}
	p.ID, p.Version, p.State = asset.Ref.Name, 1, "published"
	p.Published = published(p)
	p.Versions = []string{p.Published}
	return p, nil
}

func definitionBody(value Object, typ string) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for _, key := range []string{"id", "revision", "created", "changed", "archived", "state", "installed", "published"} {
		delete(body, key)
	}
	body["type"], err = json.Marshal(typ)
	if err != nil {
		return nil, err
	}
	return json.Marshal(body)
}
