package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

func (b *Build) dispatchEntity() platform.Entity {
	return platform.Entity{Type: platform.DispatchType, Title: "Device dispatch", Plural: "Device dispatches", Model: platform.DispatchRun{}, Display: "subject",
		Description: "Declared commands, durable delivery attempts, status reads and authorized human intervention. Control loops and safety interlocks remain local to the equipment.",
		Scope: platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}, Participants: func(value any) []string {
			run := value.(platform.DispatchRun)
			return append([]string{run.Member}, b.host.(host.ConfirmationHost).Holding(ID, run.Definition.ReviewerRole)...)
		}}, Withheld: "withheld", Derived: []platform.Derivation{{From: "subject", Fields: []string{"reason", "input", "request", "source", "connection", "observation", "device", "attempts", "readback", "definition", "assets", "compensationCommand", "compensationRequest"}}}}
}

func dispatchActions() []platform.Action {
	return []platform.Action{
		{Schema: platform.DispatchAnswerSchema, Target: platform.DispatchType, Title: "Record dispatch status", Description: "Retain transport evidence and the declared status read.", Capability: "dispatches", Automation: true, Payload: []platform.Field{}},
		{Schema: platform.DispatchTickSchema, Target: platform.DispatchType, Title: "Advance dispatch work", Description: "Apply the business deadline and explicit intervention policy.", Capability: "dispatches", Automation: true, Payload: []platform.Field{}},
		{Schema: platform.DispatchReviewSchema, Target: platform.DispatchType, Title: "Review device dispatch", Description: "Resend, change target, mark complete or cancel with recorded evidence.", Capability: "dispatches", Roles: []string{platform.AnyMember}, Payload: []platform.Field{
			{Name: "outcome", Type: "string", Required: true, Choices: []string{"retry", "retarget", "complete", "cancel"}},
			{Name: "target", Type: "string", Description: "The replacement external connection"},
			{Name: "reason", Type: "string", Required: true, Description: "Evidence supporting the intervention"},
		}},
	}
}

func dispatchCommand(object Object, name string) (platform.DispatchCommand, bool) {
	i := slices.IndexFunc(object.DispatchCommands, func(command platform.DispatchCommand) bool { return command.Name == name })
	if i < 0 {
		return platform.DispatchCommand{}, false
	}
	return object.DispatchCommands[i], true
}

func (b *Build) checkDispatch(spec *platform.Dispatch, object Object, action Action) error {
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
	if spec.ReviewerRole != Builder && !slices.ContainsFunc(object.Access, func(access Access) bool { return access.Role == spec.ReviewerRole }) {
		return fmt.Errorf("The dispatch reviewer role is not declared")
	}
	info, _ := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
	lookup := ruleLookup(object, b.conditionLookup)
	if spec.Key != "$subject" {
		field, found := info.Field(strings.TrimPrefix(spec.Key, "record."))
		if !found || !slices.Contains([]string{"text", "reference"}, field.Type) || !field.ReadOnly {
			return fmt.Errorf("Dispatch business key must use the subject ID or a read-only text/reference field")
		}
	}
	used := map[string]bool{}
	scope := ruleSpec(object, action, action.Transaction.Reads)
	for _, set := range spec.Payload {
		if !named(set.Field) || used[set.Field] || set.Field == "id" || set.Field == "key" {
			return fmt.Errorf("Dispatch parameters must have distinct declared names")
		}
		used[set.Field] = true
		expr, err := dispatchParameter(set, scope)
		if err != nil {
			return err
		}
		if _, err := platform.RuleSchema(expr, scope, lookup); err != nil {
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
	if b.host != nil {
		_, err := b.bindDispatch(*spec, object)
		return err
	}
	return nil
}

func dispatchParameter(set platform.TxnSet, scope platform.RuleReadSpec) (platform.Expr, error) {
	kind := "text"
	if set.Expr != nil {
		kind = set.Expr.Kind
	} else if input, found := scope.Inputs[strings.TrimPrefix(set.From, "input.")]; found {
		kind = input.Kind
	}
	if set.Expr == nil && strings.HasPrefix(set.From, "input.") {
		set.From = strings.TrimPrefix(set.From, "input.")
	}
	if slices.Contains([]string{"set", "object", "json", ""}, kind) {
		return platform.Expr{}, fmt.Errorf("Dispatch parameters require scalar expressions")
	}
	return assignmentExpr(kind, set, scope)
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

func (b *Build) bindDispatch(spec platform.Dispatch, object Object) (dispatchBindings, error) {
	out := dispatchBindings{Commands: map[string]platform.AssetRef{}}
	command, found := dispatchCommand(object, spec.Command)
	if !found {
		return out, fmt.Errorf("Dispatch command is not declared")
	}
	writeback := func(id string) (platform.AssetRef, error) {
		record, known := b.host.Record(WritebackType + "/" + id)
		w, valid := record.(Writeback)
		if !known || !valid {
			return platform.AssetRef{}, fmt.Errorf("Dispatch writeback is unavailable")
		}
		fixed, err := b.ExecutionWriteback(w)
		if err != nil {
			return platform.AssetRef{}, err
		}
		if fixed.State != "published" || fixed.On != "dispatch" || fixed.Object != "build."+object.Name || len(fixed.Result) != 0 {
			return platform.AssetRef{}, fmt.Errorf("Dispatch needs a published dispatch-only writeback without answer field mutations")
		}
		path, err := url.Parse(fixed.Path)
		if err != nil || path.IsAbs() || path.Host != "" || path.User != nil || strings.HasPrefix(fixed.Path, "//") {
			return platform.AssetRef{}, fmt.Errorf("Dispatch command paths must be relative to the external connection")
		}
		return platform.AssetRef{App: ID, Kind: platform.AssetWriteback, Name: fixed.Name}, nil
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
	record, known := b.host.Record(ConnectionType + "/" + spec.Target)
	connection, valid := record.(Connection)
	if !known || !valid {
		return out, fmt.Errorf("Dispatch target connection is unavailable")
	}
	connection, err = b.ExecutionConnection(connection)
	if err != nil {
		return out, err
	}
	if connection.State != "ready" || !slices.Contains([]string{"http", "odata"}, connection.Kind) {
		return out, fmt.Errorf("Dispatch target must be a ready external connection")
	}
	out.Target = platform.AssetRef{App: ID, Kind: platform.AssetConnection, Name: connection.Name}
	if spec.Readback.Source != "" {
		record, known := b.host.Record(SourceType + "/" + spec.Readback.Source)
		source, valid := record.(Source)
		if !known || !valid {
			return out, fmt.Errorf("Dispatch status source is unavailable")
		}
		source, err = b.ExecutionSource(source)
		if err != nil {
			return out, err
		}
		if source.State != "published" || source.Connection == "" || source.Header != "" {
			return out, fmt.Errorf("Dispatch status source must be published and use a controlled connection")
		}
		path, err := url.Parse(source.URL)
		if err != nil || path.IsAbs() || path.Host != "" || path.User != nil || strings.HasPrefix(source.URL, "//") {
			return out, fmt.Errorf("Dispatch status paths must be relative to the source connection")
		}
		out.Source = &platform.AssetRef{App: ID, Kind: platform.AssetSource, Name: source.Name}
	}
	if spec.Readback.Observation != "" {
		record, known := b.host.Record(ObservationType + "/" + spec.Readback.Observation)
		observation, valid := record.(Observation)
		if !known || !valid {
			return out, fmt.Errorf("Dispatch observation is unavailable")
		}
		definition, err := b.ExecutionObservation(observation)
		if err != nil {
			return out, err
		}
		if definition.Object.Name != "build."+object.Name {
			return out, fmt.Errorf("Dispatch observation must read its business subject")
		}
		out.Observation = &platform.AssetRef{App: ID, Kind: platform.AssetObservation, Name: definition.Name}
	}
	if spec.Readback.Record != "" {
		owner, _, _ := strings.Cut(spec.Readback.Record, ".")
		out.Record = &platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: spec.Readback.Record}
	}
	return out, nil
}

func (b *Build) bindDispatchAssets(assets []platform.ReleaseAsset) ([]platform.ReleaseAsset, error) {
	bindings := map[string]dispatchBindings{}
	for i, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return nil, err
		}
		for _, action := range object.Actions {
			if action.Transaction == nil || action.Transaction.Dispatch == nil {
				continue
			}
			binding, err := b.bindDispatch(*action.Transaction.Dispatch, object)
			if err != nil {
				return nil, err
			}
			bindings[asset.Ref.Name+"."+action.Name] = binding
		}
		if len(object.DispatchCommands) > 0 {
			if err := mapDispatchIntegrations(&object, b.integrationName); err != nil {
				return nil, err
			}
			var err error
			asset.Body, err = definitionBody(object, asset.Ref.Name)
			if err != nil {
				return nil, err
			}
			assets[i], err = platform.WithDependencies(asset)
			if err != nil {
				return nil, err
			}
		}
	}
	for i, asset := range assets {
		binding, known := bindings[asset.Ref.Name]
		if asset.Ref.Kind != platform.AssetAction || !known {
			continue
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(asset.Body, &body); err != nil {
			return nil, err
		}
		body["dispatchBindings"] = platform.Raw(binding)
		asset.Body = platform.Raw(body)
		var err error
		assets[i], err = platform.WithDependencies(asset)
		if err != nil {
			return nil, err
		}
	}
	return assets, nil
}

// Portable objects retain logical integration names. Installation and retained
// execution resolve those names through the same target inventory as sources
// and writebacks, without changing the candidate's frozen bytes.
func mapDispatchIntegrations(object *Object, resolve func(platform.AssetKind, string) (string, error)) error {
	for i := range object.DispatchCommands {
		name, err := resolve(platform.AssetWriteback, object.DispatchCommands[i].Writeback)
		if err != nil {
			return err
		}
		object.DispatchCommands[i].Writeback = name
	}
	for _, action := range object.Actions {
		if action.Transaction == nil || action.Transaction.Dispatch == nil {
			continue
		}
		dispatch := action.Transaction.Dispatch
		for _, item := range []struct {
			kind  platform.AssetKind
			value *string
		}{
			{platform.AssetConnection, &dispatch.Target},
			{platform.AssetSource, &dispatch.Readback.Source},
			{platform.AssetObservation, &dispatch.Readback.Observation},
		} {
			if *item.value == "" {
				continue
			}
			value, err := resolve(item.kind, *item.value)
			if err != nil {
				return err
			}
			*item.value = value
		}
	}
	return nil
}

func (b *Build) dispatchSchemaObject(object Object) (Object, error) {
	if len(object.DispatchCommands) == 0 {
		return object, nil
	}
	var portable Object
	if err := json.Unmarshal(platform.Raw(object), &portable); err != nil {
		return object, err
	}
	if err := mapDispatchIntegrations(&portable, b.integrationName); err != nil {
		return object, err
	}
	return portable, nil
}

func (b *Build) openDispatch(c platform.Caller, receipt *pb.ChangeRecord, txn *platform.BusinessTxn, scope platform.RuleReadSpec, subject map[string]any, values map[string]any, at time.Time) *kernel.Error {
	if txn.Dispatch == nil {
		return nil
	}
	transport := c.DispatchTransport()
	if transport == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Dispatch transport is unavailable")
	}
	if c.Simulating() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Device dispatch does not execute in isolated trials")
	}
	spec := *txn.Dispatch
	release, assets, err := transport.FreezeDispatch(c, txn.Subject, receipt.GetSubmission().GetSchema().GetName(), spec, at)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	var object Object
	for _, asset := range assets {
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetObject && asset.Ref.Name == txn.Subject {
			if err := json.Unmarshal(asset.Body, &object); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Dispatch subject publication is invalid")
			}
			break
		}
	}
	if err := mapDispatchIntegrations(&object, b.integrationID); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	command, known := dispatchCommand(object, spec.Command)
	if !known {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Dispatch command is absent from its frozen publication")
	}
	key := receipt.GetSubmission().GetTarget().GetId()
	if spec.Key != "$subject" {
		raw, _, problem := c.ReadRecordPath(txn.Subject, key, strings.Split(strings.TrimPrefix(spec.Key, "record."), "."), at)
		if problem != nil {
			return problem
		}
		if json.Unmarshal(raw, &key) != nil || strings.TrimSpace(key) == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch business key is missing")
		}
	}
	digest := sha256.Sum256([]byte(txn.Subject + ":" + command.Name + ":" + key))
	id := "dispatch:" + hex.EncodeToString(digest[:16])
	if _, exists := platform.Get[platform.DispatchRun](c, id); exists {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This business key already has a dispatch; review the existing command")
	}
	payload := map[string]any{}
	for _, set := range spec.Payload {
		expr, err := dispatchParameter(set, scope)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		value, problem := ruleAssignment(expr, scope, c, subject, values, at, b.conditionLookup)
		if problem != nil {
			return problem
		}
		payload[set.Field] = value
	}
	payload["key"] = key
	run := platform.DispatchRun{Record: platform.Record{ID: id}, Subject: txn.Subject + "/" + receipt.GetSubmission().GetTarget().GetId(), Action: receipt.GetSubmission().GetSchema().GetName(), Member: c.ID, Command: command, Definition: spec, Target: spec.Target, Key: key, State: "pending", StartedAt: at, StateAt: at, Deadline: at.Add(time.Duration(spec.DeadlineSeconds) * time.Second), ReadDue: at, Release: release, Assets: assets, Input: platform.Raw(payload), Readback: []platform.DispatchReading{}, Attempts: []platform.DispatchAttempt{}, Reviewers: []string{}}
	run.Reviewers = append(run.Reviewers, b.host.(host.ConfirmationHost).Holding(ID, spec.ReviewerRole)...)
	connection := func(id string) (Connection, error) {
		conn, found := platform.Get[Connection](c, id)
		if !found {
			return Connection{}, fmt.Errorf("Dispatch connection is unavailable")
		}
		return b.ExecutionConnection(conn)
	}
	prepare := func(command platform.DispatchCommand) (json.RawMessage, error) {
		w, found := platform.Get[Writeback](c, command.Writeback)
		if !found {
			return nil, fmt.Errorf("Dispatch writeback is unavailable")
		}
		w, err = b.ExecutionWriteback(w)
		if err != nil {
			return nil, err
		}
		conn, err := connection(spec.Target)
		if err != nil {
			return nil, err
		}
		requestInput := run.Input
		if command.Name != run.Command.Name {
			reverseInput := make(map[string]any, len(payload))
			for name, value := range payload {
				reverseInput[name] = value
			}
			reverseInput["key"] = run.Key + ":compensate"
			requestInput = platform.Raw(reverseInput)
		}
		req, err := w.Request(conn, receipt.GetSubmission().GetTarget().GetId(), requestInput)
		if err != nil {
			return nil, err
		}
		req.Dispatch, req.SafeRetry = id, command.SafeRetry()
		return platform.Raw(req), nil
	}
	run.Request, err = prepare(command)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	if command.Compensate != "" {
		reverse, _ := dispatchCommand(object, command.Compensate)
		run.CompensationCommand = &reverse
		run.CompensationRequest, err = prepare(reverse)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
	}
	if spec.Readback.Source != "" {
		source, found := platform.Get[Source](c, spec.Readback.Source)
		if !found {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch status source is unavailable")
		}
		source, err = b.ExecutionSource(source)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		conn, err := connection(source.Connection)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		source.Cursor = ""
		run.Source, run.Connection = platform.Raw(source), platform.Raw(conn)
	}
	if spec.Readback.Observation != "" {
		observation, found := platform.Get[Observation](c, spec.Readback.Observation)
		if !found {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch observation is unavailable")
		}
		definition, err := b.ExecutionObservation(observation)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		conn, err := connection(observation.Connection)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		device, _, problem := c.ReadRecordPath(txn.Subject, receipt.GetSubmission().GetTarget().GetId(), []string{definition.DeviceField}, at)
		if problem != nil {
			return problem
		}
		if json.Unmarshal(device, &run.Device) != nil || run.Device == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch observation device is missing")
		}
		run.Observation, run.Connection = platform.Raw(definition), platform.Raw(conn)
	}
	if err := b.queueDispatch(c, receipt, &run, at); err != nil {
		return err
	}
	return c.Put(receipt, run)
}

func (b *Build) queueDispatch(c platform.Caller, receipt *pb.ChangeRecord, run *platform.DispatchRun, at time.Time) *kernel.Error {
	run.Attempt++
	run.Intent = fmt.Sprintf("%s:%s:dispatch:%s:%d:%s%s", c.Tenant, ID, run.ID, run.Attempt, WritebackEndpoint, run.Target)
	run.Attempts = append(run.Attempts, platform.DispatchAttempt{Number: run.Attempt, Intent: run.Intent, Target: run.Target, State: "pending", At: at, Compensation: run.Compensating})
	if len(run.Attempts) > 20 {
		run.Attempts = run.Attempts[len(run.Attempts)-20:]
	}
	run.State, run.StateAt, run.ReadDue = "pending", at, at
	if run.Compensating {
		run.State = "compensating"
	}
	return c.DispatchTransport().QueueDispatch(c, receipt, *run, at)
}

func dispatchTerminal(state string) bool {
	return slices.Contains([]string{"completed", "cancelled", "compensated"}, state)
}

func (b *Build) updateDispatch(c platform.Caller, submission *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, submission, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		run, found := platform.Get[platform.DispatchRun](c, submission.GetTarget().GetId())
		if !found {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The dispatch does not exist")
		}
		var in struct {
			Outcome, Reason, Target, Intent string
			Revision                        uint32
			Answer                          json.RawMessage
			Reading                         *platform.DispatchReading
		}
		if json.Unmarshal(submission.GetPayload(), &in) != nil || len(in.Reason) > 4096 || len(in.Answer) > 64<<10 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid dispatch status")
		}
		manual := submission.GetSchema().GetName() == platform.DispatchReviewSchema
		transport := c.DispatchTransport()
		if transport == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Dispatch transport is unavailable")
		}
		if manual {
			if c.Agent || !c.Holds(ID, run.Definition.ReviewerRole) || strings.TrimSpace(in.Reason) == "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The dispatch needs its current reviewer and evidence")
			}
			typ, id, _ := strings.Cut(run.Subject, "/")
			if _, problem := c.ReadRecord(typ, id, at); problem != nil {
				return nil, problem
			}
			if !slices.Contains([]string{"retry", "retarget", "complete", "cancel"}, in.Outcome) || in.Reading != nil || in.Intent != "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid dispatch intervention")
			}
			if !slices.Contains([]string{"unknown", "stuck", "failed"}, run.State) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only an unresolved dispatch can be reviewed")
			}
			if in.Outcome == "retarget" && (in.Target == "" || in.Target == run.Target) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a different external target")
			}
		} else if !c.Automation {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch status belongs to its original transport")
		}
		if in.Reading != nil {
			if in.Revision != run.Revision || len(in.Reading.Observed) > 4096 || len(in.Reading.Reason) > 4096 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The dispatch changed while its status was read")
			}
			if problem := transport.CheckDispatchRead(run, at); problem != nil {
				return nil, problem
			}
		}
		if !manual && submission.GetSchema().GetName() != platform.DispatchTickSchema && !slices.Contains([]string{"accepted", "running", "completed", "stuck", "failed", "unknown"}, in.Outcome) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid dispatch result")
		}
		var replacementRequest, replacementConnection, replacementCompensation json.RawMessage
		if manual && in.Outcome == "retarget" {
			var problem *kernel.Error
			replacementRequest, replacementConnection, problem = transport.RetargetDispatch(c, run, in.Target, at)
			if problem != nil {
				return nil, problem
			}
			if len(run.CompensationRequest) > 0 {
				reverse := run
				reverse.Request = run.CompensationRequest
				replacementCompensation, _, problem = transport.RetargetDispatch(c, reverse, in.Target, at)
				if problem != nil {
					return nil, problem
				}
			}
		}
		return func(receipt *pb.ChangeRecord) {
			fail := func(problem *kernel.Error) bool {
				if problem != nil {
					c.FailDecision(problem)
					return true
				}
				return false
			}
			if manual {
				resolution := run
				resolution.State = "cancelled"
				if in.Outcome == "complete" {
					resolution.State = "completed"
				}
				if fail(transport.ResolveDispatchDelivery(c, resolution, at)) {
					return
				}
				run.Reason = in.Reason
				switch in.Outcome {
				case "complete":
					run.State = "completed"
					if run.Compensating {
						run.State = "compensated"
					}
				case "cancel":
					run.State = "cancelled"
				case "retarget", "retry":
					if in.Outcome == "retarget" {
						run.Target, run.Request, run.Connection = in.Target, replacementRequest, replacementConnection
						if len(replacementCompensation) > 0 {
							run.CompensationRequest = replacementCompensation
						}
					}
					run.Deadline = at.Add(time.Duration(run.Definition.DeadlineSeconds) * time.Second)
					run.PolicyApplied, run.Escalated = false, false
					if fail(b.queueDispatch(c, receipt, &run, at)) {
						return
					}
				}
				run.StateAt = at
			} else if !dispatchTerminal(run.State) {
				if in.Intent != "" {
					i := slices.IndexFunc(run.Attempts, func(attempt platform.DispatchAttempt) bool { return attempt.Intent == in.Intent })
					if i >= 0 {
						answer := in.Answer
						if len(answer) > 4096 {
							answer = platform.Raw(clipAnswer(answer))
						}
						run.Attempts[i].State, run.Attempts[i].Reason, run.Attempts[i].Answer = in.Outcome, in.Reason, answer
					}
				}
				if in.Reading != nil {
					in.Reading.At = at
					run.Readback = append(run.Readback, *in.Reading)
					if len(run.Readback) > 20 {
						run.Readback = run.Readback[len(run.Readback)-20:]
					}
					run.ReadDue = at.Add(time.Duration(run.Definition.Readback.EverySeconds) * time.Second)
				}
				if in.Outcome != "" && (in.Intent == "" || in.Intent == run.Intent) {
					// Delivery acceptance proves receipt, never execution. A late
					// HTTP answer must not erase a newer status read.
					if in.Reading != nil || !slices.Contains([]string{"running", "stuck", "failed"}, run.State) {
						if run.State != in.Outcome {
							run.StateAt = at
						}
						run.State, run.Reason = in.Outcome, in.Reason
						if in.Reading != nil && (run.State == "running" || run.State == "accepted") {
							run.PolicyApplied, run.Escalated = false, false
						}
						if run.Compensating && run.State == "completed" {
							run.State = "compensated"
						}
					}
				}
				if in.Reading != nil && in.Outcome != "unknown" {
					if fail(transport.ResolveDispatchDelivery(c, run, at)) {
						return
					}
				}
				if !at.Before(run.Deadline) && !dispatchTerminal(run.State) && run.State != "unknown" && run.State != "failed" {
					if run.State != "stuck" {
						run.StateAt = at
					}
					run.State, run.Reason = "stuck", "The dispatch reached its business deadline"
				}
			}
			if fail(b.dispatchState(c, receipt, &run, at)) {
				return
			}
			if fail(c.Put(receipt, run)) {
				return
			}
		}, nil
	})
}

func (b *Build) dispatchState(c platform.Caller, receipt *pb.ChangeRecord, run *platform.DispatchRun, at time.Time) *kernel.Error {
	if dispatchTerminal(run.State) {
		if err := c.DispatchTransport().ResolveDispatchDelivery(c, *run, at); err != nil {
			return err
		}
		c.Assign(receipt, platform.Assignment{Key: run.ID, Close: true})
		return nil
	}
	if !slices.Contains([]string{"stuck", "failed", "unknown"}, run.State) {
		c.Assign(receipt, platform.Assignment{Key: run.ID, Close: true})
		return nil
	}
	policy := "manual"
	if run.State == "stuck" {
		policy = run.Definition.OnStuck
	} else if run.State == "failed" {
		policy = run.Definition.OnFail
	}
	if !run.PolicyApplied && !run.Compensating && run.State != "unknown" {
		run.PolicyApplied = true
		switch policy {
		case "retry":
			if !run.Command.SafeRetry() {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This command cannot be automatically resent")
			}
			if at.Before(run.Deadline) {
				if err := c.DispatchTransport().ResolveDispatchDelivery(c, *run, at); err != nil {
					return err
				}
				return b.queueDispatch(c, receipt, run, at)
			}
		case "compensate":
			if run.CompensationCommand != nil && len(run.CompensationRequest) > 0 {
				if err := c.DispatchTransport().ResolveDispatchDelivery(c, *run, at); err != nil {
					return err
				}
				run.Compensating, run.Request = true, run.CompensationRequest
				run.Deadline = at.Add(time.Duration(run.Definition.DeadlineSeconds) * time.Second)
				return b.queueDispatch(c, receipt, run, at)
			}
		case "escalate":
			if !run.Escalated {
				run.Escalated = true
				c.Notify(platform.Notification{Key: "dispatch-attention:" + run.ID, Title: "Device dispatch needs attention", Body: run.Reason, Ref: platform.DispatchType + "/" + run.ID}, at, platform.Recipient{AppRole: run.Definition.ReviewerRole})
			}
		}
	}
	c.Assign(receipt, platform.Assignment{Key: run.ID, Ref: platform.DispatchType + "/" + run.ID, Title: run.Command.Title, Body: fmt.Sprintf("%s · %s\n%s", run.Command.Title, run.Subject, run.Reason), To: []platform.Recipient{{AppRole: run.Definition.ReviewerRole}}})
	return nil
}

func (b *Build) runDispatches(c platform.Caller, at time.Time) *kernel.Error {
	runs, err := platform.PendingDispatches(c)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if !at.Before(run.Deadline) && !slices.Contains([]string{"stuck", "failed", "unknown"}, run.State) {
			_, err := platform.Decide(c, b, dispatchSubmission(c, run.ID, platform.DispatchTickSchema, fmt.Sprintf("deadline:%d", run.Revision), map[string]any{}, at), at)
			return err
		}
	}
	return nil
}

func dispatchSubmission(c platform.Caller, id, schema, key string, payload any, at time.Time) *pb.Submission {
	return &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "dispatch:" + id + ":" + key, Target: &pb.EntityRef{Type: platform.DispatchType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
}
