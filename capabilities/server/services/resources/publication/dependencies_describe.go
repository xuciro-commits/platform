package publication

import (
	"bytes"
	"encoding/json"
	"fmt"
	runtimeprotocol "platformserver/kernel/protocol"
	"slices"
	"strings"

	"platformserver/platform"
)

func (resolver Resolver) describeAsset(asset platform.ReleaseAsset, historical bool) (platform.Dependencies, error) {
	out := platform.Dependencies{Ref: asset.Ref, Requires: []platform.AssetRef{}, Provides: []platform.Capability{}, Version: asset.SourceVersion}
	if err := asset.Ref.Check(); err != nil {
		return out, err
	}
	if _, err := CanonicalAssetBody(asset.Body); err != nil {
		return out, fmt.Errorf("%s: %w", asset.Ref, err)
	}
	var identity struct{ Name, Type, Schema string }
	if err := json.Unmarshal(asset.Body, &identity); err != nil {
		return out, err
	}
	name := identity.Name
	if asset.Ref.Kind == platform.AssetObject {
		name = identity.Type
	}
	if asset.Ref.Kind == platform.AssetAction {
		name = identity.Schema
	}
	if name != asset.Ref.Name {
		return out, fmt.Errorf("release asset %s descriptor identity differs", asset.Ref)
	}
	var bindings struct {
		Objects   map[string]platform.AssetRef `json:"objectBindings"`
		Protocols map[string]platform.AssetRef `json:"protocolBindings"`
	}
	if err := json.Unmarshal(asset.Body, &bindings); err != nil {
		return out, err
	}
	object := func(name string) platform.AssetRef {
		if ref, ok := bindings.Objects[name]; ok {
			return ref
		}
		app, ok := platform.OwnerOfType(name), strings.Contains(name, ".")
		if !ok {
			app = asset.Ref.App
		}
		return platform.AssetRef{App: app, Kind: platform.AssetObject, Name: name}
	}
	add := func(ref platform.AssetRef) {
		if ref.Name != "" {
			out.Requires = append(out.Requires, ref)
		}
	}
	addObject := func(name string) {
		if name == "enterprise.element" {
			return
		}
		if asset.Ref.Kind == platform.AssetObject && name == asset.Ref.Name {
			return
		}
		if name != "" {
			add(object(name))
		}
	}
	readSpec := func(spec *platform.RuleReadSpec) {
		if spec == nil {
			return
		}
		for _, collection := range spec.Collections {
			if collection.Query != "" {
				add(platform.AssetRef{App: collection.App, Kind: platform.AssetQuery, Name: collection.Query})
			}
			if collection.Object != "" && collection.Object != asset.Ref.Name {
				addObject(collection.Object)
			}
		}
	}
	capability := func(kind, name, side string, roles []string, input, output platform.ValueSchema, fields []platform.Field) {
		out.Provides = append(out.Provides, platform.Capability{Kind: kind, Name: name, Side: side, Roles: slices.Clone(roles), Inputs: input, Outputs: output, Parameters: fields})
	}
	switch asset.Ref.Kind {
	case platform.AssetPage:
		var p platform.Page
		if err := json.Unmarshal(asset.Body, &p); err != nil {
			return out, err
		}
		if err := resolver.CheckPage(p); err != nil {
			return out, err
		}
		out.Requires = append(out.Requires, resolver.PageDependencies(p)...)
	case platform.AssetApp:
		var app platform.Application
		decoder := json.NewDecoder(bytes.NewReader(asset.Body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&app); err != nil {
			return out, err
		}
		if err := resolver.CheckApplicationProfile(app.Profile); err != nil {
			return out, err
		}
		if err := resolver.CheckApplicationVariables(app); err != nil {
			return out, err
		}
		if err := resolver.CheckApplicationResources(app); err != nil {
			return out, err
		}
		out.Requires = append(out.Requires, resolver.ApplicationDependencies(app, asset.Ref.App)...)
	case platform.AssetObject:
		requires, provides, err := resolver.ObjectDependencies(asset, historical)
		if err != nil {
			return out, err
		}
		out.Requires = append(out.Requires, requires...)
		out.Provides = append(out.Provides, provides...)
	case platform.AssetAction:
		if _, err := resolver.ActionDescriptor(asset.Body); err != nil {
			return out, err
		}
		requires, provides, err := resolver.ActionDependencies(asset, historical)
		if err != nil {
			return out, err
		}
		out.Requires = append(out.Requires, requires...)
		out.Provides = append(out.Provides, provides...)
	case platform.AssetObservation:
		var observation platform.ObservationDefinition
		if err := json.Unmarshal(asset.Body, &observation); err != nil {
			return out, err
		}
		if err := observation.Check(); err != nil {
			return out, err
		}
		add(observation.Connection)
		add(observation.Object)
		modes := []string{"summary", "points"}
		if observation.Intervals != nil {
			modes = append(modes, "intervals")
		}
		capability("query", observation.Name, platform.CapabilityPure, observation.Roles, platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"record": {Type: "string"}, "since": {Type: "string"}, "until": {Type: "string"}, "mode": {Type: "string", Enum: modes}, "limit": {Type: "integer"}}, Required: []string{"record", "since", "until", "mode"}}, platform.ValueSchema{}, nil)
		if observation.Decision != nil {
			add(observation.Decision.Action)
			capability("operation", observation.Name+".evaluate", platform.CapabilityAuthoritative, observation.Roles, platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"record": {Type: "string"}, "until": {Type: "string"}, "key": {Type: "string"}}, Required: []string{"record", "until", "key"}}, platform.ValueSchema{}, nil)
		}
	case platform.AssetQuery:
		var query platform.NamedQuery
		if err := json.Unmarshal(asset.Body, &query); err != nil {
			return out, err
		}
		if err := query.Check(); err != nil {
			return out, err
		}
		for _, dep := range query.Dependencies() {
			if !historical && dep.Kind == platform.AssetObject {
				dep = object(dep.Name)
			}
			add(dep)
		}
		if !historical && query.Interface != "" {
			owner, _, _ := strings.Cut(query.Interface, ".")
			add(platform.AssetRef{App: owner, Kind: platform.AssetInterface, Name: query.Interface})
		}
		input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
		if query.By != "" {
			input.Properties["for"] = platform.ValueSchema{Type: "string"}
			input.Required = []string{"for"}
		}
		capability("query", query.Name, platform.CapabilityPure, nil, input, platform.ValueSchema{}, nil)
	case platform.AssetLinkType:
		var link platform.LinkType
		if err := json.Unmarshal(asset.Body, &link); err != nil {
			return out, err
		}
		if err := link.Check(); err != nil {
			return out, err
		}
		if link.Child.App != asset.Ref.App {
			return out, fmt.Errorf("link type must be declared by its child owner")
		}
		add(link.Parent)
		add(link.Child)
	case platform.AssetObjectTypeGroup:
		var group platform.ObjectTypeGroup
		if err := json.Unmarshal(asset.Body, &group); err != nil {
			return out, err
		}
		if err := group.Check(); err != nil {
			return out, err
		}
		out.Requires = append(out.Requires, group.Members...)
	case platform.AssetPropertyType:
		var property platform.PropertyType
		if err := json.Unmarshal(asset.Body, &property); err != nil {
			return out, err
		}
		if err := property.Check(); err != nil {
			return out, err
		}
	case platform.AssetValueType:
		var value platform.ValueType
		if err := json.Unmarshal(asset.Body, &value); err != nil {
			return out, err
		}
		if err := value.Check(); err != nil {
			return out, err
		}
	case platform.AssetFunction:
		var f platform.AIFunction
		if err := json.Unmarshal(asset.Body, &f); err != nil {
			return out, err
		}
		if err := f.Check(); err != nil {
			return out, err
		}
		if historical {
			add(platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetObject, Name: f.Object})
		} else {
			addObject(f.Object)
		}
		capability("function", f.Name, platform.CapabilityEffect, f.Roles, platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"source": {Type: "string"}}, Required: []string{"source"}}, runtimeprotocol.CapabilityFieldsSchema(f.Output), nil)
	case platform.AssetCompute:
		var operation platform.Operation
		if err := json.Unmarshal(asset.Body, &operation); err != nil {
			return out, err
		}
		if err := operation.Check(); err != nil {
			return out, err
		}
		if !historical && operation.Rules != nil {
			addObject(operation.Rules.Reads.Object)
			readSpec(&operation.Rules.Reads)
		}
		capability("operation", operation.Name, platform.CapabilityPure, operation.Roles, operation.Input, operation.Output, nil)
	case platform.AssetFlow:
		var flow runtimeprotocol.FlowReleaseDescriptor
		if err := json.Unmarshal(asset.Body, &flow); err != nil {
			return out, err
		}
		if flow.Subject.Name != "" && flow.Subject.Kind != platform.AssetObject || !json.Valid(flow.Definition) || len(flow.Definition) == 0 || flow.Definition[0] != '{' {
			return out, fmt.Errorf("release flow %s needs an optional subject and complete definition", asset.Ref)
		}
		add(flow.Subject)
		for _, binding := range append(append(slices.Clone(flow.Functions), flow.Operations...), flow.Queries...) {
			add(binding.Ref)
		}
		for _, ref := range flow.Actions {
			if ref.Kind != platform.AssetAction {
				return out, fmt.Errorf("flow %s binds a non-action", asset.Ref)
			}
			add(ref)
		}
		for _, ref := range flow.Dependencies {
			add(ref)
		}
		capability("operation", flow.Name, platform.CapabilityAuthoritative, nil, platform.ValueSchema{}, platform.ValueSchema{}, nil)
	case platform.AssetAgent:
		var agent platform.AgentReleaseDescriptor
		if err := json.Unmarshal(asset.Body, &agent); err != nil {
			return out, err
		}
		if err := resolver.CheckAgent(agent); err != nil {
			return out, err
		}
		if historical {
			for _, ref := range resolver.AgentDependencies(agent, asset.Ref.App) {
				add(ref)
			}
		} else {
			for _, tool := range agent.Tools {
				if protocol, _, ok := strings.Cut(tool, "#"); ok {
					ref, found := bindings.Protocols[protocol]
					if !found || ref.Kind != platform.AssetProtocol || ref.Check() != nil {
						return out, fmt.Errorf("agent %s tool %s needs its declared provider protocol binding", asset.Ref, tool)
					}
					add(ref)
					continue
				}
				if strings.HasPrefix(tool, "read:") || strings.HasPrefix(tool, "emit:") {
					if !agent.Native {
						return out, fmt.Errorf("agent %s tool %s is not a published native declaration", asset.Ref, tool)
					}
					continue
				}
				query, queryTool := strings.CutPrefix(tool, "query:")
				if ref, fixed := agent.ToolBindings[tool]; fixed {
					kind := platform.AssetAction
					name := tool
					if queryTool {
						kind = platform.AssetQuery
						name = query
					}
					if ref.Kind != kind || ref.App != asset.Ref.App || ref.Name != name {
						return out, fmt.Errorf("agent %s tool %s binding differs from its original declaration", asset.Ref, tool)
					}
					add(ref)
					continue
				}
				if agent.Native {
					return out, fmt.Errorf("agent %s tool %s needs a fixed declared asset binding", asset.Ref, tool)
				}
				if queryTool && query != "" {
					add(platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetQuery, Name: query})
					continue
				}
				if platform.OwnerOfType(tool) != asset.Ref.App || strings.ContainsAny(tool, "#:") {
					return out, fmt.Errorf("agent %s tool %s is not a declared local action", asset.Ref, tool)
				}
				add(platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetAction, Name: tool})
			}
		}
		capability("operation", agent.Name, platform.CapabilityEffect, nil, platform.ValueSchema{}, platform.ValueSchema{}, nil)
	case platform.AssetEvaluation:
		evaluation, err := runtimeprotocol.ReadEvaluationSet(asset.Body)
		if err != nil {
			return out, err
		}
		add(evaluation.Function.Ref)
	case platform.AssetTestPlan:
		plan, err := runtimeprotocol.ReadTestPlan(asset.Body)
		if err != nil {
			return out, err
		}
		for _, root := range plan.Dependencies() {
			add(root)
		}
	case platform.AssetSampleSet:
		var samples platform.SampleSet
		if err := json.Unmarshal(asset.Body, &samples); err != nil {
			return out, err
		}
		if err := samples.Check(); err != nil {
			return out, err
		}
		out.Requires = append(out.Requires, samples.Dependencies()...)
	case platform.AssetInterface:
		var shape platform.Interface
		if err := json.Unmarshal(asset.Body, &shape); err != nil {
			return out, err
		}
		if err := shape.Check(asset.Ref.App); err != nil {
			return out, err
		}
		fields := make([]platform.Field, 0, len(shape.Fields))
		for _, field := range shape.Fields {
			fields = append(fields, platform.Field{Name: field.Name, Type: field.Type, Description: field.Title})
		}
		capability("interface", shape.Name, platform.CapabilityPure, nil, platform.ValueSchema{}, runtimeprotocol.CapabilityFieldsSchema(fields), fields)
	case platform.AssetProtocol:
		dependencies, err := describeProtocolDependencies(asset)
		if err != nil {
			return out, err
		}
		out.Requires = append(out.Requires, dependencies.Requires...)
		out.Provides = append(out.Provides, dependencies.Provides...)
	case platform.AssetMediaSet, platform.AssetSchedule, platform.AssetListener, platform.AssetConnection, platform.AssetDataset, platform.AssetSource, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching:
		dependencies, err := describeIntegrationDependencies(asset, historical, object)
		if err != nil {
			return out, err
		}
		out.Requires = append(out.Requires, dependencies.Requires...)
		out.Provides = append(out.Provides, dependencies.Provides...)
	}
	return completeAssetDependencies(asset, out)
}
