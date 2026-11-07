package host

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"platformserver/kernel/transport"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

type PackageOwnership struct {
	ID            string                  `json:"id"`
	Version       string                  `json:"version"`
	Project       string                  `json:"project"`
	Name          string                  `json:"name"`
	Title         string                  `json:"title"`
	CreateProject bool                    `json:"createProject"`
	Entries       []PackageOwnershipEntry `json:"entries"`
	Problems      []string                `json:"problems"`
}

type PackageOwnershipEntry struct {
	ID        string            `json:"id"`
	Project   string            `json:"project"`
	ManagedBy string            `json:"managedBy"`
	Version   uint32            `json:"version"`
	Ref       platform.AssetRef `json:"ref"`
	From      string            `json:"from"`
}

func (t *Tenant) packageOwnership(descriptor platform.PackageDescriptor) (PackageOwnership, error) {
	hash := sha256.Sum256([]byte(descriptor.ID))
	plan := PackageOwnership{ID: descriptor.ID, Version: descriptor.Version, Project: "package:" + descriptor.ID, Name: fmt.Sprintf("pkg%x", hash[:12]), Title: descriptor.Title, Entries: []PackageOwnershipEntry{}, Problems: []string{}}
	project := t.records.Type(build.AppType).Rows().Get(plan.Project)
	plan.CreateProject = project == nil
	if project != nil && project.Value.Interface().(build.Application).Archived {
		plan.Problems = append(plan.Problems, "The destination project is archived")
	}
	if descriptor.Blueprint == nil {
		return plan, nil
	}
	assets, err := t.app(build.ID).(build.ConstructionDrafts).CompleteBlueprintAssets(descriptor.Blueprint.Assets)
	if err != nil {
		return plan, err
	}
	refs := map[platform.AssetRef]bool{}
	for _, asset := range assets {
		refs[asset.Ref] = true
	}
	for _, row := range t.records.Type(build.ResourceType).Rows().All() {
		resource := row.Value.Interface().(build.Resource)
		if !refs[resource.Ref] || resource.ManagedBy == descriptor.ID {
			continue
		}
		if resource.Folder != "" || resource.Draft == resource.Project && resource.Type == build.AppType {
			plan.Problems = append(plan.Problems, "Move the resource out of its folder before rehoming it: "+resource.Ref.String())
			continue
		}
		plan.Entries = append(plan.Entries, PackageOwnershipEntry{ID: resource.ID, Project: plan.Project, ManagedBy: descriptor.ID, Version: resource.Version, Ref: resource.Ref, From: resource.ManagedBy})
	}
	slices.SortFunc(plan.Entries, func(left, right PackageOwnershipEntry) int { return strings.Compare(left.ID, right.ID) })
	return plan, nil
}

func (h *Host) routesPackageOwnership(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/packages/{id}/ownership", Summary: "Review an explicit resource ownership transfer before installing a split product", Answer: PackageOwnership{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		c := t.caller(m, t.app("platform"), false)
		if refusal := authorizationRefusal(t.authorizeNative(c, "GET /v1/packages/{id}/ownership", "read", []string{"admin"}, true)); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		catalog := t.app("platform").(interface{ PackageViews() []platform.PackageView })
		for _, view := range catalog.PackageViews() {
			if view.Descriptor.ID != r.PathValue("id") || view.Descriptor.Version != r.URL.Query().Get("version") {
				continue
			}
			plan, err := t.packageOwnership(view.Descriptor)
			if err != nil {
				WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			WriteJSON(w, http.StatusOK, plan)
			return
		}
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "The exact product version is unavailable"})
	})
}
