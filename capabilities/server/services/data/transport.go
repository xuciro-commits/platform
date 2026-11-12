package data

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type Transport struct {
	Assets          func() ([]platform.ReleaseAsset, error)
	Release         func() string
	Protocol        func(platform.ConfirmationParty) (*platform.ConfirmationProtocol, error)
	App             func(string) platform.App
	Member          func(string) (platform.Member, bool)
	Readable        func(platform.Member, string, time.Time) bool
	Applicant       func(platform.Member) platform.Caller
	ApplicantReads  func(platform.Caller, string) bool
	Bind            func(object, release string) ([]platform.AssetBinding, error)
	Credential      func(WritebackRequest) *kernel.Error
	Intend          func(platform.Effect)
	Complete        func(c platform.Caller, run platform.ConfirmationRun, member platform.Member, action string, sub *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error)
	InFlight        func(owned func(platform.Effect) bool) bool
	Queued          func(id string) (platform.Effect, bool, bool)
	Settle          func(id, state, reason string, at time.Time)
	SettleAll       func(owned func(platform.Effect) bool, state, reason string, at time.Time)
	Retry           func(c platform.Caller, id string, at time.Time) *kernel.Error
	Publisher       func(Source) (platform.Member, bool)
	Markings        func(connection string) platform.MarkingSet
	ObservationRead func(member platform.Member, definition platform.ObservationDefinition, typ, id string, at time.Time) (bool, *kernel.Error)
	Record          func(string) (any, bool)
	Owner           *Owner
}

func (t Transport) FreezeConfirmationProtocol(p platform.ConfirmationParty) (*platform.ConfirmationProtocol, error) {
	return t.Protocol(p)
}

func (t Transport) FreezeConfirmation(c platform.Caller, subject string, spec platform.Confirmation, at time.Time) (string, []platform.ReleaseAsset, error) {
	if !c.Staging() || c.App != App {
		return "", nil, fmt.Errorf("Confirmation requires its original private authority")
	}
	assets, err := t.Assets()
	if err != nil {
		return "", nil, err
	}
	roots := []platform.AssetRef{{App: App, Kind: platform.AssetObject, Name: subject}}
	for _, p := range spec.Parties {
		if p.Protocol != "" {
			original, err := t.Protocol(p)
			if err != nil {
				return "", nil, err
			}
			for _, schema := range []string{original.Action, original.CompensateAction} {
				if schema != "" {
					roots = append(roots, platform.AssetRef{App: original.Provider, Kind: platform.AssetAction, Name: schema})
				}
			}
		}
		for _, id := range []string{p.Writeback, p.CompensateWriteback} {
			if id == "" {
				continue
			}
			w, ok := platform.Get[Writeback](c, id)
			if !ok {
				return "", nil, fmt.Errorf("Confirmation writeback is unavailable")
			}
			roots = append(roots, platform.AssetRef{App: App, Kind: platform.AssetWriteback, Name: w.Name})
		}
		if p.Reconcile != nil {
			s, ok := platform.Get[Source](c, p.Reconcile.Source)
			if !ok {
				return "", nil, fmt.Errorf("Reconciliation source is unavailable")
			}
			roots = append(roots, platform.AssetRef{App: App, Kind: platform.AssetSource, Name: s.Name})
		}
	}
	closed, err := platform.Candidate(roots, assets)
	if err != nil {
		return "", nil, err
	}
	if len(closed.Bytes) > 192<<10 {
		return "", nil, fmt.Errorf("Confirmation definition exceeds its retained execution budget")
	}
	return t.Release(), closed.Assets, nil
}

func (t Transport) QueueConfirmation(c platform.Caller, r *pb.ChangeRecord, run platform.ConfirmationRun, index int, compensate bool, at time.Time) *kernel.Error {
	if !c.Staging() || c.App != App || index < 0 || index >= len(run.Parties) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Confirmation transport requires its original owner")
	}
	p := run.Parties[index]
	declaration := run.Definition.Parties[index]
	intent, body := p.Intent, p.Request
	if compensate {
		intent, body = p.Compensation, p.CompensationRequest
	}
	if declaration.Protocol != "" {
		if p.Protocol == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The confirmation has no original protocol mapping")
		}
		provider := t.App(p.Protocol.Provider)
		if provider == nil || provider.Manifest().Version != p.Protocol.Version {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original confirmation provider version is unavailable")
		}
		action := declaration.Action
		if compensate {
			action = declaration.CompensateAction
		}
		mapped := false
		for _, pv := range provider.Manifest().Provides {
			if pv.Protocol.ID() == declaration.Protocol && pv.Actions[action] == map[bool]string{false: p.Protocol.Action, true: p.Protocol.CompensateAction}[compensate] && pv.Actions[action] != "" {
				mapped = true
			}
		}
		if !mapped {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original confirmation protocol mapping is unavailable")
		}
		member, known := t.Member(run.Member)
		applicant := t.Applicant(member)
		if !known || !member.Active() || !t.ApplicantReads(applicant, run.Subject) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The confirmation applicant can no longer read the subject")
		}
		receipt := proto.Clone(r).(*pb.ChangeRecord)
		receipt.Submission.PrincipalId = member.ID
		receipt.Submission.Target = &pb.EntityRef{Type: platform.ConfirmationType, Id: run.ID}
		target := declaration.Target
		if target == "" || target == "$subject" {
			_, target, _ = strings.Cut(run.Subject, "/")
		}
		applicant.Request(receipt, platform.Request{Tag: p.Name + map[bool]string{true: ":compensate"}[compensate], Key: intent, Provider: p.Protocol.Provider, Protocol: declaration.Protocol, Action: action, Target: target, Payload: run.Input, Reply: platform.ConfirmationAnswerSchema})
		return nil
	}
	var req WritebackRequest
	if json.Unmarshal(body, &req) != nil || req.Confirmation != run.ID || req.Party != p.Name || req.Version != 1 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Confirmation delivery has no frozen request")
	}
	applications, err := t.Bind(strings.SplitN(run.Subject, "/", 2)[0], run.Release)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	if refusal := t.Credential(req); refusal != nil {
		return refusal
	}
	key := p.Key
	if compensate {
		key += ":compensate"
	}
	x := platform.Effect{ID: intent, App: App, Endpoint: WritebackEndpoint + req.Connection, Event: App + "/confirmation", Key: key, Target: run.Subject, At: at, State: "pending", Due: at, Body: string(body), Applications: applications, Compensates: req.Compensates}
	t.Intend(x)
	return nil
}

func (t Transport) CompleteConfirmation(c platform.Caller, r *pb.ChangeRecord, run platform.ConfirmationRun, action string, at time.Time) (string, *kernel.Error) {
	if !c.Staging() {
		return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Confirmation completion requires a private decision")
	}
	member, known := t.Member(run.Member)
	if !known || !member.Active() {
		return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The confirmation applicant is no longer active")
	}
	typ, id, _ := strings.Cut(run.Subject, "/")
	if !strings.HasPrefix(action, typ+".") {
		action = typ + "." + action
	}
	sub := &pb.Submission{TenantId: c.Tenant, PrincipalId: member.ID, Authority: App, IdempotencyKey: "confirmation:" + run.ID + ":" + action, Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: action, Version: 1}, Payload: []byte(`{}`), CorrelationId: run.ID}
	receipt, refusal := t.Complete(c, run, member, action, sub, at)
	if refusal != nil {
		return "", refusal
	}
	return receipt.GetChangeId(), nil
}

func (t Transport) ResolveConfirmationDelivery(c platform.Caller, run platform.ConfirmationRun, index int, compensated bool, at time.Time) *kernel.Error {
	if !c.Staging() || c.App != App || index < 0 || index >= len(run.Parties) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Confirmation resolution requires its original owner")
	}
	p := run.Parties[index]
	inFlight := t.InFlight(func(e platform.Effect) bool { return e.ID == p.Intent || e.ID == p.Compensation })
	if inFlight {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The confirmation delivery is still in flight")
	}
	resolve := func(id, state, reason string) *kernel.Error {
		effect, sending, ok := t.Queued(id)
		if !ok || platform.EffectSettled(effect.State) {
			return nil
		}
		var request WritebackRequest
		if effect.App != App || json.Unmarshal([]byte(effect.Body), &request) != nil || request.Confirmation != run.ID || request.Party != p.Name {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The confirmation does not own this delivery")
		}
		if sending {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The confirmation delivery is still in flight")
		}
		t.Settle(id, state, reason, at)
		return nil
	}
	if compensated {
		if p.CompensationState == "confirmed" {
			if err := resolve(p.Compensation, "delivered", p.CompensationReason); err != nil {
				return err
			}
			return resolve(p.Intent, "compensated", p.CompensationReason)
		}
		if p.CompensationState == "rejected" {
			return resolve(p.Compensation, "rejected", p.CompensationReason)
		}
	} else if p.State == "confirmed" {
		return resolve(p.Intent, "delivered", p.Reason)
	} else if p.State == "rejected" {
		return resolve(p.Intent, "rejected", p.Reason)
	}
	return nil
}

func (t Transport) RetryConfirmationDelivery(c platform.Caller, run platform.ConfirmationRun, index int, compensated bool, at time.Time) *kernel.Error {
	if !c.Staging() || c.App != App || index < 0 || index >= len(run.Parties) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Confirmation resolution requires its original owner")
	}
	p := run.Parties[index]
	if p.Protocol != nil {
		return nil
	}
	id := p.Intent
	if compensated {
		id = p.Compensation
	}
	return t.Retry(c, id, at)
}

func (t Transport) CheckConfirmationRead(run platform.ConfirmationRun, index int, at time.Time) *kernel.Error {
	if index < 0 || index >= len(run.Parties) || index >= len(run.Definition.Parties) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The confirmation declaration is unavailable")
	}
	member, known := t.Member(run.Member)
	if !known || !member.Active() || !t.Readable(member, run.Subject, at) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The confirmation applicant can no longer read the subject")
	}
	var source Source
	var connection Connection
	policy := run.Definition.Parties[index].Reconcile
	if policy == nil || json.Unmarshal(run.Parties[index].Source, &source) != nil || json.Unmarshal(run.Parties[index].Connection, &connection) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original reconciliation descriptor is unavailable")
	}
	publisher, known := t.Publisher(source)
	if !known || !publisher.Active() || !t.Readable(publisher, SourceType+"/"+policy.Source, at) || (platform.MarkingSet{Level: connection.Marking, Compartments: connection.Compartments}).Guarded() || t.Markings(connection.ID).Guarded() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Reconciliation source access is unavailable")
	}
	return nil
}

func (t Transport) FreezeDispatch(c platform.Caller, subject, action string, spec platform.Dispatch, at time.Time) (string, []platform.ReleaseAsset, error) {
	if !c.Staging() || c.App != App {
		return "", nil, fmt.Errorf("Dispatch requires its original private authority")
	}
	assets, err := t.Assets()
	if err != nil {
		return "", nil, err
	}
	roots := []platform.AssetRef{{App: App, Kind: platform.AssetObject, Name: subject}, {App: App, Kind: platform.AssetAction, Name: action}}
	if spec.Readback.Source != "" {
		source, found := platform.Get[Source](c, spec.Readback.Source)
		if !found {
			return "", nil, fmt.Errorf("Dispatch status source is unavailable")
		}
		roots = append(roots, platform.AssetRef{App: App, Kind: platform.AssetSource, Name: source.Name})
	}
	if spec.Readback.Observation != "" {
		observation, found := platform.Get[Observation](c, spec.Readback.Observation)
		if !found {
			return "", nil, fmt.Errorf("Dispatch status observation is unavailable")
		}
		roots = append(roots, platform.AssetRef{App: App, Kind: platform.AssetObservation, Name: observation.Name})
	}
	closed, err := platform.Candidate(roots, assets)
	if err != nil {
		return "", nil, err
	}
	if len(closed.Bytes) > 192<<10 {
		return "", nil, fmt.Errorf("Dispatch definition exceeds its retained execution budget")
	}
	return t.Release(), closed.Assets, nil
}

func (t Transport) QueueDispatch(c platform.Caller, r *pb.ChangeRecord, run platform.DispatchRun, at time.Time) *kernel.Error {
	if !c.Staging() || c.App != App || run.Intent == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch transport requires its original owner")
	}
	command, body, key := run.Command, run.Request, run.Key
	if run.Compensating {
		if run.CompensationCommand == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Dispatch has no original reverse command")
		}
		command, body, key = *run.CompensationCommand, run.CompensationRequest, run.Key+":compensate"
	}
	var request WritebackRequest
	if json.Unmarshal(body, &request) != nil || request.Dispatch != run.ID || request.Version != 1 || request.Connection != run.Target || request.SafeRetry != command.SafeRetry() || key == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch delivery has no original frozen request")
	}
	if refusal := t.Credential(request); refusal != nil {
		return refusal
	}
	request.DispatchKey = key
	typ, _, _ := strings.Cut(run.Subject, "/")
	applications, err := t.Bind(typ, run.Release)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	x := platform.Effect{ID: run.Intent, App: App, Endpoint: WritebackEndpoint + request.Connection, Event: App + "/dispatch", Key: fmt.Sprintf("%s:%d", run.ID, run.Attempt), Target: run.Subject, At: at, State: "pending", Due: at, Body: string(platform.Raw(request)), Applications: applications}
	t.Intend(x)
	return nil
}

func (t Transport) ResolveDispatchDelivery(c platform.Caller, run platform.DispatchRun, at time.Time) *kernel.Error {
	if !c.Staging() || c.App != App {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch resolution requires its original owner")
	}
	owned := func(x platform.Effect) bool {
		var request WritebackRequest
		return x.App == App && x.Event == App+"/dispatch" && json.Unmarshal([]byte(x.Body), &request) == nil && request.Dispatch == run.ID
	}
	inFlight := t.InFlight(owned)
	if inFlight {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The dispatch delivery is still in flight")
	}
	state := "discarded"
	if slices.Contains([]string{"accepted", "running", "stuck", "failed", "completed", "compensated"}, run.State) {
		state = "delivered"
	}
	t.SettleAll(owned, state, run.Reason, at)
	return nil
}

func (t Transport) CheckDispatchRead(run platform.DispatchRun, at time.Time) *kernel.Error {
	member, known := t.Member(run.Member)
	if !known || !member.Active() || !t.Readable(member, run.Subject, at) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The dispatch applicant can no longer read the subject")
	}
	read := run.Definition.Readback
	if read.Record != "" {
		return nil
	}
	var connection Connection
	if json.Unmarshal(run.Connection, &connection) != nil || connection.ID == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original dispatch read connection is unavailable")
	}
	if (platform.MarkingSet{Level: connection.Marking, Compartments: connection.Compartments}).Guarded() || t.Markings(connection.ID).Guarded() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch status requires an unclassified source projection")
	}
	if read.Source != "" {
		var source Source
		if json.Unmarshal(run.Source, &source) != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original dispatch status source is unavailable")
		}
		publisher, found := t.Publisher(source)
		if !found || !publisher.Active() || !t.Readable(publisher, SourceType+"/"+read.Source, at) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch status source access is unavailable")
		}
		return nil
	}
	var definition platform.ObservationDefinition
	typ, id, valid := strings.Cut(run.Subject, "/")
	if json.Unmarshal(run.Observation, &definition) != nil || !valid || definition.Object.Name != typ {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original dispatch observation is unavailable")
	}
	withheld, refusal := t.ObservationRead(member, definition, typ, id, at)
	if refusal != nil {
		return refusal
	}
	if withheld {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The dispatch observation device is outside this member's permissions")
	}
	return nil
}

func (t Transport) RetargetDispatch(c platform.Caller, run platform.DispatchRun, target string, at time.Time) (json.RawMessage, json.RawMessage, *kernel.Error) {
	if !c.Staging() || c.App != App || c.Agent || !c.Holds(App, run.Definition.ReviewerRole) || target == "" {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch retargeting requires its original owner")
	}
	typ, id, _ := strings.Cut(run.Subject, "/")
	if _, refusal := c.ReadRecord(typ, id, at); refusal != nil {
		return nil, nil, refusal
	}
	value, found := t.Record(ConnectionType + "/" + target)
	connection, valid := value.(Connection)
	if !found || !valid || connection.State != "ready" || connection.Archived {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The selected dispatch connection is not ready")
	}
	connection, err := t.Owner.ExecutionConnection(connection)
	if err != nil || !slices.Contains([]string{"http", "odata"}, connection.Kind) {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch requires a published HTTP connection")
	}
	var request WritebackRequest
	if json.Unmarshal(run.Request, &request) != nil || request.Dispatch != run.ID {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original dispatch request is unavailable")
	}
	path, err := url.Parse(request.Path)
	if err != nil || path.IsAbs() || path.Host != "" {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Changing target requires a relative command path")
	}
	request.Connection, request.URL, request.Secret, request.AllowPrivate = connection.ID, connection.Resolve(request.Path), connection.Secret, connection.AllowPrivate
	readConnection := run.Connection
	var original Connection
	if json.Unmarshal(run.Connection, &original) == nil && original.ID == run.Target {
		readConnection = platform.Raw(connection)
	}
	return platform.Raw(request), readConnection, nil
}
