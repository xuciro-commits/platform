package platformserver

import (
	"encoding/json"
	"errors"
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
	contentPins := map[platform.AssetRef]string{}
	// Rebuild the traversal after selecting retained page bytes. This discards
	// bindings collected from superseded descriptors, regardless of root order.
	restart := errors.New("restart retained page closure")
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
		if ref.Kind == platform.AssetObject {
			var object struct {
				Fields []struct {
					Property *platform.AssetBinding `json:"property,omitempty"`
				} `json:"fields"`
				Sets    []platform.RecordSet `json:"sets,omitempty"`
				Actions []struct {
					Transaction *platform.BusinessTxn `json:"transaction,omitempty"`
				} `json:"actions,omitempty"`
			}
			if json.Unmarshal(asset.Body, &object) != nil {
				return fmt.Errorf("invalid property consumer")
			}
			for _, set := range object.Sets {
				if set.Binding != nil {
					bindings = append(bindings, *set.Binding)
				}
			}
			var bindSteps func([]platform.TxnStep)
			bindSteps = func(steps []platform.TxnStep) {
				for _, step := range steps {
					if step.Link != nil {
						bindings = append(bindings, step.Link.Binding)
					}
					if step.ForEach != nil {
						bindSteps(step.ForEach.Steps)
					}
				}
			}
			for _, action := range object.Actions {
				if action.Transaction != nil {
					bindSteps(action.Transaction.Steps)
				}
			}
			for _, f := range object.Fields {
				if f.Property != nil {
					bindings = append(bindings, *f.Property)
				}
			}
		} else if ref.Kind == platform.AssetEvaluation {
			descriptor, err := platform.ReadEvaluationSet(asset.Body)
			if err != nil {
				return err
			}
			bindings = append(bindings, descriptor.Function)
		} else if ref.Kind == platform.AssetFlow {
			var flow platform.FlowReleaseDescriptor
			if err := json.Unmarshal(asset.Body, &flow); err != nil {
				return err
			}
			bindings = append(slices.Clone(flow.Functions), flow.Operations...)
			bindings = append(bindings, flow.Queries...)
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
				if e := section.Embedding; e != nil {
					ref := e.Page.Ref
					if prior := contentPins[ref]; prior != "" && prior != e.ContentVersion {
						return fmt.Errorf("release binds conflicting contents of page %s", ref)
					}
					if prior := pins[ref]; prior != "" && prior != e.Page.SourceVersion {
						return fmt.Errorf("release binds conflicting source versions of page %s", ref)
					}
					contentPins[ref], pins[ref] = e.ContentVersion, e.Page.SourceVersion
					index, ok := indices[ref]
					if !ok {
						return fmt.Errorf("embedded page %s is unavailable", ref)
					}
					var current platform.Page
					if json.Unmarshal(assets[index].Body, &current) != nil {
						return fmt.Errorf("embedded page descriptor is invalid")
					}
					digest, err := platform.PageContentVersion(current)
					if err != nil {
						return err
					}
					if digest != e.ContentVersion || assets[index].SourceVersion != e.Page.SourceVersion {
						if slices.Contains(fixed, ref) {
							return fmt.Errorf("embedded page %s differs from its explicit release root", ref)
						}
						registry, err := t.assetRegistryLocked()
						if err != nil {
							return err
						}
						asset, err := registry.ResolvePinned(ref, e.Page.SourceVersion, e.ContentVersion)
						if err != nil {
							return err
						}
						if assets[index].SourceVersion != asset.SourceVersion {
							return fmt.Errorf("embedded page source version is incompatible")
						}
						assets[index] = asset
						return restart
					}
				}
				bindings = append(bindings, section.ExplorationBindings()...)
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
			if binding.Ref.Check() != nil || binding.SourceVersion == "" {
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
	for pass := 0; ; pass++ {
		clear(visited)
		clear(pins)
		clear(contentPins)
		var err error
		for _, root := range roots {
			if err = visit(root); err != nil {
				break
			}
		}
		if err == nil {
			break
		}
		if !errors.Is(err, restart) {
			return platform.ReleaseCandidate{}, err
		}
		if pass >= 4*len(assets) {
			return platform.ReleaseCandidate{}, fmt.Errorf("page content bindings do not converge")
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
		registry, err := t.assetRegistryLocked()
		if err != nil {
			return platform.ReleaseCandidate{}, err
		}
		asset, err := registry.ResolvePinned(ref, version)
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
