package build

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/construction"
	"reflect"
	"slices"
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
	assets, err := releaseAssets(objects, pages, apps, b.Manifest().Version)
	if err != nil {
		return nil, err
	}
	for _, read := range []func() ([]platform.ReleaseAsset, error){
		b.functionAssets, b.agentAssets, b.queryAssets, b.linkTypeAssets, b.propertyTypeAssets,
		b.CodeReleaseAssets, b.alertAssets, b.migrationPlanAssets,
	} {
		owned, err := read()
		if err != nil {
			return nil, err
		}
		assets = append(assets, owned...)
	}
	return assets, nil
}

func (b *Build) releaseInventory() ([]construction.Object, []Page, []Application, error) {
	if b.host == nil {
		return nil, nil, nil, fmt.Errorf("builder has no host")
	}
	c := b.host.Automation(platform.Caller{}, construction.ID)
	objects, err := readDefinitionInventory[construction.Object](c)
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
		saved, ok := construction.ReadPublication[Page](record.Published)
		if !ok {
			return false, fmt.Errorf("invalid published page %s", record.ID)
		}
		if saved.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (b *Build) validateObjectInstallation(c platform.Caller, record construction.Object) error {
	entity := Entity(record)
	pages, generated, err := b.objectInstallationPages(record)
	if err != nil {
		return err
	}
	if b.jointMode {
		return nil // The complete private installation checks replaced and retained dependents together.
	}
	return b.host.ValidateInstallDependents(c, entity, objectActions(record, entity), generated, pages...)
}

// An explicitly published page owns its name after it replaces an object's
// generated page. Updating the object must not silently overwrite that page.
func (b *Build) objectInstallationPages(record construction.Object) ([]platform.Page, string, error) {
	explicit, err := b.PublishedPage(record.Name)
	if err != nil {
		return nil, "", err
	}
	if explicit {
		return nil, "", nil
	}
	return []platform.Page{construction.CompileObjectPage(record)}, record.Name, nil
}

// releaseAssets uses the saved publication, never a later mutable draft. The
// object body retains every declarative field (including access, conditions
// and approvals), excluding only record identity/stamps and editor state.
// An action depends on that complete object, so changing a rule changes its
// closed release even when the action's display metadata stays the same.
func releaseAssets(objects []construction.Object, pages []Page, apps []Application, sourceVersion string) ([]platform.ReleaseAsset, error) {
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
		var saved construction.Object
		if err := json.Unmarshal([]byte(record.Published), &saved); err != nil {
			return nil, fmt.Errorf("invalid published object %s: %w", record.ID, err)
		}
		if saved.ID != record.ID {
			return nil, fmt.Errorf("published object %s belongs to another record", record.ID)
		}
		ref := platform.AssetRef{App: construction.ID, Kind: platform.AssetObject, Name: construction.TypeOf(saved.Name)}
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
				Ref:             platform.AssetRef{App: construction.ID, Kind: platform.AssetAction, Name: action.Schema},
				ContractVersion: 1, SourceVersion: sourceVersion, Body: actionBody,
			})
			if err != nil {
				return nil, err
			}
			if err := add(asset, false); err != nil {
				return nil, err
			}
		}
		pageAsset, err := platform.PageReleaseAsset(construction.ID, sourceVersion, construction.CompileObjectPage(saved))
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
		asset, err := platform.PageReleaseAsset(construction.ID, sourceVersion, descriptor(saved))
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
		asset, err := platform.ApplicationReleaseAsset(construction.ID, sourceVersion, applicationDescriptor(saved))
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

func definitionBody(value construction.Object, typ string) (json.RawMessage, error) {
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
