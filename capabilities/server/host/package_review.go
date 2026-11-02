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
)

type PackageReview struct {
	ID         string                                `json:"id"`
	Version    string                                `json:"version"`
	Changes    []platform.PackageResourceChange      `json:"changes"`
	Extensions []releaseowner.ReleaseExtensionReview `json:"extensions"`
	Problems   []string                              `json:"problems,omitempty"`
}

func (h *Host) routesPackageReview(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/packages/{id}/review", Summary: "Review the exact product's resource differences and customer extensions", Answer: PackageReview{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		c := t.caller(m, t.app("platform"), false)
		if refusal := authorizationRefusal(t.authorizeNative(c, "GET /v1/packages/{id}/review", "read", []string{"admin"}, true)); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		catalog := t.app("platform").(interface{ PackageViews() []platform.PackageView })
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
		before, after := map[platform.AssetRef]platform.ReleaseAsset{}, map[platform.AssetRef]platform.ReleaseAsset{}
		if held := chosen.Installed; held != nil && held.State != "retired" {
			for _, view := range views {
				if view.Descriptor.ID == held.ID && view.Descriptor.Version == held.Version && view.Descriptor.Blueprint != nil {
					for _, asset := range view.Descriptor.Blueprint.Assets {
						before[asset.Ref] = asset
					}
				}
			}
		}

		if blueprint := chosen.Descriptor.Blueprint; blueprint != nil {
			for _, asset := range blueprint.Assets {
				after[asset.Ref] = asset
			}
			extensions, err := t.releaseExtensionReview(platform.ReleaseCandidate{Assets: blueprint.Assets})
			if err != nil {
				answer.Problems = append(answer.Problems, err.Error())
			} else {
				for _, extension := range extensions {
					if _, affected := after[extension.Base.Ref]; affected {
						answer.Extensions = append(answer.Extensions, extension)
						if extension.Issue != "" {
							answer.Problems = append(answer.Problems, extension.Issue)
						}
					}
				}
			}
		}
		if r.URL.Query().Get("mode") == "uninstall" {
			after = map[platform.AssetRef]platform.ReleaseAsset{}
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
