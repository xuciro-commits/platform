package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"platformserver/platform"
)

func (b *Build) prepareReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, err
	}
	var publications []platform.ReleasePublication
	add := func(schema string, record any) error {
		raw, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("%s: %w", schema, err)
		}
		publications = append(publications, platform.ReleasePublication{Schema: schema, Image: raw})
		return nil
	}
	for _, asset := range assets {
		if asset.Ref.App != ID {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetObject:
			code, err := b.retainedCodeObject(asset)
			if err != nil {
				return nil, err
			}
			if code {
				continue
			}
			var frozen Object
			if err := json.Unmarshal(asset.Body, &frozen); err != nil || TypeOf(frozen.Name) != asset.Ref.Name {
				return nil, fmt.Errorf("invalid saved object %s", asset.Ref)
			}
			if err := mapDispatchIntegrations(&frozen, b.integrationID); err != nil {
				return nil, err
			}
			i := slices.IndexFunc(objects, func(o Object) bool { return !o.Archived && o.Name == frozen.Name })
			if i < 0 {
				return nil, fmt.Errorf("saved object %s has no matching draft; renamed or archived definitions need an upgrade plan", asset.Ref)
			}
			frozen.Record = objects[i].Record
			objects[i].State, objects[i].Installed = "published", TypeOf(frozen.Name)
			objects[i].Published = published(frozen)
			objects[i].ExtensionSource = frozen.ExtensionSource
			objects[i].SchemaVersions = frozen.SchemaVersions
			if err := add(SchemaPublish, objects[i]); err != nil {
				return nil, err
			}
			all, err := readDefinitionInventory[ActionType](b.host.Automation(platform.Caller{}, ID))
			if err != nil {
				return nil, err
			}
			bindings := slices.Clone(frozen.ActionTypes)
			if len(bindings) == 0 {
				for _, definition := range frozen.Actions {
					name := TypeOf(frozen.Name) + "." + definition.Name
					index := slices.IndexFunc(all, func(action ActionType) bool { return action.Name == name })
					if index >= 0 {
						bindings = append(bindings, platform.AssetBinding{Ref: platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: name}, SourceVersion: strconv.Itoa(all[index].Version)})
					}
				}
			}
			for _, binding := range bindings {
				if !slices.ContainsFunc(assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == binding.Ref }) {
					continue
				}
				index := slices.IndexFunc(all, func(action ActionType) bool { return action.Name == binding.Ref.Name })
				if index < 0 {
					continue
				}
				version, err := strconv.Atoi(binding.SourceVersion)
				if err != nil || version < 1 {
					return nil, fmt.Errorf("action type %s has no retained version %s", binding.Ref.Name, binding.SourceVersion)
				}
				definition := slices.IndexFunc(frozen.Actions, func(definition Action) bool { return TypeOf(frozen.Name)+"."+definition.Name == binding.Ref.Name })
				if definition < 0 {
					return nil, fmt.Errorf("action type %s has no frozen definition", binding.Ref.Name)
				}
				action := all[index]
				selected := ActionType{Record: platform.Record{ID: action.ID}, Name: action.Name, Object: action.Object, Title: frozen.Actions[definition].Title, Definition: frozen.Actions[definition], State: "published", Version: version}
				if version <= len(action.Versions) && string(platform.Raw(action.Versions[version-1].Definition)) == string(platform.Raw(selected.Definition)) {
					selected.Commands = action.Versions[version-1].Commands
				}
				action.State, action.Published = "published", string(platform.Raw(selected))
				if err := add(SchemaActionType, action); err != nil {
					return nil, err
				}
			}
		case platform.AssetPage:
			i := slices.IndexFunc(pages, func(p Page) bool { return !p.Archived && p.Name == asset.Ref.Name })
			if i < 0 {
				generated, err := generatedReleasePage(asset, assets, b.Manifest().Version)
				if err != nil {
					return nil, err
				}
				if generated {
					continue
				}
			}
			var pageDescriptor platform.Page
			if err := json.Unmarshal(asset.Body, &pageDescriptor); err != nil || pageDescriptor.Name != asset.Ref.Name {
				return nil, fmt.Errorf("invalid saved page %s", asset.Ref)
			}
			current := Page{Record: platform.Record{ID: asset.Ref.Name}, Name: asset.Ref.Name}
			if i >= 0 {
				current = pages[i]
			}
			if previous, ok := ReadPublication[Page](current.Published); ok && !maps.Equal(pageFunctionBindings(pageDescriptor), pageFunctionBindings(descriptor(previous))) {
				return nil, fmt.Errorf("page %s changes AI function bindings; publish the binding through its owner before release activation", asset.Ref)
			}
			frozen, err := pageDraft(pageDescriptor)
			if err != nil {
				return nil, fmt.Errorf("%w in page %s", err, asset.Ref)
			}
			frozen.Record = current.Record
			frozen.Versions = slices.Clone(current.Versions)
			frozen.Published = current.Published
			if err := retainPagePublication(&frozen); err != nil {
				return nil, err
			}
			current.State, current.Published, current.Versions = "published", frozen.Published, frozen.Versions
			if err := add(SchemaRelease, current); err != nil {
				return nil, err
			}
		case platform.AssetApp:
			i := slices.IndexFunc(apps, func(a Application) bool { return !a.Archived && a.Name == asset.Ref.Name })
			if i < 0 {
				return nil, fmt.Errorf("saved application %s has no matching draft", asset.Ref)
			}
			var frozen Application
			if err := json.Unmarshal(asset.Body, &frozen); err != nil || frozen.Name != asset.Ref.Name {
				return nil, fmt.Errorf("invalid saved application %s", asset.Ref)
			}
			frozen.Record = apps[i].Record
			apps[i].State, apps[i].Published = "published", published(frozen)
			if err := add(SchemaHandOver, apps[i]); err != nil {
				return nil, err
			}
		case platform.AssetFlow:
			i := slices.IndexFunc(processes, func(p Process) bool { return !p.Archived && TypeOf(p.Name) == asset.Ref.Name })
			if i < 0 {
				return nil, fmt.Errorf("saved flow %s has no matching owner", asset.Ref)
			}
			frozen, err := b.ProcessFromAsset(asset)
			if err != nil {
				return nil, err
			}
			if prior, ok := ReadPublication[Process](processes[i].Published); ok {
				priorAsset, err := processReleaseAsset(prior, asset.SourceVersion)
				if err == nil {
					a, _ := upgradedProcessBody(priorAsset.Body)
					z, _ := upgradedProcessBody(asset.Body)
					if reflect.DeepEqual(a, z) {
						continue
					}
				}
			}
			if processes[i].Version >= 64 {
				return nil, fmt.Errorf("flow version family is full")
			}
			frozen.Record, frozen.Version, frozen.State = processes[i].Record, processes[i].Version+1, "published"
			frozen.Layout = processes[i].Layout
			processes[i].Version, processes[i].State = frozen.Version, "published"
			processes[i].Published = published(frozen)
			processes[i].Versions = append(slices.Clone(processes[i].Versions), processes[i].Published)
			if err := add(SchemaProcess, processes[i]); err != nil {
				return nil, err
			}
		}
	}

	for _, k := range definitionKinds {
		images, err := k.prepare(b, assets)
		if err != nil {
			return nil, err
		}
		publications = append(publications, images...)
	}
	for _, prepare := range []func([]platform.ReleaseAsset) ([]platform.ReleasePublication, error){b.prepareAgentReleasePublications, b.prepareTestPlanPublications, b.PrepareCodeReleasePublications, b.prepareTablePublications, b.prepareAlertPublications} {
		images, err := prepare(assets)
		if err != nil {
			return nil, err
		}
		publications = append(publications, images...)
	}
	return publications, nil
}

func generatedReleasePage(page platform.ReleaseAsset, assets []platform.ReleaseAsset, version string) (bool, error) {
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return false, err
		}
		if TypeOf(object.Name) != page.Ref.Name {
			continue
		}
		generated, err := platform.PageReleaseAsset(ID, version, CompileObjectPage(object))
		if err != nil {
			return false, err
		}
		var expected, actual any
		if err := json.Unmarshal(generated.Body, &expected); err != nil {
			return false, err
		}
		if err := json.Unmarshal(page.Body, &actual); err != nil {
			return false, err
		}
		return reflect.DeepEqual(expected, actual), nil
	}
	return false, nil
}

func pageFunctionBindings(p platform.Page) map[string]string {
	bindings := map[string]string{}
	safe, err := p.Safety()
	if err != nil {
		return bindings
	}
	for _, trigger := range safe.ActionTriggers {
		if trigger.Function != nil {
			bindings[trigger.Function.Ref.String()] = trigger.Function.SourceVersion
		}
		if trigger.Operation != nil {
			bindings[trigger.Operation.Ref.String()] = trigger.Operation.SourceVersion
		}
	}
	return bindings
}

func pageDraft(pageDescriptor platform.Page) (Page, error) {
	raw, err := json.Marshal(pageDescriptor)
	if err != nil {
		return Page{}, err
	}
	var original map[string]json.RawMessage
	if err = json.Unmarshal(raw, &original); err != nil {
		return Page{}, err
	}
	original["object"] = platform.Raw(pageDescriptor.Object.Name)
	if pageDescriptor.Format != 3 && pageDescriptor.Format != 4 {
		original["list"], original["detail"] = original["listFields"], original["detailFields"]
		delete(original, "listFields")
		delete(original, "detailFields")
		if pageDescriptor.Actions != nil {
			actions := make([]string, 0, len(pageDescriptor.Actions))
			for _, action := range pageDescriptor.Actions {
				actions = append(actions, action.Name)
			}
			original["actions"] = platform.Raw(actions)
		}
		sections := make([]map[string]json.RawMessage, 0, len(pageDescriptor.Sections))
		for _, section := range pageDescriptor.Sections {
			var value map[string]json.RawMessage
			if err = json.Unmarshal(platform.Raw(section), &value); err != nil {
				return Page{}, err
			}
			if section.Object.Name != "" {
				value["object"] = platform.Raw(section.Object.Name)
			} else {
				delete(value, "object")
			}
			if section.Query.Name != "" {
				value["query"] = platform.Raw(section.Query.App + "." + section.Query.Name)
			} else {
				delete(value, "query")
			}
			if section.Actions != nil {
				actions := make([]string, 0, len(section.Actions))
				for _, action := range section.Actions {
					actions = append(actions, action.Name)
				}
				value["actions"] = platform.Raw(actions)
			}
			if section.InlineEdit != nil {
				value["inlineEdit"] = platform.Raw(InlineEdit{Action: section.InlineEdit.Action.Name, Fields: section.InlineEdit.Fields})
			}
			if section.Function != nil {
				var version int
				if _, err = fmt.Sscanf(section.Function.SourceVersion, ValueDefinitionVersion+".function-%d", &version); err != nil || version < 1 {
					return Page{}, fmt.Errorf("invalid saved function binding")
				}
				value["function"] = platform.Raw(platform.FunctionRef{Name: section.Function.Ref.Name, Version: version})
			}
			sections = append(sections, value)
		}
		if pageDescriptor.Sections != nil {
			original["sections"] = platform.Raw(sections)
		}
	}
	var frozen Page
	if err = json.Unmarshal(platform.Raw(original), &frozen); err != nil {
		return Page{}, err
	}
	return frozen, nil
}

type Draft struct {
	Type  string
	Name  string
	Image json.RawMessage
}

func (b *Build) releaseDraftsOf(candidate platform.ReleaseCandidate) ([]Draft, error) {
	authored := map[string]bool{}
	if b.host != nil {
		pages, err := readDefinitionInventory[Page](b.host.Automation(platform.Caller{}, ID))
		if err != nil {
			return nil, err
		}
		for _, page := range pages {
			if !page.Archived {
				authored[page.Name] = true
			}
		}
	}
	var out []Draft
	add := func(typ, name string, record any) error {
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		out = append(out, Draft{Type: typ, Name: name, Image: raw})
		return nil
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != ID {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetObject:
			var o Object
			if err := json.Unmarshal(asset.Body, &o); err != nil || o.Name == "" {
				continue
			}
			if err := mapDispatchIntegrations(&o, b.integrationID); err != nil {
				return nil, err
			}
			if err := add(ObjectType, o.Name, o); err != nil {
				return nil, err
			}
		case platform.AssetPage:
			if !authored[asset.Ref.Name] {
				generated, err := generatedReleasePage(asset, candidate.Assets, b.Manifest().Version)
				if err != nil {
					return nil, err
				}
				if generated {
					continue
				}
			}
			var descriptor platform.Page
			if err := json.Unmarshal(asset.Body, &descriptor); err != nil {
				return nil, fmt.Errorf("invalid saved page %s", asset.Ref)
			}
			p, err := pageDraft(descriptor)
			if err != nil {
				return nil, fmt.Errorf("%w in page %s", err, asset.Ref)
			}
			if err := add(PageType, p.Name, p); err != nil {
				return nil, err
			}
		case platform.AssetApp:
			var a Application
			if err := json.Unmarshal(asset.Body, &a); err != nil {
				return nil, fmt.Errorf("invalid saved application %s", asset.Ref)
			}
			if err := add(AppType, a.Name, a); err != nil {
				return nil, err
			}
		case platform.AssetFlow:
			p, err := b.ProcessFromAsset(asset)
			if err != nil {
				return nil, err
			}
			if err := add(ProcessType, p.Name, p); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func (*Build) AssetKinds() []platform.AssetKind {
	kinds := []platform.AssetKind{
		platform.AssetObject, platform.AssetAction, platform.AssetPage, platform.AssetApp, platform.AssetFlow,
		platform.AssetCompute, platform.AssetAgent, platform.AssetEvaluation, platform.AssetTestPlan, platform.AssetSampleSet, platform.AssetAlert,
	}
	for _, entry := range integrationEntries {
		kinds = append(kinds, entry.kind)
	}
	for _, definition := range definitionKinds {
		kinds = append(kinds, definition.assetKind())
	}
	return kinds
}

func (b *Build) ResolvePinned(ref platform.AssetRef, version string, contentVersion ...string) (platform.ReleaseAsset, error) {
	if ref.App != ID {
		return platform.ReleaseAsset{}, fmt.Errorf("asset has another owner")
	}
	if len(contentVersion) > 1 || len(contentVersion) == 1 && ref.Kind != platform.AssetPage {
		return platform.ReleaseAsset{}, fmt.Errorf("content pins only identify one retained page")
	}
	if ValueIntegrationKind(ref.Kind) {
		rows, err := b.integrationInventory(ref.Kind)
		if err != nil {
			return platform.ReleaseAsset{}, err
		}
		for _, r := range rows {
			name, ordinal, accepted, history := integrationMetadata(r)
			if name != ref.Name {
				continue
			}
			for _, raw := range append(slices.Clone(history), accepted) {
				if raw == "" {
					continue
				}
				var a platform.ReleaseAsset
				if err := ValueDecodeIntegration([]byte(raw), &a); err != nil {
					return a, err
				}
				if a.Ref == ref && a.SourceVersion == version {
					return a, nil
				}
			}
			if ordinal == 0 && accepted == "" && version == integrationVersion(1, ref.Kind) {
				return b.integrationAsset(ref.Kind, r, 1)
			}
		}
		return platform.ReleaseAsset{}, fmt.Errorf("integration version %s@%s is not retained", ref, version)
	}
	if len(contentVersion) == 1 {
		if version != b.Manifest().Version {
			return platform.ReleaseAsset{}, fmt.Errorf("unsupported page source version")
		}
		if page, ok := b.PageContent(ref.Name, contentVersion[0]); ok {
			return platform.PageReleaseAsset(ID, version, page)
		}
		return platform.ReleaseAsset{}, fmt.Errorf("page content %s@%s is not retained", ref, contentVersion[0])
	}
	if ref.Kind == platform.AssetSampleSet {
		return b.sampleSetPinned(ref, version)
	}
	if ref.Kind == platform.AssetAlert {
		return b.alertPinned(ref, version)
	}
	if k := definitionKindOf(ref.Kind); k != nil {
		return k.resolve(b, ref.Name, version)
	}
	if ref.Kind == platform.AssetEvaluation || ref.Kind == platform.AssetTestPlan {
		return b.testPlanPinned(ref, version)
	}
	if ref.Kind == platform.AssetAgent {
		for _, record := range b.agents {
			asset, err := agentReleaseAsset(record, b.Manifest().Version)
			if err != nil {
				return platform.ReleaseAsset{}, err
			}
			if asset.Ref == ref && asset.SourceVersion == version {
				return asset, nil
			}
		}
		return b.retainedAgentAsset(ref, version)
	}
	if ref.Kind == platform.AssetCompute {
		return b.OperationReleaseAsset(ref.Name, version)
	}
	assets, err := b.ReleaseAssets()
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, asset := range assets {
		if asset.Ref == ref && asset.SourceVersion == version {
			return asset, nil
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("asset version %s@%s is unavailable", ref, version)
}
