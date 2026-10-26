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
	generated, err := releaseAssets(objects, nil, nil, b.Manifest().Version)
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
		default:
			return nil, fmt.Errorf("test roots use saved objects")
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
	if len(out) == 0 || len(out) > construction.JointDraftLimit {
		return nil, fmt.Errorf("test roots need one to %d saved drafts", construction.JointDraftLimit)
	}
	return out, nil
}
