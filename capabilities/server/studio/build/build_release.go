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

func recordKind(kind platform.AssetKind) bool {
	switch kind {
	case platform.AssetObject, platform.AssetPage, platform.AssetApp, platform.AssetFlow:
		return true
	}
	return false
}

func (b *Build) lookupEntity(typ string) (platform.EntityInfo, bool) {
	if info, ok := b.jointEntities[typ]; ok {
		return info, true
	}
	if b.host == nil {
		return platform.EntityInfo{}, false
	}
	return b.host.Entity(typ)
}

func (b *Build) declaresRole(role string) bool {
	for _, model := range b.jointEntities {
		if _, known := model.Scope.Levels[role]; known {
			return true
		}
	}
	catalog := b.ledger.Catalog
	if b.installationCatalog != nil {
		catalog = b.installationCatalog
	}
	return slices.Contains((platform.Manifest{Actions: catalog, Roles: b.manifestRoles()}).AllRoles(), role)
}

func (b *Build) jointObjectEntities(objects []Object, drafts []JointDraftRef) map[string]platform.EntityInfo {
	joint := map[string]platform.EntityInfo{}
	for _, draft := range drafts {
		if draft.Kind != platform.AssetObject {
			continue
		}
		i := slices.IndexFunc(objects, func(o Object) bool { return o.ID == draft.ID && !o.Archived })
		if i < 0 {
			continue
		}
		record := objects[i]
		info, err := platform.Describe(ID, Entity(record), func(reflect.Type) string { return "" })
		if err != nil {
			continue
		}
		joint[info.Type] = info
	}
	return joint
}

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
	for _, read := range []func() ([]platform.ReleaseAsset, error){
		b.kindAssets(&functionKind), b.agentAssets, b.kindAssets(&queryKind), b.kindAssets(&linkKind), b.kindAssets(&propertyKind), b.kindAssets(&valueKind),
		b.CodeReleaseAssets, b.alertAssets, b.integrationAssets, b.testPlanAssets, b.sampleSetAssets,
	} {
		owned, err := read()
		if err != nil {
			return nil, err
		}
		assets = append(assets, owned...)
	}
	assets, err = b.bindConfirmationAssets(assets)
	if err != nil {
		return nil, err
	}
	return b.bindDispatchAssets(assets)
}

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
		if _, err := ReadProcessImage(image); err != nil {
			return platform.ReleaseAsset{}, err
		}
		for _, raw := range record.Versions {
			p, ok := ReadPublication[Process](raw)
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

func readDefinitionInventory[T any](c platform.Caller) ([]T, error) {
	rows, err := readDefinitionInventoryOf(c, reflect.TypeFor[T]())
	if err != nil {
		return nil, err
	}
	var all []T
	for _, row := range rows {
		all = append(all, row.(T))
	}
	return all, nil
}

func readDefinitionInventoryOf(c platform.Caller, typ reflect.Type) ([]any, error) {
	var all []any
	for {
		rows, count, err := c.FindOf(typ, platform.Query{Limit: 500, Offset: len(all), Sort: []string{"id"}})
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

func (b *Build) PublishedPage(name string) (bool, error) {
	_, pages, _, err := b.releaseInventory()
	if err != nil {
		return false, err
	}
	for _, record := range pages {
		if record.Archived || record.Published == "" {
			continue
		}
		saved, ok := ReadPublication[Page](record.Published)
		if !ok {
			return false, fmt.Errorf("invalid published page %s", record.ID)
		}
		if saved.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (b *Build) validateObjectInstallation(c platform.Caller, record Object) error {
	if processes := b.host.BuildProcesses(); processes != nil {
		current, exists := platform.Get[Object](b.host.Automation(platform.Caller{}, ID), record.ID)
		if previous, ok := ReadPublication[Object](current.Published); exists && ok {
			oldBody, err := definitionBody(previous, TypeOf(previous.Name))
			if err != nil {
				return err
			}
			newBody, err := definitionBody(record, TypeOf(record.Name))
			if err != nil {
				return err
			}
			if string(oldBody) != string(newBody) {
				running, err := processes.HasUnboundRunningDependency(TypeOf(previous.Name))
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
	previous, _ := ReadPublication[Object](current.Published)
	if !b.jointMode {
		for _, process := range processes {
			if process.Published == "" {
				continue
			}
			raw, _ := json.Marshal(process)
			installed, err := ReadProcessImage(raw)
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
	}
	pages, generated, err := b.objectInstallationPages(record)
	if err != nil {
		return err
	}
	if b.jointMode {
		return nil
	}
	return b.host.ValidateInstallDependents(c, entity, objectActions(record, entity), generated, pages...)
}

func (b *Build) objectInstallationPages(record Object) ([]platform.Page, string, error) {
	explicit, err := b.PublishedPage(record.Name)
	if err != nil {
		return nil, "", err
	}
	if explicit {
		return nil, "", nil
	}
	return []platform.Page{CompileObjectPage(record)}, record.Name, nil
}

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
		asset, err := platform.WithDependencies(platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: sourceVersion, Body: body})
		if err != nil {
			return nil, err
		}
		if err := add(asset, false); err != nil {
			return nil, err
		}
		for _, action := range objectActions(saved, Entity(saved)) {
			actionBody, err := json.Marshal(struct {
				Schema string          `json:"schema"`
				Target string          `json:"target"`
				Roles  []string        `json:"roles"`
				Action platform.Action `json:"action"`
			}{Schema: action.Schema, Target: action.Target, Roles: action.Roles, Action: action})
			if err != nil {
				return nil, fmt.Errorf("encode action %s: %w", action.Schema, err)
			}
			asset, err := platform.WithDependencies(platform.ReleaseAsset{
				Ref:             platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: action.Schema},
				ContractVersion: 1, SourceVersion: sourceVersion, Body: actionBody,
			})
			if err != nil {
				return nil, err
			}
			if err := add(asset, false); err != nil {
				return nil, err
			}
		}
		pageAsset, err := platform.PageReleaseAsset(ID, sourceVersion, CompileObjectPage(saved))
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
		saved, err := ReadProcessImage(image)
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
			if step.Protocol != "" {
				protocol, _, _ := strings.Cut(step.Protocol, "/")
				dependencies = append(dependencies, platform.AssetRef{App: owner, Kind: platform.AssetProtocol, Name: protocol})
			}
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
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: sourceVersion, Body: body})
}

func (b *Build) ProcessFromAsset(asset platform.ReleaseAsset) (Process, error) {
	var envelope platform.FlowReleaseDescriptor
	var p Process
	if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetFlow || asset.ContractVersion != 1 || platform.CheckAssetDependencies(asset) != nil {
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
	got, err := upgradedProcessBody(asset.Body)
	var want any
	if err != nil || json.Unmarshal(expected.Body, &want) != nil || expected.Ref != asset.Ref || !reflect.DeepEqual(got, want) {
		return p, fmt.Errorf("process %s definition and compiled bindings differ", asset.Ref)
	}
	p.ID, p.Version, p.State = asset.Ref.Name, 1, "published"
	p.Published = published(p)
	p.Versions = []string{p.Published}
	return p, nil
}

func upgradedProcessBody(raw json.RawMessage) (any, error) {
	var body any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	var walk func(any) error
	walk = func(value any) error {
		switch node := value.(type) {
		case map[string]any:
			for key, child := range node {
				if condition, ok := child.(map[string]any); ok && key == "condition" && (condition["left"] != nil || condition["terms"] != nil) {
					legacy, _ := json.Marshal(condition)
					upgraded, err := platform.UpgradeCondition(legacy)
					if err != nil {
						return err
					}
					current, _ := json.Marshal(upgraded)
					var decoded any
					_ = json.Unmarshal(current, &decoded)
					node[key] = decoded
					continue
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range node {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return body, walk(body)
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
