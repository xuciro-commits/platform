package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type AgentReleaseDescriptor struct {
	Name         string              `json:"name"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	Model        string              `json:"model,omitempty"`
	ModelOptions ModelOptions        `json:"modelOptions,omitzero"`
	Instructions string              `json:"instructions"`
	Tools        []string            `json:"tools"`
	ToolBindings map[string]AssetRef `json:"toolBindings,omitempty"`
	Budget       Budget              `json:"budget"`
	Checkpoints  []string            `json:"checkpoints,omitempty"`
	HandoffRole  string              `json:"handoffRole,omitempty"`
	Cases        json.RawMessage     `json:"cases,omitempty"`
	Native       bool                `json:"native,omitempty"`
	Guard        bool                `json:"guard,omitempty"`
	Handoff      bool                `json:"handoff,omitempty"`
}

func (a AgentReleaseDescriptor) Check() error {
	if !functionName.MatchString(a.Name) || a.Title == "" || strings.TrimSpace(a.Instructions) == "" || len(a.Instructions) > 65536 || len(a.Tools) > 1000 || a.Budget.Steps < 0 || a.Budget.Tokens < 0 || a.Budget.Actions < 0 || a.Budget.Cost < 0 || len(a.Cases) > 262144 {
		return fmt.Errorf("agent needs a valid identity, instructions and bounded tools/budget")
	}
	if len(a.Cases) > 0 && (!json.Valid(a.Cases) || a.Cases[0] != '[') {
		return fmt.Errorf("agent evaluation cases need an array")
	}
	for tool, ref := range a.ToolBindings {
		if !slices.Contains(a.Tools, tool) || ref.Check() != nil {
			return fmt.Errorf("agent tool binding must retain an original declared tool and asset")
		}
	}
	for _, tool := range a.Checkpoints {
		if !slices.Contains(a.Tools, tool) {
			return fmt.Errorf("agent checkpoint is not a declared tool")
		}
	}
	return nil
}

func (a AgentReleaseDescriptor) Dependencies(owner string) []AssetRef {
	refs := []AssetRef{}
	for _, tool := range a.Tools {
		if ref, ok := a.ToolBindings[tool]; ok {
			refs = append(refs, ref)
			continue
		}
		if a.Native && !strings.Contains(tool, ".") && !strings.HasPrefix(tool, "query:") {
			continue
		}
		if name, ok := strings.CutPrefix(tool, "query:"); ok {
			refs = append(refs, AssetRef{App: owner, Kind: AssetQuery, Name: name})
			continue
		}
		if strings.ContainsAny(tool, "#:") {
			continue
		}
		refs = append(refs, AssetRef{App: owner, Kind: AssetAction, Name: tool})
	}
	slices.SortFunc(refs, compareRef)
	return slices.Compact(refs)
}

const (
	CapabilityPure          = "pure"
	CapabilityEffect        = "effect"
	CapabilityAuthoritative = "authoritative"
)

type Dependencies struct {
	Ref      AssetRef     `json:"ref"`
	Requires []AssetRef   `json:"requires"`
	Provides []Capability `json:"provides"`
	Version  string       `json:"version"`
}

type Capability struct {
	Kind       string      `json:"kind"`
	Name       string      `json:"name"`
	Inputs     ValueSchema `json:"inputs"`
	Outputs    ValueSchema `json:"outputs"`
	Roles      []string    `json:"roles"`
	Side       string      `json:"side"`
	Parameters []Field     `json:"parameters,omitempty"`
}

func (c Capability) Check() error {
	if !slices.Contains([]string{"action", "query", "function", "operation", "protocol", "interface"}, c.Kind) || c.Name == "" || !slices.Contains([]string{CapabilityPure, CapabilityEffect, CapabilityAuthoritative}, c.Side) {
		return fmt.Errorf("capability %q needs a supported kind, stable name and side", c.Name)
	}
	for _, schema := range []ValueSchema{c.Inputs, c.Outputs} {
		if schema.Type != "" {
			if err := schema.Check(); err != nil {
				return fmt.Errorf("capability %s: %w", c.Name, err)
			}
		}
	}
	return nil
}

type ProtocolReleaseDescriptor struct {
	Name           string              `json:"name"`
	Version        int                 `json:"version"`
	Actions        []Action            `json:"actions"`
	Reads          []string            `json:"reads"`
	ActionBindings map[string]AssetRef `json:"actionBindings,omitempty"`
	ActionRoles    map[string][]string `json:"actionRoles,omitempty"`
	ReadBindings   map[string]AssetRef `json:"readBindings,omitempty"`
}

func (a ReleaseAsset) MarshalJSON() ([]byte, error) {
	type wire ReleaseAsset
	var provides *[]Capability
	if a.Provides != nil {
		provides = &a.Provides
	}
	return json.Marshal(struct {
		wire
		Provides *[]Capability `json:"provides,omitempty"`
	}{wire: wire(a), Provides: provides})
}

func WithDependencies(asset ReleaseAsset) (ReleaseAsset, error) {
	description, err := DescribeAsset(asset)
	if err != nil {
		return ReleaseAsset{}, err
	}
	asset.Requires, asset.Provides = description.Requires, description.Provides
	return asset, nil
}

func DescribeAsset(asset ReleaseAsset) (Dependencies, error) {
	return describeAsset(asset, false)
}

func describeAsset(asset ReleaseAsset, historical bool) (Dependencies, error) {
	out := Dependencies{Ref: asset.Ref, Requires: []AssetRef{}, Provides: []Capability{}, Version: asset.SourceVersion}
	if err := asset.Ref.Check(); err != nil {
		return out, err
	}
	if _, err := canonicalBody(asset.Body); err != nil {
		return out, fmt.Errorf("%s: %w", asset.Ref, err)
	}
	var identity struct{ Name, Type, Schema string }
	if err := json.Unmarshal(asset.Body, &identity); err != nil {
		return out, err
	}
	name := identity.Name
	if asset.Ref.Kind == AssetObject {
		name = identity.Type
	}
	if asset.Ref.Kind == AssetAction {
		name = identity.Schema
	}
	if name != asset.Ref.Name {
		return out, fmt.Errorf("release asset %s descriptor identity differs", asset.Ref)
	}
	var bindings struct {
		Objects   map[string]AssetRef `json:"objectBindings"`
		Protocols map[string]AssetRef `json:"protocolBindings"`
	}
	if err := json.Unmarshal(asset.Body, &bindings); err != nil {
		return out, err
	}
	object := func(name string) AssetRef {
		if ref, ok := bindings.Objects[name]; ok {
			return ref
		}
		app, _, ok := strings.Cut(name, ".")
		if !ok {
			app = asset.Ref.App
		}
		return AssetRef{App: app, Kind: AssetObject, Name: name}
	}
	add := func(ref AssetRef) {
		if ref.Name != "" {
			out.Requires = append(out.Requires, ref)
		}
	}
	addObject := func(name string) {
		if name == "enterprise.element" {
			return
		}
		if asset.Ref.Kind == AssetObject && name == asset.Ref.Name {
			return
		}
		if name != "" {
			add(object(name))
		}
	}
	readSpec := func(spec *RuleReadSpec) {
		if spec == nil {
			return
		}
		for _, collection := range spec.Collections {
			if collection.Query != "" {
				add(AssetRef{App: collection.App, Kind: AssetQuery, Name: collection.Query})
			}
			if collection.Object != "" && collection.Object != asset.Ref.Name {
				addObject(collection.Object)
			}
		}
	}
	capability := func(kind, name, side string, roles []string, input, output ValueSchema, fields []Field) {
		out.Provides = append(out.Provides, Capability{Kind: kind, Name: name, Side: side, Roles: slices.Clone(roles), Inputs: input, Outputs: output, Parameters: fields})
	}
	switch asset.Ref.Kind {
	case AssetPage:
		var p Page
		if err := json.Unmarshal(asset.Body, &p); err != nil {
			return out, err
		}
		if err := p.CheckSafety(); err != nil {
			return out, err
		}
		out.Requires = append(out.Requires, pageDependencies(p)...)
	case AssetApp:
		var app Application
		decoder := json.NewDecoder(bytes.NewReader(asset.Body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&app); err != nil {
			return out, err
		}
		if err := app.CheckVariables(); err != nil {
			return out, err
		}
		if err := app.CheckResources(); err != nil {
			return out, err
		}
		out.Requires = append(out.Requires, app.Dependencies(asset.Ref.App)...)
	case AssetObject:
		info, err := queryObjectDescriptor(asset.Body)
		if err != nil {
			return out, err
		}
		for _, field := range info.Fields {
			if field.Property != nil {
				add(field.Property.Ref)
			}
			if !historical && field.Ref != "" && field.Ref != "enterprise.element" {
				add(object(field.Ref))
			}
		}
		if historical {
			break
		}
		capability("interface", asset.Ref.Name, CapabilityPure, nil, ValueSchema{}, CapabilityEntitySchema(info), nil)
		for _, name := range info.Implements {
			owner, _, _ := strings.Cut(name, ".")
			add(AssetRef{App: owner, Kind: AssetInterface, Name: name})
		}
		if info.Extends != "" {
			addObject(info.Extends)
		}
		if info.ExtensionSource != nil {
			add(info.ExtensionSource.Base.Ref)
		}
		var declaration struct {
			Creation    *BusinessTxn  `json:"createTransaction"`
			Reads       *RuleReadSpec `json:"reads"`
			Constraints []Constraint  `json:"constraints"`
			Sets        []RecordSet   `json:"sets"`
			Actions     []struct {
				Name        string            `json:"name"`
				Reads       *RuleReadSpec     `json:"reads"`
				Transaction *BusinessTxn      `json:"transaction"`
				Adoption    *ProposalAdoption `json:"adoption"`
				Creates     []struct {
					Object string `json:"object"`
				} `json:"creates"`
				Posts []struct {
					Object string `json:"object"`
				} `json:"posts"`
			} `json:"actions"`
		}
		if err := json.Unmarshal(asset.Body, &declaration); err != nil {
			return out, err
		}
		readSpec(declaration.Reads)
		if declaration.Creation != nil {
			var visit func(TxnStep)
			visit = func(step TxnStep) {
				if step.Target.Type != "" {
					addObject(step.Target.Type)
				}
				if step.Call != nil {
					add(step.Call.Ref)
				}
				if step.Link != nil {
					add(step.Link.Binding.Ref)
				}
				if step.Intervals != nil {
					add(step.Intervals.Predecessors.Ref)
				}
				if step.ForEach != nil {
					for _, nested := range step.ForEach.Steps {
						visit(nested)
					}
				}
			}
			for _, step := range declaration.Creation.Steps {
				visit(step)
			}
		}
		constraints := append(slices.Clone(info.Constraints), declaration.Constraints...)
		for _, constraint := range constraints {
			readSpec(constraint.Reads)
		}
		for _, set := range append(slices.Clone(info.Sets), declaration.Sets...) {
			addObject(set.Object)
			if set.Binding != nil {
				add(set.Binding.Ref)
			}
		}
		for _, action := range declaration.Actions {
			readSpec(action.Reads)
			for _, created := range action.Creates {
				addObject(created.Object)
			}
			for _, posted := range action.Posts {
				addObject(posted.Object)
			}
			if action.Adoption != nil {
				ref := action.Adoption.Function
				owner := ref.App
				if owner == "" {
					owner = asset.Ref.App
				}
				add(AssetRef{App: owner, Kind: AssetFunction, Name: ref.Name})
			}
			if txn := action.Transaction; txn != nil {
				readSpec(txn.Reads)
				var refusal func([]Refusal)
				var step func(TxnStep)
				refusal = func(rules []Refusal) {
					for _, r := range rules {
						if r.Record != nil {
							step(*r.Record)
						}
						if r.Route != nil {
							addObject(r.Route.Type)
							add(AssetRef{App: asset.Ref.App, Kind: AssetAction, Name: r.Route.Type + "." + r.Route.Action})
						}
					}
				}
				step = func(s TxnStep) {
					if s.Call != nil {
						add(s.Call.Ref)
					}
					if s.Target.Type != "" && s.Target.Type != asset.Ref.Name {
						addObject(s.Target.Type)
					}
					if s.Link != nil {
						add(s.Link.Binding.Ref)
					}
					if s.Intervals != nil {
						add(s.Intervals.Predecessors.Ref)
					}
					refusal(s.Refusals)
					if s.ForEach != nil {
						for _, nested := range s.ForEach.Steps {
							step(nested)
						}
					}
				}
				for _, s := range txn.Steps {
					step(s)
				}
				refusal(txn.Refusals)

			}
		}
	case AssetAction:
		action, err := releaseActionDescriptor(asset.Body)
		if err != nil {
			return out, err
		}
		if historical {
			break
		}
		addObject(action.Target)
		for _, field := range action.Payload {
			addObject(field.Ref)
		}
		for _, use := range action.Uses {
			protocol, _, _ := strings.Cut(use, "#")
			if ref, ok := bindings.Protocols[protocol]; ok {
				add(ref)
			}
		}
		var owner struct {
			Roles []string `json:"roles"`
		}
		_ = json.Unmarshal(asset.Body, &owner)
		if owner.Roles != nil {
			action.Roles = owner.Roles
		}
		var confirmation struct {
			Bindings *struct {
				Parties []struct {
					Name                string    `json:"name"`
					Writeback           *AssetRef `json:"writeback"`
					CompensateWriteback *AssetRef `json:"compensateWriteback"`
					Source              *AssetRef `json:"source"`
					Protocol            *AssetRef `json:"protocol"`
					Action              *AssetRef `json:"action"`
					CompensateAction    *AssetRef `json:"compensateAction"`
				} `json:"parties"`
				OnComplete      AssetRef  `json:"onComplete"`
				OnPartialAction *AssetRef `json:"onPartialAction"`
			} `json:"confirmationBindings"`
		}
		if err := json.Unmarshal(asset.Body, &confirmation); err != nil {
			return out, err
		}
		if resolved := confirmation.Bindings; resolved != nil {
			add(resolved.OnComplete)
			if resolved.OnPartialAction != nil {
				add(*resolved.OnPartialAction)
			}
			for _, party := range resolved.Parties {
				for _, ref := range []*AssetRef{party.Writeback, party.CompensateWriteback, party.Source, party.Protocol, party.Action, party.CompensateAction} {
					if ref != nil {
						add(*ref)
					}
				}
			}
		}
		var dispatch struct {
			Bindings *struct {
				Commands            map[string]AssetRef `json:"commands"`
				Writeback           AssetRef            `json:"writeback"`
				Target              AssetRef            `json:"target"`
				CompensateWriteback *AssetRef           `json:"compensateWriteback"`
				Source              *AssetRef           `json:"source"`
				Observation         *AssetRef           `json:"observation"`
				Record              *AssetRef           `json:"record"`
			} `json:"dispatchBindings"`
		}
		if err := json.Unmarshal(asset.Body, &dispatch); err != nil {
			return out, err
		}
		if binding := dispatch.Bindings; binding != nil {
			for _, command := range binding.Commands {
				add(command)
			}
			add(binding.Writeback)
			add(binding.Target)
			for _, ref := range []*AssetRef{binding.CompensateWriteback, binding.Source, binding.Observation, binding.Record} {
				if ref != nil {
					add(*ref)
				}
			}
		}
		var transaction struct {
			Bindings map[string]AssetRef `json:"transactionBindings"`
		}
		if err := json.Unmarshal(asset.Body, &transaction); err != nil {
			return out, err
		}
		for _, ref := range transaction.Bindings {
			add(ref)
		}
		capability("action", action.Schema, CapabilityAuthoritative, action.Roles, CapabilityFieldsSchema(action.Payload), ValueSchema{}, action.Payload)
	case AssetObservation:
		var observation ObservationDefinition
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
		capability("query", observation.Name, CapabilityPure, observation.Roles, ValueSchema{Type: "object", Properties: map[string]ValueSchema{"record": {Type: "string"}, "since": {Type: "string"}, "until": {Type: "string"}, "mode": {Type: "string", Enum: modes}, "limit": {Type: "integer"}}, Required: []string{"record", "since", "until", "mode"}}, ValueSchema{}, nil)
		if observation.Decision != nil {
			add(observation.Decision.Action)
			capability("operation", observation.Name+".evaluate", CapabilityAuthoritative, observation.Roles, ValueSchema{Type: "object", Properties: map[string]ValueSchema{"record": {Type: "string"}, "until": {Type: "string"}, "key": {Type: "string"}}, Required: []string{"record", "until", "key"}}, ValueSchema{}, nil)
		}
	case AssetQuery:
		var query NamedQuery
		if err := json.Unmarshal(asset.Body, &query); err != nil {
			return out, err
		}
		if err := query.Check(); err != nil {
			return out, err
		}
		for _, dep := range query.Dependencies() {
			if !historical && dep.Kind == AssetObject {
				dep = object(dep.Name)
			}
			add(dep)
		}
		if !historical && query.Interface != "" {
			owner, _, _ := strings.Cut(query.Interface, ".")
			add(AssetRef{App: owner, Kind: AssetInterface, Name: query.Interface})
		}
		input := ValueSchema{Type: "object", Properties: map[string]ValueSchema{}}
		if query.By != "" {
			input.Properties["for"] = ValueSchema{Type: "string"}
			input.Required = []string{"for"}
		}
		capability("query", query.Name, CapabilityPure, nil, input, ValueSchema{}, nil)
	case AssetLinkType:
		var link LinkType
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
	case AssetPropertyType:
		var property PropertyType
		if err := json.Unmarshal(asset.Body, &property); err != nil {
			return out, err
		}
		if err := property.Check(); err != nil {
			return out, err
		}
	case AssetFunction:
		var f AIFunction
		if err := json.Unmarshal(asset.Body, &f); err != nil {
			return out, err
		}
		if err := f.Check(); err != nil {
			return out, err
		}
		if historical {
			add(AssetRef{App: asset.Ref.App, Kind: AssetObject, Name: f.Object})
		} else {
			addObject(f.Object)
		}
		capability("function", f.Name, CapabilityEffect, f.Roles, ValueSchema{Type: "object", Properties: map[string]ValueSchema{"source": {Type: "string"}}, Required: []string{"source"}}, CapabilityFieldsSchema(f.Output), nil)
	case AssetAlert:
		var alert AlertRule
		if err := json.Unmarshal(asset.Body, &alert); err != nil {
			return out, err
		}
		if err := alert.Check(); err != nil {
			return out, err
		}
		add(alert.Object)
	case AssetCompute:
		var operation Operation
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
		capability("operation", operation.Name, CapabilityPure, operation.Roles, operation.Input, operation.Output, nil)
	case AssetFlow:
		var flow FlowReleaseDescriptor
		if err := json.Unmarshal(asset.Body, &flow); err != nil {
			return out, err
		}
		if flow.Subject.Name != "" && flow.Subject.Kind != AssetObject || !json.Valid(flow.Definition) || len(flow.Definition) == 0 || flow.Definition[0] != '{' {
			return out, fmt.Errorf("release flow %s needs an optional subject and complete definition", asset.Ref)
		}
		add(flow.Subject)
		for _, binding := range append(append(slices.Clone(flow.Functions), flow.Operations...), flow.Queries...) {
			add(binding.Ref)
		}
		for _, ref := range flow.Actions {
			if ref.Kind != AssetAction {
				return out, fmt.Errorf("flow %s binds a non-action", asset.Ref)
			}
			add(ref)
		}
		for _, ref := range flow.Dependencies {
			add(ref)
		}
		capability("operation", flow.Name, CapabilityAuthoritative, nil, ValueSchema{}, ValueSchema{}, nil)
	case AssetAgent:
		var agent AgentReleaseDescriptor
		if err := json.Unmarshal(asset.Body, &agent); err != nil {
			return out, err
		}
		if err := agent.Check(); err != nil {
			return out, err
		}
		if historical {
			for _, ref := range agent.Dependencies(asset.Ref.App) {
				add(ref)
			}
		} else {
			for _, tool := range agent.Tools {
				if protocol, _, ok := strings.Cut(tool, "#"); ok {
					ref, found := bindings.Protocols[protocol]
					if !found || ref.Kind != AssetProtocol || ref.Check() != nil {
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
					kind := AssetAction
					name := tool
					if queryTool {
						kind = AssetQuery
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
					add(AssetRef{App: asset.Ref.App, Kind: AssetQuery, Name: query})
					continue
				}
				if !strings.HasPrefix(tool, asset.Ref.App+".") || strings.ContainsAny(tool, "#:") {
					return out, fmt.Errorf("agent %s tool %s is not a declared local action", asset.Ref, tool)
				}
				add(AssetRef{App: asset.Ref.App, Kind: AssetAction, Name: tool})
			}
		}
		capability("operation", agent.Name, CapabilityEffect, nil, ValueSchema{}, ValueSchema{}, nil)
	case AssetEvaluation:
		evaluation, err := ReadEvaluationSet(asset.Body)
		if err != nil {
			return out, err
		}
		add(evaluation.Function.Ref)
	case AssetTestPlan:
		plan, err := ReadTestPlan(asset.Body)
		if err != nil {
			return out, err
		}
		for _, root := range plan.Dependencies() {
			add(root)
		}
	case AssetSampleSet:
		var samples SampleSet
		if err := json.Unmarshal(asset.Body, &samples); err != nil {
			return out, err
		}
		if err := samples.Check(); err != nil {
			return out, err
		}
		out.Requires = append(out.Requires, samples.Dependencies()...)
	case AssetInterface:
		var shape Interface
		if err := json.Unmarshal(asset.Body, &shape); err != nil {
			return out, err
		}
		if err := shape.Check(asset.Ref.App); err != nil {
			return out, err
		}
		fields := make([]Field, 0, len(shape.Fields))
		for _, field := range shape.Fields {
			fields = append(fields, Field{Name: field.Name, Type: field.Type, Description: field.Title})
		}
		capability("interface", shape.Name, CapabilityPure, nil, ValueSchema{}, CapabilityFieldsSchema(fields), fields)
	case AssetProtocol:
		var protocol ProtocolReleaseDescriptor
		if err := json.Unmarshal(asset.Body, &protocol); err != nil {
			return out, err
		}
		if protocol.Version < 1 {
			return out, fmt.Errorf("protocol %s needs a pinned version", asset.Ref)
		}
		for _, action := range protocol.Actions {
			roles := action.Roles
			if frozen, ok := protocol.ActionRoles[action.Schema]; ok {
				roles = frozen
			}
			capability("protocol", protocol.Name+"#"+action.Schema, CapabilityAuthoritative, roles, CapabilityFieldsSchema(action.Payload), ValueSchema{}, action.Payload)
		}
		for _, read := range protocol.Reads {
			capability("protocol", protocol.Name+"#"+read, CapabilityPure, nil, ValueSchema{}, ValueSchema{}, nil)
		}
		for _, ref := range protocol.ActionBindings {
			add(ref)
		}
		for _, ref := range protocol.ReadBindings {
			add(ref)
		}
	case AssetConnection, AssetDataset, AssetSource, AssetPipeline, AssetWriteback, AssetMatching:
		if historical {
			break
		}
		var integration struct {
			Connection    string `json:"connection"`
			Dataset       string `json:"dataset"`
			Object        string `json:"object"`
			Input         string `json:"input"`
			OutputDataset string `json:"outputDataset"`
			OutputObject  string `json:"outputObject"`
			On            string `json:"on"`
			Steps         []struct {
				Dataset string `json:"dataset"`
			} `json:"steps"`
			Prefer []struct {
				Producer string    `json:"producer"`
				Binding  *AssetRef `json:"binding"`
			} `json:"prefer"`
			ProducerBindings map[string]AssetRef `json:"producers"`
		}
		if err := json.Unmarshal(asset.Body, &integration); err != nil {
			return out, err
		}
		for kind, names := range map[AssetKind][]string{AssetConnection: {integration.Connection}, AssetDataset: {integration.Dataset, integration.Input, integration.OutputDataset}} {
			for _, name := range names {
				if name != "" {
					add(AssetRef{App: asset.Ref.App, Kind: kind, Name: name})
				}
			}
		}
		addObject(integration.Object)
		addObject(integration.OutputObject)
		for _, step := range integration.Steps {
			if step.Dataset != "" {
				add(AssetRef{App: asset.Ref.App, Kind: AssetDataset, Name: step.Dataset})
			}
		}
		if asset.Ref.Kind == AssetWriteback && !slices.Contains([]string{"", "confirmation", "dispatch", "transaction"}, integration.On) {
			ref := object(integration.Object)
			ref.Kind = AssetAction
			ref.Name = integration.Object + "." + integration.On
			add(ref)
		}
		for _, preference := range integration.Prefer {
			if preference.Binding != nil {
				add(*preference.Binding)
			} else if ref, ok := integration.ProducerBindings[preference.Producer]; ok {
				add(ref)
			}
		}
		if slices.Contains([]AssetKind{AssetSource, AssetPipeline, AssetWriteback, AssetMatching}, asset.Ref.Kind) {
			capability("operation", asset.Ref.Name, CapabilityEffect, nil, ValueSchema{}, ValueSchema{}, nil)
		}
	}
	slices.SortFunc(out.Requires, compareRef)
	out.Requires = slices.Compact(out.Requires)
	for _, ref := range out.Requires {
		if err := ref.Check(); err != nil {
			return out, fmt.Errorf("%s has invalid binding: %w", asset.Ref, err)
		}
	}
	slices.SortFunc(out.Provides, func(a, b Capability) int {
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		return strings.Compare(a.Name, b.Name)
	})
	for i, c := range out.Provides {
		if err := c.Check(); err != nil {
			return out, err
		}
		if i > 0 && c.Kind == out.Provides[i-1].Kind && c.Name == out.Provides[i-1].Name {
			return out, fmt.Errorf("%s repeats capability %s", asset.Ref, c.Name)
		}
	}
	return out, nil
}

func CapabilityFieldsSchema(fields []Field) ValueSchema {
	schema := ValueSchema{Type: "object", Properties: map[string]ValueSchema{}}
	for _, field := range fields {
		value := CapabilityFieldSchema(field.Type, field.Description, field.Choices)
		if value.Type == "" {
			return ValueSchema{}
		}
		schema.Properties[field.Name] = value
		if field.Required {
			schema.Required = append(schema.Required, field.Name)
		}
	}
	slices.Sort(schema.Required)
	return schema
}

func CapabilityFieldSchema(kind, description string, choices []string) ValueSchema {
	value := ValueSchema{Description: description}
	switch kind {
	case "integer":
		value.Type = "integer"
	case "decimal", "number":
		value.Type = "number"
	case "boolean":
		value.Type = "boolean"
	case "string[]", "tags", "references":
		item := ValueSchema{Type: "string"}
		value.Type = "array"
		value.Items = &item
		value.MaxItems = 1000
	case "money":
		value.Type = "object"
		value.Properties = map[string]ValueSchema{"amount": {Type: "integer"}, "currency": {Type: "string"}}
		value.Required = []string{"amount", "currency"}
	case "json":
		return ValueSchema{}
	default:
		value.Type = "string"
		value.Enum = slices.Clone(choices)
	}
	return value
}

func CapabilityEntitySchema(info EntityInfo) ValueSchema {
	schema := ValueSchema{Type: "object", Properties: map[string]ValueSchema{"id": {Type: "string"}, "revision": {Type: "integer"}, "archived": {Type: "boolean"}}, Required: []string{"id", "revision"}}
	for _, field := range info.Fields {
		value := capabilityRecordFieldSchema(field)
		if value.Type == "" {
			return ValueSchema{}
		}
		schema.Properties[field.Name] = value
	}
	if schema.Check() != nil {
		return ValueSchema{}
	}
	return schema
}

func capabilityRecordFieldSchema(field FieldInfo) ValueSchema {
	if field.Type != "lines" {
		return CapabilityFieldSchema(field.Type, field.Help, field.Choices)
	}
	row := ValueSchema{Type: "object", Properties: map[string]ValueSchema{}}
	for _, column := range field.Fields {
		value := capabilityRecordFieldSchema(column)
		if value.Type == "" {
			return ValueSchema{}
		}
		row.Properties[column.Name] = value
	}
	return ValueSchema{Type: "array", Items: &row, MaxItems: 1000, Description: field.Help}
}

func pageDependencies(original Page) []AssetRef {
	page, err := original.Safety()
	if err != nil {
		return nil
	}
	refs := []AssetRef{}
	if page.Object.Name != "" {
		refs = append(refs, page.Object)
	}
	refs = append(refs, page.ModelDependencies()...)
	refs = append(refs, page.QueryReferences()...)
	refs = append(refs, page.NavigationTargets()...)
	for _, compute := range page.ComputeResources() {
		refs = append(refs, compute.Operation.Ref)
	}
	for _, selection := range page.InputSchema.Selections {
		refs = append(refs, selection.Object)
	}
	for _, binding := range page.DataBindings {
		if binding.Object.Name != "" {
			refs = append(refs, binding.Object)
		}
		if binding.Query != nil {
			refs = append(refs, binding.Query.Ref)
		}
		for _, resource := range binding.Resources {
			if resource.Asset != nil {
				refs = append(refs, resource.Asset.Ref)
			}
			if resource.Object != nil {
				refs = append(refs, *resource.Object)
			}
		}
	}
	for _, trigger := range page.ActionTriggers {
		refs = append(refs, trigger.Actions...)
		if trigger.InlineEdit != nil {
			refs = append(refs, trigger.InlineEdit.Action)
		}
		if trigger.Function != nil {
			refs = append(refs, trigger.Function.Ref)
		}
		if trigger.Operation != nil {
			refs = append(refs, trigger.Operation.Ref)
		}
		for _, event := range trigger.Events {
			for _, effect := range event.Effects {
				if effect.Action != nil {
					refs = append(refs, effect.Action.Ref)
				}
			}
		}
	}
	return refs
}

func checkReleaseBindings(ref AssetRef, body []byte, declared []AssetRef) error {
	description, err := describeAsset(ReleaseAsset{Ref: ref, Body: body}, true)
	if err != nil {
		return err
	}
	for _, dep := range description.Requires {
		if !slices.Contains(declared, dep) {
			return fmt.Errorf("release asset %s omits bound dependency %s", ref, dep)
		}
	}
	return nil
}

func CheckAssetDependencies(asset ReleaseAsset) error { return checkAssetDescription(asset) }

func checkAssetDescription(asset ReleaseAsset) error {
	if asset.Provides == nil {
		return checkReleaseBindings(asset.Ref, asset.Body, asset.Requires)
	}
	description, err := DescribeAsset(asset)
	if err != nil {
		return err
	}
	actual := slices.Clone(asset.Requires)
	slices.SortFunc(actual, compareRef)
	for _, dependency := range description.Requires {
		if !slices.Contains(actual, dependency) {
			return fmt.Errorf("release asset %s omits bound dependency %s", asset.Ref, dependency)
		}
	}
	if !slices.Equal(actual, description.Requires) {
		return fmt.Errorf("%s requires list differs from its body references", asset.Ref)
	}
	for _, capability := range asset.Provides {
		if err := capability.Check(); err != nil {
			return err
		}
	}
	actualCapabilities, _ := json.Marshal(asset.Provides)
	expectedCapabilities, _ := json.Marshal(description.Provides)
	if !bytes.Equal(actualCapabilities, expectedCapabilities) {
		return fmt.Errorf("%s provides list differs from its owner declaration", asset.Ref)
	}
	return nil
}

type DependencyGraph struct {
	nodes       map[AssetRef]Dependencies
	usedBy      map[AssetRef][]AssetRef
	navigation  map[[2]AssetRef]bool
	selfData    map[AssetRef]bool
	containment map[[2]AssetRef]bool
}

func NewDependencyGraph(descriptors []Dependencies) (*DependencyGraph, error) {
	graph := &DependencyGraph{nodes: map[AssetRef]Dependencies{}, usedBy: map[AssetRef][]AssetRef{}, navigation: map[[2]AssetRef]bool{}, selfData: map[AssetRef]bool{}, containment: map[[2]AssetRef]bool{}}
	for _, description := range descriptors {
		if err := description.Ref.Check(); err != nil {
			return nil, err
		}
		if _, exists := graph.nodes[description.Ref]; exists {
			return nil, fmt.Errorf("asset %s is declared twice", description.Ref)
		}
		description.Requires = slices.Clone(description.Requires)
		slices.SortFunc(description.Requires, compareRef)
		for i, ref := range description.Requires {
			if err := ref.Check(); err != nil {
				return nil, fmt.Errorf("%s: %w", description.Ref, err)
			}
			if i > 0 && ref == description.Requires[i-1] {
				return nil, fmt.Errorf("%s repeats dependency %s", description.Ref, ref)
			}
			graph.usedBy[ref] = append(graph.usedBy[ref], description.Ref)
		}
		for i, capability := range description.Provides {
			if err := capability.Check(); err != nil {
				return nil, fmt.Errorf("%s: %w", description.Ref, err)
			}
			if slices.ContainsFunc(description.Provides[:i], func(other Capability) bool { return other.Kind == capability.Kind && other.Name == capability.Name }) {
				return nil, fmt.Errorf("%s repeats capability %s", description.Ref, capability.Name)
			}
		}
		graph.nodes[description.Ref] = description
		if description.Ref.Kind == AssetObject {
			for _, ref := range description.Requires {
				if ref.App == description.Ref.App && ref.Kind == AssetAction && strings.HasPrefix(ref.Name, description.Ref.Name+".") {
					graph.containment[[2]AssetRef{description.Ref, ref}] = true
					graph.containment[[2]AssetRef{ref, description.Ref}] = true
				}
			}
		}
	}
	for ref, owners := range graph.usedBy {
		slices.SortFunc(owners, compareRef)
		graph.usedBy[ref] = slices.Compact(owners)
	}
	return graph, nil
}

func NewReleaseDependencyGraph(assets []ReleaseAsset) (*DependencyGraph, error) {
	descriptions := make([]Dependencies, 0, len(assets))
	for _, asset := range assets {
		descriptions = append(descriptions, Dependencies{Ref: asset.Ref, Requires: asset.Requires, Provides: asset.Provides, Version: asset.SourceVersion})
	}
	graph, err := NewDependencyGraph(descriptions)
	if err != nil {
		return nil, err
	}
	graph.containment = map[[2]AssetRef]bool{}
	lookup := map[AssetRef]ReleaseAsset{}
	for _, asset := range assets {
		lookup[asset.Ref] = asset
	}
	for _, asset := range assets {
		if asset.Ref.Kind == AssetObject {
			for _, ref := range asset.Requires {
				if ref.Kind == AssetFunction {
					var object struct {
						Actions []struct {
							Adoption *ProposalAdoption `json:"adoption"`
						} `json:"actions"`
					}
					var function AIFunction
					if json.Unmarshal(asset.Body, &object) == nil && json.Unmarshal(lookup[ref].Body, &function) == nil && function.Object == asset.Ref.Name && slices.ContainsFunc(object.Actions, func(action struct {
						Adoption *ProposalAdoption `json:"adoption"`
					}) bool {
						return action.Adoption != nil && action.Adoption.Function.Name == ref.Name && (action.Adoption.Function.App == ref.App || action.Adoption.Function.App == "" && asset.Ref.App == ref.App)
					}) {
						graph.containment[[2]AssetRef{asset.Ref, ref}] = true
						graph.containment[[2]AssetRef{ref, asset.Ref}] = true
					}
				}
				if ref.App == asset.Ref.App && ref.Kind == AssetAction {
					if target, ok := lookup[ref]; ok {
						action, err := releaseActionDescriptor(target.Body)
						if err == nil && action.Target == asset.Ref.Name {
							graph.containment[[2]AssetRef{asset.Ref, ref}] = true
							graph.containment[[2]AssetRef{ref, asset.Ref}] = true
						}
					}
				}
			}

			info, err := queryObjectDescriptor(asset.Body)
			if err == nil {
				graph.selfData[asset.Ref] = slices.ContainsFunc(info.Fields, func(field FieldInfo) bool {
					return field.Ref == asset.Ref.Name && (field.Type == "reference" || field.Type == "references")
				})
			}
		}
		if asset.Ref.Kind == AssetPage {
			for _, ref := range asset.Requires {
				if pageNavigationEdge(asset, ref) {
					graph.navigation[[2]AssetRef{asset.Ref, ref}] = true
				}
			}
		}
	}
	return graph, nil
}

func (g *DependencyGraph) Requires(ref AssetRef) []AssetRef {
	return slices.Clone(g.nodes[ref].Requires)
}
func (g *DependencyGraph) Closure(roots []AssetRef) ([]AssetRef, error) {
	refs, err := g.walk(roots, false)
	slices.SortFunc(refs, compareRef)
	return refs, err
}

func (g *DependencyGraph) InstallationOrder() ([]AssetRef, error) {
	roots := make([]AssetRef, 0, len(g.nodes))
	for ref := range g.nodes {
		roots = append(roots, ref)
	}
	slices.SortFunc(roots, compareRef)
	return g.walk(roots, true)
}

func (g *DependencyGraph) walk(roots []AssetRef, installedPrerequisites bool) ([]AssetRef, error) {
	visited, visiting := map[AssetRef]bool{}, map[AssetRef]bool{}
	var path, out []AssetRef
	var visit func(AssetRef, AssetRef) error
	visit = func(ref, from AssetRef) error {
		if err := ref.Check(); err != nil {
			return err
		}
		if visiting[ref] {
			if start := slices.Index(path, ref); start >= 0 && slices.ContainsFunc(path[start:], func(node AssetRef) bool { return node.Kind == AssetLinkType }) && !slices.ContainsFunc(path[start:], func(node AssetRef) bool { return node.Kind != AssetObject && node.Kind != AssetLinkType }) {
				return nil
			}
			if start := slices.Index(path, ref); start >= 0 && len(path)-start == 2 && g.containment[[2]AssetRef{from, ref}] {
				return nil
			}
			if from.Kind == AssetObject && ref.Kind == AssetObject && from != ref {
				return nil
			}
			if from == ref && g.selfData[ref] {
				return nil
			}
			if g.navigation[[2]AssetRef{from, ref}] {
				start := slices.Index(path, ref)
				navigation := start >= 0
				for i := start; i >= 0 && i+1 < len(path); i++ {
					navigation = navigation && g.navigation[[2]AssetRef{path[i], path[i+1]}]
				}
				if navigation {
					return nil
				}
			}
			parts := make([]string, 0, len(path)+1)
			for _, part := range append(slices.Clone(path), ref) {
				parts = append(parts, part.String())
			}
			return fmt.Errorf("release dependency cycle: %s", strings.Join(parts, " -> "))
		}
		if visited[ref] {
			return nil
		}
		node, exists := g.nodes[ref]
		if !exists {
			if installedPrerequisites {
				return nil
			}
			if from.Name != "" {
				return fmt.Errorf("%s requires missing asset %s", from, ref)
			}
			return fmt.Errorf("release requires missing asset %s", ref)
		}
		visiting[ref] = true
		path = append(path, ref)
		for _, dependency := range node.Requires {
			if installedPrerequisites && ref.Kind == AssetObject && dependency.Kind == AssetFunction && g.containment[[2]AssetRef{ref, dependency}] {
				continue
			}
			if err := visit(dependency, ref); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		delete(visiting, ref)
		visited[ref] = true
		out = append(out, ref)
		return nil
	}
	for _, root := range roots {
		if err := visit(root, AssetRef{}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (g *DependencyGraph) Reachable(roots []AssetRef) ([]AssetRef, error) {
	seen := map[AssetRef]bool{}
	queue := slices.Clone(roots)
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		if err := ref.Check(); err != nil {
			return nil, err
		}
		if seen[ref] {
			continue
		}
		node, exists := g.nodes[ref]
		if !exists {
			return nil, fmt.Errorf("dependency requires missing asset %s", ref)
		}
		seen[ref] = true
		queue = append(queue, node.Requires...)
	}
	out := make([]AssetRef, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	slices.SortFunc(out, compareRef)
	return out, nil
}

func (g *DependencyGraph) CheckExecutionBindings(asset ReleaseAsset, assets map[AssetRef]ReleaseAsset) error {
	if asset.Ref.Kind == AssetApp {
		return nil
	}
	for _, dependency := range asset.Requires {
		if dependency.Kind != AssetAction || dependency.App == asset.Ref.App {
			continue
		}
		if asset.Ref.Kind == AssetPage || asset.Ref.Kind == AssetObservation {
			continue
		}
		bound := false
		for _, protocolRef := range asset.Requires {
			if protocolRef.Kind != AssetProtocol || protocolRef.App != dependency.App {
				continue
			}
			var protocol ProtocolReleaseDescriptor
			if frozen, ok := assets[protocolRef]; ok && json.Unmarshal(frozen.Body, &protocol) == nil {
				for _, action := range protocol.ActionBindings {
					if action == dependency {
						bound = true
						break
					}
				}
			}
		}
		if !bound {
			return fmt.Errorf("%s has undeclared cross-application action %s; bind its provider protocol", asset.Ref, dependency)
		}
	}
	return nil
}

func (g *DependencyGraph) Impact(changed []AssetRef) ([]AssetRef, error) {
	seen := map[AssetRef]bool{}
	queue := slices.Clone(changed)
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		if err := ref.Check(); err != nil {
			return nil, err
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		queue = append(queue, g.usedBy[ref]...)
	}
	out := make([]AssetRef, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	slices.SortFunc(out, compareRef)
	return out, nil
}

func (g *DependencyGraph) CheckReadBindings(asset ReleaseAsset) error {
	if asset.Ref.Kind != AssetPage {
		return nil
	}
	var page Page
	if err := json.Unmarshal(asset.Body, &page); err != nil {
		return err
	}
	var reads []AssetRef
	if page.Object.Name != "" {
		reads = append(reads, page.Object)
	}
	reads = append(reads, page.QueryReferences()...)
	for _, compute := range page.ComputeResources() {
		reads = append(reads, compute.Operation.Ref)
	}
	for _, section := range page.Sections {
		if section.Operation != nil {
			reads = append(reads, section.Operation.Ref)
		}
		if section.Query.Name != "" {
			reads = append(reads, section.Query)
		}
	}
	for _, ref := range reads {
		if ref.Kind == AssetAction || ref.Kind == AssetFlow || ref.Kind == AssetFunction || ref.Kind == AssetWriteback || ref.Kind == AssetSource || ref.Kind == AssetPipeline {
			return fmt.Errorf("%s read binding %s is not pure", asset.Ref, ref)
		}
		capabilities := g.nodes[ref].Provides
		if ref.Kind == AssetObservation {
			if !slices.ContainsFunc(capabilities, func(capability Capability) bool {
				return capability.Kind == "query" && capability.Name == ref.Name && capability.Side == CapabilityPure
			}) {
				return fmt.Errorf("%s read binding %s has no pure observation query", asset.Ref, ref)
			}
			continue
		}
		for _, capability := range capabilities {
			if capability.Side != CapabilityPure {
				return fmt.Errorf("%s read binding %s provides %s capability %s", asset.Ref, ref, capability.Side, capability.Name)
			}
		}
	}
	return nil
}

func ValidDigest(value string) bool {
	value = strings.TrimPrefix(value, "sha256:")
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
