package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"platformserver/platform"
	automationauthoring "platformserver/services/automate/authoring"
	dataowner "platformserver/services/data"
)

func (b *Build) prepareReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	objects, pages, apps, err := b.releaseInventory()
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
			if err := mapDispatchIntegrations(&frozen, b.data.IntegrationID); err != nil {
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
			legacyBindings := len(bindings) == 0
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
					return nil, fmt.Errorf("action type %s is unavailable", binding.Ref.Name)
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
				if !legacyBindings {
					if version > len(action.Versions) {
						return nil, fmt.Errorf("action type %s has no retained version %d", binding.Ref.Name, version)
					}
					retained := action.Versions[version-1]
					selected := ActionType{Record: platform.Record{ID: action.ID}, Name: action.Name, Object: action.Object, Title: retained.Definition.Title, Definition: retained.Definition, Commands: retained.Commands, State: "published", Version: version, Versions: slices.Clone(action.Versions[:version])}
					selected.Published = selected.Snapshot(version)
					if err := add(SchemaActionType, selected); err != nil {
						return nil, err
					}
					continue
				}
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
			current := Page{Record: platform.Record{ID: asset.Ref.Name}, Name: asset.Ref.Name}
			if i >= 0 {
				current = pages[i]
			}
			prepared, generated, err := b.apps.PreparePagePublication(asset, assets, current)
			if err != nil {
				return nil, err
			}
			if generated {
				continue
			}
			if err := add(SchemaRelease, prepared); err != nil {
				return nil, err
			}
		case platform.AssetApp:
			i := slices.IndexFunc(apps, func(a Application) bool { return !a.Archived && a.Name == asset.Ref.Name })
			if i < 0 {
				return nil, fmt.Errorf("saved application %s has no matching draft", asset.Ref)
			}
			prepared, err := b.apps.PrepareApplicationPublication(asset, apps[i])
			if err != nil {
				return nil, err
			}
			if err := add(SchemaHandOver, prepared); err != nil {
				return nil, err
			}
		}
	}
	automationPublications, err := b.automation.PrepareReleasePublications(assets)
	if err != nil {
		return nil, err
	}
	publications = append(publications, automationPublications...)

	for _, k := range definitionKinds {
		images, err := k.prepare(b, assets)
		if err != nil {
			return nil, err
		}
		publications = append(publications, images...)
	}
	images, err := b.functions.Prepare(assets)
	if err != nil {
		return nil, err
	}
	publications = append(publications, images...)
	for _, prepare := range []func([]platform.ReleaseAsset) ([]platform.ReleasePublication, error){b.prepareAgentReleasePublications, b.prepareTestPlanPublications, b.PrepareCodeReleasePublications, b.prepareTablePublications} {
		images, err := prepare(assets)
		if err != nil {
			return nil, err
		}
		publications = append(publications, images...)
	}
	return publications, nil
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
			if err := mapDispatchIntegrations(&o, b.data.IntegrationID); err != nil {
				return nil, err
			}
			if len(o.ActionTypes) == 0 {
				for _, definition := range o.Actions {
					ref := platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: TypeOf(o.Name) + "." + definition.Name}
					binding := platform.AssetBinding{Ref: ref}
					if index := slices.IndexFunc(candidate.Assets, func(item platform.ReleaseAsset) bool { return item.Ref == ref }); index >= 0 {
						binding.SourceVersion = candidate.Assets[index].SourceVersion
					}
					o.ActionTypes = append(o.ActionTypes, binding)
				}
			}
			o.Actions = nil
			if err := add(ObjectType, o.Name, o); err != nil {
				return nil, err
			}
		case platform.AssetPage:
			if !authored[asset.Ref.Name] {
				generated, err := b.apps.IsGeneratedReleasePage(asset, candidate.Assets)
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
			p, err := b.apps.PageFromDescriptor(descriptor)
			if err != nil {
				return nil, fmt.Errorf("%w in page %s", err, asset.Ref)
			}
			if err := add(PageType, p.Name, p); err != nil {
				return nil, err
			}
		case platform.AssetApp:
			a, err := b.apps.ApplicationFromAsset(asset)
			if err != nil {
				return nil, err
			}
			if err := add(AppType, a.Name, a); err != nil {
				return nil, err
			}
		case platform.AssetFlow:
			p, err := b.ProcessFromAsset(asset)
			if err != nil {
				return nil, err
			}
			if err := add(automationauthoring.ProcessType, p.Name, p); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func (*Build) AssetKinds() []platform.AssetKind {
	var kinds []platform.AssetKind
	for _, descriptor := range resourceDescriptors {
		if !descriptor.released {
			continue
		}
		if !slices.Contains(kinds, descriptor.kind) {
			kinds = append(kinds, descriptor.kind)
		}
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
	if dataowner.IsIntegration(ref.Kind) {
		return b.data.ResolvePinned(ref, version)
	}
	if len(contentVersion) == 1 {
		if version != b.Manifest().Version {
			return platform.ReleaseAsset{}, fmt.Errorf("unsupported page source version")
		}
		if page, ok := b.apps.PageContent(ref.Name, contentVersion[0]); ok {
			return b.apps.PageAssetFromView(page, version)
		}
		return platform.ReleaseAsset{}, fmt.Errorf("page content %s@%s is not retained", ref, contentVersion[0])
	}
	if ref.Kind == platform.AssetSampleSet {
		return b.sampleSetPinned(ref, version)
	}
	if k := definitionKindOf(ref.Kind); k != nil {
		return k.resolve(b, ref.Name, version)
	}
	if ref.Kind == platform.AssetFunction {
		return b.functions.Resolve(ref.Name, version)
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
