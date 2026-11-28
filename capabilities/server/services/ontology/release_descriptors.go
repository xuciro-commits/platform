package ontology

import (
	"encoding/json"
	"fmt"
	runtimeprotocol "platformserver/kernel/protocol"
	"slices"
	"strings"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func ObjectDescriptor(body []byte) (platform.RecordTypeInfo, error) {
	var shape struct {
		Type            string                       `json:"type"`
		Implements      []string                     `json:"implements"`
		Extends         string                       `json:"extends"`
		ExtensionSource *platform.ExtensionSource    `json:"extensionSource"`
		Entity          *platform.RecordTypeInfo     `json:"entity"`
		ObjectView      *platform.ObjectView         `json:"objectView,omitempty"`
		Fields          []map[string]json.RawMessage `json:"fields"`
		Lifecycle       *platform.LifecycleInfo      `json:"lifecycle"`
		States          []platform.State             `json:"states"`
		Actions         []struct {
			Transaction *platform.BusinessTxn `json:"transaction"`
			Name        string                `json:"name"`
			Title       string                `json:"title"`
			From        []string              `json:"from"`
			Inputs      []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Required bool   `json:"required"`
				Choices  string `json:"choices"`
			} `json:"inputs"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(body, &shape); err != nil {
		return platform.RecordTypeInfo{}, err
	}
	if shape.Entity != nil {
		if shape.Entity.Type != shape.Type {
			return platform.RecordTypeInfo{}, fmt.Errorf("frozen native object identity differs")
		}
		return *shape.Entity, nil
	}
	var fields []platform.FieldInfo
	for _, raw := range shape.Fields {
		if choicesRaw, ok := raw["choices"]; ok {
			var source string
			if json.Unmarshal(choicesRaw, &source) == nil {
				raw["choices"], _ = json.Marshal(append([]string{}, ValueChoices(source)...))
				if titles := ChoiceTitles(source); titles != nil {
					raw["choiceTitles"], _ = json.Marshal(titles)
				}
			}
		}
		if whenRaw, ok := raw["when"]; ok {
			var source string
			if json.Unmarshal(whenRaw, &source) == nil {
				condition, err := platform.ParseWhen(source)
				if err != nil {
					return platform.RecordTypeInfo{}, err
				}
				if condition == nil {
					delete(raw, "when")
				} else {
					raw["when"], _ = json.Marshal(condition)
				}
			}
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return platform.RecordTypeInfo{}, err
		}
		var field platform.FieldInfo
		if err := json.Unmarshal(encoded, &field); err != nil {
			return platform.RecordTypeInfo{}, err
		}
		fields = append(fields, field)
	}
	owner := platform.OwnerOfType(shape.Type)
	if shape.Lifecycle == nil && len(shape.States) > 0 {
		life := platform.LifecycleInfo{Field: "state", Initial: shape.States[0].Name, States: shape.States}
		choices := []string{}
		for _, state := range shape.States {
			choices = append(choices, state.Name)
		}
		fields = append(fields, platform.FieldInfo{Name: "state", Title: "State", Type: "choice", ReadOnly: true, Choices: choices})
		for _, action := range shape.Actions {
			inputs := make([]platform.Field, 0, len(action.Inputs))
			for _, input := range action.Inputs {
				kind := input.Type
				if kind == "choice" {
					kind = "string"
				}
				inputs = append(inputs, platform.Field{Name: input.Name, Type: kind, Required: input.Required, Choices: ValueChoices(input.Choices)})
			}
			if to, input := TransactionDestination(shape.Type, "state", action.Transaction, inputs); len(to) > 0 {
				if slices.ContainsFunc(to, func(state string) bool { return !slices.Contains(choices, state) }) {
					return platform.RecordTypeInfo{}, fmt.Errorf("frozen object transaction destination differs")
				}
				life.Transitions = append(life.Transitions, platform.TransitionInfo{Name: action.Name, Schema: shape.Type + "." + action.Name, Title: action.Title, From: action.From, To: to, ToInput: input})
				continue
			}
			life.Transitions = append(life.Transitions, platform.TransitionInfo{Name: action.Name, Schema: shape.Type + "." + action.Name, Title: action.Title, From: action.From, To: slices.Clone(action.From)})
		}
		shape.Lifecycle = &life
	}
	return platform.RecordTypeInfo{App: owner, Type: shape.Type, ObjectView: shape.ObjectView, Standard: []string{shape.Type + ".create", shape.Type + ".edit", shape.Type + ".archive"}, Fields: fields, Lifecycle: shape.Lifecycle, Implements: shape.Implements, Extends: shape.Extends, ExtensionSource: shape.ExtensionSource}, nil
}

func ActionDescriptor(body json.RawMessage) (platform.Action, error) {
	var flat platform.Action
	if err := json.Unmarshal(body, &flat); err != nil {
		return platform.Action{}, err
	}
	var envelope struct {
		Action *platform.Action `json:"action"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return platform.Action{}, err
	}
	if envelope.Action != nil {
		if envelope.Action.Schema != flat.Schema || envelope.Action.Target != flat.Target {
			return platform.Action{}, fmt.Errorf("frozen action declaration identity differs")
		}
		return *envelope.Action, nil
	}
	return flat, nil
}

func ObjectDependencies(asset platform.ReleaseAsset, historical bool) ([]platform.AssetRef, []platform.Capability, error) {
	info, err := ObjectDescriptor(asset.Body)
	if err != nil {
		return nil, nil, err
	}
	var requires []platform.AssetRef
	var provides []platform.Capability
	var bindings struct {
		Objects   map[string]platform.AssetRef `json:"objectBindings"`
		Protocols map[string]platform.AssetRef `json:"protocolBindings"`
	}
	if err := json.Unmarshal(asset.Body, &bindings); err != nil {
		return nil, nil, err
	}
	object := func(name string) platform.AssetRef {
		if ref, ok := bindings.Objects[name]; ok {
			ref.Name = publication.ObjectAssetName(ref.Name)
			return ref
		}
		base := publication.ObjectAssetName(name)
		app, ok := platform.OwnerOfType(base), strings.Contains(base, ".")
		if !ok {
			app = asset.Ref.App
		}
		return platform.AssetRef{App: app, Kind: platform.AssetObject, Name: base}
	}
	addObject := func(name string) {
		base := publication.ObjectAssetName(name)
		if base == "enterprise.element" || base == "" || asset.Ref.Kind == platform.AssetObject && base == asset.Ref.Name {
			return
		}
		requires = append(requires, object(name))
	}
	readSpec := func(spec *platform.RuleReadSpec) {
		if spec == nil {
			return
		}
		for _, collection := range spec.Collections {
			if collection.Query != "" {
				requires = append(requires, platform.AssetRef{App: collection.App, Kind: platform.AssetQuery, Name: collection.Query})
			}
			if collection.Object != "" && collection.Object != asset.Ref.Name {
				addObject(collection.Object)
			}
		}
	}
	for _, field := range info.Fields {
		if field.Property != nil {
			requires = append(requires, field.Property.Ref)
		}
		if field.ValueType != nil {
			requires = append(requires, field.ValueType.Ref)
		}
		if !historical && field.Ref != "" && field.Ref != "enterprise.element" {
			addObject(field.Ref)
		}
	}
	if historical {
		return requires, nil, nil
	}
	if info.ObjectView != nil {
		if err := info.ObjectView.Check(); err != nil {
			return nil, nil, err
		}
		requires = append(requires, info.ObjectView.Application, info.ObjectView.Page)
	}
	provides = append(provides, platform.Capability{Kind: "interface", Name: asset.Ref.Name, Side: platform.CapabilityPure, Inputs: platform.ValueSchema{}, Outputs: runtimeprotocol.CapabilityEntitySchema(info)})
	for _, name := range info.Implements {
		owner := platform.OwnerOfType(name)
		requires = append(requires, platform.AssetRef{App: owner, Kind: platform.AssetInterface, Name: name})
	}
	if info.Extends != "" {
		addObject(info.Extends)
	}
	if info.ExtensionSource != nil {
		requires = append(requires, info.ExtensionSource.Base.Ref)
	}
	var declaration struct {
		Creation    *platform.BusinessTxn  `json:"createTransaction"`
		Reads       *platform.RuleReadSpec `json:"reads"`
		Constraints []platform.Constraint  `json:"constraints"`
		Sets        []platform.RecordSet   `json:"sets"`
		Depletion   *struct {
			Ledger string `json:"ledger"`
		} `json:"depletion"`
		Actions []struct {
			Uses        []string                   `json:"uses"`
			Reads       *platform.RuleReadSpec     `json:"reads"`
			Transaction *platform.BusinessTxn      `json:"transaction"`
			Adoption    *platform.ProposalAdoption `json:"adoption"`
			Creates     []struct {
				Object string `json:"object"`
			} `json:"creates"`
			Posts []struct {
				Object string `json:"object"`
			} `json:"posts"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(asset.Body, &declaration); err != nil {
		return nil, nil, err
	}
	readSpec(declaration.Reads)
	if declaration.Depletion != nil {
		addObject(declaration.Depletion.Ledger)
	}
	if declaration.Creation != nil {
		var visit func([]platform.TxnStep)
		visit = func(steps []platform.TxnStep) {
			for _, step := range steps {
				if step.Target.Type != "" {
					addObject(step.Target.Type)
				}
				if step.Call != nil {
					requires = append(requires, step.Call.Ref)
				}
				if step.Link != nil {
					requires = append(requires, step.Link.Binding.Ref)
				}
				if step.Intervals != nil {
					requires = append(requires, step.Intervals.Predecessors.Ref)
				}
				if step.ForEach != nil {
					visit(step.ForEach.Steps)
				}
			}
		}
		visit(declaration.Creation.Steps)
	}
	for _, constraint := range append(slices.Clone(info.Constraints), declaration.Constraints...) {
		readSpec(constraint.Reads)
	}
	for _, set := range append(slices.Clone(info.Sets), declaration.Sets...) {
		addObject(set.Object)
		if set.Binding != nil {
			requires = append(requires, set.Binding.Ref)
		}
	}
	for _, action := range declaration.Actions {
		for _, use := range action.Uses {
			proto, _, _ := strings.Cut(use, "#")
			if ref, ok := bindings.Protocols[proto]; ok && ref.Name != "" {
				requires = append(requires, ref)
			}
		}
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
			requires = append(requires, platform.AssetRef{App: owner, Kind: platform.AssetFunction, Name: ref.Name})
		}
		if txn := action.Transaction; txn != nil {
			readSpec(txn.Reads)
			WalkBusinessTxn(txn, func(step platform.TxnStep) {
				if step.Call != nil {
					requires = append(requires, step.Call.Ref)
				}
				if step.Target.Type != "" && step.Target.Type != asset.Ref.Name {
					addObject(step.Target.Type)
				}
				if step.Link != nil {
					requires = append(requires, step.Link.Binding.Ref)
				}
				if step.Intervals != nil {
					requires = append(requires, step.Intervals.Predecessors.Ref)
				}
				for _, refusal := range step.Refusals {
					if refusal.Route != nil {
						addObject(refusal.Route.Type)
						requires = append(requires, platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetAction, Name: refusal.Route.Type + "." + refusal.Route.Action})
					}
				}
			})
			for _, refusal := range txn.Refusals {
				if refusal.Route != nil {
					addObject(refusal.Route.Type)
					requires = append(requires, platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetAction, Name: refusal.Route.Type + "." + refusal.Route.Action})
				}
			}
		}
	}
	return requires, provides, nil
}

func ActionDependencies(asset platform.ReleaseAsset, historical bool) ([]platform.AssetRef, []platform.Capability, error) {
	action, err := ActionDescriptor(asset.Body)
	if err != nil || historical {
		return nil, nil, err
	}
	var requires []platform.AssetRef
	var bindings struct {
		Objects   map[string]platform.AssetRef `json:"objectBindings"`
		Protocols map[string]platform.AssetRef `json:"protocolBindings"`
	}
	if err := json.Unmarshal(asset.Body, &bindings); err != nil {
		return nil, nil, err
	}
	object := func(name string) platform.AssetRef {
		if ref, ok := bindings.Objects[name]; ok {
			ref.Name = publication.ObjectAssetName(ref.Name)
			return ref
		}
		base := publication.ObjectAssetName(name)
		app, ok := platform.OwnerOfType(base), strings.Contains(base, ".")
		if !ok {
			app = asset.Ref.App
		}
		return platform.AssetRef{App: app, Kind: platform.AssetObject, Name: base}
	}
	add := func(ref platform.AssetRef) {
		if ref.Name != "" {
			requires = append(requires, ref)
		}
	}
	addObject := func(name string) {
		base := publication.ObjectAssetName(name)
		if base != "" && base != "enterprise.element" {
			add(object(name))
		}
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
	var actionBody struct {
		Definition *struct {
			Transaction *platform.BusinessTxn `json:"transaction"`
		} `json:"definition"`
		Transaction *platform.BusinessTxn `json:"transaction"`
	}
	_ = json.Unmarshal(asset.Body, &actionBody)
	txnSpec := actionBody.Transaction
	if txnSpec == nil && actionBody.Definition != nil {
		txnSpec = actionBody.Definition.Transaction
	}
	if txnSpec != nil && action.Target != "" {
		WalkBusinessTxn(txnSpec, func(step platform.TxnStep) {
			if step.Request != nil && step.Request.Reply != "" {
				verb := step.Request.Reply[strings.LastIndex(step.Request.Reply, ".")+1:]
				add(platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetAction, Name: action.Target + "." + verb})
			}
		})
	}
	var relationship struct {
		Roles []string `json:"roles"`
	}
	_ = json.Unmarshal(asset.Body, &relationship)
	if relationship.Roles != nil {
		action.Roles = relationship.Roles
	}
	var confirmation struct {
		Bindings *struct {
			Parties []struct {
				Writeback           *platform.AssetRef `json:"writeback"`
				CompensateWriteback *platform.AssetRef `json:"compensateWriteback"`
				Source              *platform.AssetRef `json:"source"`
				Protocol            *platform.AssetRef `json:"protocol"`
				Action              *platform.AssetRef `json:"action"`
				CompensateAction    *platform.AssetRef `json:"compensateAction"`
			} `json:"parties"`
			OnComplete      platform.AssetRef  `json:"onComplete"`
			OnPartialAction *platform.AssetRef `json:"onPartialAction"`
		} `json:"confirmationBindings"`
	}
	if err := json.Unmarshal(asset.Body, &confirmation); err != nil {
		return nil, nil, err
	}
	if resolved := confirmation.Bindings; resolved != nil {
		add(resolved.OnComplete)
		if resolved.OnPartialAction != nil {
			add(*resolved.OnPartialAction)
		}
		for _, party := range resolved.Parties {
			for _, ref := range []*platform.AssetRef{party.Writeback, party.CompensateWriteback, party.Source, party.Protocol, party.Action, party.CompensateAction} {
				if ref != nil {
					add(*ref)
				}
			}
		}
	}
	var dispatch struct {
		Bindings *struct {
			Commands            map[string]platform.AssetRef `json:"commands"`
			Writeback           platform.AssetRef            `json:"writeback"`
			Target              platform.AssetRef            `json:"target"`
			CompensateWriteback *platform.AssetRef           `json:"compensateWriteback"`
			Source              *platform.AssetRef           `json:"source"`
			Observation         *platform.AssetRef           `json:"observation"`
			Record              *platform.AssetRef           `json:"record"`
		} `json:"dispatchBindings"`
	}
	if err := json.Unmarshal(asset.Body, &dispatch); err != nil {
		return nil, nil, err
	}
	if binding := dispatch.Bindings; binding != nil {
		for _, command := range binding.Commands {
			add(command)
		}
		add(binding.Writeback)
		add(binding.Target)
		for _, ref := range []*platform.AssetRef{binding.CompensateWriteback, binding.Source, binding.Observation, binding.Record} {
			if ref != nil {
				add(*ref)
			}
		}
	}
	var transaction struct {
		Bindings map[string]platform.AssetRef `json:"transactionBindings"`
	}
	if err := json.Unmarshal(asset.Body, &transaction); err != nil {
		return nil, nil, err
	}
	for _, ref := range transaction.Bindings {
		add(ref)
	}
	provides := []platform.Capability{{Kind: "action", Name: action.Schema, Side: platform.CapabilityAuthoritative, Roles: action.Roles, Inputs: runtimeprotocol.CapabilityFieldsSchema(action.Payload), Parameters: action.Payload}}
	return requires, provides, nil
}
