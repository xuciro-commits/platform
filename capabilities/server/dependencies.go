package platformserver

import releaseowner "platformserver/studio/releases"
import "platformserver/studio/authoring"

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type DependencyView struct {
	Descriptors []platform.Dependencies `json:"descriptors"`
	Requires    []platform.AssetRef     `json:"requires"`
	UsedBy      []platform.AssetRef     `json:"usedBy"`
}

type ProvidedCapability struct {
	Ref        platform.AssetRef   `json:"ref"`
	Version    string              `json:"version"`
	Capability platform.Capability `json:"capability"`
}

func (t *Tenant) definitionAsset(def platform.Definition) (platform.ReleaseAsset, error) {
	var body any
	switch {
	case def.Entity != nil:
		body = struct {
			Type   string              `json:"type"`
			Entity platform.EntityInfo `json:"entity"`
		}{def.Ref.Name, *def.Entity}
		if def.Source == "code" {
			if owner := t.app(def.Ref.App); owner != nil {
				for _, entity := range owner.Manifest().Entities {
					if entity.Type == def.Ref.Name {
						body = releaseowner.CodeObjectDescriptor(entity, *def.Entity)
						break
					}
				}
			}
		}
	case def.Action != nil:
		body = releaseowner.CodeActionDescriptor(*def.Action)
	case def.Page != nil:
		body = def.Page
	case def.Application != nil:
		body = def.Application
	case def.Query != nil:
		body = def.Query
	case def.Function != nil:
		body = def.Function
	case def.Operation != nil:
		body = def.Operation
	case def.Agent != nil:
		body = def.Agent
	case def.PropertyType != nil:
		body = def.PropertyType
	case def.ValueType != nil:
		body = def.ValueType
	case def.LinkType != nil:
		body = def.LinkType
	case def.SampleSet != nil:
		body = def.SampleSet
	case def.Interface != nil:
		body = def.Interface
	case def.Protocol != nil:
		body = def.Protocol
	case def.Observation != nil:
		body = def.Observation
	default:
		return platform.ReleaseAsset{}, fmt.Errorf("asset %s has no typed definition body", def.Ref)
	}
	raw, err := json.Marshal(body)
	return platform.ReleaseAsset{Ref: def.Ref, SourceVersion: def.Version, ContractVersion: def.ContractVersion, Body: raw}, err
}

func (t *Tenant) describeOwnedAsset(asset platform.ReleaseAsset) (platform.ReleaseAsset, error) {
	if !slices.Contains([]platform.AssetKind{platform.AssetObject, platform.AssetAction, platform.AssetAgent, platform.AssetQuery, platform.AssetFunction, platform.AssetCompute}, asset.Ref.Kind) {
		return platform.WithDependencies(asset)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(asset.Body, &body); err != nil {
		return asset, err
	}
	bindings := map[string]platform.AssetRef{}
	var collect func(any)
	collect = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, item := range value {
				if key == "ref" || key == "target" || key == "object" || key == "extends" {
					if name, ok := item.(string); ok && name != "" {
						if info, exists := t.entity(name); exists {
							prefix, _, _ := strings.Cut(name, ".")
							if prefix != info.App {
								bindings[name] = platform.AssetRef{App: info.App, Kind: platform.AssetObject, Name: name}
							}
						} else if name == "enterprise.element" || key == "target" && t.authorityOf(name) != "" {
							bindings[name] = platform.AssetRef{}
						}
					}
				}
				if key == "implementations" {
					if types, ok := item.([]any); ok {
						for _, value := range types {
							if name, ok := value.(string); ok {
								if info, exists := t.entity(name); exists && strings.SplitN(name, ".", 2)[0] != info.App {
									bindings[name] = platform.AssetRef{App: info.App, Kind: platform.AssetObject, Name: name}
								}
							}
						}
					}
				}
				collect(item)
			}
		case []any:
			for _, item := range value {
				collect(item)
			}
		}
	}
	var value any
	if err := json.Unmarshal(asset.Body, &value); err != nil {
		return asset, err
	}
	collect(value)
	if len(bindings) > 0 {
		encoded, err := json.Marshal(bindings)
		if err != nil {
			return asset, err
		}
		body["objectBindings"] = encoded
		asset.Body, err = json.Marshal(body)
		if err != nil {
			return asset, err
		}
	}
	protocols := map[string]platform.AssetRef{}
	var uses []string
	if asset.Ref.Kind == platform.AssetAction {
		var declaration struct {
			Action platform.Action `json:"action"`
		}
		if err := json.Unmarshal(asset.Body, &declaration); err != nil {
			return asset, err
		}
		uses = declaration.Action.Uses
	}
	if asset.Ref.Kind == platform.AssetAgent {
		var declaration platform.AgentReleaseDescriptor
		if err := json.Unmarshal(asset.Body, &declaration); err != nil {
			return asset, err
		}
		toolBindings := map[string]platform.AssetRef{}
		for _, tool := range declaration.Tools {
			if protocol, _, ok := strings.Cut(tool, "#"); ok {
				uses = append(uses, protocol)
				continue
			}
			if name, ok := strings.CutPrefix(tool, "query:"); ok {
				toolBindings[tool] = platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetQuery, Name: name}
			} else if owner := t.app(asset.Ref.App); owner != nil {
				if _, ok := owner.Manifest().Actions.Action(tool); ok {
					toolBindings[tool] = platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetAction, Name: tool}
				}
			}
		}
		if len(toolBindings) > 0 {
			encoded, err := json.Marshal(toolBindings)
			if err != nil {
				return asset, err
			}
			body["toolBindings"] = encoded
		}
	}
	for _, use := range uses {
		protocol, _, _ := strings.Cut(use, "#")
		name, _, _ := strings.Cut(protocol, "/")
		ref := platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetProtocol, Name: name}
		if provider, ok := t.resolve(protocol); ok {
			ref.App = provider.provider.Manifest().ID
		}
		protocols[protocol] = ref
	}
	if len(protocols) > 0 {
		encoded, err := json.Marshal(protocols)
		if err != nil {
			return asset, err
		}
		body["protocolBindings"] = encoded
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return asset, err
	}
	asset.Body = raw
	return platform.WithDependencies(asset)
}

func (t *Tenant) describeDefinition(def platform.Definition) (platform.Definition, error) {
	asset, err := t.definitionAsset(def)
	if err != nil {
		return def, err
	}
	asset, err = t.describeOwnedAsset(asset)
	if err != nil {
		return def, err
	}
	def.Requires, def.Provides = asset.Requires, asset.Provides
	return def, nil
}

func (t *Tenant) visibleProvides(def platform.Definition, m platform.Member, entities map[string]platform.EntityInfo) []platform.Capability {
	provided := def.Provides
	if def.Action != nil || def.Query != nil || def.Function != nil || def.Operation != nil || def.Entity != nil || def.Protocol != nil || def.Observation != nil {
		if projected, err := t.describeDefinition(def); err == nil {
			provided = projected.Provides
		} else {
			return []platform.Capability{}
		}
	}
	out := []platform.Capability{}
	for _, capability := range provided {
		if len(capability.Roles) > 0 && !m.May(def.Ref.App, capability.Name, capability.Roles).Allow {
			continue
		}
		if capability.Kind == "query" && def.Query != nil {
			input, output := authoring.QueryCapabilitySchemas(*def.Query, entities[def.Query.Object])
			if input != nil {
				capability.Inputs = *input
			}
			if output != nil {
				capability.Outputs = *output
			}
		}
		out = append(out, capability)
	}
	return out
}

func dependencyDirections(descriptors []platform.Dependencies, focus platform.AssetRef) ([]platform.AssetRef, []platform.AssetRef, error) {
	graph, err := platform.NewDependencyGraph(descriptors)
	if err != nil {
		return nil, nil, err
	}
	requires, err := graph.Reachable([]platform.AssetRef{focus})
	if err != nil {
		return nil, nil, err
	}
	usedBy, err := graph.Impact([]platform.AssetRef{focus})
	if err != nil {
		return nil, nil, err
	}
	return slices.DeleteFunc(requires, func(ref platform.AssetRef) bool { return ref == focus }), slices.DeleteFunc(usedBy, func(ref platform.AssetRef) bool { return ref == focus }), nil
}

func (t *Tenant) DependencyView(m platform.Member, focus platform.AssetRef, app string) (DependencyView, *kernel.Error) {
	view := DependencyView{Descriptors: []platform.Dependencies{}, Requires: []platform.AssetRef{}, UsedBy: []platform.AssetRef{}}
	if err := t.admits(m); err != nil {
		return view, err
	}
	definitions, err := t.definitionsFor(m)
	if err != nil {
		return view, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Asset dependencies are unavailable")
	}
	for _, def := range definitions {
		view.Descriptors = append(view.Descriptors, platform.Dependencies{Ref: def.Ref, Version: def.Version, Requires: def.Requires, Provides: def.Provides})
	}
	if focus != (platform.AssetRef{}) {
		if err := focus.Check(); err != nil {
			return view, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a valid dependency asset")
		}
		if !slices.ContainsFunc(view.Descriptors, func(d platform.Dependencies) bool { return d.Ref == focus }) {
			return DependencyView{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Asset dependencies are unavailable to this member")
		}
		view.Requires, view.UsedBy, err = dependencyDirections(view.Descriptors, focus)
		if err != nil {
			return view, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Asset dependencies are unavailable")
		}
		related := map[platform.AssetRef]bool{focus: true}
		for _, ref := range append(slices.Clone(view.Requires), view.UsedBy...) {
			related[ref] = true
		}
		view.Descriptors = slices.DeleteFunc(view.Descriptors, func(d platform.Dependencies) bool { return !related[d.Ref] })
	} else if app != "" {
		view.Descriptors = slices.DeleteFunc(view.Descriptors, func(d platform.Dependencies) bool { return d.Ref.App != app })
	}
	return view, nil
}

func (t *Tenant) ProvidedCapabilities(m platform.Member, kind, side string) ([]ProvidedCapability, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return nil, err
	}
	if kind != "" && !slices.Contains([]string{"action", "query", "function", "operation", "protocol", "interface"}, kind) ||
		side != "" && !slices.Contains([]string{"pure", "effect", "authoritative"}, side) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a valid capability kind and side")
	}
	definitions, err := t.definitionsFor(m)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Asset capabilities are unavailable")
	}
	out := []ProvidedCapability{}
	for _, definition := range definitions {
		for _, capability := range definition.Provides {
			if kind != "" && capability.Kind != kind || side != "" && capability.Side != side {
				continue
			}
			out = append(out, ProvidedCapability{Ref: definition.Ref, Version: definition.Version, Capability: capability})
		}
	}
	slices.SortFunc(out, func(a, b ProvidedCapability) int {
		return strings.Compare(a.Ref.String()+"#"+a.Capability.Name, b.Ref.String()+"#"+b.Capability.Name)
	})
	return out, nil
}
