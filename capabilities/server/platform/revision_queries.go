package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

func checkFrozenQueries(original Page, assets map[AssetRef]ReleaseAsset) error {
	page, err := original.Safety()
	if err != nil {
		return err
	}
	lookup := func(typ string) (EntityInfo, bool) {
		asset, ok := assets[ModelObject(typ)]
		if !ok {
			for ref, value := range assets {
				if ref.Kind == AssetObject && ref.Name == typ {
					asset = value
					ok = true
					break
				}
			}
		}
		info, err := queryObjectDescriptor(asset.Body)
		return ConstraintEntity(info), ok && err == nil
	}
	criteria := func(ref AssetRef) (*RuleCriteria, bool) {
		asset, ok := assets[ref]
		action, err := releaseActionDescriptor(asset.Body)
		if !ok || err != nil {
			return nil, false
		}
		reads := RuleReadSpec{Object: action.Target, Inputs: map[string]ExprType{}}
		mapped := map[string]bool{}
		for _, binding := range page.DataBindings {
			for _, model := range binding.ModelBindings() {
				if model.Options != nil && model.Options.Rule != nil && model.Options.Rule.Action == ref {
					for name := range model.Options.Rule.Inputs {
						mapped[name] = true
					}
				}
			}
		}
		for _, field := range action.Payload {
			if mapped[field.Name] {
				reads.Inputs[field.Name] = ExprType{Kind: RuleFieldKind(field.Type), Object: field.Ref}
			}
		}
		return &RuleCriteria{Reads: reads}, true
	}
	for _, binding := range page.DataBindings {
		if err := page.CheckDataBinding(binding, lookup, criteria); err != nil {
			return err
		}
		for _, model := range binding.ModelBindings() {
			sets := []*ModelRecordSet{model.Set}
			if model.Options != nil {
				sets = append(sets, model.Options.Set)
			}
			for _, set := range sets {
				if set == nil || set.Query == nil {
					continue
				}
				asset, ok := assets[set.Query.Ref]
				var query NamedQuery
				if !ok || asset.SourceVersion != set.Query.SourceVersion || json.Unmarshal(asset.Body, &query) != nil || query.Object != model.Type {
					return fmt.Errorf("binding %s frozen model source differs", binding.ID)
				}
			}
		}
		for _, resource := range binding.Resources {
			if resource.Asset == nil {
				continue
			}
			asset, ok := assets[resource.Asset.Ref]
			if !ok || resource.Asset.SourceVersion != "" && asset.SourceVersion != resource.Asset.SourceVersion {
				return fmt.Errorf("binding %s exact resource is unavailable", binding.ID)
			}
		}
	}
	for _, trigger := range page.ActionTriggers {
		object := page.Object.Name
		for _, binding := range page.DataBindings {
			if binding.ID == trigger.Binding {
				object = binding.ObjectRef(page).Name
			}
		}
		for _, ref := range trigger.Actions {
			asset, ok := assets[ref]
			action, err := releaseActionDescriptor(asset.Body)
			if !ok || err != nil || StaticControlAction(action) || action.Target != object {
				return fmt.Errorf("page action owner or target is unavailable")
			}
		}
		if trigger.InlineEdit != nil {
			asset, ok := assets[trigger.InlineEdit.Action]
			action, err := releaseActionDescriptor(asset.Body)
			info, found := lookup(object)
			if !ok || !found || err != nil || (Section{InlineEdit: trigger.InlineEdit}).CheckTableEdit(info, action) != nil {
				return fmt.Errorf("page edit fields differ from original action")
			}
		}
		if len(trigger.Defaults) > 0 {
			if len(trigger.Actions) != 1 {
				return fmt.Errorf("page parameters need one original action")
			}
			action, err := releaseActionDescriptor(assets[trigger.Actions[0]].Body)
			if err != nil {
				return err
			}
			info, _ := lookup(object)
			for _, mapping := range trigger.Defaults {
				field, ok := info.Field(mapping.Field)
				index := slices.IndexFunc(action.Payload, func(f Field) bool { return f.Name == mapping.Parameter })
				if !ok || index < 0 || !ActionParameterCompatible(field, action.Payload[index]) {
					return fmt.Errorf("page action parameter differs from original readable field")
				}
			}
		}
		if trigger.Function != nil {
			asset, ok := assets[trigger.Function.Ref]
			var function AIFunction
			if !ok || asset.SourceVersion != trigger.Function.SourceVersion || json.Unmarshal(asset.Body, &function) != nil || function.Object != object {
				return fmt.Errorf("page function original source is unavailable")
			}
		}
		if trigger.Operation != nil {
			asset, ok := assets[trigger.Operation.Ref]
			var operation Operation
			if !ok || asset.SourceVersion != trigger.Operation.SourceVersion || json.Unmarshal(asset.Body, &operation) != nil || operation.Check() != nil {
				return fmt.Errorf("page operation original source is unavailable")
			}
		}
	}
	if err := page.InputSchema.CheckVariables(); err != nil {
		return err
	}
	if err := page.InputSchema.CheckQuerySets(); err != nil {
		return err
	}
	for id, compute := range page.ComputeResources() {
		asset, ok := assets[compute.Operation.Ref]
		var op Operation
		object, found := lookup(page.RecordResourceObject(compute.RecordVariable).Name)
		if !ok || !found || asset.SourceVersion != compute.Operation.SourceVersion || json.Unmarshal(asset.Body, &op) != nil || page.CheckComputeResource(compute, object, op, page.InputSchema.Variables[id].Type, lookup) != nil {
			return fmt.Errorf("frozen compute resource contract is unavailable")
		}
	}
	for _, v := range page.InputSchema.Variables {
		if v.Source == nil {
			continue
		}
		if v.Mode == "property" && v.Source.Object != nil {
			info, ok := lookup(v.Source.Object.Name)
			if !ok || page.CheckPropertySchema(v, info) != nil {
				return fmt.Errorf("frozen property schema is unavailable")
			}
		}
		if v.Scope == "application" && v.Mode == "resource" && v.Type == "filter" && v.Source.Object != nil {
			info, ok := lookup(v.Source.Object.Name)
			if !ok || CheckFilterSchema(*v.Source, info) != nil {
				return fmt.Errorf("frozen application filter schema is unavailable")
			}
		}
	}
	namedSources := map[AssetBinding]NamedQuery{}
	for id, plan := range page.InputSchema.Queries {
		if plan.Interface != "" {
			named, err := frozenPageInterfaceQuery(plan, assets)
			if err != nil {
				return err
			}
			if err := page.CheckInterfaceQuery(plan, named); err != nil {
				return err
			}
			continue
		}
		asset, ok := assets[plan.Object]
		object, err := queryObjectDescriptor(asset.Body)
		if !ok || err != nil {
			return fmt.Errorf("page query %s object is unavailable", id)
		}
		var named *Definition
		if plan.Query != nil {
			asset, ok := assets[plan.Query.Ref]
			if !ok || asset.SourceVersion != plan.Query.SourceVersion {
				return fmt.Errorf("page query %s exact source is unavailable", id)
			}
			if plan.Query.Ref.Kind == AssetLinkType {
				var link LinkType
				if json.Unmarshal(asset.Body, &link) != nil {
					return fmt.Errorf("frozen link source is invalid")
				}
				parentAsset, pok := assets[link.Parent]
				childAsset, cok := assets[link.Child]
				parent, pe := queryObjectDescriptor(parentAsset.Body)
				child, ce := queryObjectDescriptor(childAsset.Body)
				parent.App = link.Parent.App
				child.App = link.Child.App
				if !pok || !cok || pe != nil || ce != nil || link.CheckSchema(parent, child) != nil {
					return fmt.Errorf("frozen link endpoints are unavailable")
				}
				named = &Definition{Ref: asset.Ref, Version: asset.SourceVersion, LinkType: &link}
			} else {
				var query NamedQuery
				if json.Unmarshal(asset.Body, &query) != nil {
					return fmt.Errorf("frozen named query is invalid")
				}
				named = &Definition{Ref: asset.Ref, Version: asset.SourceVersion, Query: &query}
				namedSources[*plan.Query] = query
			}
		}
		for _, v := range page.InputSchema.Variables {
			if v.Source != nil && v.Source.Query == id {
				if err := page.InputSchema.CheckAggregateScalar(v, object); err != nil {
					return err
				}
			}
		}
		if err := page.CheckBoundQuery(id, named); err != nil {
			return err
		}
		if err := page.CheckQuerySchema(plan, object, named); err != nil {
			return err
		}
	}
	return page.CheckQuerySetConditions(namedSources)
}

func queryObjectDescriptor(body []byte) (EntityInfo, error) {
	var shape struct {
		Type            string                       `json:"type"`
		Implements      []string                     `json:"implements"`
		Extends         string                       `json:"extends"`
		ExtensionSource *ExtensionSource             `json:"extensionSource"`
		Entity          *EntityInfo                  `json:"entity"`
		ObjectView      *ObjectView                  `json:"objectView,omitempty"`
		Fields          []map[string]json.RawMessage `json:"fields"`
		Lifecycle       *LifecycleInfo               `json:"lifecycle"`
		States          []State                      `json:"states"`
		Actions         []struct {
			Transaction *BusinessTxn `json:"transaction"`
			Name        string       `json:"name"`
			Title       string       `json:"title"`
			From        []string     `json:"from"`
			To          string       `json:"to"`
			ToInput     string       `json:"toInput"`
			Inputs      []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Required bool   `json:"required"`
				Choices  string `json:"choices"`
			} `json:"inputs"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(body, &shape); err != nil {
		return EntityInfo{}, err
	}
	if shape.Entity != nil {
		if shape.Entity.Type != shape.Type {
			return EntityInfo{}, fmt.Errorf("frozen native object identity differs")
		}
		return *shape.Entity, nil
	}
	var fields []FieldInfo
	for _, raw := range shape.Fields {
		if choicesRaw, ok := raw["choices"]; ok {
			var source string
			if json.Unmarshal(choicesRaw, &source) == nil {
				options := []string{}
				for _, part := range strings.Split(source, ",") {
					if value := strings.TrimSpace(part); value != "" {
						options = append(options, value)
					}
				}
				raw["choices"], _ = json.Marshal(options)
			}
		}
		if whenRaw, ok := raw["when"]; ok {
			var source string
			if json.Unmarshal(whenRaw, &source) == nil {
				condition, err := ParseWhen(source)
				if err != nil {
					return EntityInfo{}, err
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
			return EntityInfo{}, err
		}
		var field FieldInfo
		if err := json.Unmarshal(encoded, &field); err != nil {
			return EntityInfo{}, err
		}
		fields = append(fields, field)
	}
	owner, _, _ := strings.Cut(shape.Type, ".")
	if shape.Lifecycle == nil && len(shape.States) > 0 {
		l := LifecycleInfo{Field: "state", Initial: shape.States[0].Name, States: shape.States}
		choices := []string{}
		for _, s := range shape.States {
			choices = append(choices, s.Name)
		}
		fields = append(fields, FieldInfo{Name: "state", Title: "State", Type: "choice", ReadOnly: true, Choices: choices})
		for _, a := range shape.Actions {
			inputs := make([]Field, 0, len(a.Inputs))
			for _, input := range a.Inputs {
				kind := input.Type
				if kind == "choice" {
					kind = "string"
				}
				var options []string
				for _, option := range strings.Split(input.Choices, ",") {
					if value := strings.TrimSpace(option); value != "" {
						options = append(options, value)
					}
				}
				inputs = append(inputs, Field{Name: input.Name, Type: kind, Required: input.Required, Choices: options})
			}
			if to, input := transactionDestination(shape.Type, "state", a.Transaction, inputs); len(to) > 0 {
				if slices.ContainsFunc(to, func(state string) bool { return !slices.Contains(choices, state) }) {
					return EntityInfo{}, fmt.Errorf("frozen object transaction destination differs")
				}
				l.Transitions = append(l.Transitions, TransitionInfo{Name: a.Name, Schema: shape.Type + "." + a.Name, Title: a.Title, From: a.From, To: to, ToInput: input})
				continue
			}
			to := []string{}
			if a.To != "" {
				to = append(to, a.To)
			}
			if a.ToInput != "" {
				to = nil
				valid := false
				for _, input := range a.Inputs {
					if input.Name == a.ToInput && input.Type == "choice" && input.Required {
						valid = true
						for _, raw := range strings.Split(input.Choices, ",") {
							state := strings.TrimSpace(raw)
							if state != "" {
								to = append(to, state)
							}
						}
					}
				}
				if !valid || a.To != "" || len(to) == 0 || slices.ContainsFunc(to, func(state string) bool { return !slices.Contains(choices, state) }) {
					return EntityInfo{}, fmt.Errorf("frozen object destination input differs")
				}
			} else if a.To == "" {
				to = slices.Clone(a.From)
			}
			l.Transitions = append(l.Transitions, TransitionInfo{Name: a.Name, Schema: shape.Type + "." + a.Name, Title: a.Title, From: a.From, To: to, ToInput: a.ToInput})
		}
		shape.Lifecycle = &l
	}
	return EntityInfo{App: owner, Type: shape.Type, ObjectView: shape.ObjectView, Standard: []string{shape.Type + ".create", shape.Type + ".edit", shape.Type + ".archive"}, Fields: fields, Lifecycle: shape.Lifecycle, Implements: shape.Implements, Extends: shape.Extends, ExtensionSource: shape.ExtensionSource}, nil
}

func releaseActionDescriptor(body json.RawMessage) (Action, error) {
	var flat Action
	if err := json.Unmarshal(body, &flat); err != nil {
		return Action{}, err
	}
	var envelope struct {
		Action *Action `json:"action"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Action{}, err
	}
	if envelope.Action != nil {
		if envelope.Action.Schema != flat.Schema || envelope.Action.Target != flat.Target {
			return Action{}, fmt.Errorf("frozen action declaration identity differs")
		}
		return *envelope.Action, nil
	}
	return flat, nil
}
