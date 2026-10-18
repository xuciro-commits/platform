package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

const confirmationType = platform.ConfirmationType
const confirmationAnswer = platform.ConfirmationAnswerSchema
const confirmationReview = confirmationType + ".review"
const confirmationTick = confirmationType + ".tick"

func (b *Build) confirmationEntity() platform.Entity {
	return platform.Entity{Type: confirmationType, Title: "Business confirmation", Plural: "Business confirmations", Model: platform.ConfirmationRun{}, Display: "subject",
		Description: "Independent party receipts, unknown outcomes, source reconciliation and declared compensation.",
		Scope: platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}, Participants: func(value any) []string {
			r := value.(platform.ConfirmationRun)
			return append([]string{r.Member}, b.host.(host.ConfirmationHost).Holding(ID, r.Definition.ReviewerRole)...)
		}},
		Withheld: "withheld", Derived: []platform.Derivation{{From: "subject", Fields: []string{"input", "parties", "definition", "assets"}}},
	}
}
func confirmationActions() []platform.Action {
	return []platform.Action{
		{Schema: confirmationAnswer, Target: confirmationType, Title: "Record confirmation answer", Description: "Retain the original party request identity and answer.", Capability: "confirmations", Automation: true, Payload: []platform.Field{}},
		{Schema: confirmationTick, Target: confirmationType, Title: "Advance confirmation work", Description: "Apply deadlines and retain completion results through original owned work.", Capability: "confirmations", Automation: true, Payload: []platform.Field{}},
		{Schema: confirmationReview, Target: confirmationType, Title: "Review business confirmation", Description: "Resolve or retry a party from checked evidence, or request its declared compensation.", Capability: "confirmations", Roles: []string{platform.AnyMember}, Payload: []platform.Field{{Name: "party", Type: "string", Description: "The declared party"}, {Name: "outcome", Type: "string", Required: true, Choices: []string{"accepted", "refused", "retry", "compensate", "complete"}, Description: "The evidenced decision"}, {Name: "compensation", Type: "boolean", Description: "Resolve the compensation instead of the original request"}, {Name: "reason", Type: "string", Required: true, Description: "Evidence supporting the decision"}}},
	}
}

func (b *Build) checkConfirmation(spec *platform.Confirmation, object Object) error {
	if spec == nil {
		return nil
	}
	if err := spec.Check(); err != nil {
		return err
	}
	for _, party := range spec.Parties {
		if party.Reconcile == nil || party.Reconcile.KeyInput == "" {
			continue
		}
		declared := false
		for _, action := range object.Actions {
			if action.Transaction == nil || action.Transaction.Confirmation != spec {
				continue
			}
			declared = slices.ContainsFunc(action.Inputs, func(input Input) bool {
				return input.Name == party.Reconcile.KeyInput && input.Required && (input.Type == "text" || input.Type == "reference")
			})
		}
		if !declared {
			return fmt.Errorf("Reconciliation key input must name a required text or reference input of the initiating action")
		}
	}
	subject := "build." + object.Name
	if spec.ReviewerRole != Builder && !slices.ContainsFunc(object.Access, func(a Access) bool { return a.Role == spec.ReviewerRole }) {
		return fmt.Errorf("The confirmation reviewer role is not declared")
	}
	for _, name := range []string{spec.OnComplete, spec.OnPartialAction} {
		if name == "" {
			continue
		}
		schema := name
		if !strings.HasPrefix(schema, subject+".") {
			schema = subject + "." + name
		}
		name := strings.TrimPrefix(schema, subject+".")
		i := slices.IndexFunc(object.Actions, func(a Action) bool { return a.Name == name })
		if i < 0 || object.Actions[i].Approval != nil || object.Actions[i].Transaction != nil && object.Actions[i].Transaction.Confirmation != nil {
			return fmt.Errorf("Confirmation completion needs a subject action without another approval or confirmation")
		}
		if object.Actions[i].Adoption != nil {
			return fmt.Errorf("Confirmation completion cannot consume an AI proposal")
		}
	}
	if b.host != nil {
		_, err := b.bindConfirmation(spec, subject)
		return err
	}
	return nil
}

// Stable transport bindings belong to the initiating action. The common
// descriptor reads them from the same frozen body used for publication.
type confirmationPartyBindings struct {
	Name                string             `json:"name"`
	Writeback           *platform.AssetRef `json:"writeback,omitempty"`
	CompensateWriteback *platform.AssetRef `json:"compensateWriteback,omitempty"`
	Source              *platform.AssetRef `json:"source,omitempty"`
	Protocol            *platform.AssetRef `json:"protocol,omitempty"`
	Action              *platform.AssetRef `json:"action,omitempty"`
	CompensateAction    *platform.AssetRef `json:"compensateAction,omitempty"`
}

type confirmationBindings struct {
	Parties         []confirmationPartyBindings `json:"parties"`
	OnComplete      platform.AssetRef           `json:"onComplete"`
	OnPartialAction *platform.AssetRef          `json:"onPartialAction,omitempty"`
}

func (b *Build) bindConfirmation(spec *platform.Confirmation, subject string) (confirmationBindings, error) {
	out := confirmationBindings{}
	for _, party := range spec.Parties {
		binding := confirmationPartyBindings{Name: party.Name}
		for _, item := range []struct {
			id  string
			ref **platform.AssetRef
		}{
			{party.Writeback, &binding.Writeback}, {party.CompensateWriteback, &binding.CompensateWriteback},
		} {
			if item.id == "" {
				continue
			}
			record, ok := b.host.Record(WritebackType + "/" + item.id)
			w, valid := record.(Writeback)
			if !ok || !valid {
				return out, fmt.Errorf("Confirmation writeback is unavailable")
			}
			*item.ref = &platform.AssetRef{App: ID, Kind: platform.AssetWriteback, Name: w.Name}
		}
		if party.Reconcile != nil {
			record, ok := b.host.Record(SourceType + "/" + party.Reconcile.Source)
			s, valid := record.(Source)
			if !ok || !valid {
				return out, fmt.Errorf("Reconciliation source is unavailable")
			}
			binding.Source = &platform.AssetRef{App: ID, Kind: platform.AssetSource, Name: s.Name}
		}
		if party.Protocol != "" {
			p, err := b.host.(host.ConfirmationHost).FreezeConfirmationProtocol(party)
			if err != nil {
				return out, err
			}
			name, _, _ := strings.Cut(party.Protocol, "/")
			binding.Protocol = &platform.AssetRef{App: p.Provider, Kind: platform.AssetProtocol, Name: name}
			binding.Action = &platform.AssetRef{App: p.Provider, Kind: platform.AssetAction, Name: p.Action}
			if p.CompensateAction != "" {
				binding.CompensateAction = &platform.AssetRef{App: p.Provider, Kind: platform.AssetAction, Name: p.CompensateAction}
			}
		}
		out.Parties = append(out.Parties, binding)
	}
	owner, _, _ := strings.Cut(subject, ".")
	action := func(name string) platform.AssetRef {
		if !strings.HasPrefix(name, subject+".") {
			name = subject + "." + name
		}
		return platform.AssetRef{App: owner, Kind: platform.AssetAction, Name: name}
	}
	out.OnComplete = action(spec.OnComplete)
	if spec.OnPartialAction != "" {
		ref := action(spec.OnPartialAction)
		out.OnPartialAction = &ref
	}
	return out, nil
}

func (b *Build) bindConfirmationAssets(assets []platform.ReleaseAsset) ([]platform.ReleaseAsset, error) {
	bindings := map[string]confirmationBindings{}
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return nil, err
		}
		for _, action := range object.Actions {
			if action.Transaction == nil || action.Transaction.Confirmation == nil {
				continue
			}
			binding, err := b.bindConfirmation(action.Transaction.Confirmation, asset.Ref.Name)
			if err != nil {
				return nil, err
			}
			bindings[asset.Ref.Name+"."+action.Name] = binding
		}
	}
	for i, asset := range assets {
		binding, ok := bindings[asset.Ref.Name]
		if asset.Ref.Kind != platform.AssetAction || !ok {
			continue
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(asset.Body, &body); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(binding)
		if err != nil {
			return nil, err
		}
		body["confirmationBindings"] = raw
		asset.Body, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
		assets[i], err = platform.WithDependencies(asset)
		if err != nil {
			return nil, err
		}
	}
	return assets, nil
}

func (b *Build) confirmationProtocols() []platform.Consumption {
	out := []platform.Consumption{}
	for _, entity := range b.installed {
		if entity.Lifecycle == nil {
			continue
		}
		for _, action := range entity.Lifecycle.Transitions {
			if action.Txn == nil || action.Txn.Confirmation == nil {
				continue
			}
			for _, p := range action.Txn.Confirmation.Parties {
				if p.Protocol != "" && !slices.ContainsFunc(out, func(c platform.Consumption) bool { return c.Protocol == p.Protocol }) {
					out = append(out, platform.Consumption{Protocol: p.Protocol, Optional: true})
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b platform.Consumption) int { return strings.Compare(a.Protocol, b.Protocol) })
	return out
}

func (b *Build) openConfirmation(c platform.Caller, r *pb.ChangeRecord, txn *platform.BusinessTxn, at time.Time) *kernel.Error {
	if txn.Confirmation == nil {
		return nil
	}
	spec := *txn.Confirmation
	transport := c.ConfirmationTransport()
	if transport == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Confirmation transport is unavailable")
	}
	if c.Simulating() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "External confirmation does not execute in isolated trials")
	}
	release, assets, err := transport.FreezeConfirmation(c, txn.Subject, spec, at)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	id := "confirmation:" + r.GetChangeId()
	run := platform.ConfirmationRun{Record: platform.Record{ID: id}, Subject: txn.Subject + "/" + r.GetSubmission().GetTarget().GetId(), Action: r.GetSubmission().GetSchema().GetName(), Member: c.ID, State: "pending", Deadline: at.Add(time.Duration(spec.DeadlineSeconds) * time.Second), Definition: spec, Release: release, Assets: assets, Input: slices.Clone(r.GetSubmission().GetPayload()), Parties: []platform.ConfirmationProgress{}, Reviewers: []string{}}
	for _, member := range b.host.(host.ConfirmationHost).Holding(ID, spec.ReviewerRole) {
		run.Reviewers = append(run.Reviewers, member)
	}
	for _, party := range spec.Parties {
		p := platform.ConfirmationProgress{Name: party.Name, Title: party.Title, State: "pending", Key: id + ":" + party.Name, Intent: c.Tenant + ":" + ID + ":" + id + ":" + party.Name}
		if party.Protocol != "" {
			p.Protocol, err = transport.FreezeConfirmationProtocol(party)
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
		}
		prepare := func(writeback string, compensate bool) (json.RawMessage, int, error) {
			if writeback == "" {
				return nil, 0, nil
			}
			w, known := platform.Get[Writeback](c, writeback)
			if !known {
				return nil, 0, fmt.Errorf("Confirmation writeback is unavailable")
			}
			w, err = b.ExecutionWriteback(w)
			if err != nil {
				return nil, 0, err
			}
			if w.State != "published" || w.Object != txn.Subject {
				return nil, 0, fmt.Errorf("Confirmation writeback must be published")
			}
			conn, known := platform.Get[Connection](c, w.Connection)
			if !known {
				return nil, 0, fmt.Errorf("Confirmation connection is unavailable")
			}
			conn, err = b.ExecutionConnection(conn)
			if err != nil {
				return nil, 0, err
			}
			req, err := w.Request(conn, r.GetSubmission().GetTarget().GetId(), run.Input)
			if err != nil {
				return nil, 0, err
			}
			req.Confirmation, req.Party = id, party.Name
			if compensate {
				req.Compensates = p.Intent
			}
			return platform.Raw(req), w.DefinitionVersion, nil
		}
		p.Request, p.RequestVersion, err = prepare(party.Writeback, false)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		p.Intent = confirmationIntent(c.Tenant, p.Key, p.Request, p.Protocol)
		p.CompensationRequest, _, err = prepare(party.CompensateWriteback, true)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		if party.Reconcile != nil {
			s, known := platform.Get[Source](c, party.Reconcile.Source)
			if !known {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Reconciliation source is unavailable")
			}
			s, err = b.ExecutionSource(s)
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			if s.Header != "" || s.Connection == "" {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Reconciliation needs a controlled connection and named credentials")
			}
			conn, known := platform.Get[Connection](c, s.Connection)
			if !known {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Reconciliation connection is unavailable")
			}
			conn, err = b.ExecutionConnection(conn)
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			p.Connection = platform.Raw(conn)
			s.Cursor = ""
			if s.State != "published" {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Reconciliation source must be published")
			}
			p.Source, p.SourceVersion = platform.Raw(s), s.ExecutionVersion()
			p.ReconcileDue = at
		}
		run.Parties = append(run.Parties, p)
	}
	if err := c.Put(r, run); err != nil {
		return err
	}
	for i := range run.Parties {
		if err := transport.QueueConfirmation(c, r, run, i, false, at); err != nil {
			return err
		}
	}
	return nil
}

func (b *Build) updateConfirmation(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		run, ok := platform.Get[platform.ConfirmationRun](c, s.GetTarget().GetId())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The confirmation does not exist")
		}
		var in struct {
			Party, Outcome, Reason, Tag, Completion, CompletionError string
			Compensation                                             bool
			Revision                                                 uint32
			Answer                                                   json.RawMessage
			Difference                                               *platform.ConfirmationDifference
		}
		if json.Unmarshal(s.GetPayload(), &in) != nil || len(in.Reason) > 4096 || len(in.Answer) > 64<<10 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid confirmation answer")
		}
		manual := s.GetSchema().GetName() == confirmationReview
		if manual {
			if c.Agent || !c.Holds(ID, run.Definition.ReviewerRole) || strings.TrimSpace(in.Reason) == "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The confirmation needs its current reviewer and evidence")
			}
			typ, id, _ := strings.Cut(run.Subject, "/")
			if _, err := c.ReadRecord(typ, id, at); err != nil {
				return nil, err
			}
			if !slices.Contains([]string{"accepted", "refused", "retry", "compensate", "complete"}, in.Outcome) || in.Difference != nil || in.Tag != "" || in.Completion != "" || in.CompletionError != "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid confirmation decision")
			}
		} else if !c.Automation {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Confirmation receipts belong to their original transport")
		}
		if in.Tag != "" {
			in.Party, _, _ = strings.Cut(in.Tag, ":")
			in.Compensation = strings.HasSuffix(in.Tag, ":compensate")
		}
		i := slices.IndexFunc(run.Parties, func(p platform.ConfirmationProgress) bool { return p.Name == in.Party })
		if s.GetSchema().GetName() != confirmationTick && in.Outcome != "complete" && i < 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a declared confirmation party")
		}
		transport := c.ConfirmationTransport()
		if transport == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Confirmation transport is unavailable")
		}
		if in.Difference != nil {
			if manual || in.Revision != run.Revision || len(in.Difference.Observed) > 4096 || len(in.Difference.Error) > 4096 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The confirmation changed while its source was read")
			}
			if err := transport.CheckConfirmationRead(run, i, at); err != nil {
				return nil, err
			}
		}
		if manual && in.Outcome == "complete" && (run.State != "expired" && run.CompletionError == "" || slices.ContainsFunc(run.Parties, func(p platform.ConfirmationProgress) bool { return p.State != "confirmed" || p.Compensation != "" })) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Completion requires every original party to be confirmed")
		}
		if manual && i >= 0 && in.Outcome != "complete" {
			p := run.Parties[i]
			state := p.State
			if in.Compensation {
				state = p.CompensationState
				if p.Compensation == "" || in.Outcome == "compensate" {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "There is no compensation to review")
				}
			} else if p.Compensation != "" && in.Outcome != "compensate" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Review the compensation for this party")
			}
			if in.Outcome == "retry" && state != "rejected" && state != "failed" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only a rejected or failed request may be retried")
			}
			if (in.Outcome == "accepted" || in.Outcome == "refused") && state != "unknown" && state != "rejected" && state != "failed" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This party has no unresolved result to review")
			}
			if in.Outcome == "compensate" && (p.State != "confirmed" && p.State != "unknown" || run.Definition.Parties[i].CompensateWriteback == "" && run.Definition.Parties[i].CompensateAction == "") {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Compensation needs a declared reverse request and an applied or unknown original result")
			}
		}
		return func(r *pb.ChangeRecord) {
			if i >= 0 && in.Outcome != "complete" {
				p := &run.Parties[i]
				if in.Outcome == "compensate" {
					b.compensateParty(c, r, &run, i, at)
				} else if in.Outcome == "retry" {
					if err := transport.RetryConfirmationDelivery(c, run, i, in.Compensation, at); err != nil {
						c.FailDecision(err)
						return
					}
					if in.Compensation {
						p.CompensationState, p.CompensationReason = "pending", in.Reason
					} else {
						p.State, p.Reason = "pending", in.Reason
						run.State = "pending"
					}
					if p.Protocol != nil {
						if err := transport.QueueConfirmation(c, r, run, i, in.Compensation, at); err != nil {
							c.FailDecision(err)
							return
						}
					}
				} else {
					state := map[string]string{"accepted": "confirmed", "refused": "rejected", "unknown": "unknown", "failed": "failed"}[in.Outcome]
					if state == "" {
						c.FailDecision(platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid confirmation result"))
						return
					}
					answer := in.Answer
					if len(answer) == 0 && !manual && in.Difference == nil {
						answer = slices.Clone(s.GetPayload())
					}
					if in.Compensation {
						p.CompensationState, p.CompensationAt, p.CompensationReason, p.CompensationAnswer = state, at, in.Reason, answer
					} else if p.State != "confirmed" && p.State != "rejected" && p.State != "failed" || manual {
						p.State, p.At, p.Reason, p.Answer = state, at, in.Reason, answer
					}
					if manual || in.Difference != nil || in.Compensation && state == "confirmed" {
						if err := transport.ResolveConfirmationDelivery(c, run, i, in.Compensation, at); err != nil {
							c.FailDecision(err)
							return
						}
					}
				}
				if in.Difference != nil {
					p.Differences = append(p.Differences, *in.Difference)
					if len(p.Differences) > 20 {
						p.Differences = p.Differences[len(p.Differences)-20:]
					}
					if spec := run.Definition.Parties[i].Reconcile; spec != nil {
						p.ReconcileDue = at.Add(time.Duration(spec.EverySeconds) * time.Second)
					}
				}
			}
			if in.Completion != "" || in.CompletionError != "" {
				run.Completion, run.CompletionError = in.Completion, in.CompletionError
			}
			if in.Outcome == "complete" {
				run.State, run.CompletionError = "confirmed", ""
			}
			b.confirmationState(c, r, &run, at)
			if run.State == "expired" && run.Definition.OnExpire == "compensate" || run.State == "compensating" {
				for i, p := range run.Parties {
					if (p.State == "confirmed" || p.State == "unknown") && p.Compensation == "" {
						b.compensateParty(c, r, &run, i, at)
					}
				}
			}
			if err := c.Put(r, run); err != nil {
				c.FailDecision(err)
			}
		}, nil
	})
}

func (b *Build) compensateParty(c platform.Caller, r *pb.ChangeRecord, run *platform.ConfirmationRun, i int, at time.Time) {
	p := &run.Parties[i]
	if p.Compensation != "" {
		return
	}
	p.Compensation, p.CompensationState = confirmationIntent(c.Tenant, p.Key+":compensate", p.CompensationRequest, p.Protocol), "pending"
	run.State = "compensating"
	if err := c.ConfirmationTransport().QueueConfirmation(c, r, *run, i, true, at); err != nil {
		p.CompensationState, p.CompensationAt, p.CompensationReason = "rejected", at, err.Code.String()+": "+err.Message
	}
}

func confirmationIntent(tenant, key string, request json.RawMessage, protocol *platform.ConfirmationProtocol) string {
	endpoint := ""
	if protocol != nil {
		endpoint = "protocol:" + protocol.Provider
	} else {
		var req WritebackRequest
		_ = json.Unmarshal(request, &req)
		endpoint = WritebackEndpoint + req.Connection
	}
	return tenant + ":" + ID + ":confirmation:" + key + ":" + endpoint
}

func (b *Build) confirmationState(c platform.Caller, r *pb.ChangeRecord, run *platform.ConfirmationRun, at time.Time) {
	if run.State == "compensating" || run.State == "compensated" {
		unresolved := slices.ContainsFunc(run.Parties, func(p platform.ConfirmationProgress) bool {
			return p.State == "pending" || (p.State == "confirmed" || p.State == "unknown") && (p.Compensation == "" || p.CompensationState != "confirmed")
		})
		if !unresolved {
			run.State = "compensated"
			c.Assign(r, platform.Assignment{Key: run.ID, Close: true})
		} else if slices.ContainsFunc(run.Parties, func(p platform.ConfirmationProgress) bool {
			return p.CompensationState == "unknown" || p.CompensationState == "rejected" || p.CompensationState == "failed"
		}) {
			c.Assign(r, confirmationAssignment(*run))
		}
		return
	}
	if run.Completion != "" {
		return // A declared partial result never silently turns into full completion.
	}
	all := !slices.ContainsFunc(run.Parties, func(p platform.ConfirmationProgress) bool { return p.State != "confirmed" })
	if all && (at.Before(run.Deadline) || run.State == "confirmed") {
		run.State = "confirmed"
		c.Assign(r, platform.Assignment{Key: run.ID, Close: true})
		return
	}
	if slices.ContainsFunc(run.Parties, func(p platform.ConfirmationProgress) bool { return p.State == "rejected" || p.State == "failed" }) {
		run.State, run.Partial = "partial", true
		if run.Definition.OnPartial == "reject" {
			run.State = "rejected"
		}
	}
	if !at.Before(run.Deadline) && run.State != "confirmed" && run.State != "rejected" {
		run.State = "expired"
	}
	unknown := slices.ContainsFunc(run.Parties, func(p platform.ConfirmationProgress) bool { return p.State == "unknown" })
	if run.State == "partial" || run.State == "expired" || run.State == "rejected" || unknown || run.CompletionError != "" {
		c.Assign(r, confirmationAssignment(*run))
	}
	if run.State == "expired" && run.Definition.OnExpire == "escalate" && !run.Escalated {
		run.Escalated = true
		c.Notify(platform.Notification{Key: "confirmation-expired:" + run.ID, Title: "Business confirmation overdue", Body: "A declared party has not confirmed before the business deadline.", Ref: confirmationType + "/" + run.ID}, at, platform.Recipient{AppRole: run.Definition.ReviewerRole})
	}
}

func confirmationAssignment(run platform.ConfirmationRun) platform.Assignment {
	return platform.Assignment{Key: run.ID, Ref: confirmationType + "/" + run.ID, Title: "Review business confirmation", Body: "A party rejected, did not answer, or reached its deadline.", To: []platform.Recipient{{AppRole: run.Definition.ReviewerRole}}}
}

func (b *Build) Run(c platform.Caller, job string, at time.Time) *kernel.Error {
	if job == "dispatches" {
		return b.runDispatches(c, at)
	}
	if job != "confirmations" {
		return nil
	}
	runs, err := platform.PendingConfirmations(c)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.Completion != "" || run.CompletionError != "" || run.State == "compensated" {
			continue
		}
		if !at.Before(run.Deadline) && !slices.Contains([]string{"confirmed", "expired", "compensating", "rejected"}, run.State) {
			_, err := platform.Decide(c, b, confirmationSubmission(c, run.ID, confirmationTick, "deadline", map[string]any{}, at), at)
			return err
		}
		action := ""
		if run.State == "confirmed" {
			action = run.Definition.OnComplete
		} else if run.Partial && run.Definition.OnPartial != "hold" && run.State != "expired" && run.State != "compensating" {
			action = run.Definition.OnPartialAction
		}
		if action != "" {
			change, problem := c.ConfirmationTransport().CompleteConfirmation(c, nil, run, action, at)
			payload := map[string]any{"completion": change}
			if problem != nil {
				payload["completionError"] = problem.Code.String() + ": " + problem.Message
			}
			key := fmt.Sprintf("completion:%d", run.Revision)
			_, err := platform.Decide(c, b, confirmationSubmission(c, run.ID, confirmationTick, key, payload, at), at)
			return err
		}
	}
	return nil
}

func confirmationSubmission(c platform.Caller, id, schema, key string, payload any, at time.Time) *pb.Submission {
	return &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "confirmation:" + id + ":" + key, Target: &pb.EntityRef{Type: confirmationType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
}
