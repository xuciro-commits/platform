package build

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/internal/construction"
	"platformserver/platform"
)

// CompleteBlueprintAssets uses exactly the ordinary object publisher's
// generated actions/pages. Explicit owner-produced pages retain precedence.
func (b *Build) CompleteBlueprintAssets(input []platform.ReleaseAsset) ([]platform.ReleaseAsset, error) {
	var objects []construction.Object
	for _, asset := range input {
		if asset.Ref.App == construction.ID && asset.Ref.Kind == platform.AssetObject {
			var object construction.Object
			if err := json.Unmarshal(asset.Body, &object); err != nil || construction.TypeOf(object.Name) != asset.Ref.Name {
				return nil, fmt.Errorf("invalid blueprint object %s", asset.Ref)
			}
			object.ID = object.Name
			object.Published = published(object)
			objects = append(objects, object)
		}
	}
	generated, err := releaseAssets(objects, nil, nil, nil, b.Manifest().Version)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(input)
	seen := map[platform.AssetRef]bool{}
	for _, asset := range input {
		seen[asset.Ref] = true
	}
	for _, asset := range generated {
		if !seen[asset.Ref] {
			out = append(out, asset)
		}
	}
	return out, nil
}

// RootDrafts resolves portable logical roots to the same saved drafts the
// existing joint review and simulation already use.
func (b *Build) RootDrafts(roots []platform.AssetRef) ([]construction.JointDraftRef, error) {
	objects, _, _, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, err
	}
	var out []construction.JointDraftRef
	seen := map[construction.JointDraftRef]bool{}
	for _, root := range roots {
		id := ""
		if root.App != construction.ID {
			return nil, fmt.Errorf("test root %s has no Build draft", root)
		}
		switch root.Kind {
		case platform.AssetObject:
			for _, o := range objects {
				if !o.Archived && construction.TypeOf(o.Name) == root.Name {
					if id != "" {
						return nil, fmt.Errorf("test root %s is ambiguous", root)
					}
					id = o.ID
				}
			}
		case platform.AssetFlow:
			for _, p := range processes {
				if !p.Archived && construction.TypeOf(p.Name) == root.Name {
					if id != "" {
						return nil, fmt.Errorf("test root %s is ambiguous", root)
					}
					id = p.ID
				}
			}
		default:
			return nil, fmt.Errorf("test roots use objects or workflows")
		}
		if id == "" {
			return nil, fmt.Errorf("test root %s has no saved draft", root)
		}
		selected := []construction.JointDraftRef{{Kind: root.Kind, ID: id}}
		dependencies, err := b.ReferencedDrafts(root.Kind, id)
		if err != nil {
			return nil, err
		}
		selected = append(selected, dependencies...)
		for _, draft := range selected {
			if !seen[draft] {
				seen[draft] = true
				out = append(out, draft)
			}
		}
	}
	if len(out) == 0 || len(out) > 64 {
		return nil, fmt.Errorf("test roots need one to 64 saved drafts")
	}
	return out, nil
}

func portableConnectionDraft(asset platform.ReleaseAsset) (construction.Connection, error) {
	var definition construction.ConnectionDefinition
	if err := construction.ValueDecodeIntegration(asset.Body, &definition); err != nil {
		return construction.Connection{}, err
	}
	// Site addresses, credentials and concrete protocol settings are filled in
	// this environment before check/publication, never copied from a package.
	return construction.Connection{Name: definition.Name, Title: definition.Title, Kind: definition.Kind, Marking: definition.Marking, State: "draft"}, nil
}

func (b *Build) draftIntegrationTargets(assets []platform.ReleaseAsset) {
	for _, asset := range assets {
		if asset.Ref.App != construction.ID || !construction.ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		id := asset.Ref.Name
		rows, err := b.integrationInventory(asset.Ref.Kind)
		if err == nil {
			for _, row := range rows {
				name, _, _, _ := integrationMetadata(row)
				if name == asset.Ref.Name {
					id = integrationRecord(row).ID
					break
				}
			}
		}
		b.releaseDraftIDs[asset.Ref] = id
	}
}
