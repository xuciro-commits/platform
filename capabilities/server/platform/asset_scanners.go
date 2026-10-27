package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// One scanner per definition kind. The list is data: adding a kind adds one
// entry and one scanner, and no host file reads the kinds themselves. The
// switch this replaced named every kind in one 557-line function, so each new
// kind edited the host.
//
// A scanner records only what the definition requires and provides; the shared
// sort, compact and duplicate checks stay in describeAsset.
type assetBindings struct {
	Objects   map[string]AssetRef `json:"objectBindings"`
	Protocols map[string]AssetRef `json:"protocolBindings"`
}

type assetScan struct {
	asset      ReleaseAsset
	historical bool
	out        Dependencies
	bindings   assetBindings
}

var assetScanners = map[AssetKind]func(*assetScan) error{
	AssetPage:         (*assetScan).scanAssetPage,
	AssetApp:          (*assetScan).scanAssetApp,
	AssetObject:       (*assetScan).scanAssetObject,
	AssetAction:       (*assetScan).scanAssetAction,
	AssetObservation:  (*assetScan).scanAssetObservation,
	AssetQuery:        (*assetScan).scanAssetQuery,
	AssetLinkType:     (*assetScan).scanAssetLinkType,
	AssetPropertyType: (*assetScan).scanAssetPropertyType,
	AssetFunction:     (*assetScan).scanAssetFunction,
	AssetAlert:        (*assetScan).scanAssetAlert,
	AssetCompute:      (*assetScan).scanAssetCompute,
	AssetFlow:         (*assetScan).scanAssetFlow,
	AssetAgent:        (*assetScan).scanAssetAgent,
	AssetEvaluation:   (*assetScan).scanAssetEvaluation,
	AssetTestPlan:     (*assetScan).scanAssetTestPlan,
	AssetSampleSet:    (*assetScan).scanAssetSampleSet,
	AssetInterface:    (*assetScan).scanAssetInterface,
	AssetProtocol:     (*assetScan).scanAssetProtocol,
	AssetConnection:   (*assetScan).scanAssetConnection,
	AssetDataset:      (*assetScan).scanAssetConnection,
	AssetSource:       (*assetScan).scanAssetConnection,
	AssetPipeline:     (*assetScan).scanAssetConnection,
	AssetWriteback:    (*assetScan).scanAssetConnection,
	AssetMatching:     (*assetScan).scanAssetConnection,
}

func (s *assetScan) object(name string) AssetRef {
	if ref, ok := s.bindings.Objects[name]; ok {
		return ref
	}
	app, _, ok := strings.Cut(name, ".")
	if !ok {
		app = s.asset.Ref.App
	}
	return AssetRef{App: app, Kind: AssetObject, Name: name}

}

func (s *assetScan) add(ref AssetRef) {
	if ref.Name != "" {
		s.out.Requires = append(s.out.Requires, ref)
	}

}

func (s *assetScan) addObject(name string) {
	if name == "enterprise.element" {
		return
	}
	if s.asset.Ref.Kind == AssetObject && name == s.asset.Ref.Name {
		return
	}
	if name != "" {
		s.add(s.object(name))
	}

}

func (s *assetScan) readSpec(spec *RuleReadSpec) {
	if spec == nil {
		return
	}
	for _, collection := range spec.Collections {
		if collection.Query != "" {
			s.add(AssetRef{App: collection.App, Kind: AssetQuery, Name: collection.Query})
		}
		if collection.Object != "" && collection.Object != s.asset.Ref.Name {
			s.addObject(collection.Object)
		}
	}

}

func (s *assetScan) capability(kind, name, side string, roles []string, input, output ValueSchema, fields []Field) {
	s.out.Provides = append(s.out.Provides, Capability{Kind: kind, Name: name, Side: side, Roles: slices.Clone(roles), Inputs: input, Outputs: output, Parameters: fields})

}

func (s *assetScan) scanAssetPage() error {
	var p Page
	if err := json.Unmarshal(s.asset.Body, &p); err != nil {
		return err
	}
	p = p.EffectiveModelPage(nil)
	if err := p.Document.Check(p.Sections); err != nil {
		return err
	}
	if err := p.CheckCollectionPorts(); err != nil {
		return err
	}
	if err := p.CheckRecordPorts(); err != nil {
		return err
	}
	s.out.Requires = append(s.out.Requires, pageDependencies(p)...)
	return nil
}

func (s *assetScan) scanAssetApp() error {
	var app Application
	decoder := json.NewDecoder(bytes.NewReader(s.asset.Body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&app); err != nil {
		return err
	}
	if err := app.CheckVariables(); err != nil {
		return err
	}
	if err := app.CheckResources(); err != nil {
		return err
	}
	s.out.Requires = append(s.out.Requires, app.Dependencies(s.asset.Ref.App)...)
	return nil
}

func (s *assetScan) scanAssetObject() error {
	info, err := queryObjectDescriptor(s.asset.Body)
	if err != nil {
		return err
	}
	for _, field := range info.Fields {
		if field.Property != nil {
			s.add(field.Property.Ref)
		}
		if !s.historical && field.Ref != "" && field.Ref != "enterprise.element" {
			s.add(s.object(field.Ref))
		}
	}
	if s.historical {
		return nil
	}
	s.capability("interface", s.asset.Ref.Name, CapabilityPure, nil, ValueSchema{}, CapabilityEntitySchema(info), nil)
	for _, name := range info.Implements {
		owner, _, _ := strings.Cut(name, ".")
		s.add(AssetRef{App: owner, Kind: AssetInterface, Name: name})
	}
	if info.Extends != "" {
		s.addObject(info.Extends)
	}
	if info.ExtensionSource != nil {
		s.add(info.ExtensionSource.Base.Ref)
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
	if err := json.Unmarshal(s.asset.Body, &declaration); err != nil {
		return err
	}
	s.readSpec(declaration.Reads)
	if declaration.Creation != nil {
		var visit func(TxnStep)
		visit = func(step TxnStep) {
			if step.Target.Type != "" {
				s.addObject(step.Target.Type)
			}
			if step.Call != nil {
				s.add(step.Call.Ref)
			}
			if step.Link != nil {
				s.add(step.Link.Binding.Ref)
			}
			if step.Intervals != nil {
				s.add(step.Intervals.Predecessors.Ref)
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
		s.readSpec(constraint.Reads)
	}
	for _, set := range append(slices.Clone(info.Sets), declaration.Sets...) {
		s.addObject(set.Object)
		if set.Binding != nil {
			s.add(set.Binding.Ref)
		}
	}
	for _, action := range declaration.Actions {
		s.readSpec(action.Reads)
		for _, created := range action.Creates {
			s.addObject(created.Object)
		}
		for _, posted := range action.Posts {
			s.addObject(posted.Object)
		}
		if action.Adoption != nil {
			ref := action.Adoption.Function
			owner := ref.App
			if owner == "" {
				owner = s.asset.Ref.App
			}
			s.add(AssetRef{App: owner, Kind: AssetFunction, Name: ref.Name})
		}
		if txn := action.Transaction; txn != nil {
			s.readSpec(txn.Reads)
			var refusal func([]Refusal)
			var step func(TxnStep)
			refusal = func(rules []Refusal) {
				for _, r := range rules {
					if r.Record != nil {
						step(*r.Record)
					}
					if r.Route != nil {
						s.addObject(r.Route.Type)
						s.add(AssetRef{App: s.asset.Ref.App, Kind: AssetAction, Name: r.Route.Type + "." + r.Route.Action})
					}
				}
			}
			step = func(st TxnStep) {
				if st.Call != nil {
					s.add(st.Call.Ref)
				}
				if st.Target.Type != "" && st.Target.Type != s.asset.Ref.Name {
					s.addObject(st.Target.Type)
				}
				if st.Link != nil {
					s.add(st.Link.Binding.Ref)
				}
				if st.Intervals != nil {
					s.add(st.Intervals.Predecessors.Ref)
				}
				refusal(st.Refusals)
				if st.ForEach != nil {
					for _, nested := range st.ForEach.Steps {
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
	return nil
}

func (s *assetScan) scanAssetAction() error {
	action, err := releaseActionDescriptor(s.asset.Body)
	if err != nil {
		return err
	}
	if s.historical {
		return nil
	}
	s.addObject(action.Target)
	for _, field := range action.Payload {
		s.addObject(field.Ref)
	}
	for _, use := range action.Uses {
		protocol, _, _ := strings.Cut(use, "#")
		if ref, ok := s.bindings.Protocols[protocol]; ok {
			s.add(ref)
		}
	}
	var owner struct {
		Roles []string `json:"roles"`
	}
	_ = json.Unmarshal(s.asset.Body, &owner)
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
	if err := json.Unmarshal(s.asset.Body, &confirmation); err != nil {
		return err
	}
	if resolved := confirmation.Bindings; resolved != nil {
		s.add(resolved.OnComplete)
		if resolved.OnPartialAction != nil {
			s.add(*resolved.OnPartialAction)
		}
		for _, party := range resolved.Parties {
			for _, ref := range []*AssetRef{party.Writeback, party.CompensateWriteback, party.Source, party.Protocol, party.Action, party.CompensateAction} {
				if ref != nil {
					s.add(*ref)
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
	if err := json.Unmarshal(s.asset.Body, &dispatch); err != nil {
		return err
	}
	if binding := dispatch.Bindings; binding != nil {
		for _, command := range binding.Commands {
			s.add(command)
		}
		s.add(binding.Writeback)
		s.add(binding.Target)
		for _, ref := range []*AssetRef{binding.CompensateWriteback, binding.Source, binding.Observation, binding.Record} {
			if ref != nil {
				s.add(*ref)
			}
		}
	}
	var transaction struct {
		Bindings map[string]AssetRef `json:"transactionBindings"`
	}
	if err := json.Unmarshal(s.asset.Body, &transaction); err != nil {
		return err
	}
	for _, ref := range transaction.Bindings {
		s.add(ref)
	}
	s.capability("action", action.Schema, CapabilityAuthoritative, action.Roles, CapabilityFieldsSchema(action.Payload), ValueSchema{}, action.Payload)
	return nil
}

func (s *assetScan) scanAssetObservation() error {
	var observation ObservationDefinition
	if err := json.Unmarshal(s.asset.Body, &observation); err != nil {
		return err
	}
	if err := observation.Check(); err != nil {
		return err
	}
	s.add(observation.Connection)
	s.add(observation.Object)
	modes := []string{"summary", "points"}
	if observation.Intervals != nil {
		modes = append(modes, "intervals")
	}
	s.capability("query", observation.Name, CapabilityPure, observation.Roles, ValueSchema{Type: "object", Properties: map[string]ValueSchema{"record": {Type: "string"}, "since": {Type: "string"}, "until": {Type: "string"}, "mode": {Type: "string", Enum: modes}, "limit": {Type: "integer"}}, Required: []string{"record", "since", "until", "mode"}}, ValueSchema{}, nil)
	if observation.Decision != nil {
		s.add(observation.Decision.Action)
		s.capability("operation", observation.Name+".evaluate", CapabilityAuthoritative, observation.Roles, ValueSchema{Type: "object", Properties: map[string]ValueSchema{"record": {Type: "string"}, "until": {Type: "string"}, "key": {Type: "string"}}, Required: []string{"record", "until", "key"}}, ValueSchema{}, nil)
	}
	return nil
}

func (s *assetScan) scanAssetQuery() error {
	var query NamedQuery
	if err := json.Unmarshal(s.asset.Body, &query); err != nil {
		return err
	}
	if err := query.Check(); err != nil {
		return err
	}
	for _, dep := range query.Dependencies() {
		if !s.historical && dep.Kind == AssetObject {
			dep = s.object(dep.Name)
		}
		s.add(dep)
	}
	if !s.historical && query.Interface != "" {
		owner, _, _ := strings.Cut(query.Interface, ".")
		s.add(AssetRef{App: owner, Kind: AssetInterface, Name: query.Interface})
	}
	input := ValueSchema{Type: "object", Properties: map[string]ValueSchema{}}
	if query.By != "" {
		input.Properties["for"] = ValueSchema{Type: "string"}
		input.Required = []string{"for"}
	}
	s.capability("query", query.Name, CapabilityPure, nil, input, ValueSchema{}, nil)
	return nil
}

func (s *assetScan) scanAssetLinkType() error {
	var link LinkType
	if err := json.Unmarshal(s.asset.Body, &link); err != nil {
		return err
	}
	if err := link.Check(); err != nil {
		return err
	}
	if link.Child.App != s.asset.Ref.App {
		return fmt.Errorf("link type must be declared by its child owner")
	}
	s.add(link.Parent)
	s.add(link.Child)
	return nil
}

func (s *assetScan) scanAssetPropertyType() error {
	var property PropertyType
	if err := json.Unmarshal(s.asset.Body, &property); err != nil {
		return err
	}
	if err := property.Check(); err != nil {
		return err
	}
	return nil
}

func (s *assetScan) scanAssetFunction() error {
	var f AIFunction
	if err := json.Unmarshal(s.asset.Body, &f); err != nil {
		return err
	}
	if err := f.Check(); err != nil {
		return err
	}
	if s.historical {
		s.add(AssetRef{App: s.asset.Ref.App, Kind: AssetObject, Name: f.Object})
	} else {
		s.addObject(f.Object)
	}
	s.capability("function", f.Name, CapabilityEffect, f.Roles, ValueSchema{Type: "object", Properties: map[string]ValueSchema{"source": {Type: "string"}}, Required: []string{"source"}}, CapabilityFieldsSchema(f.Output), nil)
	return nil
}

func (s *assetScan) scanAssetAlert() error {
	var alert AlertRule
	if err := json.Unmarshal(s.asset.Body, &alert); err != nil {
		return err
	}
	if err := alert.Check(); err != nil {
		return err
	}
	s.add(alert.Object)
	return nil
}

func (s *assetScan) scanAssetCompute() error {
	var operation Operation
	if err := json.Unmarshal(s.asset.Body, &operation); err != nil {
		return err
	}
	if err := operation.Check(); err != nil {
		return err
	}
	if !s.historical && operation.Rules != nil {
		s.addObject(operation.Rules.Reads.Object)
		s.readSpec(&operation.Rules.Reads)
	}
	s.capability("operation", operation.Name, CapabilityPure, operation.Roles, operation.Input, operation.Output, nil)
	return nil
}

func (s *assetScan) scanAssetFlow() error {
	var flow FlowReleaseDescriptor
	if err := json.Unmarshal(s.asset.Body, &flow); err != nil {
		return err
	}
	if flow.Subject.Name != "" && flow.Subject.Kind != AssetObject || !json.Valid(flow.Definition) || len(flow.Definition) == 0 || flow.Definition[0] != '{' {
		return fmt.Errorf("release flow %s needs an optional subject and complete definition", s.asset.Ref)
	}
	s.add(flow.Subject)
	for _, binding := range append(append(slices.Clone(flow.Functions), flow.Operations...), flow.Queries...) {
		s.add(binding.Ref)
	}
	for _, ref := range flow.Actions {
		if ref.Kind != AssetAction {
			return fmt.Errorf("flow %s binds a non-action", s.asset.Ref)
		}
		s.add(ref)
	}
	for _, ref := range flow.Dependencies {
		s.add(ref)
	}
	s.capability("operation", flow.Name, CapabilityAuthoritative, nil, ValueSchema{}, ValueSchema{}, nil)
	return nil
}

func (s *assetScan) scanAssetAgent() error {
	var agent AgentReleaseDescriptor
	if err := json.Unmarshal(s.asset.Body, &agent); err != nil {
		return err
	}
	if err := agent.Check(); err != nil {
		return err
	}
	if s.historical {
		for _, ref := range agent.Dependencies(s.asset.Ref.App) {
			s.add(ref)
		}
	} else {
		for _, tool := range agent.Tools {
			if protocol, _, ok := strings.Cut(tool, "#"); ok {
				ref, found := s.bindings.Protocols[protocol]
				if !found || ref.Kind != AssetProtocol || ref.Check() != nil {
					return fmt.Errorf("agent %s tool %s needs its declared provider protocol binding", s.asset.Ref, tool)
				}
				s.add(ref)
				continue
			}
			if strings.HasPrefix(tool, "read:") || strings.HasPrefix(tool, "emit:") {
				if !agent.Native {
					return fmt.Errorf("agent %s tool %s is not a published native declaration", s.asset.Ref, tool)
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
				if ref.Kind != kind || ref.App != s.asset.Ref.App || ref.Name != name {
					return fmt.Errorf("agent %s tool %s binding differs from its original declaration", s.asset.Ref, tool)
				}
				s.add(ref)
				continue
			}
			if agent.Native {
				return fmt.Errorf("agent %s tool %s needs a fixed declared asset binding", s.asset.Ref, tool)
			}
			if queryTool && query != "" {
				s.add(AssetRef{App: s.asset.Ref.App, Kind: AssetQuery, Name: query})
				continue
			}
			if !strings.HasPrefix(tool, s.asset.Ref.App+".") || strings.ContainsAny(tool, "#:") {
				return fmt.Errorf("agent %s tool %s is not a declared local action", s.asset.Ref, tool)
			}
			s.add(AssetRef{App: s.asset.Ref.App, Kind: AssetAction, Name: tool})
		}
	}
	s.capability("operation", agent.Name, CapabilityEffect, nil, ValueSchema{}, ValueSchema{}, nil)
	return nil
}

func (s *assetScan) scanAssetEvaluation() error {
	evaluation, err := ReadEvaluationSet(s.asset.Body)
	if err != nil {
		return err
	}
	s.add(evaluation.Function.Ref)
	return nil
}

func (s *assetScan) scanAssetTestPlan() error {
	plan, err := ReadTestPlan(s.asset.Body)
	if err != nil {
		return err
	}
	for _, root := range plan.Dependencies() {
		s.add(root)
	}
	return nil
}

func (s *assetScan) scanAssetSampleSet() error {
	var samples SampleSet
	if err := json.Unmarshal(s.asset.Body, &samples); err != nil {
		return err
	}
	if err := samples.Check(); err != nil {
		return err
	}
	s.out.Requires = append(s.out.Requires, samples.Dependencies()...)
	return nil
}

func (s *assetScan) scanAssetInterface() error {
	var shape Interface
	if err := json.Unmarshal(s.asset.Body, &shape); err != nil {
		return err
	}
	if err := shape.Check(s.asset.Ref.App); err != nil {
		return err
	}
	fields := make([]Field, 0, len(shape.Fields))
	for _, field := range shape.Fields {
		fields = append(fields, Field{Name: field.Name, Type: field.Type, Description: field.Title})
	}
	s.capability("interface", shape.Name, CapabilityPure, nil, ValueSchema{}, CapabilityFieldsSchema(fields), fields)
	return nil
}

func (s *assetScan) scanAssetProtocol() error {
	var protocol ProtocolReleaseDescriptor
	if err := json.Unmarshal(s.asset.Body, &protocol); err != nil {
		return err
	}
	if protocol.Version < 1 {
		return fmt.Errorf("protocol %s needs a pinned version", s.asset.Ref)
	}
	for _, action := range protocol.Actions {
		roles := action.Roles
		if frozen, ok := protocol.ActionRoles[action.Schema]; ok {
			roles = frozen
		}
		s.capability("protocol", protocol.Name+"#"+action.Schema, CapabilityAuthoritative, roles, CapabilityFieldsSchema(action.Payload), ValueSchema{}, action.Payload)
	}
	for _, read := range protocol.Reads {
		s.capability("protocol", protocol.Name+"#"+read, CapabilityPure, nil, ValueSchema{}, ValueSchema{}, nil)
	}
	for _, ref := range protocol.ActionBindings {
		s.add(ref)
	}
	for _, ref := range protocol.ReadBindings {
		s.add(ref)
	}
	return nil
}

func (s *assetScan) scanAssetConnection() error {
	if s.historical {
		return nil
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
	if err := json.Unmarshal(s.asset.Body, &integration); err != nil {
		return err
	}
	for kind, names := range map[AssetKind][]string{AssetConnection: {integration.Connection}, AssetDataset: {integration.Dataset, integration.Input, integration.OutputDataset}} {
		for _, name := range names {
			if name != "" {
				s.add(AssetRef{App: s.asset.Ref.App, Kind: kind, Name: name})
			}
		}
	}
	s.addObject(integration.Object)
	s.addObject(integration.OutputObject)
	for _, step := range integration.Steps {
		if step.Dataset != "" {
			s.add(AssetRef{App: s.asset.Ref.App, Kind: AssetDataset, Name: step.Dataset})
		}
	}
	if s.asset.Ref.Kind == AssetWriteback && !slices.Contains([]string{"", "confirmation", "dispatch", "transaction"}, integration.On) {
		ref := s.object(integration.Object)
		ref.Kind = AssetAction
		ref.Name = integration.Object + "." + integration.On
		s.add(ref)
	}
	for _, preference := range integration.Prefer {
		if preference.Binding != nil {
			s.add(*preference.Binding)
		} else if ref, ok := integration.ProducerBindings[preference.Producer]; ok {
			s.add(ref)
		}
	}
	if slices.Contains([]AssetKind{AssetSource, AssetPipeline, AssetWriteback, AssetMatching}, s.asset.Ref.Kind) {
		s.capability("operation", s.asset.Ref.Name, CapabilityEffect, nil, ValueSchema{}, ValueSchema{}, nil)
	}
	return nil
}
