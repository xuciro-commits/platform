package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const (
	CapabilityPure          = "pure"
	CapabilityEffect        = "effect"
	CapabilityAuthoritative = "authoritative"
)

// Dependencies is the one owner-produced description used by release closure,
// lineage, impact and callable discovery. It carries no permission grant.
type Dependencies struct {
	Ref      AssetRef     `json:"ref"`
	Requires []AssetRef   `json:"requires"`
	Provides []Capability `json:"provides"`
	Version  string       `json:"version"`
}

type Capability struct {
	Kind    string      `json:"kind"`
	Name    string      `json:"name"`
	Inputs  ValueSchema `json:"inputs"`
	Outputs ValueSchema `json:"outputs"`
	Roles   []string    `json:"roles"`
	Side    string      `json:"side"`
	// Parameters preserves an owner's opaque JSON payload rather than
	// inventing a structural schema for it. Empty schemas mean owner-defined.
	Parameters []Field `json:"parameters,omitempty"`
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

// ProtocolReleaseDescriptor freezes the original protocol and its provider
// bindings. Reads remain Probe/read calls; actions remain independent requests.
type ProtocolReleaseDescriptor struct {
	Name           string              `json:"name"`
	Version        int                 `json:"version"`
	Actions        []Action            `json:"actions"`
	Reads          []string            `json:"reads"`
	ActionBindings map[string]AssetRef `json:"actionBindings,omitempty"`
	ActionRoles    map[string][]string `json:"actionRoles,omitempty"`
	ReadBindings   map[string]AssetRef `json:"readBindings,omitempty"`
}

// MarshalJSON retains historical omission of Provides, while a newly produced
// descriptor with no callable capabilities still seals an explicit empty list.
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

// DescribeAsset derives edges and capabilities from complete owner bytes. A
// caller-supplied Requires/Provides list is never used as semantic input.
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
		p = p.EffectiveModelPage(nil)
		if err := p.Document.Check(p.Sections); err != nil {
			return out, err
		}
		if err := p.CheckCollectionPorts(); err != nil {
			return out, err
		}
		if err := p.CheckRecordPorts(); err != nil {
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
			if !historical && field.Ref != "" {
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
		capability("query", observation.Name, CapabilityPure, observation.Roles, ValueSchema{Type: "object", Properties: map[string]ValueSchema{"record": {Type: "string"}, "since": {Type: "string"}, "until": {Type: "string"}, "mode": {Type: "string", Enum: []string{"summary", "points"}}, "limit": {Type: "integer"}}, Required: []string{"record", "since", "until", "mode"}}, ValueSchema{}, nil)
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
	case AssetMigration:
		var migration MigrationDescriptor
		if err := json.Unmarshal(asset.Body, &migration); err != nil {
			return out, err
		}
		if err := migration.Check(); err != nil {
			return out, err
		}
		for _, verb := range []string{"start", "advance", "cancel"} {
			capability("operation", migration.Name+"."+verb, CapabilityAuthoritative, nil, ValueSchema{}, ValueSchema{}, nil)
		}
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

// CapabilityFieldsSchema projects original payload fields. An empty Type
// leaves opaque JSON with its owner instead of falsely advertising text.
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

// CapabilityEntitySchema describes an object's original fields. The host
// applies its existing field permissions before using this projection.
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

func pageDependencies(page Page) []AssetRef {
	var refs []AssetRef
	if page.Object.Name != "" {
		refs = append(refs, page.Object)
	}
	refs = append(refs, page.NavigationTargets()...)
	refs = append(refs, page.ModelDependencies()...)
	for _, resource := range page.ComputeResources() {
		refs = append(refs, resource.Operation.Ref)
	}
	for _, embedding := range page.EmbeddingBindings() {
		refs = append(refs, embedding.Page.Ref)
	}
	refs = append(refs, page.Actions...)
	refs = append(refs, page.QueryReferences()...)
	for _, selection := range page.Selections {
		refs = append(refs, selection.Object)
	}
	for _, section := range page.Sections {
		if section.Function != nil {
			refs = append(refs, section.Function.Ref)
		}
		if section.Operation != nil {
			refs = append(refs, section.Operation.Ref)
		}
		if section.Object.Name != "" {
			refs = append(refs, section.Object)
		}
		if section.Query.Name != "" {
			refs = append(refs, section.Query)
		}
		for _, group := range section.RecordLinks {
			refs = append(refs, group.Object)
		}
		refs = append(refs, section.ServiceDependencies()...)
		refs = append(refs, section.ExplorationReferences()...)
		refs = append(refs, section.Actions...)
		if section.InlineEdit != nil {
			refs = append(refs, section.InlineEdit.Action)
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

// CheckAssetDependencies validates the frozen owner description. Historical
// assets keep the original subset rules; newly produced assets must be exact.
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
