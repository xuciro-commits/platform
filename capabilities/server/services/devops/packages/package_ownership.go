package packages

import (
	"slices"
	"strings"

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

func (m Managed) Ownership(project *build.Application, owner build.ConstructionDrafts) (PackageOwnership, error) {
	id, name := m.Project()
	plan := PackageOwnership{ID: m.Descriptor.ID, Version: m.Descriptor.Version, Project: id, Name: name, Title: m.Descriptor.Title, CreateProject: project == nil, Entries: []PackageOwnershipEntry{}, Problems: []string{}}
	if project != nil && project.Archived {
		plan.Problems = append(plan.Problems, "The destination project is archived")
	}
	if m.Descriptor.Blueprint == nil {
		return plan, nil
	}
	assets, err := owner.CompleteBlueprintAssets(m.Descriptor.Blueprint.Assets)
	if err != nil {
		return plan, err
	}
	refs := map[platform.AssetRef]bool{}
	for _, asset := range assets {
		refs[asset.Ref] = true
	}
	for _, resource := range m.Resources {
		if !refs[resource.Ref] || resource.ManagedBy == m.Descriptor.ID {
			continue
		}
		if resource.Folder != "" || resource.Draft == resource.Project && resource.Type == build.AppType {
			plan.Problems = append(plan.Problems, "Move the resource out of its folder before rehoming it: "+resource.Ref.String())
			continue
		}
		plan.Entries = append(plan.Entries, PackageOwnershipEntry{ID: resource.ID, Project: plan.Project, ManagedBy: m.Descriptor.ID, Version: resource.Version, Ref: resource.Ref, From: resource.ManagedBy})
	}
	slices.SortFunc(plan.Entries, func(left, right PackageOwnershipEntry) int { return strings.Compare(left.ID, right.ID) })
	return plan, nil
}
