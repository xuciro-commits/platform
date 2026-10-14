package platformserver

import (
	"fmt"
	buildapi "platformserver/internal/host/buildapi"
	"platformserver/platform"
	"slices"
)

// bindApplications freezes memberships alongside the original run/intent.
// Several artifacts may use the same resource; these bindings grant nothing.
func (t *Tenant) bindApplications(resource platform.AssetRef, release, dependencies string, jobOwner ...string) ([]platform.AssetBinding, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return nil, err
	}
	if release != "" && dependencies != "" {
		candidate, err := t.releases.candidate(release)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == resource }) {
			available = candidate.Assets
		}
	}
	lookup := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, asset := range available {
		lookup[asset.Ref] = asset
	}
	out := []platform.AssetBinding{}
	for _, root := range available {
		if root.Ref.Kind != platform.AssetApp {
			continue
		}
		graph := applicationRunGraph(root.Ref, lookup)
		uses := graph[resource]
		if len(jobOwner) > 0 {
			for ref := range graph {
				if ref.App == jobOwner[0] {
					uses = true
					break
				}
			}
		}
		if !uses {
			continue
		}
		candidate, err := t.candidateWithBindings([]platform.AssetRef{root.Ref}, available, []platform.AssetRef{root.Ref})
		if err != nil {
			continue
		}
		if dependencies != "" {
			selected, err := t.candidateWithBindings([]platform.AssetRef{resource}, candidate.Assets, []platform.AssetRef{resource})
			if err != nil || selected.ID != dependencies {
				continue
			}
		}
		out = append(out, platform.AssetBinding{Ref: root.Ref, SourceVersion: candidate.ID})
	}
	slices.SortFunc(out, func(a, b platform.AssetBinding) int { return compareApplicationRef(a.Ref, b.Ref) })
	return out, nil
}
func compareApplicationRef(a, b platform.AssetRef) int {
	if a.String() < b.String() {
		return -1
	}
	if a.String() > b.String() {
		return 1
	}
	return 0
}
func checkApplicationBindings(bindings []platform.AssetBinding) error {
	if len(bindings) > 1000 {
		return fmt.Errorf("application bindings exceed the owner budget")
	}
	seen := map[platform.AssetRef]bool{}
	for _, binding := range bindings {
		if binding.Ref.Check() != nil || binding.Ref.Kind != platform.AssetApp || binding.SourceVersion == "" || seen[binding.Ref] {
			return fmt.Errorf("application binding is invalid or repeated")
		}
		seen[binding.Ref] = true
	}
	return nil
}

func checkRunApplicationBindings(record any) error {
	var bindings []platform.AssetBinding
	switch run := record.(type) {
	case platform.FlowInstance:
		bindings = run.Applications
	case AgentRunRecord:
		bindings = run.Applications
	case buildapi.FunctionRun:
		bindings = run.Applications
	}
	return checkApplicationBindings(bindings)
}
func (h hostView) ApplicationBindings(resource platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
	return h.t.bindApplications(resource, release, dependencies)
}
