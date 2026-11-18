package build

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"

	"reflect"
	"slices"
	"strings"

	"platformserver/kernel/records"
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
	catalog := b.ledger.Catalog()
	if b.installationCatalog != nil {
		catalog = b.installationCatalog
	}
	return slices.Contains((platform.Manifest{Actions: catalog, Roles: b.manifestRoles()}).AllRoles(), role)
}

func (b *Build) jointObjectEntities(objects []ontology.Object, drafts []JointDraftRef) map[string]platform.EntityInfo {
	joint := map[string]platform.EntityInfo{}
	for _, draft := range drafts {
		if draft.Kind != platform.AssetObject {
			continue
		}
		i := slices.IndexFunc(objects, func(o ontology.Object) bool { return o.ID == draft.ID && !o.Archived })
		if i < 0 {
			continue
		}
		record := objects[i]
		info, err := records.Describe(ID, Entity(record), func(reflect.Type) string { return "" })
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
		b.functions.Assets, b.agent.Assets, b.queries.Assets, b.kindAssets(&linkKind), b.kindAssets(&interfaceKind), b.kindAssets(&propertyKind), b.kindAssets(&valueKind), b.kindAssets(&objectTypeGroupKind),
		b.CodeReleaseAssets, b.data.Assets, b.testPlans.Assets, b.sampleSets.Assets,
	} {
		owned, err := read()
		if err != nil {
			return nil, err
		}
		assets = append(assets, owned...)
	}
	return b.data.BindReleaseAssets(assets)
}

func (b *Build) releaseInventory() ([]ontology.Object, []protocol.Page, []protocol.Application, error) {
	if b.host == nil {
		return nil, nil, nil, fmt.Errorf("builder has no host")
	}
	c := b.host.Automation(platform.Caller{}, ID)
	objects, err := readDefinitionInventory[ontology.Object](c)
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

func (b *Build) validateObjectInstallation(c platform.Caller, record ontology.Object) error {
	return ontology.ValidateObjectInstallation(c, record, ontology.ObjectInstallationEnvironment{
		Host: b.host, Entity: Entity, CheckProcesses: b.automation.CheckObjectDefinition,
		Pages: b.objectInstallationPages, Actions: objectActions, JointMode: func() bool { return b.jointMode },
	})
}

func (b *Build) objectInstallationPages(record ontology.Object) ([]platform.Page, string, error) {
	explicit, err := b.apps.HasPublishedPage(record.Name)
	if err != nil {
		return nil, "", err
	}
	if explicit {
		return nil, "", nil
	}
	return []platform.Page{b.application.ObjectPage(record, ID)}, record.Name, nil
}

func (b *Build) releaseAssets(objects []ontology.Object, pages []protocol.Page, apps []protocol.Application, processes []automationauthoring.Process, sourceVersion string) ([]platform.ReleaseAsset, error) {
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
	if err := ontology.AppendObjectAssets(objects, sourceVersion, add, b.resolver.WithDependencies, func(object ontology.Object) []platform.Action {
		return objectActions(object, Entity(object))
	}, func(object ontology.Object, version string) (platform.ReleaseAsset, error) {
		return b.apps.PageAssetFromView(b.application.ObjectPage(object, ID), version)
	}); err != nil {
		return nil, err
	}

	for _, record := range pages {
		if record.Archived || record.Published == "" {
			continue
		}
		var saved protocol.Page
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
		var saved protocol.Application
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
