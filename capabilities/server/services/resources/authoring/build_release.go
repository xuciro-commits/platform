package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
	automationauthoring "platformserver/services/automate/authoring"
	ontology "platformserver/services/ontology"
)

func recordKind(kind platform.AssetKind) bool {
	descriptor, ok := resourceDescriptorOfKind(kind)
	return ok && descriptor.family == recordResources
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
	processes, err := b.automation.Inventory()
	if err != nil {
		return nil, err
	}
	assets, err := b.releaseAssets(objects, pages, apps, processes, b.Manifest().Version)
	if err != nil {
		return nil, err
	}
	for _, read := range []func() ([]platform.ReleaseAsset, error){
		b.functions.Assets, b.agentAssets, b.kindAssets(&queryKind), b.kindAssets(&linkKind), b.kindAssets(&interfaceKind), b.kindAssets(&propertyKind), b.kindAssets(&valueKind), b.kindAssets(&objectTypeGroupKind),
		b.CodeReleaseAssets, b.automation.AlertAssets, b.data.Assets, b.testPlanAssets, b.sampleSetAssets,
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

func (b *Build) releaseInventory() ([]Object, []Page, []Application, error) {
	if b.host == nil {
		return nil, nil, nil, fmt.Errorf("builder has no host")
	}
	c := b.host.Automation(platform.Caller{}, ID)
	objects, err := readDefinitionInventory[Object](c)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read builder objects: %w", err)
	}
	pages, err := b.apps.Pages()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read builder pages: %w", err)
	}
	apps, err := b.apps.Applications()
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

func (b *Build) validateObjectInstallation(c platform.Caller, record Object) error {
	entity := Entity(record)
	current, _ := platform.Get[Object](b.host.Automation(platform.Caller{}, ID), record.ID)
	previous, _ := ReadPublication[Object](current.Published)
	changed, compatible := false, false
	if previous.Name != "" {
		oldBody, err := definitionBody(previous, TypeOf(previous.Name))
		if err != nil {
			return err
		}
		newBody, err := definitionBody(record, TypeOf(record.Name))
		if err != nil {
			return err
		}
		changed = string(oldBody) != string(newBody)
		compatible = CompatibleObjectExpansion(previous, record)
	}
	if err := b.automation.CheckObjectDefinition(c, entity, Entity(previous), changed, compatible); err != nil {
		return err
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
	explicit, err := b.apps.HasPublishedPage(record.Name)
	if err != nil {
		return nil, "", err
	}
	if explicit {
		return nil, "", nil
	}
	return []platform.Page{ontology.CompileObjectPage(record, ID)}, record.Name, nil
}

func (b *Build) releaseAssets(objects []Object, pages []Page, apps []Application, processes []automationauthoring.Process, sourceVersion string) ([]platform.ReleaseAsset, error) {
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
			actionVersion := sourceVersion
			for _, binding := range saved.ActionTypes {
				if binding.Ref.Name == action.Schema {
					actionVersion += ".action-" + binding.SourceVersion
				}
			}
			asset, err := platform.WithDependencies(platform.ReleaseAsset{
				Ref:             platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: action.Schema},
				ContractVersion: 1, SourceVersion: actionVersion, Body: actionBody,
			})
			if err != nil {
				return nil, err
			}
			if err := add(asset, false); err != nil {
				return nil, err
			}
		}
		pageAsset, err := b.apps.ObjectPageAsset(saved, sourceVersion)
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
		asset, err := b.apps.PageAsset(saved, sourceVersion)
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
		asset, err := b.apps.ApplicationAsset(saved, sourceVersion)
		if err != nil {
			return nil, err
		}
		if err := add(asset, false); err != nil {
			return nil, err
		}
	}
	processAssets, err := b.automation.ProcessAssets(processes, sourceVersion)
	if err != nil {
		return nil, err
	}
	for _, asset := range processAssets {
		if err := add(asset, false); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(assets, func(a, b platform.ReleaseAsset) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return assets, nil
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
