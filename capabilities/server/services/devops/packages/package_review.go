package packages

import (
	"bytes"
	"encoding/json"
	"platformserver/kernel/protocol"

	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

type PackageReview struct {
	ID         string                            `json:"id"`
	Version    string                            `json:"version"`
	Changes    []protocol.PackageResourceChange  `json:"changes"`
	Extensions []platform.PackageImpactExtension `json:"extensions"`
	Removals   []PackageRemovalImpact            `json:"removals"`
	Problems   []string                          `json:"problems,omitempty"`
	Impact     platform.PackageLifecycleImpact   `json:"impact"`
}

type PackageRemovalImpact struct {
	Resource platform.AssetRef `json:"resource"`
	Consumer platform.AssetRef `json:"consumer"`
}

func FindView(views []protocol.PackageView, id, version string) *protocol.PackageView {
	for i, view := range views {
		if view.Descriptor.ID == id && view.Descriptor.Version == version {
			return &views[i]
		}
	}
	return nil
}

func RemovalImpacts(resources []protocol.Resource, definitions []platform.Definition, active func(string) bool, removed, replaced map[platform.AssetRef]bool) []platform.PackageImpactReference {
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
	Resolver  publication.Resolver
}

func (rv Review) Of(views []protocol.PackageView, chosen *protocol.PackageView, uninstall bool) (PackageReview, error) {
	answer := PackageReview{ID: chosen.Descriptor.ID, Version: chosen.Descriptor.Version, Changes: []protocol.PackageResourceChange{}, Extensions: []platform.PackageImpactExtension{}}
	before, after := map[platform.AssetRef]platform.ReleaseAsset{}, map[platform.AssetRef]platform.ReleaseAsset{}
	space := ""
	if held := chosen.Installed; held != nil && held.State != "retired" {
		space = held.Space
		for _, view := range views {
			if view.Descriptor.ID == held.ID && view.Descriptor.Version == held.Version && view.Descriptor.Blueprint != nil {
				desc := view.Descriptor
				if space != "" {
					var err error
					if desc, err = Rebind(desc, space); err != nil {
						return answer, err
					}
				}
				assets, err := rv.Blueprint(desc.Blueprint.Assets)
				if err != nil {
					return answer, err
				}
				for _, asset := range assets {
					before[asset.Ref] = asset
				}
			}
		}
	}
	chosenDesc := chosen.Descriptor
	if space != "" && chosenDesc.Blueprint != nil {
		var err error
		if chosenDesc, err = Rebind(chosenDesc, space); err != nil {
			return answer, err
		}
	}
	if blueprint := chosenDesc.Blueprint; blueprint != nil {
		assets, err := rv.Blueprint(blueprint.Assets)
		if err != nil {
			return answer, err
		}
		for _, asset := range assets {
			complete, err := rv.Resolver.WithDependencies(asset)
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
		entry := protocol.PackageResourceChange{Ref: ref, Change: change, Before: old.SourceVersion, After: asset.SourceVersion}
		if exists && ref.Kind == platform.AssetObject {
			entry.Fields = diffObjectFields(ref.Name, old.Body, asset.Body, !chosen.Downgrade)
			for _, field := range entry.Fields {
				if field.Incompatible && field.Reason != "" {
					answer.Problems = append(answer.Problems, field.Reason)
				}
			}
		}
		answer.Changes = append(answer.Changes, entry)
	}
	for ref, asset := range before {
		if _, exists := after[ref]; !exists {
			answer.Changes = append(answer.Changes, protocol.PackageResourceChange{Ref: ref, Change: "Removed", Before: asset.SourceVersion})
		}
	}
	slices.SortFunc(answer.Changes, func(a, b protocol.PackageResourceChange) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return answer, nil
}

type reviewedObjectField struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Required bool            `json:"required"`
	Ref      string          `json:"ref"`
	Choices  json.RawMessage `json:"choices"`
}

func diffObjectFields(object string, beforeRaw, afterRaw json.RawMessage, checkUpgrade bool) []protocol.PackageFieldChange {
	var oldDoc, newDoc struct {
		Fields []reviewedObjectField `json:"fields"`
	}
	if json.Unmarshal(beforeRaw, &oldDoc) != nil || json.Unmarshal(afterRaw, &newDoc) != nil {
		return nil
	}
	oldMap := map[string]reviewedObjectField{}
	for _, field := range oldDoc.Fields {
		oldMap[field.Name] = field
	}
	newMap := map[string]reviewedObjectField{}
	for _, field := range newDoc.Fields {
		newMap[field.Name] = field
	}
	var out []protocol.PackageFieldChange
	for _, field := range newDoc.Fields {
		prev, exists := oldMap[field.Name]
		if !exists {
			item := protocol.PackageFieldChange{Name: field.Name, Change: "added", After: field.Type}
			if checkUpgrade && field.Required {
				item.Incompatible = true
				item.Reason = "Adding required field " + object + "." + field.Name + " is incompatible with existing records"
			}
			out = append(out, item)
			continue
		}
		if prev.Type != field.Type || prev.Required != field.Required || prev.Ref != field.Ref || !bytes.Equal(prev.Choices, field.Choices) {
			item := protocol.PackageFieldChange{Name: field.Name, Change: "changed", Before: prev.Type, After: field.Type}
			if checkUpgrade {
				item.Incompatible = true
				from, to := prev.Type, field.Type
				if prev.Type == field.Type && prev.Ref != field.Ref {
					from, to = prev.Type+" "+prev.Ref, field.Type+" "+field.Ref
				}
				item.Reason = "Changing field " + object + "." + field.Name + " from " + from + " to " + to + " is incompatible with existing records"
			}
			out = append(out, item)
		}
	}
	for _, prev := range oldDoc.Fields {
		if _, exists := newMap[prev.Name]; !exists {
			item := protocol.PackageFieldChange{Name: prev.Name, Change: "removed", Before: prev.Type}
			if checkUpgrade {
				item.Incompatible = true
				item.Reason = "Removing field " + object + "." + prev.Name + " is incompatible with existing records"
			}
			out = append(out, item)
		}
	}
	slices.SortFunc(out, func(a, b protocol.PackageFieldChange) int { return strings.Compare(a.Name, b.Name) })
	return out
}
