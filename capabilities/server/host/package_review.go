package host

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"platformserver/kernel/transport"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	build "platformserver/services/resources/authoring"
)

type PackageReview struct {
	ID         string                                `json:"id"`
	Version    string                                `json:"version"`
	Changes    []platform.PackageResourceChange      `json:"changes"`
	Extensions []releaseowner.ReleaseExtensionReview `json:"extensions"`
	Removals   []PackageRemovalImpact                `json:"removals"`
	Problems   []string                              `json:"problems,omitempty"`
	Impact     platform.PackageLifecycleImpact       `json:"impact"`
}

type PackageRemovalImpact struct {
	Resource platform.AssetRef `json:"resource"`
	Consumer platform.AssetRef `json:"consumer"`
}

func (t *Tenant) packageRemovalImpacts(removed, replaced map[platform.AssetRef]bool) []PackageRemovalImpact {
	archived := map[platform.AssetRef]bool{}
	for _, row := range t.Projection.Type(build.ResourceType).Rows().All() {
		resource := row.Value.Interface().(build.Resource)
		archived[resource.Ref] = resource.Archived
	}
	out := []PackageRemovalImpact{}
	for _, definition := range t.definitions {
		if removed[definition.Ref] || replaced[definition.Ref] || archived[definition.Ref] || !t.ownerActive(definition.Ref.App) {
			continue
		}
		for _, dependency := range definition.Requires {
			if removed[dependency] {
				out = append(out, PackageRemovalImpact{Resource: dependency, Consumer: definition.Ref})
			}
		}
	}
	slices.SortFunc(out, func(a, b PackageRemovalImpact) int {
		return strings.Compare(a.Resource.String()+"/"+a.Consumer.String(), b.Resource.String()+"/"+b.Consumer.String())
	})
	return slices.Compact(out)
}

func (h *Host) routesPackageReview(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/packages/{id}/review", Summary: "Review the exact product's resource differences and customer extensions", Answer: PackageReview{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		c := t.caller(m, t.app("platform"), false)
		if refusal := authorizationRefusal(t.authorizeNative(c, "GET /v1/packages/{id}/review", "read", []string{"admin"}, true)); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		catalog := t.app("platform").(interface {
			PackageViews() []platform.PackageView
			LifecycleImpact(platform.Caller, string, []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error)
		})
		views := catalog.PackageViews()
		var chosen *platform.PackageView
		for i, view := range views {
			if view.Descriptor.ID == r.PathValue("id") && view.Descriptor.Version == r.URL.Query().Get("version") {
				chosen = &views[i]
				break
			}
		}
		if chosen == nil {
			WriteJSON(w, http.StatusNotFound, map[string]string{"error": "The exact product version is unavailable"})
			return
		}
		answer := PackageReview{ID: chosen.Descriptor.ID, Version: chosen.Descriptor.Version, Changes: []platform.PackageResourceChange{}, Extensions: []releaseowner.ReleaseExtensionReview{}}
		uninstall := r.URL.Query().Get("mode") == "uninstall"
		before, after := map[platform.AssetRef]platform.ReleaseAsset{}, map[platform.AssetRef]platform.ReleaseAsset{}
		owner := t.app(build.ID).(build.ConstructionDrafts)
		if held := chosen.Installed; held != nil && held.State != "retired" {
			for _, view := range views {
				if view.Descriptor.ID == held.ID && view.Descriptor.Version == held.Version && view.Descriptor.Blueprint != nil {
					assets, err := owner.CompleteBlueprintAssets(view.Descriptor.Blueprint.Assets)
					if err != nil {
						WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
						return
					}
					for _, asset := range assets {
						before[asset.Ref] = asset
					}
				}
			}
		}

		if blueprint := chosen.Descriptor.Blueprint; blueprint != nil {
			assets, err := owner.CompleteBlueprintAssets(blueprint.Assets)
			if err != nil {
				WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			for i, asset := range assets {
				complete, err := platform.WithDependencies(asset)
				if err != nil {
					WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
					return
				}
				assets[i] = complete
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
		impact, impactErr := catalog.LifecycleImpact(c, chosen.Descriptor.ID, next)
		if impactErr != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": impactErr.Error()})
			return
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
		}
		if !uninstall {
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
		WriteJSON(w, http.StatusOK, answer)
	})
}
