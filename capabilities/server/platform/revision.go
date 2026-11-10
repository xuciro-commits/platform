package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
)

type ReleaseAsset struct {
	Ref             AssetRef        `json:"ref"`
	ContractVersion int             `json:"contractVersion"`
	SourceVersion   string          `json:"sourceVersion"`
	Requires        []AssetRef      `json:"requires"`
	Provides        []Capability    `json:"provides,omitempty"`
	Body            json.RawMessage `json:"body"`
}

type FlowReleaseDescriptor struct {
	Native       bool            `json:"native,omitempty"`
	Name         string          `json:"name"`
	Subject      AssetRef        `json:"subject"`
	Actions      []AssetRef      `json:"actions"`
	Definition   json.RawMessage `json:"definition"`
	Functions    []AssetBinding  `json:"functions,omitempty"`
	Operations   []AssetBinding  `json:"operations,omitempty"`
	Queries      []AssetBinding  `json:"queries,omitempty"`
	Dependencies []AssetRef      `json:"dependencies,omitempty"`
}

type AssetBinding struct {
	Ref           AssetRef `json:"ref"`
	SourceVersion string   `json:"sourceVersion"`
}

func PageReleaseAsset(app, sourceVersion string, page Page) (ReleaseAsset, error) {
	if err := page.CheckSafety(); err != nil {
		return ReleaseAsset{}, err
	}
	body, err := json.Marshal(page)
	if err != nil {
		return ReleaseAsset{}, err
	}
	return WithDependencies(ReleaseAsset{Ref: AssetRef{App: app, Kind: AssetPage, Name: page.Name},
		ContractVersion: 1, SourceVersion: sourceVersion, Body: body})
}

func ApplicationReleaseAsset(app, sourceVersion string, application Application) (ReleaseAsset, error) {
	if err := application.CheckVariables(); err != nil {
		return ReleaseAsset{}, err
	}
	if err := application.CheckResources(); err != nil {
		return ReleaseAsset{}, err
	}
	body, err := json.Marshal(application)
	if err != nil {
		return ReleaseAsset{}, err
	}
	return WithDependencies(ReleaseAsset{Ref: AssetRef{App: app, Kind: AssetApp, Name: application.Name},
		ContractVersion: 1, SourceVersion: sourceVersion, Body: body})
}

type ReleaseCandidate struct {
	ID     string
	Bytes  []byte
	Assets []ReleaseAsset
}

func (c ReleaseCandidate) Closure(roots []AssetRef) (ReleaseCandidate, error) {
	checked, err := ReadCandidate(c.ID, c.Bytes)
	if err != nil {
		return ReleaseCandidate{}, err
	}
	return candidate(roots, checked.Assets, true)
}

type releaseBytes struct {
	Format int            `json:"format"`
	Assets []ReleaseAsset `json:"assets"`
}

func Candidate(roots []AssetRef, available []ReleaseAsset) (ReleaseCandidate, error) {
	return candidate(roots, available, false)
}

func candidate(roots []AssetRef, available []ReleaseAsset, historical bool) (ReleaseCandidate, error) {
	var empty ReleaseCandidate
	if len(roots) == 0 {
		return empty, fmt.Errorf("a release needs at least one root")
	}
	available = slices.Clone(available)
	lookup := make(map[AssetRef]ReleaseAsset, len(available))
	for i, asset := range available {
		if !historical && asset.Provides == nil {
			description, err := DescribeAsset(asset)
			if err != nil {
				return empty, err
			}
			asset.Provides = description.Provides
			available[i] = asset
		}
		if err := asset.Ref.Check(); err != nil {
			return empty, err
		}
		if _, found := lookup[asset.Ref]; found {
			return empty, fmt.Errorf("asset %s is declared twice", asset.Ref)
		}
		lookup[asset.Ref] = asset
	}
	graph, err := NewReleaseDependencyGraph(available)
	if err != nil {
		return empty, err
	}
	refs, err := graph.Reachable(roots)
	if err != nil {
		return empty, err
	}
	for _, ref := range refs {
		if ref.Kind != AssetObject {
			continue
		}
		info, err := queryObjectDescriptor(lookup[ref].Body)
		if err != nil {
			return empty, err
		}
		if info.ObjectView != nil {
			if err := checkObjectView(ref, *info.ObjectView, lookup); err != nil {
				return empty, fmt.Errorf("%s: %w", ref, err)
			}
		}
	}
	if _, err := graph.Closure(roots); err != nil {
		return empty, err
	}
	var closed []ReleaseAsset
	replacedViews := map[string]AssetRef{}
	for _, ref := range refs {
		asset := lookup[ref]
		if asset.SourceVersion == "" || asset.ContractVersion <= 0 {
			return empty, fmt.Errorf("release asset %s has no pinned source or contract", ref)
		}
		body, err := canonicalBody(asset.Body)
		if err != nil {
			return empty, fmt.Errorf("release asset %s: %w", ref, err)
		}
		if err := checkAssetDescription(asset); err != nil {
			return empty, err
		}
		if ref.Kind == AssetObservation {
			var observation ObservationDefinition
			if err := json.Unmarshal(body, &observation); err != nil {
				return empty, err
			}
			if err := CheckObservationDefinition(observation, func(typ string) (EntityInfo, bool) {
				target, exists := lookup[observation.Object]
				if typ != observation.Object.Name || !exists {
					return EntityInfo{}, false
				}
				info, err := queryObjectDescriptor(target.Body)
				return info, err == nil
			}); err != nil {
				return empty, fmt.Errorf("observation %s: %w", ref, err)
			}
			if observation.Decision != nil {
				actionAsset, exists := lookup[observation.Decision.Action]
				action, err := releaseActionDescriptor(actionAsset.Body)
				if !exists || err != nil {
					return empty, fmt.Errorf("observation %s action is unavailable", ref)
				}
				if err := CheckObservationAction(observation, action); err != nil {
					return empty, fmt.Errorf("observation %s: %w", ref, err)
				}
			}
		}
		if ref.Kind == AssetAlert {
			var alert AlertRule
			if json.Unmarshal(body, &alert) != nil || alert.Check() != nil {
				return empty, fmt.Errorf("invalid alert rule")
			}
			target, exists := lookup[alert.Object]
			info, err := queryObjectDescriptor(target.Body)
			if !exists || err != nil {
				return empty, fmt.Errorf("alert %s object is unavailable", ref)
			}
			if _, exists := info.Field(alert.Field); !exists {
				return empty, fmt.Errorf("alert %s field %s is unavailable", ref, alert.Field)
			}
		}
		if ref.Kind == AssetLinkType {
			var l LinkType
			if json.Unmarshal(body, &l) != nil || asset.ContractVersion != l.Contract() {
				return empty, fmt.Errorf("link asset has an incompatible cardinality contract")
			}
		}
		if ref.Kind == AssetObject {
			info, err := queryObjectDescriptor(body)
			if err != nil {
				return empty, err
			}
			if source := info.ExtensionSource; source != nil {
				base, exists := lookup[source.Base.Ref]
				shape, err := queryObjectDescriptor(base.Body)
				if !exists || err != nil || !slices.Contains(asset.Requires, source.Base.Ref) {
					return empty, fmt.Errorf("extension %s needs its base object in the frozen closure", ref)
				}
				if err := source.Check(info.Extends); err != nil {
					return empty, err
				}
				if shape.App != source.Base.Ref.App {
					return empty, fmt.Errorf("extension base authority differs from its frozen closure")
				}
			}
			for _, f := range info.Fields {
				if f.Property == nil {
					continue
				}
				source, ok := lookup[f.Property.Ref]
				var p PropertyType
				if !ok || source.SourceVersion != f.Property.SourceVersion || json.Unmarshal(source.Body, &p) != nil || p.CheckField(f) != nil {
					return empty, fmt.Errorf("frozen object field has an unavailable or incompatible property")
				}
			}
			for _, f := range info.Fields {
				if f.ValueType == nil {
					continue
				}
				source, ok := lookup[f.ValueType.Ref]
				var v ValueType
				if !ok || source.SourceVersion != f.ValueType.SourceVersion || json.Unmarshal(source.Body, &v) != nil || v.CheckField(f) != nil {
					return empty, fmt.Errorf("frozen object field has an unavailable or incompatible value type")
				}
			}
		}
		if ref.Kind == AssetEvaluation {
			evaluation, err := ReadEvaluationSet(body)
			if err != nil {
				return empty, err
			}
			function, ok := lookup[evaluation.Function.Ref]
			var definition AIFunction
			if !ok || function.SourceVersion != evaluation.Function.SourceVersion || json.Unmarshal(function.Body, &definition) != nil || definition.Check() != nil {
				return empty, fmt.Errorf("evaluation set needs its pinned function")
			}
			for _, test := range evaluation.Policy.Cases {
				if !definition.EvaluationInput(test.Input) || definition.ValidateOutput(test.Expected) != nil {
					return empty, fmt.Errorf("evaluation cases differ from the pinned function")
				}
			}
		}
		if ref.Kind == AssetApp {
			var application Application
			if err := json.Unmarshal(body, &application); err != nil {
				return empty, err
			}
			if err := application.CheckVariables(); err != nil {
				return empty, err
			}
			if err := application.CheckExtension(); err != nil {
				return empty, err
			}
			if application.Extends != nil {
				entry := application.Entry
				if entry == "" {
					entry = application.Extends.App
				}
				for _, replacement := range application.Replacements {
					key := entry + "/" + replacement.View
					if previous, exists := replacedViews[key]; exists {
						return empty, fmt.Errorf("application view %s/%s is replaced by both %s and %s", entry, replacement.View, previous, ref)
					}
					replacedViews[key] = ref
				}
			}
			contracts := make([]ReleaseAsset, 0, len(lookup))
			for _, asset := range lookup {
				contracts = append(contracts, asset)
			}
			if err := checkApplicationProfileClosure(ref, contracts); err != nil {
				return empty, err
			}
			if err := checkFrozenQueries(application.QueryPage(), lookup); err != nil {
				return empty, err
			}
			for _, name := range application.Pages {
				pageRef := AssetRef{App: ref.App, Kind: AssetPage, Name: name}
				target, ok := lookup[pageRef]
				var page Page
				if !ok || json.Unmarshal(target.Body, &page) != nil {
					return empty, fmt.Errorf("application page %s is unavailable", pageRef)
				}
				if err := application.CheckPageVariables(page); err != nil {
					return empty, err
				}
			}
		}
		if ref.Kind == AssetFlow || ref.Kind == AssetPage {
			var bindings []AssetBinding
			if ref.Kind == AssetFlow {
				var flow FlowReleaseDescriptor
				if err := json.Unmarshal(body, &flow); err != nil {
					return empty, err
				}
				bindings = append(slices.Clone(flow.Functions), flow.Operations...)
				bindings = append(bindings, flow.Queries...)
			} else {
				var page Page
				if err := json.Unmarshal(body, &page); err != nil {
					return empty, err
				}
				for _, trigger := range page.ActionTriggers {
					for _, event := range trigger.Events {
						for _, effect := range event.Effects {
							if effect.Kind == "action" && effect.Action != nil {
								asset, ok := lookup[effect.Action.Ref]
								action, err := releaseActionDescriptor(asset.Body)
								if !ok || err != nil || action.New == (effect.Action.RecordVariable != "") {
									return empty, fmt.Errorf("page action effect %s is unavailable", effect.Action.Ref)
								}
								for parameter := range effect.Action.Inputs {
									if !slices.ContainsFunc(action.Payload, func(f Field) bool { return f.Name == parameter }) {
										return empty, fmt.Errorf("page action effect %s has no parameter %s", effect.Action.Ref, parameter)
									}
								}
							}
						}
						if n := event.Navigation(); n != nil {
							target, ok := lookup[n.Page]
							var targetPage Page
							if !ok || json.Unmarshal(target.Body, &targetPage) != nil {
								return empty, fmt.Errorf("page navigation target %s is unavailable", n.Page)
							}
							if err := CheckPageNavigation(page, targetPage, *n); err != nil {
								return empty, err
							}
						}
					}
				}
				if err := CheckPageEmbeddingGraph(ref, lookup); err != nil {
					return empty, err
				}
				if err := checkFrozenQueries(page, lookup); err != nil {
					return empty, err
				}

				for _, c := range page.ComputeResources() {
					bindings = append(bindings, c.Operation)
				}
				safe, err := page.Safety()
				if err != nil {
					return empty, err
				}
				for _, trigger := range safe.ActionTriggers {
					if trigger.Function != nil {
						bindings = append(bindings, *trigger.Function)
					}
					if trigger.Operation != nil {
						bindings = append(bindings, *trigger.Operation)
					}
				}

			}
			seen := map[AssetRef]bool{}
			for _, binding := range bindings {
				dependency, ok := lookup[binding.Ref]
				if !ok || seen[binding.Ref] && ref.Kind == AssetFlow || binding.Ref.Kind != AssetFunction && binding.Ref.Kind != AssetCompute && !(ref.Kind == AssetFlow && binding.Ref.Kind == AssetQuery) || binding.SourceVersion == "" || dependency.SourceVersion != binding.SourceVersion {
					return empty, fmt.Errorf("%s needs exact function dependency %s at %s", ref, binding.Ref, binding.SourceVersion)
				}
				seen[binding.Ref] = true
			}
		}
		if asset.Provides != nil {
			if err := graph.CheckExecutionBindings(asset, lookup); err != nil {
				return empty, err
			}
			if err := graph.CheckReadBindings(asset); err != nil {
				return empty, err
			}
		}
		asset.Requires = graph.Requires(ref)
		if asset.Requires == nil {
			asset.Requires = []AssetRef{}
		}
		asset.Body = body
		closed = append(closed, asset)
	}
	slices.SortFunc(closed, func(a, b ReleaseAsset) int { return compareRef(a.Ref, b.Ref) })
	raw, err := json.Marshal(releaseBytes{Format: 1, Assets: closed})
	if err != nil {
		return empty, fmt.Errorf("encode release candidate: %w", err)
	}
	hash := sha256.Sum256(raw)
	return ReleaseCandidate{ID: "sha256-v1:" + hex.EncodeToString(hash[:]), Bytes: raw, Assets: closed}, nil
}

func ReadCandidate(id string, raw []byte) (ReleaseCandidate, error) {
	var wire releaseBytes
	if len(raw) == 0 || len(raw) > 16<<20 {
		return ReleaseCandidate{}, fmt.Errorf("release candidate is empty or exceeds 16 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return ReleaseCandidate{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF || wire.Format != 1 {
		return ReleaseCandidate{}, fmt.Errorf("invalid release format or trailing data")
	}
	roots := make([]AssetRef, len(wire.Assets))
	for i, asset := range wire.Assets {
		roots[i] = asset.Ref
	}
	checked, err := candidate(roots, wire.Assets, true)
	if err != nil {
		return ReleaseCandidate{}, err
	}
	if checked.ID != id || !bytes.Equal(checked.Bytes, raw) {
		return ReleaseCandidate{}, fmt.Errorf("release candidate identity or canonical bytes differ")
	}
	return checked, nil
}
