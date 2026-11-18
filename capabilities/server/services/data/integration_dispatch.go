package data

import (
	"encoding/json"
	"fmt"
	"net/url"
	ruleprotocol "platformserver/kernel/protocol"

	"reflect"
	"slices"
	"strings"

	"platformserver/kernel/records"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
	"platformserver/services/resources/publication"
)

func (o *Owner) DispatchEntity() platform.Entity {
	return platform.Entity{Type: ruleprotocol.DispatchType, Title: "Device dispatch", Plural: "Device dispatches", Model: platform.DispatchRun{}, Display: "subject",
		Description: "Declared commands, durable delivery attempts, status reads and authorized human intervention. Control loops and safety interlocks remain local to the equipment.",
		Scope: ScopeWithParticipants(func(value any) []string {
			run := value.(platform.DispatchRun)
			return append([]string{run.Member}, o.integration.Holding(App, run.Definition.ReviewerRole)...)
		}), Withheld: "withheld", Derived: []platform.Derivation{{From: "subject", Fields: []string{"reason", "input", "request", "source", "connection", "observation", "device", "attempts", "readback", "definition", "assets", "compensationCommand", "compensationRequest"}}}}
}

func DispatchActions() []platform.Action {
	return []platform.Action{
		{Schema: ruleprotocol.DispatchAnswerSchema, Target: ruleprotocol.DispatchType, Title: "Record dispatch status", Description: "Retain transport evidence and the declared status read.", Capability: "dispatches", Automation: true, Payload: []platform.Field{}},
		{Schema: ruleprotocol.DispatchTickSchema, Target: ruleprotocol.DispatchType, Title: "Advance dispatch work", Description: "Apply the business deadline and explicit intervention policy.", Capability: "dispatches", Automation: true, Payload: []platform.Field{}},
		{Schema: ruleprotocol.DispatchReviewSchema, Target: ruleprotocol.DispatchType, Title: "Review device dispatch", Description: "Resend, change target, mark complete or cancel with recorded evidence.", Capability: "dispatches", Roles: []string{platform.AnyMember}, Payload: []platform.Field{
			{Name: "outcome", Type: "string", Required: true, Choices: []string{"retry", "retarget", "complete", "cancel"}},
			{Name: "target", Type: "string", Description: "The replacement external connection"},
			{Name: "reason", Type: "string", Required: true, Description: "Evidence supporting the intervention"},
		}},
	}
}

func dispatchCommand(object ontology.Object, name string) (platform.DispatchCommand, bool) {
	i := slices.IndexFunc(object.DispatchCommands, func(command platform.DispatchCommand) bool { return command.Name == name })
	if i < 0 {
		return platform.DispatchCommand{}, false
	}
	return object.DispatchCommands[i], true
}

func (o *Owner) CheckDispatch(spec *platform.Dispatch, object ontology.Object, action ontology.Action, engines ...platform.RuleEngine) error {
	if spec == nil {
		return nil
	}
	command, ok := dispatchCommand(object, spec.Command)
	if !ok {
		return fmt.Errorf("Dispatch command is not declared by its subject")
	}
	if err := spec.Check(command); err != nil {
		return err
	}
	if action.Transaction.Confirmation != nil {
		return fmt.Errorf("A transaction declares either a confirmation or a device dispatch")
	}
	if spec.ReviewerRole != Builder && !slices.ContainsFunc(object.Access, func(access ontology.Access) bool { return access.Role == spec.ReviewerRole }) {
		return fmt.Errorf("The dispatch reviewer role is not declared")
	}
	info, _ := records.Describe(App, ontology.CompileObjectEntity(object, ontology.ObjectRuntimeEnvironment{}), func(reflect.Type) string { return "" })
	lookup := ontology.RuleLookup(object, App, Builder, o.integration.Lookup)
	if spec.Key != "$subject" {
		field, found := info.Field(strings.TrimPrefix(spec.Key, "record."))
		if !found || !slices.Contains([]string{"text", "reference"}, field.Type) || !field.ReadOnly {
			return fmt.Errorf("Dispatch business key must use the subject ID or a read-only text/reference field")
		}
	}
	used := map[string]bool{}
	scope := ontology.RuleSpec(object, App, action, action.Transaction.Reads)
	for _, set := range spec.Payload {
		if !publication.ValidName(set.Field) || used[set.Field] || set.Field == "id" || set.Field == "key" {
			return fmt.Errorf("Dispatch parameters must have distinct declared names")
		}
		used[set.Field] = true
		expr, err := ontology.DispatchParameter(set, scope)
		if err != nil {
			return err
		}
		if _, err := ruleprotocol.RuleSchema(expr, scope, lookup, engines...); err != nil {
			return fmt.Errorf("Dispatch parameter %s: %w", set.Field, err)
		}
	}
	if command.Compensate != "" {
		reverse, found := dispatchCommand(object, command.Compensate)
		if !found || reverse.Name == command.Name || reverse.Compensate != "" {
			return fmt.Errorf("Dispatch compensation must name a distinct, non-recursive command")
		}
	}
	if read := spec.Readback; read.Record != "" {
		entity, found := lookup(read.Record)
		if !found {
			return fmt.Errorf("Dispatch status record is unavailable")
		}
		for _, name := range []string{read.KeyField, read.ResultField, read.ReasonField} {
			if name == "" || name == "id" {
				continue
			}
			if _, found := entity.Field(name); !found {
				return fmt.Errorf("Dispatch status field is not declared")
			}
		}
	}
	if o.integration.App != nil {
		_, err := o.bindDispatch(*spec, object)
		return err
	}
	return nil
}

type dispatchBindings struct {
	Commands            map[string]platform.AssetRef `json:"commands"`
	Writeback           platform.AssetRef            `json:"writeback"`
	Target              platform.AssetRef            `json:"target"`
	CompensateWriteback *platform.AssetRef           `json:"compensateWriteback,omitempty"`
	Source              *platform.AssetRef           `json:"source,omitempty"`
	Observation         *platform.AssetRef           `json:"observation,omitempty"`
	Record              *platform.AssetRef           `json:"record,omitempty"`
}

func (o *Owner) bindDispatch(spec platform.Dispatch, object ontology.Object) (dispatchBindings, error) {
	out := dispatchBindings{Commands: map[string]platform.AssetRef{}}
	command, found := dispatchCommand(object, spec.Command)
	if !found {
		return out, fmt.Errorf("Dispatch command is not declared")
	}
	writeback := func(id string) (platform.AssetRef, error) {
		prospective, selected, err := o.Prospective(platform.AssetWriteback, id, o.integration.JointDeclarations())
		if err != nil {
			return platform.AssetRef{}, err
		}
		var fixed Writeback
		if selected {
			fixed = prospective.(Writeback)
		} else {
			record, known := o.integration.Record(WritebackType + "/" + id)
			w, valid := record.(Writeback)
			if !known || !valid {
				return platform.AssetRef{}, fmt.Errorf("Dispatch writeback is unavailable")
			}
			fixed, err = o.ExecutionWriteback(w)
			if err != nil {
				return platform.AssetRef{}, err
			}
		}
		if !selected && fixed.State != "published" || fixed.On != "dispatch" || fixed.Object != "build."+object.Name || len(fixed.Result) != 0 {
			return platform.AssetRef{}, fmt.Errorf("Dispatch needs a published dispatch-only writeback without answer field mutations")
		}
		path, err := url.Parse(fixed.Path)
		if err != nil || path.IsAbs() || path.Host != "" || path.User != nil || strings.HasPrefix(fixed.Path, "//") {
			return platform.AssetRef{}, fmt.Errorf("Dispatch command paths must be relative to the external connection")
		}
		return platform.AssetRef{App: App, Kind: platform.AssetWriteback, Name: fixed.Name}, nil
	}
	var err error
	for _, declared := range object.DispatchCommands {
		ref, err := writeback(declared.Writeback)
		if err != nil {
			return out, err
		}
		out.Commands[declared.Name] = ref
	}
	out.Writeback, err = writeback(command.Writeback)
	if err != nil {
		return out, err
	}
	if command.Compensate != "" {
		reverse, known := dispatchCommand(object, command.Compensate)
		if !known {
			return out, fmt.Errorf("Dispatch compensation command is unavailable")
		}
		ref, err := writeback(reverse.Writeback)
		if err != nil {
			return out, err
		}
		out.CompensateWriteback = &ref
	}
	record, known := o.integration.Record(ConnectionType + "/" + spec.Target)
	connection, valid := record.(Connection)
	if !known || !valid {
		return out, fmt.Errorf("Dispatch target connection is unavailable")
	}
	connection, err = o.ConnectionConfiguration(connection)
	if err != nil {
		return out, err
	}
	if !slices.Contains([]string{"http", "odata"}, connection.Kind) {
		return out, fmt.Errorf("Dispatch target must be an HTTP or OData connection")
	}
	out.Target = platform.AssetRef{App: App, Kind: platform.AssetConnection, Name: connection.Name}
	if spec.Readback.Source != "" {
		prospective, selected, err := o.Prospective(platform.AssetSource, spec.Readback.Source, o.integration.JointDeclarations())
		if err != nil {
			return out, err
		}
		var source Source
		if selected {
			source = prospective.(Source)
		} else {
			record, known := o.integration.Record(SourceType + "/" + spec.Readback.Source)
			var valid bool
			source, valid = record.(Source)
			if !known || !valid {
				return out, fmt.Errorf("Dispatch status source is unavailable")
			}
			source, err = o.ExecutionSource(source)
			if err != nil {
				return out, err
			}
		}
		if !selected && source.State != "published" || source.Connection == "" || source.Header != "" {
			return out, fmt.Errorf("Dispatch status source must be published and use a controlled connection")
		}
		path, err := url.Parse(source.URL)
		if err != nil || path.IsAbs() || path.Host != "" || path.User != nil || strings.HasPrefix(source.URL, "//") {
			return out, fmt.Errorf("Dispatch status paths must be relative to the source connection")
		}
		out.Source = &platform.AssetRef{App: App, Kind: platform.AssetSource, Name: source.Name}
	}
	if spec.Readback.Observation != "" {
		prospective, selected, err := o.Prospective(platform.AssetObservation, spec.Readback.Observation, o.integration.JointDeclarations())
		if err != nil {
			return out, err
		}
		var definition platform.ObservationDefinition
		if selected {
			definition, err = o.ObservationDefinition(prospective.(Observation))
		} else {
			record, known := o.integration.Record(ObservationType + "/" + spec.Readback.Observation)
			observation, valid := record.(Observation)
			if !known || !valid {
				return out, fmt.Errorf("Dispatch observation is unavailable")
			}
			definition, err = o.ExecutionObservation(observation)
		}
		if err != nil {
			return out, err
		}
		if definition.Object.Name != "build."+object.Name {
			return out, fmt.Errorf("Dispatch observation must read its business subject")
		}
		out.Observation = &platform.AssetRef{App: App, Kind: platform.AssetObservation, Name: definition.Name}
	}
	if spec.Readback.Record != "" {
		owner, _, _ := strings.Cut(spec.Readback.Record, ".")
		out.Record = &platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: spec.Readback.Record}
	}
	return out, nil
}

func (o *Owner) BindDispatchAssets(assets []platform.ReleaseAsset) ([]platform.ReleaseAsset, error) {
	bindings := map[string]dispatchBindings{}
	effects := map[string]map[string]platform.AssetRef{}
	for i, asset := range assets {
		if asset.Ref.App != App || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object ontology.Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return nil, err
		}
		for _, action := range object.Actions {
			if action.Transaction != nil {
				refs, err := o.txnEffectBindings(action.Transaction)
				if err != nil {
					return nil, err
				}
				if len(refs) > 0 {
					effects[asset.Ref.Name+"."+action.Name] = refs
				}
			}
			if action.Transaction == nil || action.Transaction.Dispatch == nil {
				continue
			}
			binding, err := o.bindDispatch(*action.Transaction.Dispatch, object)
			if err != nil {
				return nil, err
			}
			bindings[asset.Ref.Name+"."+action.Name] = binding
		}
		if object.CreateTransaction != nil {
			refs, err := o.txnEffectBindings(object.CreateTransaction)
			if err != nil {
				return nil, err
			}
			if len(refs) > 0 {
				effects[asset.Ref.Name+".create"] = refs
			}
		}
		if objectHasIntegrations(object) {
			if err := ontology.MapObjectIntegrations(&object, o.IntegrationName); err != nil {
				return nil, err
			}
			var err error
			asset.Body, err = ontology.DefinitionBody(object, asset.Ref.Name)
			if err != nil {
				return nil, err
			}
			assets[i], err = o.resolver.WithDependencies(asset)
			if err != nil {
				return nil, err
			}
		}
	}
	for i, asset := range assets {
		binding, known := bindings[asset.Ref.Name]
		refs := effects[asset.Ref.Name]
		if asset.Ref.Kind != platform.AssetAction || !known && len(refs) == 0 {
			continue
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(asset.Body, &body); err != nil {
			return nil, err
		}
		if known {
			body["dispatchBindings"] = platform.Raw(binding)
		}
		if len(refs) > 0 {
			body["transactionBindings"] = platform.Raw(refs)
		}
		asset.Body = platform.Raw(body)
		var err error
		assets[i], err = o.resolver.WithDependencies(asset)
		if err != nil {
			return nil, err
		}
	}
	return assets, nil
}

func objectHasIntegrations(object ontology.Object) bool {
	found := len(object.DispatchCommands) > 0
	visit := func(txn *platform.BusinessTxn) {
		if txn != nil {
			ontology.WalkBusinessTxn(txn, func(step platform.TxnStep) { found = found || step.Effect != nil })
		}
	}
	visit(object.CreateTransaction)
	for _, action := range object.Actions {
		visit(action.Transaction)
	}
	return found
}

func (o *Owner) PortableObject(object ontology.Object) (ontology.Object, error) {
	return o.dispatchSchemaObject(object)
}

func (o *Owner) txnEffectBindings(txn *platform.BusinessTxn) (map[string]platform.AssetRef, error) {
	refs := map[string]platform.AssetRef{}
	var failure error
	ontology.WalkBusinessTxn(txn, func(step platform.TxnStep) {
		if failure != nil || step.Effect == nil {
			return
		}
		name, err := o.IntegrationName(platform.AssetWriteback, step.Effect.Writeback)
		if err != nil {
			failure = err
			return
		}
		refs[step.Name] = platform.AssetRef{App: App, Kind: platform.AssetWriteback, Name: name}
	})
	return refs, failure
}

func (o *Owner) dispatchSchemaObject(object ontology.Object) (ontology.Object, error) {
	if !objectHasIntegrations(object) {
		return object, nil
	}
	var portable ontology.Object
	if err := json.Unmarshal(platform.Raw(object), &portable); err != nil {
		return object, err
	}
	if err := ontology.MapObjectIntegrations(&portable, o.IntegrationName); err != nil {
		return object, err
	}
	return portable, nil
}
