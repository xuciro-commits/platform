package packages

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

type PackageReview struct {
	ID         string                            `json:"id"`
	Version    string                            `json:"version"`
	Changes    []platform.PackageResourceChange  `json:"changes"`
	Extensions []platform.PackageImpactExtension `json:"extensions"`
	Removals   []PackageRemovalImpact            `json:"removals"`
	Problems   []string                          `json:"problems,omitempty"`
	Impact     platform.PackageLifecycleImpact   `json:"impact"`
}

type PackageRemovalImpact struct {
	Resource platform.AssetRef `json:"resource"`
	Consumer platform.AssetRef `json:"consumer"`
}

func FindView(views []platform.PackageView, id, version string) *platform.PackageView {
	for i, view := range views {
		if view.Descriptor.ID == id && view.Descriptor.Version == version {
			return &views[i]
		}
	}
	return nil
}

func RemovalImpacts(resources []build.Resource, definitions []platform.Definition, active func(string) bool, removed, replaced map[platform.AssetRef]bool) []platform.PackageImpactReference {
	archived := map[platform.AssetRef]bool{}
	for _, resource := range resources {
		archived[resource.Ref] = resource.Archived
	}
	var out []platform.PackageImpactReference
	for _, definition := range definitions {
		if removed[definition.Ref] || replaced[definition.Ref] || archived[definition.Ref] || !active(definition.Ref.App) {
			continue
		}
		for _, dependency := range definition.Requires {
			if removed[dependency] {
				out = append(out, platform.PackageImpactReference{Resource: dependency, Consumer: definition.Ref})
			}
		}
	}
	slices.SortFunc(out, func(a, b platform.PackageImpactReference) int {
		return strings.Compare(a.Resource.String()+"/"+a.Consumer.String(), b.Resource.String()+"/"+b.Consumer.String())
	})
	return slices.Compact(out)
}

type Review struct {
	Blueprint func([]platform.ReleaseAsset) ([]platform.ReleaseAsset, error)
	Impact    func(id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error)
}

func (rv Review) Of(views []platform.PackageView, chosen *platform.PackageView, uninstall bool) (PackageReview, error) {
	answer := PackageReview{ID: chosen.Descriptor.ID, Version: chosen.Descriptor.Version, Changes: []platform.PackageResourceChange{}, Extensions: []platform.PackageImpactExtension{}}
	before, after := map[platform.AssetRef]platform.ReleaseAsset{}, map[platform.AssetRef]platform.ReleaseAsset{}
	if held := chosen.Installed; held != nil && held.State != "retired" {
		for _, view := range views {
			if view.Descriptor.ID == held.ID && view.Descriptor.Version == held.Version && view.Descriptor.Blueprint != nil {
				assets, err := rv.Blueprint(view.Descriptor.Blueprint.Assets)
				if err != nil {
					return answer, err
				}
				for _, asset := range assets {
					before[asset.Ref] = asset
				}
			}
		}
	}
	if blueprint := chosen.Descriptor.Blueprint; blueprint != nil {
		assets, err := rv.Blueprint(blueprint.Assets)
		if err != nil {
			return answer, err
		}
		for _, asset := range assets {
			complete, err := platform.WithDependencies(asset)
			if err != nil {
				return answer, err
			}
			after[complete.Ref] = complete
		}
	}
	if uninstall {
		after = map[platform.AssetRef]platform.ReleaseAsset{}
	}
	next := make([]platform.ReleaseAsset, 0, len(after))
	for _, asset := range after {
		next = append(next, asset)
	}
	impact, err := rv.Impact(chosen.Descriptor.ID, next)
	if err != nil {
		return answer, err
	}
	answer.Impact = impact
	answer.Extensions = impact.Extensions
	if !uninstall {
		for _, extension := range impact.Extensions {
			if extension.Issue != "" {
				answer.Problems = append(answer.Problems, extension.Issue)
			}
		}
	}
	answer.Removals = make([]PackageRemovalImpact, 0, len(impact.References))
	for _, ref := range impact.References {
		answer.Removals = append(answer.Removals, PackageRemovalImpact(ref))
	}
	if uninstall {
		answer.Problems = append(answer.Problems, impact.Dependencies...)
		for _, run := range impact.Runs {
			answer.Problems = append(answer.Problems, "Work is still in flight: "+run.Kind+"/"+run.ID)
		}
	} else {
		for _, impact := range answer.Removals {
			answer.Problems = append(answer.Problems, "Remove references to "+impact.Resource.String()+" from "+impact.Consumer.String()+" before upgrading the product")
		}
	}
	for ref, asset := range after {
		old, exists := before[ref]
		var oldBody, newBody any
		_ = json.Unmarshal(old.Body, &oldBody)
		_ = json.Unmarshal(asset.Body, &newBody)
		if exists && reflect.DeepEqual(oldBody, newBody) {
			continue
		}
		change := "Added"
		if exists {
			change = "Changed"
		}
		answer.Changes = append(answer.Changes, platform.PackageResourceChange{Ref: ref, Change: change, Before: old.SourceVersion, After: asset.SourceVersion})
	}
	for ref, asset := range before {
		if _, exists := after[ref]; !exists {
			answer.Changes = append(answer.Changes, platform.PackageResourceChange{Ref: ref, Change: "Removed", Before: asset.SourceVersion})
		}
	}
	slices.SortFunc(answer.Changes, func(a, b platform.PackageResourceChange) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return answer, nil
}
