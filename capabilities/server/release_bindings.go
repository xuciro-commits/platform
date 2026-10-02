package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"platformserver/platform"
)

// A page or flow's explicit binding survives newer installations. Resolve each edge
// through its original owner before the language-neutral candidate validator
// checks the complete bytes. A single candidate cannot contain two versions
// of one named asset, or replace an explicitly requested asset root.
func (t *Tenant) candidateWithBindings(roots []platform.AssetRef, available []platform.ReleaseAsset, fixed []platform.AssetRef) (platform.ReleaseCandidate, error) {
	assets := slices.Clone(available)
	indices := map[platform.AssetRef]int{}
	for i, asset := range assets {
		indices[asset.Ref] = i
	}
	visited := map[platform.AssetRef]bool{}
	pins := map[platform.AssetRef]string{}
	var visit func(platform.AssetRef) error
	visit = func(ref platform.AssetRef) error {
		if visited[ref] {
			return nil
		}
		visited[ref] = true
		i, ok := indices[ref]
		if !ok {
			return fmt.Errorf("release requires missing asset %s", ref)
		}
		asset := assets[i]
		var bindings []platform.AssetBinding
		if ref.Kind == platform.AssetFlow {
			var flow platform.FlowReleaseDescriptor
			if err := json.Unmarshal(asset.Body, &flow); err != nil {
				return err
			}
			bindings = append(slices.Clone(flow.Functions), flow.Operations...)
		} else if ref.Kind == platform.AssetApp {
			var app platform.Application
			if err := json.Unmarshal(asset.Body, &app); err != nil {
				return err
			}
			for _, q := range app.Queries {
				if q.Query != nil {
					bindings = append(bindings, *q.Query)
				}
			}
		} else if ref.Kind == platform.AssetPage {
			var page platform.Page
			if err := json.Unmarshal(asset.Body, &page); err != nil {
				return err
			}
			for _, section := range page.Sections {
				if section.Function != nil {
					bindings = append(bindings, *section.Function)
				}
				if section.Operation != nil {
					bindings = append(bindings, *section.Operation)
				}
			}
			if page.Document != nil {
				for _, q := range page.Document.Queries {
					if q.Query != nil {
						bindings = append(bindings, *q.Query)
					}
				}
			}
		}
		for _, binding := range bindings {
			if binding.Ref.Kind != platform.AssetLinkType && binding.Ref.Kind != platform.AssetQuery && binding.Ref.Kind != platform.AssetFunction && binding.Ref.Kind != platform.AssetCompute || binding.SourceVersion == "" {
				return fmt.Errorf("%s has an invalid version binding", ref)
			}
			if prior := pins[binding.Ref]; prior != "" && prior != binding.SourceVersion {
				return fmt.Errorf("release binds conflicting versions of asset %s", binding.Ref)
			}
			pins[binding.Ref] = binding.SourceVersion
		}
		for _, dep := range asset.Requires {
			if err := visit(dep); err != nil {
				return err
			}
		}
		return nil
	}
	for _, root := range roots {
		if err := visit(root); err != nil {
			return platform.ReleaseCandidate{}, err
		}
	}
	for _, ref := range slices.SortedFunc(maps.Keys(pins), func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) }) {
		version := pins[ref]
		i, exists := indices[ref]
		if !exists {
			return platform.ReleaseCandidate{}, fmt.Errorf("release requires missing bound asset %s", ref)
		}
		if assets[i].SourceVersion == version {
			continue
		}
		if slices.Contains(fixed, ref) {
			return platform.ReleaseCandidate{}, fmt.Errorf("asset %s differs from the version pinned by its dependents", ref)
		}
		var asset platform.ReleaseAsset
		var err error
		if ref.Kind == platform.AssetLinkType {
			owner, ok := t.app(ref.App).(interface {
				LinkTypeReleaseAsset(string, string) (platform.ReleaseAsset, error)
			})
			if !ok {
				return platform.ReleaseCandidate{}, fmt.Errorf("link type has no retained owner")
			}
			asset, err = owner.LinkTypeReleaseAsset(ref.Name, version)
		} else if ref.Kind == platform.AssetQuery {
			owner, ok := t.app(ref.App).(interface {
				QueryReleaseAsset(string, string) (platform.ReleaseAsset, error)
			})
			if !ok {
				return platform.ReleaseCandidate{}, fmt.Errorf("query %s has no retained version owner", ref)
			}
			asset, err = owner.QueryReleaseAsset(ref.Name, version)
		} else if ref.Kind == platform.AssetCompute {
			owner, ok := t.app(ref.App).(interface {
				OperationReleaseAsset(string, string) (platform.ReleaseAsset, error)
			})
			if !ok {
				return platform.ReleaseCandidate{}, fmt.Errorf("compute %s has no retained version owner", ref)
			}
			asset, err = owner.OperationReleaseAsset(ref.Name, version)
		} else {
			owner, ok := t.app(ref.App).(interface {
				FunctionReleaseAsset(string, string) (platform.ReleaseAsset, error)
			})
			if !ok {
				return platform.ReleaseCandidate{}, fmt.Errorf("function %s has no retained version owner", ref)
			}
			asset, err = owner.FunctionReleaseAsset(ref.Name, version)
		}
		if err != nil {
			return platform.ReleaseCandidate{}, err
		}
		if asset.Ref != ref || asset.SourceVersion != version {
			return platform.ReleaseCandidate{}, fmt.Errorf("asset %s returned a different retained version", ref)
		}
		assets[i] = asset
	}
	return platform.Candidate(roots, assets)
}
