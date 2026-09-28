package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

// ReleaseAssets returns the builder's installed, unfiltered definitions. The
// member-facing Definitions projection must never be used to calculate a
// release identity: it omits rules and fields the reader cannot see.
func (b *Build) ReleaseAssets() ([]platform.ReleaseAsset, error) {
	if b.host == nil {
		return nil, fmt.Errorf("builder has no host")
	}
	c := b.host.Automation(platform.Caller{}, ID)
	objects, objectCount, err := platform.Find[Object](c, platform.Query{Limit: 1001, Sort: []string{"id"}})
	if err != nil {
		return nil, fmt.Errorf("read builder objects: %w", err)
	}
	pages, pageCount, err := platform.Find[Page](c, platform.Query{Limit: 1001, Sort: []string{"id"}})
	if err != nil {
		return nil, fmt.Errorf("read builder pages: %w", err)
	}
	apps, appCount, err := platform.Find[Application](c, platform.Query{Limit: 1001, Sort: []string{"id"}})
	if err != nil {
		return nil, fmt.Errorf("read builder applications: %w", err)
	}
	if objectCount > 1000 || pageCount > 1000 || appCount > 1000 {
		return nil, fmt.Errorf("builder release inventory exceeds 1000 definitions of one kind")
	}
	return releaseAssets(objects, pages, apps, b.Manifest().Version)
}

// releaseAssets uses the saved publication, never a later mutable draft. The
// object body retains every declarative field (including access, conditions
// and approvals), excluding only record identity/stamps and editor state.
// An action depends on that complete object, so changing a rule changes its
// closed release even when the action's display metadata stays the same.
func releaseAssets(objects []Object, pages []Page, apps []Application, sourceVersion string) ([]platform.ReleaseAsset, error) {
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
			if field.Ref != "" && field.Ref != ref.Name {
				app, _, ok := strings.Cut(field.Ref, ".")
				if !ok {
					return nil, fmt.Errorf("object %s field %s has invalid reference %s", ref, field.Name, field.Ref)
				}
				requires = append(requires, platform.AssetRef{App: app, Kind: platform.AssetObject, Name: field.Ref})
			}
		}
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
