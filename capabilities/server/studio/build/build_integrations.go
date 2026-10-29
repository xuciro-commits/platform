package build

import (
	"crypto/sha256"

	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"net/url"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"

	"reflect"
	"regexp"
	"slices"
	"sort"

	"strings"
	"time"
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
			return append([]string{r.Member}, b.host.(platform.ConfirmationHost).Holding(ID, r.Definition.ReviewerRole)...)
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
			prospective, selected, err := b.prospectiveIntegration(platform.AssetWriteback, item.id)
			if err != nil {
				return out, err
			}
			var w Writeback
			if selected {
				w = prospective.(Writeback)
			} else {
				record, ok := b.host.Record(WritebackType + "/" + item.id)
				var valid bool
				w, valid = record.(Writeback)
				if !ok || !valid {
					return out, fmt.Errorf("Confirmation writeback is unavailable")
				}
			}
			*item.ref = &platform.AssetRef{App: ID, Kind: platform.AssetWriteback, Name: w.Name}
		}
		if party.Reconcile != nil {
			prospective, selected, err := b.prospectiveIntegration(platform.AssetSource, party.Reconcile.Source)
			if err != nil {
				return out, err
			}
			var s Source
			if selected {
				s = prospective.(Source)
			} else {
				record, ok := b.host.Record(SourceType + "/" + party.Reconcile.Source)
				var valid bool
				s, valid = record.(Source)
				if !ok || !valid {
					return out, fmt.Errorf("Reconciliation source is unavailable")
				}
			}
			binding.Source = &platform.AssetRef{App: ID, Kind: platform.AssetSource, Name: s.Name}
		}
		if party.Protocol != "" {
			p, err := b.host.(platform.ConfirmationHost).FreezeConfirmationProtocol(party)
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

func (b *Build) consumedProtocols() []platform.Consumption {
	out := []platform.Consumption{}
	if b.host != nil {
		out = b.host.BuildProcessConsumptions()
	}

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
	for _, member := range b.host.(platform.ConfirmationHost).Holding(ID, spec.ReviewerRole) {
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

func (b *Build) connectionEntity() platform.Entity {
	return platform.Entity{Type: ConnectionType, Title: "Connection", Plural: "Connections", Model: Connection{}, Display: "title",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			next := record.(*Connection)
			if err := b.validConnection(*next); err != nil {
				return err
			}
			if old, ok := platform.Get[Connection](c, next.ID); ok && (old.Kind != next.Kind || old.Address != next.Address || old.Secret != next.Secret || old.AllowPrivate != next.AllowPrivate) {
				next.State, next.Requested, next.Last = "draft", false, nil
			}
			return nil
		},
		Description: "An external system the tenant reads from: a kind, an address and the name of its credential in the host's secret store.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "ready", Title: "Ready", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "check", Title: "Check", Description: "Reach the system once from the host and keep what answered.", From: []string{"draft", "ready"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.checkConnection},
				{Name: "retire", Title: "Retire", Description: "Stop sources from using the connection; the definition stays.", From: []string{"ready"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func connectionActions() []platform.Action {
	return []platform.Action{{Schema: SchemaConnectionChecked, Target: ConnectionType, Capability: "integrations", Title: "Keep check result", Description: "Retain what one check of a connection found.", Automation: true,
		Payload: []platform.Field{{Name: "revision", Type: "integer", Required: true, Description: "The connection revision checked"}, {Name: "check", Type: "json", Required: true, Description: "When, whether it answered and what"}}}}
}

func (b *Build) checkConnection(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	c, ok := record.(*Connection)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.validConnection(*c); err != nil {
		return err
	}
	if c.Address == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Configure the connection address before checking it")
	}
	c.Requested = true
	return nil
}

func (b *Build) validConnection(c Connection) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(c.Name, c.ID); err != nil {
		return refuse(err.Error())
	}
	if !slices.Contains(ConnectionKinds, c.Kind) {
		return refuse("A connection is one of http, odata, postgres")
	}
	if c.Address == "" && c.State != "ready" && !c.Requested {
		return nil
	}
	u, err := url.Parse(c.Address)
	if err != nil || u.Host == "" {
		return refuse("The address is not a URL")
	}
	if _, has := u.User.Password(); has || u.Query().Has("password") || u.Query().Has("passfile") {
		return refuse("The address carries no password; name a secret instead")
	}
	switch c.Kind {
	case "postgres":
		if u.Scheme != "postgres" && u.Scheme != "postgresql" {
			return refuse("A postgres connection's address starts with postgres://")
		}
	default:
		if u.Scheme != "https" && !(u.Scheme == "http" && c.AllowPrivate) {
			return refuse("A connection answers over https; http only to a private address when allowed")
		}
	}
	return nil
}

func (b *Build) submitConnectionChecked(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Revision uint32          `json:"revision"`
			Check    ConnectionCheck `json:"check"`
		}
		conn, ok := platform.Get[Connection](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Check.At.IsZero() || !conn.Requested || conn.Revision != payload.Revision {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Check result has no connection")
		}
		return func(r *pb.ChangeRecord) {
			check := payload.Check
			conn.Last, conn.Requested = &check, false
			conn.State = "draft"
			if check.OK {
				conn.State = "ready"
			}
			c.Put(r, conn)
		}, nil
	})
}

func (b *Build) datasetEntity() platform.Entity {
	return platform.Entity{Type: DatasetType, Title: "Dataset", Plural: "Datasets", Model: Dataset{}, Display: "title",
		Description: "Rows as they came from a source or a pipeline: versioned, schema inferred, read only by builders.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"}}
}

func (b *Build) datasetVersionEntity() platform.Entity {
	return platform.Entity{Type: DatasetVersionType, Title: "Dataset version", Plural: "Dataset versions", Model: DatasetVersion{}, Display: "version",
		Description: "One version of a dataset: its rows at one load.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Roles: []string{Builder, Integrator}, Capability: "integrations"}}
}

func datasetActions() []platform.Action {
	return []platform.Action{{Schema: SchemaDatasetLoad, Target: DatasetType, Capability: "integrations", Title: "Load a version", Description: "Append the rows one pull or run produced as the dataset's next version.", Roles: []string{Builder, Integrator},
		Payload: []platform.Field{{Name: "rows", Type: "json", Required: true, Description: "The rows, an array of objects"}, {Name: "producer", Type: "string", Description: "The source or pipeline that produced them"}, {Name: "marking", Type: "string", Description: "The marking the rows carry; raises the dataset's"}}}}
}

// DatasetSchema infers columns from rows: the widest type each column shows.
func DatasetSchema(rows []map[string]any) []DatasetField {
	types := map[string]string{}
	for _, row := range rows {
		for k, v := range row {
			t := platform.IntegrationTypeOf(v)
			if prev, ok := types[k]; !ok || prev == "" {
				types[k] = t
			} else if prev != t && t != "" {
				types[k] = "string"
			}
		}
	}
	out := make([]DatasetField, 0, len(types))
	for k, t := range types {
		if t == "" {
			t = "string"
		}
		out = append(out, DatasetField{Name: k, Type: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (b *Build) submitDatasetLoad(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Rows     []map[string]any `json:"rows"`
			Producer string           `json:"producer"`
			Marking  string           `json:"marking"`
		}
		ds, ok := platform.Get[Dataset](c, s.GetTarget().GetId())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The dataset does not exist")
		}
		fixed, err := b.ExecutionDataset(ds)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		if json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Rows == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A load carries an array of rows")
		}
		if len(payload.Rows) > ValueSourceRows {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A version holds at most {n} rows", ValueSourceRows)
		}
		return func(r *pb.ChangeRecord) {
			schema := DatasetSchema(payload.Rows)
			var drift []string
			before := map[string]bool{}
			for _, f := range ds.Schema {
				before[f.Name] = true
			}
			after := map[string]bool{}
			for _, f := range schema {
				after[f.Name] = true
				if ds.Version > 0 && !before[f.Name] {
					drift = append(drift, "+"+f.Name)
				}
			}
			for name := range before {
				if !after[name] {
					drift = append(drift, "-"+name)
				}
			}
			sort.Strings(drift)
			data, _ := json.Marshal(payload.Rows)
			ds.Version++
			ds.Schema = schema
			ds.Last = &DatasetLoad{At: now, Version: ds.Version, Rows: len(payload.Rows), Bytes: len(data), Drift: drift}
			if payload.Producer != "" {
				ds.Producer = payload.Producer
			}
			ds.Marking = HigherMarking(ds.Marking, payload.Marking)
			c.Put(r, ds)
			version := DatasetVersion{Record: platform.Record{ID: VersionID(ds.ID, ds.Version)}, Dataset: ds.ID, Version: ds.Version, At: now, Rows: len(payload.Rows), Data: data}
			if !c.Replaying {
				version.Request = s.GetIdempotencyKey()
			}
			c.Put(r, version)
			keep := fixed.Keep
			if keep <= 0 {
				keep = DatasetKeepDefault
			}
			if old := ds.Version - keep; old >= 1 {
				if v, ok := platform.Get[DatasetVersion](c, VersionID(ds.ID, old)); ok && !v.Archived {
					v.Archived, v.Data = true, json.RawMessage("[]")
					c.Put(r, v)
				}
			}
		}, nil
	})
}

func (b *Build) dispatchEntity() platform.Entity {
	return platform.Entity{Type: platform.DispatchType, Title: "Device dispatch", Plural: "Device dispatches", Model: platform.DispatchRun{}, Display: "subject",
		Description: "Declared commands, durable delivery attempts, status reads and authorized human intervention. Control loops and safety interlocks remain local to the equipment.",
		Scope: platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}, Participants: func(value any) []string {
			run := value.(platform.DispatchRun)
			return append([]string{run.Member}, b.host.(platform.ConfirmationHost).Holding(ID, run.Definition.ReviewerRole)...)
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

func (b *Build) checkDispatch(spec *platform.Dispatch, object Object, action Action, engines ...platform.RuleEngine) error {
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
		if !Named(set.Field) || used[set.Field] || set.Field == "id" || set.Field == "key" {
			return fmt.Errorf("Dispatch parameters must have distinct declared names")
		}
		used[set.Field] = true
		expr, err := platform.DispatchParameter(set, scope)
		if err != nil {
			return err
		}
		if _, err := platform.RuleSchema(expr, scope, lookup, engines...); err != nil {
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
		prospective, selected, err := b.prospectiveIntegration(platform.AssetWriteback, id)
		if err != nil {
			return platform.AssetRef{}, err
		}
		var fixed Writeback
		if selected {
			fixed = prospective.(Writeback)
		} else {
			record, known := b.host.Record(WritebackType + "/" + id)
			w, valid := record.(Writeback)
			if !known || !valid {
				return platform.AssetRef{}, fmt.Errorf("Dispatch writeback is unavailable")
			}
			fixed, err = b.ExecutionWriteback(w)
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
	connection, err = b.ConnectionConfiguration(connection)
	if err != nil {
		return out, err
	}
	if !slices.Contains([]string{"http", "odata"}, connection.Kind) {
		return out, fmt.Errorf("Dispatch target must be an HTTP or OData connection")
	}
	out.Target = platform.AssetRef{App: ID, Kind: platform.AssetConnection, Name: connection.Name}
	if spec.Readback.Source != "" {
		prospective, selected, err := b.prospectiveIntegration(platform.AssetSource, spec.Readback.Source)
		if err != nil {
			return out, err
		}
		var source Source
		if selected {
			source = prospective.(Source)
		} else {
			record, known := b.host.Record(SourceType + "/" + spec.Readback.Source)
			var valid bool
			source, valid = record.(Source)
			if !known || !valid {
				return out, fmt.Errorf("Dispatch status source is unavailable")
			}
			source, err = b.ExecutionSource(source)
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
		out.Source = &platform.AssetRef{App: ID, Kind: platform.AssetSource, Name: source.Name}
	}
	if spec.Readback.Observation != "" {
		prospective, selected, err := b.prospectiveIntegration(platform.AssetObservation, spec.Readback.Observation)
		if err != nil {
			return out, err
		}
		var definition platform.ObservationDefinition
		if selected {
			definition, err = b.observationDefinition(prospective.(Observation))
		} else {
			record, known := b.host.Record(ObservationType + "/" + spec.Readback.Observation)
			observation, valid := record.(Observation)
			if !known || !valid {
				return out, fmt.Errorf("Dispatch observation is unavailable")
			}
			definition, err = b.ExecutionObservation(observation)
		}
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
	effects := map[string]map[string]platform.AssetRef{}
	for i, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return nil, err
		}
		for _, action := range object.Actions {
			if action.Transaction != nil {
				refs, err := b.txnEffectBindings(action.Transaction)
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
			binding, err := b.bindDispatch(*action.Transaction.Dispatch, object)
			if err != nil {
				return nil, err
			}
			bindings[asset.Ref.Name+"."+action.Name] = binding
		}
		if object.CreateTransaction != nil {
			refs, err := b.txnEffectBindings(object.CreateTransaction)
			if err != nil {
				return nil, err
			}
			if len(refs) > 0 {
				effects[asset.Ref.Name+".create"] = refs
			}
		}
		if objectHasIntegrations(object) {
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
	var failure error
	mapTxn := func(txn *platform.BusinessTxn) {
		if txn == nil {
			return
		}
		platform.WalkBusinessTxn(txn, func(step platform.TxnStep) {
			if failure == nil && step.Effect != nil {
				step.Effect.Writeback, failure = resolve(platform.AssetWriteback, step.Effect.Writeback)
			}
		})
	}
	mapTxn(object.CreateTransaction)
	for i := range object.DispatchCommands {
		name, err := resolve(platform.AssetWriteback, object.DispatchCommands[i].Writeback)
		if err != nil {
			return err
		}
		object.DispatchCommands[i].Writeback = name
	}
	for _, action := range object.Actions {
		mapTxn(action.Transaction)
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
	return failure
}

func objectHasIntegrations(object Object) bool {
	found := len(object.DispatchCommands) > 0
	visit := func(txn *platform.BusinessTxn) {
		if txn != nil {
			platform.WalkBusinessTxn(txn, func(step platform.TxnStep) { found = found || step.Effect != nil })
		}
	}
	visit(object.CreateTransaction)
	for _, action := range object.Actions {
		visit(action.Transaction)
	}
	return found
}

func (b *Build) txnEffectBindings(txn *platform.BusinessTxn) (map[string]platform.AssetRef, error) {
	refs := map[string]platform.AssetRef{}
	var failure error
	platform.WalkBusinessTxn(txn, func(step platform.TxnStep) {
		if failure != nil || step.Effect == nil {
			return
		}
		name, err := b.integrationName(platform.AssetWriteback, step.Effect.Writeback)
		if err != nil {
			failure = err
			return
		}
		refs[step.Name] = platform.AssetRef{App: ID, Kind: platform.AssetWriteback, Name: name}
	})
	return refs, failure
}

func (b *Build) dispatchSchemaObject(object Object) (Object, error) {
	if !objectHasIntegrations(object) {
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
		expr, err := platform.DispatchParameter(set, scope)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		value, problem := platform.RuleAssignment(expr, scope, c, subject, values, at, b.conditionLookup)
		if problem != nil {
			return problem
		}
		payload[set.Field] = value
	}
	payload["key"] = key
	run := platform.DispatchRun{Record: platform.Record{ID: id}, Subject: txn.Subject + "/" + receipt.GetSubmission().GetTarget().GetId(), Action: receipt.GetSubmission().GetSchema().GetName(), Member: c.ID, Command: command, Definition: spec, Target: spec.Target, Key: key, State: "pending", StartedAt: at, StateAt: at, Deadline: at.Add(time.Duration(spec.DeadlineSeconds) * time.Second), ReadDue: at, Release: release, Assets: assets, Input: platform.Raw(payload), Readback: []platform.DispatchReading{}, Attempts: []platform.DispatchAttempt{}, Reviewers: []string{}}
	run.Reviewers = append(run.Reviewers, b.host.(platform.ConfirmationHost).Holding(ID, spec.ReviewerRole)...)
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

// Matching rules (Ⅰ-G). Several systems describe the same
// party, material or site under different ids; a matching rule says when two
// rows are one entity, and whose value wins per field. It is applied wherever
// rows land in an object - a source's pull, a pipeline's run - so the object
// converges to one record per real thing without anyone writing code.
const MatchType = "build.match"

func (b *Build) matchEntity() platform.Entity {
	return platform.Entity{Type: MatchType, Title: "Matching rule", Plural: "Matching rules", Model: Match{}, Display: "title",
		Description: "When rows from different systems are one party, material or site, and whose value wins per field.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			m := record.(*Match)
			if m.State == "published" {
				fixed, err := b.ExecutionMatching(*m)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				return b.checkMatch(c, fixed)
			}
			return nil
		},
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Check the rule and apply it to every row landing in the object from now on.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishMatch},
				{Name: "pause", Title: "Pause", Description: "Stop matching; records stay as they are.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func (b *Build) publishMatch(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	m, ok := record.(*Match)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkMatch(c, *m); err != nil {
		return err
	}
	m.Publisher = c.ID
	if !c.Replaying {
		fixed, err := b.freezeDirectIntegration(platform.AssetMatching, *m)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		*m = fixed.(Match)
	}
	return nil
}

func (b *Build) checkMatch(c platform.Caller, m Match) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(m.Name, m.ID); err != nil {
		return refuse(err.Error())
	}
	info, ok := b.lookupEntity(m.Object)
	if !ok {
		return refuse("The object {object} is not installed", m.Object)
	}
	if len(m.Keys) == 0 && len(m.Prefer) == 0 {
		return refuse("A matching rule has at least one key or one preferred producer")
	}
	for _, k := range m.Keys {
		if len(k.Fields) == 0 {
			return refuse("Every key names at least one field")
		}
		if !slices.Contains([]string{"", "exact", "folded", "digits"}, k.Normalize) {
			return refuse("A key is compared exact, folded or by digits")
		}
		for _, f := range k.Fields {
			if _, ok := info.Field(f); !ok {
				return refuse("{field} is not a field of {object}", f, m.Object)
			}
		}
	}
	for _, p := range m.Prefer {
		if p.Producer == "" {
			return refuse("A preference names the producer whose value wins")
		}
		if f, ok := info.Field(p.Field); !ok || f.ReadOnly {
			return refuse("{field} is not a writable field of {object}", p.Field, m.Object)
		}
	}
	others, _, _ := platform.Find[Match](c, platform.Query{Limit: 500})
	for _, other := range others {
		if other.ID == m.ID || other.State != "published" {
			continue
		}
		fixed, err := b.ExecutionMatching(other)
		if err != nil {
			return refuse(err.Error())
		}
		if fixed.Object == m.Object {
			return refuse("{object} already has the published rule {rule}; edit that one", m.Object, other.Title)
		}
	}
	return nil
}

func (b *Build) observationEntity() platform.Entity {
	return platform.Entity{Type: ObservationType, Title: "Observation source", Plural: "Observation sources", Model: Observation{}, Display: "title",
		Description: "Read external time-series summaries and apply declared business decisions without retaining raw points.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Validate: func(c platform.Caller, value any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			record := value.(*Observation)
			if record.State == "retired" && record.Published != "" {
				if _, err := b.ExecutionObservation(*record); err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
				}
				return nil
			}
			if err := b.checkObservationDefinition(c, *record, record.State == "published"); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}, {Name: "retired", Title: "Retired", Tone: "neutral"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Retain the logical source, authorized reads and declared decision as one immutable version.", From: []string{"draft", "published", "retired"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: func(c platform.Caller, value any, _ json.RawMessage, _ time.Time) *kernel.Error {
					record := value.(*Observation)
					if c.Replaying {
						return nil
					}
					if err := b.checkObservation(c, *record); err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
					}
					record.Publisher = c.ID
					record.PublisherScopes = string(platform.Raw(c.Member.Scopes))
					fixed, err := b.freezeDirectIntegration(platform.AssetObservation, *record)
					if err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
					}
					*record = fixed.(Observation)
					return nil
				}},
				{Name: "retire", Title: "Retire", Description: "Stop new reads and evaluations while retaining every published definition.", From: []string{"published"}, To: []string{"retired"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}},
			}},
	}
}

func (b *Build) observationDefinition(record Observation) (platform.ObservationDefinition, error) {
	connection, err := b.integrationName(platform.AssetConnection, record.Connection)
	if err != nil {
		return platform.ObservationDefinition{}, err
	}
	object, ok := b.lookupEntity(record.Object)
	if !ok {
		return platform.ObservationDefinition{}, fmt.Errorf("The observation object is unavailable")
	}
	return platform.ObservationDefinition{Name: record.Name, Title: record.Title, Description: record.Description,
		Connection: platform.AssetRef{App: ID, Kind: platform.AssetConnection, Name: connection}, Object: platform.AssetRef{App: object.App, Kind: platform.AssetObject, Name: record.Object},
		DeviceField: record.DeviceField, Signal: record.Signal, Unit: record.Unit, Path: record.Path, WindowSeconds: record.WindowSeconds,
		ExpectedRetentionSeconds: record.ExpectedRetentionSeconds, ExpectedGranularitySeconds: record.ExpectedGranularitySeconds,
		Roles: slices.Clone(record.Roles), Scheduled: record.Scheduled, RequireComplete: record.RequireComplete, Decision: record.Decision, Intervals: record.Intervals}, nil
}

func (b *Build) checkObservation(c platform.Caller, record Observation) error {
	return b.checkObservationDefinition(c, record, true)
}

func (b *Build) checkObservationDefinition(c platform.Caller, record Observation, published bool) error {
	// Authoring may reference a saved model before its joint publication.
	// This private shape is used only for draft validation; execution still
	// resolves the exact installed or selected candidate definition.
	if !published && !b.jointMode {
		if _, selected := b.jointEntities[record.Object]; !selected {
			objects, err := readDefinitionInventory[Object](b.host.Automation(platform.Caller{}, ID))
			if err != nil {
				return err
			}
			for _, object := range objects {
				if object.Archived || TypeOf(object.Name) != record.Object {
					continue
				}
				shape, err := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
				if err != nil {
					return err
				}
				view := *b
				view.jointEntities = maps.Clone(b.jointEntities)
				if view.jointEntities == nil {
					view.jointEntities = map[string]platform.EntityInfo{}
				}
				view.jointEntities[record.Object] = shape
				b = &view
				break
			}
		}
	}
	if err := b.checkName(record.Name, record.ID); err != nil {
		return err
	}
	if record.Published != "" {
		var prior platform.ReleaseAsset
		if ValueDecodeIntegration([]byte(record.Published), &prior) != nil || prior.Ref.Name != record.Name {
			return fmt.Errorf("A published observation source keeps its name")
		}
	}
	definition, err := b.observationDefinition(record)
	if err != nil {
		return err
	}
	if err := platform.CheckObservationDefinition(definition, b.lookupEntity, c.Rules()); err != nil {
		return err
	}
	rows, err := b.integrationInventory(platform.AssetObservation)
	if err != nil {
		return err
	}
	for _, row := range rows {
		other := row.(Observation)
		if other.Name == record.Name && other.ID != record.ID {
			return fmt.Errorf("The observation source name is already used")
		}
	}
	for _, role := range record.Roles {
		declared := slices.Contains(b.Manifest().AllRoles(), role) || role == platform.AnyMember
		for _, model := range b.jointEntities {
			_, selected := model.Scope.Levels[role]
			declared = declared || selected
		}
		if !declared {
			return fmt.Errorf("The observation role is not declared by its owner")
		}
	}
	if definition.Decision != nil {
		owner, action, ok := b.observationAction(definition)
		if !ok || owner != definition.Decision.Action.App || action.Target != definition.Object.Name {
			return fmt.Errorf("The observation decision must use an action on its declared object")
		}
		if err := platform.CheckObservationAction(definition, action); err != nil {
			return err
		}
	}
	connection, ok := platform.Get[Connection](b.host.Automation(platform.Caller{}, ID), record.Connection)
	if !ok || connection.Archived {
		return fmt.Errorf("The observation connection is unavailable")
	}
	connection, err = b.ConnectionConfiguration(connection)
	if err != nil {
		return err
	}
	if connection.Kind != "http" {
		return fmt.Errorf("The observation connection must be an HTTP connection")
	}
	return nil
}

// A joint candidate may declare the object's action in the same publication.
// Read that original object draft rather than requiring an earlier live install.
func (b *Build) observationAction(definition platform.ObservationDefinition) (string, platform.Action, bool) {
	if _, joint := b.jointEntities[definition.Object.Name]; joint {
		objects, err := readDefinitionInventory[Object](b.host.Automation(platform.Caller{}, ID))
		if err != nil {
			return "", platform.Action{}, false
		}
		for _, object := range objects {
			if object.Archived || TypeOf(object.Name) != definition.Object.Name {
				continue
			}
			for _, action := range objectActions(object, Entity(object)) {
				if action.Schema == definition.Decision.Action.Name {
					return ID, action, true
				}
			}
		}
	}
	return b.host.Action(definition.Decision.Action.Name)
}

// ExecutionObservation returns accepted declaration bytes, never a mutable
// editor row and never observations fetched from the external store.
func (b *Build) ExecutionObservation(record Observation) (platform.ObservationDefinition, error) {
	var asset platform.ReleaseAsset
	if record.Published == "" || ValueDecodeIntegration([]byte(record.Published), &asset) != nil || asset.Ref != (platform.AssetRef{App: ID, Kind: platform.AssetObservation, Name: record.Name}) {
		return platform.ObservationDefinition{}, fmt.Errorf("The observation source is not published")
	}
	if err := validateIntegrationFamily(record, asset); err != nil {
		return platform.ObservationDefinition{}, err
	}
	var definition platform.ObservationDefinition
	if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
		return definition, err
	}
	return definition, nil
}

var _ ObservationDefinitions = (*Build)(nil)

// StepKinds are the transformations a pipeline may declare, in the order a
// builder usually needs them.
var StepKinds = []string{"select", "rename", "cast", "filter", "compute", "lookup", "join", "dedupe", "aggregate", "sort"}

func (b *Build) pipelineEntity() platform.Entity {
	return platform.Entity{Type: PipelineType, Title: "Pipeline", Plural: "Pipelines", Model: Pipeline{}, Display: "title",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			p := record.(*Pipeline)
			if !c.Replaying {
				if err := p.NormalizeFormulaRules(); err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
			}
			if p.State == "published" {
				fixed, err := b.ExecutionPipeline(*p)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				return b.checkPipeline(c, fixed)
			}
			return nil
		},
		Description: "Declared steps from a dataset to a dataset or an object, with expectations that quarantine bad rows.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Publish this pipeline definition. Use Run now to start it.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishPipeline},
				{Name: "run", Title: "Run now", Description: "Ask for one run at the next tick.", From: []string{"published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: requestRun},
				{Name: "pause", Title: "Pause", Description: "Stop running; the definition stays.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func pipelineActions() []platform.Action {
	return []platform.Action{{Schema: SchemaPipelineRan, Target: PipelineType, Capability: "integrations", Title: "Keep run result", Description: "Retain what one run of a pipeline did.", Roles: []string{Builder, Integrator},
		Payload: []platform.Field{{Name: "run", Type: "json", Required: true, Description: "The run's counts, quarantine and failures"}}}}
}

func requestRun(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	p, ok := record.(*Pipeline)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !c.Replaying {
		p.Runner, p.RunnerScopes = c.ID, string(platform.Raw(c.Member.Scopes))
	}
	p.Requested = true
	return nil
}

func (b *Build) publishPipeline(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	p, ok := record.(*Pipeline)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !c.Replaying {
		if err := p.NormalizeFormulaRules(); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	if err := b.checkPipeline(c, *p); err != nil {
		return err
	}
	if c.Replaying {
		// Old input journals retain their original publish-and-start meaning.
		p.Runner, p.Requested = c.ID, true
	}
	if !c.Replaying {
		fixed, err := b.freezeDirectIntegration(platform.AssetPipeline, *p)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		*p = fixed.(Pipeline)
	}
	return nil
}

// CheckPipeline shares publication validation with the read-only draft trial.
func (b *Build) CheckPipeline(c platform.Caller, p Pipeline) *kernel.Error {
	return b.checkPipeline(c, p)
}

func (b *Build) checkPipeline(c platform.Caller, p Pipeline) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(p.Name, p.ID); err != nil {
		return refuse(err.Error())
	}
	if _, ok := platform.Get[Dataset](c, p.Input); !ok {
		return refuse("The input dataset does not exist")
	}
	marking, markErr := b.inputMarking(c, p)
	if markErr != nil {
		return refuse(markErr.Error())
	}
	for i, s := range p.Steps {
		if err := checkStep(c, s); err != nil {
			return refuse("Step {n} ({kind}): {why}", i+1, s.Kind, err.Error())
		}
	}
	for _, e := range p.Expectations {
		if e.Column == "" || !slices.Contains([]string{"notnull", "unique", "in", "matches", "range"}, e.Rule) {
			return refuse("An expectation names a column and one of notnull, unique, in, matches, range")
		}
		if e.Rule == "matches" {
			if _, err := regexp.Compile(e.Value); err != nil {
				return refuse("The expectation on {column} has an invalid pattern", e.Column)
			}
		}
	}
	outputs := 0
	for _, set := range []bool{p.OutputDataset != "", p.OutputObject != "", p.OutputEnterprise != nil} {
		if set {
			outputs++
		}
	}
	switch {
	case outputs != 1:
		return refuse("A pipeline writes either a dataset, an object or the enterprise model")
	case p.OutputEnterprise != nil:
		if Guarded(marking) {
			return refuse("The enterprise model is tenant-readable; confidential or restricted rows cannot land there")
		}
		if err := p.OutputEnterprise.Check(); err != nil {
			return refuse(err.Error())
		}
	case p.OutputDataset != "":
		if p.OutputDataset == p.Input {
			return refuse("A pipeline does not write its own input")
		}
		if _, ok := platform.Get[Dataset](c, p.OutputDataset); !ok {
			return refuse("The output dataset does not exist")
		}
	default:
		info, ok := b.lookupEntity(p.OutputObject)
		if !ok {
			return refuse("The output object {object} is not installed", p.OutputObject)
		}
		if p.Key == "" {
			return refuse("Name the column that is the record id")
		}
		if Guarded(marking) {
			if open := UnguardedFields(info, nil); len(open) > 0 {
				return refuse("The input is {marking}; name who reads {object}'s fields {fields} first, or lower the marking", marking, p.OutputObject, strings.Join(open, ", "))
			}
		}
	}
	if p.Every != "" {
		every, err := time.ParseDuration(p.Every)
		if err != nil || every < time.Minute || every > 366*24*time.Hour {
			return refuse("A pipeline runs every period between 1m and a year, such as 15m, 1h or 24h")
		}
	}
	return nil
}

func checkStep(c platform.Caller, s platform.TransformStep) error {
	need := func(ok bool, what string) error {
		if !ok {
			return fmt.Errorf("needs %s", what)
		}
		return nil
	}
	switch s.Kind {
	case "select", "dedupe":
		return need(len(s.Columns) > 0, "columns")
	case "rename":
		return need(s.From != "" && s.To != "", "from and to")
	case "cast":
		return need(s.Column != "" && slices.Contains([]string{"string", "number", "boolean", "date"}, s.Type), "a column and a type")
	case "filter":
		return need(s.Column != "" && slices.Contains([]string{"=", "!=", "<", "<=", ">", ">=", "contains", "empty", "notempty"}, s.Op), "a column and an operator")
	case "compute":
		if s.To == "" {
			return fmt.Errorf("needs a target column")
		}
		if s.FormulaExpr == nil {
			_, err := platform.FormulaFields(s.Formula)
			return err
		}
		schema := platform.ExprSchema{Refs: map[string]platform.ExprType{}}
		var refs func(platform.Expr)
		refs = func(e platform.Expr) {
			if e.Ref != "" {
				schema.Refs[e.Ref] = platform.ExprType{Kind: e.Kind}
			}
			for _, a := range e.Args {
				refs(a)
			}
			if e.In != nil {
				refs(*e.In)
			}
		}
		refs(*s.FormulaExpr)
		_, err := c.Rules().CompileExpr(*s.FormulaExpr, schema)
		return err
	case "lookup", "join":
		if s.Dataset == "" || s.Column == "" || s.Match == "" {
			return fmt.Errorf("needs a dataset, a column here and a match column there")
		}
		if _, ok := platform.Get[Dataset](c, s.Dataset); !ok {
			return fmt.Errorf("the dataset does not exist")
		}
		if s.Kind == "lookup" && len(s.Columns) == 0 {
			return fmt.Errorf("names the columns to take")
		}
		return nil
	case "aggregate":
		if len(s.Measures) == 0 {
			return fmt.Errorf("needs measures")
		}
		for _, m := range s.Measures {
			if m.To == "" || !slices.Contains([]string{"sum", "min", "max", "count", "avg"}, m.Fn) || (m.Fn != "count" && m.Column == "") {
				return fmt.Errorf("each measure is sum, min, max, avg of a column or count, into a named column")
			}
		}
		return nil
	case "sort":
		return need(s.Column != "", "a column")
	}
	return fmt.Errorf("is not one of %s", strings.Join(StepKinds, ", "))
}

func (b *Build) submitPipelineRan(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Run PipelineRun `json:"run"`
		}
		p, ok := platform.Get[Pipeline](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Run.At.IsZero() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Run result has no pipeline")
		}
		if p.Last != nil && p.Last.Execution != nil && p.Last.Execution.Pending {
			if err := checkIntegrationExecution(p.Last.Execution, payload.Run.Execution, c.Member); err != nil {
				return nil, err
			}
			if payload.Run.DefinitionVersion != p.Last.DefinitionVersion || !payload.Run.At.Equal(p.Last.At) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Integration execution changed before its result was accepted")
			}
		} else {
			equivalent, versionErr := retainedExecutionEquivalent(p, payload.Run.DefinitionVersion)
			if versionErr != nil || !equivalent {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Integration definition changed before its result was accepted")
			}
			if err := checkIntegrationExecution(nil, payload.Run.Execution, c.Member); err != nil {
				return nil, err
			}
		}
		return func(r *pb.ChangeRecord) {
			run := payload.Run
			p.Last, p.Requested = &run, run.Execution != nil && run.Execution.Pending
			p.Marking = HigherMarking(p.Marking, run.Marking)
			c.Put(r, p)
		}, nil
	})
}

// inputMarking is the highest marking of the datasets a pipeline reads.
func (b *Build) inputMarking(c platform.Caller, p Pipeline) (string, error) {
	marking := ""
	for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
		if ds, ok := platform.Get[Dataset](c, id); ok {
			fixed, err := b.ExecutionDataset(ds)
			if err != nil {
				return "", err
			}
			marking = HigherMarking(marking, fixed.Marking)
		}
	}
	return marking, nil
}

func protocolReadName(typ, name string) string { return "protocol:" + typ + ":" + name }

func objectProvisions(object Object, entity platform.Entity) []platform.Provision {
	if len(object.Protocols) == 0 {
		return nil
	}
	catalog := platform.NewCatalog(platform.EntityActions(entity)...)
	var out []platform.Provision
	for _, declaration := range object.Protocols {
		p := platform.Provision{Protocol: platform.Protocol{Name: declaration.Name, Version: declaration.Version, Reads: slices.Clone(declaration.Reads)}, Actions: map[string]string{}, Reads: map[string]string{}, Events: map[string]string{}}
		verbs := make([]string, 0, len(declaration.Actions))
		for verb := range declaration.Actions {
			verbs = append(verbs, verb)
		}
		slices.Sort(verbs)
		for _, verb := range verbs {
			schema := entity.Type + "." + declaration.Actions[verb]
			action, ok := catalog.Action(schema)
			if !ok {
				continue // The owner validator reports missing action bindings.
			}
			action.Schema, action.Roles, action.Automation = verb, nil, false
			p.Protocol.Actions = append(p.Protocol.Actions, action)
			p.Actions[verb] = schema
		}
		for _, read := range declaration.Reads {
			p.Reads[read] = protocolReadName(entity.Type, read)
		}
		verbs = verbs[:0]
		for event := range declaration.Events {
			verbs = append(verbs, event)
		}
		slices.Sort(verbs)
		for _, event := range verbs {
			p.Events[event] = entity.Type + "." + declaration.Events[event]
			p.Protocol.Events = append(p.Protocol.Events, platform.ProtocolEvent{Name: event, Title: event})
		}
		if declaration.Contract != nil {
			p.Protocol = *declaration.Contract
		}
		out = append(out, p)
	}
	return out
}

func (b *Build) checkObjectProtocols(object Object) error {
	if len(object.Protocols) == 0 {
		return nil
	}
	if len(object.Protocols) > 16 {
		return fmt.Errorf("An object supports at most 16 protocol bindings")
	}
	if err := checkProcess(object, b.conditionLookup); err != nil {
		return err
	}
	draft := object
	draft.Protocols = slices.Clone(object.Protocols)
	for i := range draft.Protocols {
		draft.Protocols[i].Contract = nil
	}
	declaration := draft
	declaration.Protocols = nil
	entity := Entity(declaration)
	if _, err := platform.Describe(ID, entity, func(reflect.Type) string { return "?" }); err != nil {
		return err
	}
	catalog := platform.NewCatalog(platform.EntityActions(entity)...)
	generated := objectProvisions(draft, entity)
	seen := map[string]bool{}
	for i, declaration := range object.Protocols {
		actual := generated[i].Protocol
		id := actual.ID()
		if declaration.Name == "" || strings.ContainsAny(declaration.Name, "/# :\t\n") || declaration.Version < 1 || seen[id] || len(declaration.Actions) == 0 {
			return fmt.Errorf("A protocol binding needs a unique name, positive version and declared actions")
		}
		seen[id] = true
		for _, bindings := range []map[string]string{declaration.Actions, declaration.Events} {
			for verb, action := range bindings {
				_, exists := catalog.Action(entity.Type + "." + action)
				if !Named(verb) || !exists {
					return fmt.Errorf("Protocol %s maps %s to an undeclared object action", id, verb)
				}
			}
		}
		reads := map[string]bool{}
		for _, read := range declaration.Reads {
			if !Named(read) || reads[read] {
				return fmt.Errorf("Protocol reads must be unique named record projections")
			}
			reads[read] = true
		}
		if declaration.Contract != nil {
			if err := platform.CheckProtocol(*declaration.Contract, actual); err != nil {
				return err
			}
		}
		for typ, installed := range b.installed {
			for _, provision := range installed.Protocols {
				if provision.Protocol.ID() != id {
					continue
				}
				if typ != entity.Type {
					return fmt.Errorf("Protocol %s is already provided by %s", id, typ)
				}
				if err := platform.CheckProtocol(provision.Protocol, actual); err != nil {
					return err
				}
			}
		}
		if host, ok := b.host.(interface {
			ProtocolContract(string) (platform.Protocol, bool)
		}); ok {
			if contract, exists := host.ProtocolContract(id); exists {
				if err := platform.CheckProtocol(contract, actual); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (b *Build) objectProtocols() ([]platform.Provision, []string) {
	var provisions []platform.Provision
	var reads []string
	for _, typ := range sortedTypes(b.installed) {
		for _, provision := range b.installed[typ].Protocols {
			provisions = append(provisions, provision)
			for _, read := range provision.Protocol.Reads {
				reads = append(reads, provision.Reads[read])
			}
		}
	}
	return provisions, reads
}

func protocolReadSources(reads []string) map[string]string {
	sources := map[string]string{ReadReleaseProfile: "record"}
	for _, read := range reads {
		sources[read] = "record"
	}
	return sources
}

func (b *Build) readProtocol(c platform.Caller, name string) (any, *kernel.Error) {
	for _, typ := range sortedTypes(b.installed) {
		entity := b.installed[typ]
		for _, provision := range entity.Protocols {
			for _, read := range provision.Reads {
				if read != name {
					continue
				}
				if owner, ok := b.host.(interface {
					ReadProtocolRecords(platform.Caller, string) ([]any, *kernel.Error)
				}); ok {
					return owner.ReadProtocolRecords(c, typ)
				}
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Protocol records require the original member read owner")
			}
		}
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}

func (b *Build) sourceEntity() platform.Entity {
	return platform.Entity{Type: SourceType, Title: "Data source", Plural: "Data sources", Model: Source{}, Display: "title", Description: "An external JSON endpoint whose rows become records of one object, pulled on a period or on request.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			s := record.(*Source)
			if s.State == "published" {
				fixed, err := b.ExecutionSource(*s)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				if err := b.checkSourceConnection(c, *s); err != nil {
					return err
				}
				return b.checkSource(c, fixed)
			}
			return nil
		},
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Publish this source definition. Use Pull now to start it.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishSource},
				{Name: "pull", Title: "Pull now", Description: "Ask for one pull at the next tick.", From: []string{"published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.requestPull},
				{Name: "reset", Title: "Reset cursor", Description: "Forget the incremental cursor and pull everything at the next tick.", From: []string{"published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.resetCursor},
				{Name: "pause", Title: "Pause", Description: "Stop pulling; the mapping stays.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func sourceActions() []platform.Action {
	return []platform.Action{{Schema: SchemaSourcePulled, Target: SourceType, Capability: "integrations", Title: "Keep pull result", Description: "Retain what one pull of a data source decided.", Roles: []string{Builder, Integrator},
		Payload: []platform.Field{{Name: "pull", Type: "json", Required: true, Description: "The pull's counts and first failures"}}}}
}

func (b *Build) requestPull(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	s, ok := record.(*Source)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if s.Stream {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A continuous source is consumed through its Flow instance")
	}
	if problem := b.prepareSourcePull(c, s); problem != nil {
		return problem
	}
	if !c.Replaying {
		s.Puller, s.PullerScopes = c.ID, string(platform.Raw(c.Member.Scopes))
	}
	s.Requested = true
	return nil
}

func (b *Build) resetCursor(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	s, ok := record.(*Source)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if s.Stream {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A continuous source cursor belongs to its Flow instance")
	}
	if problem := b.prepareSourcePull(c, s); problem != nil {
		return problem
	}
	if s.Last != nil && s.Last.Execution != nil && s.Last.Execution.Pending {
		last, execution := *s.Last, *s.Last.Execution
		execution.Pending = false
		last.Execution = &execution
		last.Error = "The previous pull was stopped by resetting its cursor"
		s.Last = &last
	}
	if !c.Replaying {
		s.Puller, s.PullerScopes = c.ID, string(platform.Raw(c.Member.Scopes))
	}
	s.Cursor, s.Requested = "", true
	return nil
}

func (b *Build) prepareSourcePull(c platform.Caller, source *Source) *kernel.Error {
	if c.Replaying {
		return nil
	}
	fixed, err := b.ExecutionSource(*source)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	if problem := b.checkSourceConnection(c, fixed); problem != nil {
		return problem
	}
	if fixed.Connection != "" {
		connection, ok := platform.Get[Connection](c, fixed.Connection)
		if !ok {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Connection {connection} does not exist", fixed.Connection)
		}
		if _, err := b.ExecutionConnection(connection); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
		}
	}
	return nil
}

func (b *Build) publishSource(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	s, ok := record.(*Source)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkSource(c, *s); err != nil {
		return err
	}
	if c.Replaying {
		// Old input journals retain their original publish-and-start meaning.
		s.Puller, s.Requested = c.ID, !s.Stream
	}
	if !c.Replaying {
		s.Publisher, s.PublisherScopes = c.ID, string(platform.Raw(c.Member.Scopes))
		fixed, err := b.freezeDirectIntegration(platform.AssetSource, *s)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		*s = fixed.(Source)
	}
	if s.Stream {
		s.Cursor, s.Last = "", nil
	}
	return nil
}

func (b *Build) checkSource(c platform.Caller, s Source) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(s.Name, s.ID); err != nil {
		return refuse(err.Error())
	}
	if !slices.Contains([]string{"", "json", "csv", "odata", "table"}, s.Profile) {
		return refuse("A source profile is one of json, csv, odata, table")
	}
	if err := b.checkSourceConnection(c, s); err != nil {
		return err
	}
	if s.Since != "" && !identifier(s.Since) {
		return refuse("The incremental column is a plain identifier")
	}
	if s.Stream {
		if s.Profile != "table" || s.Since == "" || s.Object != "" || s.Dataset != "" || s.Key != "" || len(s.Mapping) != 0 || s.Every != "" {
			return refuse("A continuous source uses an incremental PostgreSQL table, without a materialization target or import schedule")
		}
		return nil
	}
	if s.Dataset != "" {
		if s.Object != "" {
			return refuse("A source feeds either a dataset or an object")
		}
		if _, ok := platform.Get[Dataset](c, s.Dataset); !ok {
			return refuse("The target dataset does not exist")
		}
		return checkEvery(s.Every)
	}
	if s.Connection != "" {
		if conn, ok := platform.Get[Connection](c, s.Connection); ok {
			fixed, err := b.ConnectionConfiguration(conn)
			if err != nil {
				return refuse(err.Error())
			}
			if Guarded(fixed.Marking) {
				return refuse("A confidential or restricted connection feeds a dataset; use a pipeline to apply field access rules")
			}
		}
	}
	info, ok := b.lookupEntity(s.Object)
	if !ok {
		return refuse("The target object {object} is not installed", s.Object)
	}
	if s.Key == "" || len(s.Mapping) == 0 {
		return refuse("A data source names the row field that identifies a record and maps at least one field")
	}
	seen := map[string]bool{}
	for _, m := range s.Mapping {
		f, known := info.Field(m.To)
		if m.From == "" || !known || f.ReadOnly || seen[m.To] {
			return refuse("Field mapping {to} must name a writable field of {object} once", m.To, s.Object)
		}
		if !slices.Contains([]string{"", "string", "number", "boolean", "date"}, m.Convert) {
			return refuse("Conversion {convert} is not one of string, number, boolean, date", m.Convert)
		}
		seen[m.To] = true
	}
	return checkEvery(s.Every)
}

func (b *Build) checkSourceConnection(c platform.Caller, s Source) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if s.Connection == "" {
		if s.Profile == "odata" || s.Profile == "table" {
			return refuse("An {profile} source reads through a connection", s.Profile)
		}
		u, err := url.Parse(s.URL)
		if err != nil || u.Host == "" || u.Scheme != "https" && !(u.Scheme == "http" && s.AllowPrivate) {
			return refuse("A data source answers over https; http only to a private address when allowed")
		}
		if s.Header != "" && !strings.Contains(s.Header, ":") {
			return refuse("A request header reads Name: value")
		}
		return nil
	}
	conn, ok := platform.Get[Connection](c, s.Connection)
	if ok {
		fixed, err := b.ConnectionConfiguration(conn)
		if err != nil {
			return refuse(err.Error())
		}
		conn = fixed
	}
	if !ok {
		return refuse("Connection {connection} does not exist", s.Connection)
	}
	switch {
	case (s.Profile == "odata") != (conn.Kind == "odata"), (s.Profile == "table") != (conn.Kind == "postgres"):
		return refuse("A {profile} source needs a matching connection kind", s.Profile)
	case s.Profile == "odata" && s.Entity == "":
		return refuse("An OData source names its entity set")
	case s.Profile == "table" && !tableName(s.Entity):
		return refuse("A table source names schema.table")
	case s.Profile == "table" && !simpleWhere(s.Filter):
		return refuse("A table filter is a simple comparison such as plant = '1000' and active = true")
	}
	return nil
}

func checkEvery(every string) *kernel.Error {
	if every != "" {
		d, parseErr := time.ParseDuration(every)
		if parseErr != nil || d < time.Minute || d > 366*24*time.Hour {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A source pulls every period between 1m and a year, such as 15m, 1h or 24h")
		}
	}
	return nil
}

func (b *Build) submitSourcePulled(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Pull SourcePull `json:"pull"`
		}
		src, ok := platform.Get[Source](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Pull.At.IsZero() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Pull result has no source")
		}
		if src.Last != nil && src.Last.Execution != nil && src.Last.Execution.Pending {
			if err := checkIntegrationExecution(src.Last.Execution, payload.Pull.Execution, c.Member); err != nil {
				return nil, err
			}
			if payload.Pull.DefinitionVersion != src.Last.DefinitionVersion || !payload.Pull.At.Equal(src.Last.At) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Integration execution changed before its result was accepted")
			}
		} else {
			equivalent, versionErr := retainedExecutionEquivalent(src, payload.Pull.DefinitionVersion)
			if versionErr != nil || !equivalent {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Integration definition changed before its result was accepted")
			}
			if err := checkIntegrationExecution(nil, payload.Pull.Execution, c.Member); err != nil {
				return nil, err
			}
		}
		return func(r *pb.ChangeRecord) {
			pull := payload.Pull
			src.Last, src.Requested = &pull, pull.Execution != nil && pull.Execution.Pending
			if (pull.Execution == nil || !pull.Execution.Pending) && pull.Cursor != "" && pull.Error == "" && pull.Failed == 0 {
				src.Cursor = pull.Cursor
			}
			c.Put(r, src)
		}, nil
	})
}

func checkIntegrationExecution(prior, next *IntegrationExecution, member platform.Member) *kernel.Error {
	if next == nil {
		if prior != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The pending integration execution must finish first")
		}
		return nil // historical/direct-development result without a preparation
	}
	digest, err := hex.DecodeString(next.Snapshot)
	if next.ID == "" || len(next.ID) > 64 || err != nil || len(digest) != 32 || next.Member != member.ID {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Integration execution has no retained input")
	}
	if prior != nil {
		fixed := *next
		fixed.Pending = prior.Pending
		if !reflect.DeepEqual(*prior, fixed) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The pending integration execution must finish first")
		}
	} else if !next.Pending || !reflect.DeepEqual(next.Scopes, member.Scopes) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Integration execution was not prepared")
	}
	return nil
}

var identifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func identifier(s string) bool { return identifierRE.MatchString(s) }
func tableName(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 2 {
		return false
	}
	for _, p := range parts {
		if !identifier(p) {
			return false
		}
	}
	return true
}

// simpleWhere accepts "col op literal [and ...]" with quoted strings, numbers,
// true/false and null - enough to pick a plant or a status, never a statement.
var simpleWhereRE = regexp.MustCompile(`^\s*[A-Za-z_][A-Za-z0-9_]*\s*(=|<>|!=|<|>|<=|>=)\s*('[^';]*'|-?[0-9]+(\.[0-9]+)?|true|false|null)(\s+and\s+[A-Za-z_][A-Za-z0-9_]*\s*(=|<>|!=|<|>|<=|>=)\s*('[^';]*'|-?[0-9]+(\.[0-9]+)?|true|false|null))*\s*$`)

func simpleWhere(s string) bool { return s == "" || simpleWhereRE.MatchString(strings.ToLower(s)) }

func (b *Build) writebackEntity() platform.Entity {
	return platform.Entity{Type: WritebackType, Title: "Writeback", Plural: "Writebacks", Model: Writeback{}, Display: "title",
		Description: "An accepted action on an object sent to an external system through a connection: queued, in order, retried with the same key for receiver deduplication.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			w := record.(*Writeback)
			if w.State == "published" {
				fixed, err := b.ExecutionWriteback(*w)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				if err := b.checkWritebackConnection(c, *w); err != nil {
					return err
				}
				return b.checkWriteback(c, fixed)
			}
			return nil
		},
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Check the writeback and send every matching decision from now on.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishWriteback},
				{Name: "pause", Title: "Pause", Description: "Stop sending; what is queued still goes.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func writebackActions() []platform.Action {
	return []platform.Action{{Schema: SchemaWritebackAnswered, Target: WritebackType, Capability: "integrations", Title: "Keep answer", Description: "Retain what the external system answered to one writeback.", Automation: true,
		Payload: []platform.Field{{Name: "answer", Type: "json", Required: true, Description: "The attempt's result and the answer's first bytes"}}}}
}

func (b *Build) publishWriteback(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	w, ok := record.(*Writeback)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkWriteback(c, *w); err != nil {
		return err
	}
	w.Publisher = c.ID
	if !c.Replaying {
		fixed, err := b.freezeDirectIntegration(platform.AssetWriteback, *w)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		*w = fixed.(Writeback)
	}
	return nil
}

func (b *Build) checkWriteback(c platform.Caller, w Writeback) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(w.Name, w.ID); err != nil {
		return refuse(err.Error())
	}
	if err := CheckWritebackTransport(w.Transport, w.Method); err != nil {
		return refuse(err.Error())
	}
	if err := b.checkWritebackConnection(c, w); err != nil {
		return err
	}
	info, ok := b.lookupEntity(w.Object)
	if !ok {
		return refuse("The object {object} is not installed", w.Object)
	}
	if info.App != ID {
		return refuse("A writeback currently listens to objects owned by Build; native application objects need an owner callback")
	}
	if w.On == "" || strings.ContainsAny(w.On, "./ ") {
		return refuse("After action is the verb of the object's action: create, edit or a declared action's name")
	}
	if !slices.Contains([]string{"", "POST", "PUT", "PATCH"}, w.Method) {
		return refuse("The method is POST, PUT or PATCH")
	}
	if strings.Contains(w.Path, "://") {
		return refuse("The path is relative to the connection's address")
	}
	for _, m := range append(slices.Clone(w.Mapping), w.Result...) {
		if m.From == "" || m.To == "" {
			return refuse("Every mapping names both sides")
		}
		if !slices.Contains([]string{"", "string", "number", "boolean", "date"}, m.Convert) {
			return refuse("A conversion is one of string, number, boolean, date")
		}
	}
	for _, m := range w.Result {
		if f, ok := info.Field(m.To); !ok || f.ReadOnly {
			return refuse("The answer field {field} is not a writable field of the object", m.To)
		}
	}
	return nil
}

func (b *Build) checkWritebackConnection(c platform.Caller, w Writeback) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	conn, ok := platform.Get[Connection](c, w.Connection)
	if ok {
		fixed, err := b.ConnectionConfiguration(conn)
		if err != nil {
			return refuse(err.Error())
		}
		conn = fixed
	}
	if !ok {
		return refuse("Connection {connection} does not exist", w.Connection)
	}
	if conn.Kind != "http" && conn.Kind != "odata" {
		return refuse("A writeback goes through an http or OData connection")
	}
	if w.Transport == "a2a" && conn.Kind != "http" {
		return refuse("A2A uses an HTTP connection")
	}
	return nil
}

func (b *Build) submitWritebackAnswered(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Answer WritebackAns `json:"answer"`
		}
		w, ok := platform.Get[Writeback](c, s.GetTarget().GetId())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The writeback does not exist")
		}
		if json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Answer.Result == "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An answer carries its result")
		}
		return func(r *pb.ChangeRecord) {
			switch payload.Answer.Result {
			case "delivered":
				w.Sent++
			case "rejected":
				w.Rejected++
			case "unknown":
				w.Unknown++
			default:
				w.Failed++
			}
			payload.Answer.At = now
			w.Answers = append([]WritebackAns{payload.Answer}, w.Answers...)
			if len(w.Answers) > ValueWritebackAnswerKept {
				w.Answers = w.Answers[:ValueWritebackAnswerKept]
			}
			c.Put(r, w)
		}, nil
	})
}

// Answered takes a settled writeback effect (platform.Answerer): it keeps the
// answer on the writeback and, when delivered with a Result mapping, writes
// the answer's fields onto the record through the object's own edit.
func (b *Build) answered(c platform.Caller, effect platform.Effect, out platform.Outcome, now time.Time) *kernel.Error {
	var req WritebackRequest
	if json.Unmarshal([]byte(effect.Body), &req) != nil || req.Writeback == "" {
		return nil
	}
	if req.Dispatch != "" {
		outcome := map[string]string{"delivered": "accepted", "rejected": "failed", "unknown": "unknown", "discarded": "unknown"}[out.Result]
		if effect.State == "failed" {
			outcome = "failed"
		}
		if outcome == "" {
			return nil
		}
		key := fmt.Sprintf("%s:%d:%s", effect.ID, effect.Attempts, out.Result)
		_, err := platform.Decide(c, b, dispatchSubmission(c, req.Dispatch, platform.DispatchAnswerSchema, key, map[string]any{"intent": effect.ID, "outcome": outcome, "reason": out.Detail, "answer": out.Answer}, now), now)
		return err
	}
	if req.Confirmation != "" {
		outcome := map[string]string{"delivered": "accepted", "rejected": "refused", "unknown": "unknown", "discarded": "refused"}[out.Result]
		if effect.State == "failed" {
			outcome = "failed"
		}
		tag := req.Party
		if req.Compensates != "" {
			tag += ":compensate"
		}
		key := fmt.Sprintf("%s:%d:%s", effect.ID, effect.Attempts, out.Result)
		_, err := platform.Decide(c, b, confirmationSubmission(c, req.Confirmation, confirmationAnswer, key, map[string]any{"tag": tag, "outcome": outcome, "reason": out.Detail, "answer": out.Answer}, now), now)
		return err
	}
	w, ok := platform.Get[Writeback](c, req.Writeback)
	if !ok {
		return nil
	}
	// New requests carry their original answer mapping, so an accepted reply
	// never acquires a different meaning after a compatible definition upgrade.
	if req.Version == 1 {
		w.Object, w.Result = req.Object, slices.Clone(req.Result)
		if w.Object == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Writeback request has no frozen target")
		}
	} else if req.Version != 0 || w.DefinitionVersion != 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Historical writeback request requires its original unversioned definition")
	}
	ans := WritebackAns{Effect: effect.ID, Target: effect.Target, Result: out.Result, Detail: out.Detail, Answer: clipAnswer(out.Answer)}
	if out.Result != "delivered" && out.Result != "rejected" && out.Result != "unknown" {
		ans.Result = "failed"
	}
	payload, _ := json.Marshal(map[string]any{"answer": ans})
	if _, err := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "answered:" + effect.ID + map[bool]string{true: ":unknown"}[out.Result == "unknown"],
		Target: &pb.EntityRef{Type: WritebackType, Id: w.ID}, Schema: &pb.SchemaRef{Name: SchemaWritebackAnswered, Version: 1}, Payload: payload}, now); err != nil {
		return err
	}
	typ, id, ok := strings.Cut(effect.Target, "/")
	if out.Result != "delivered" || !ok || typ != w.Object || len(w.Result) == 0 {
		return nil
	}
	fields := w.Fields(out.Answer)
	if len(fields) == 0 {
		return nil
	}
	edit, _ := json.Marshal(fields)
	_, err := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "answer-fields:" + effect.ID,
		Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + ".edit", Version: 1}, Payload: edit}, now)
	return err
}

func clipAnswer(raw []byte) string {
	s := string(raw)
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// Connections (I-A): how the tenant reaches one external
// system - a kind, an address and the *name* of a secret in the host's store.
// Sources read through a connection; the credential itself never enters a
// record, a candidate or a release, so the same definition promotes to another
// environment that holds its own secret under the same name.
const (
	ConnectionType          = "build.connection"
	SchemaConnectionChecked = "build.connection.checked"
)

// ConnectionKinds are the systems a connection reaches.
var ConnectionKinds = []string{"http", "odata", "postgres"}

type Connection struct {
	platform.Record
	Name  string `json:"name" field:"required,search" source:"record"`
	Title string `json:"title" field:"required,search" source:"record"`
	// Kind is http (REST or files), odata (an OData v2/v4 service root, e.g. SAP Gateway) or postgres (a read-only database).
	Kind string `json:"kind" field:"required" choices:"http,odata,postgres" source:"record"`
	// Address is the service root URL, or for postgres a DSN without a password.
	Address string `json:"address" help:"Service root URL, or postgres://user@host:5432/db?sslmode=require without the password" source:"record"`
	// Secret names the credential in the host's secret store: the Authorization header's value for http/odata, the password for postgres.
	Secret       string `json:"secret,omitempty" title:"Secret name" help:"The credential's name in the host's secret store, never the credential" source:"record"`
	AllowPrivate bool   `json:"allowPrivate,omitempty" title:"Allow private address" help:"Also accept http and private networks, for on-premise systems"`
	// Marking classifies everything read through it; datasets loaded from it are marked at least as high.
	Marking   string           `json:"marking,omitempty" choices:"internal,confidential,restricted" title:"Marking" help:"What is read through it is at least this: internal, confidential or restricted"`
	State     string           `json:"state" field:"readonly" source:"record"`
	Requested bool             `json:"requested,omitempty" field:"readonly" title:"Check requested"`
	Last      *ConnectionCheck `json:"last,omitempty" field:"readonly" type:"json" title:"Last check"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed" source:"record"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// ConnectionCheck is what one check found.
type ConnectionCheck struct {
	At    time.Time `json:"at"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
	// Detail is what answered: a status, a server version, an OData version.
	Detail string `json:"detail,omitempty"`
}

// Resolve is the address a source on this connection reads: an absolute URL
// stays, a relative one joins the service root.
func (c Connection) Resolve(ref string) string {
	if ref == "" {
		return c.Address
	}
	if strings.Contains(ref, "://") {
		return ref
	}
	return strings.TrimRight(c.Address, "/") + "/" + strings.TrimLeft(ref, "/")
}

// Datasets (I-C): someone else's facts kept as they came -
// a sequence of versions, each the rows one pull or one pipeline run produced,
// with the schema inferred from them. A dataset is not a business object: no
// business permission reads it, only the builder; a pipeline turns it into
// records, and every record can say which version and row it came from.
const (
	DatasetType        = "build.dataset"
	DatasetVersionType = "build.datasetversion"
	SchemaDatasetLoad  = "build.dataset.load"
	DatasetKeepDefault = 3
)

type Dataset struct {
	platform.Record
	Name  string `json:"name" field:"required,search"`
	Title string `json:"title" field:"required,search"`
	// Keep is how many versions stay; older ones are archived.
	Keep int `json:"keep,omitempty" title:"Versions kept" help:"How many versions stay readable; default 3"`
	// Marking: raised by what loads it - the connection, the pipeline's input - and by hand; lowered only by hand.
	Marking string `json:"marking,omitempty" choices:"internal,confidential,restricted" title:"Marking"`
	// Producer is what loads it: the source or pipeline that writes versions (set by them, informational).
	Producer string         `json:"producer,omitempty" field:"readonly"`
	Schema   []DatasetField `json:"schema,omitempty" field:"readonly" type:"json"`
	Version  int            `json:"version,omitempty" field:"readonly" title:"Latest version"`
	Last     *DatasetLoad   `json:"last,omitempty" field:"readonly" type:"json" title:"Latest load"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// DatasetField is one inferred column: its name and the widest type seen.
type DatasetField struct {
	Name string `json:"name"`
	Type string `json:"type"` // string, number, boolean, date, json
}

// DatasetLoad is what one version holds.
type DatasetLoad struct {
	At      time.Time `json:"at"`
	Version int       `json:"version"`
	Rows    int       `json:"rows"`
	Bytes   int       `json:"bytes"`
	// Drift lists columns that appeared or vanished since the previous version.
	Drift []string `json:"drift,omitempty"`
}

// DatasetVersion is one version's rows, kept until the dataset's Keep passes it.
type DatasetVersion struct {
	platform.Record
	Dataset string          `json:"dataset" field:"required" ref:"build.dataset"`
	Version int             `json:"version" field:"required"`
	At      time.Time       `json:"at"`
	Rows    int             `json:"rows"`
	Data    json.RawMessage `json:"data" type:"json"`
	Request string          `json:"requestKey,omitempty" field:"readonly" title:"Request key"`
}

// VersionID names a dataset's version record.
func VersionID(dataset string, version int) string { return fmt.Sprintf("%s@%d", dataset, version) }

// Rows are a dataset version's rows (the latest when version is 0).
func DatasetRows(c platform.Caller, dataset string, version int) ([]map[string]any, int, error) {
	ds, ok := platform.Get[Dataset](c, dataset)
	if !ok {
		return nil, 0, fmt.Errorf("the dataset %s does not exist", dataset)
	}
	if version <= 0 {
		version = ds.Version
	}
	if version == 0 {
		return nil, 0, nil
	}
	v, ok := platform.Get[DatasetVersion](c, VersionID(dataset, version))
	if !ok {
		return nil, 0, fmt.Errorf("version %d of %s is no longer kept", version, ds.Title)
	}
	var rows []map[string]any
	if err := json.Unmarshal(v.Data, &rows); err != nil {
		return nil, 0, fmt.Errorf("version %d of %s is unreadable", version, ds.Title)
	}
	return rows, version, nil
}

const ObservationType = "build.observation"
const SchemaObservation = ObservationType + ".publish"

// Observation is a portable read/decision declaration over an external store.
// It never holds samples, summaries or credentials.
type Observation struct {
	platform.Record
	Name                       string                         `json:"name" field:"required,search" source:"record"`
	Title                      string                         `json:"title" field:"required,search" source:"record"`
	Description                string                         `json:"description,omitempty" type:"longtext"`
	Connection                 string                         `json:"connection" field:"required" ref:"build.connection" source:"record"`
	Object                     string                         `json:"object" field:"required"`
	DeviceField                string                         `json:"deviceField" field:"required" title:"Device identity field"`
	Signal                     string                         `json:"signal" field:"required"`
	Unit                       string                         `json:"unit" field:"required"`
	Path                       string                         `json:"path" field:"required" help:"Relative adapter path on the logical connection"`
	WindowSeconds              int                            `json:"windowSeconds" field:"required" title:"Decision window in seconds"`
	ExpectedRetentionSeconds   int                            `json:"expectedRetentionSeconds" field:"required" title:"Expected external retention in seconds"`
	ExpectedGranularitySeconds int                            `json:"expectedGranularitySeconds" field:"required" title:"Expected external granularity in seconds"`
	Roles                      []string                       `json:"roles" field:"required"`
	Scheduled                  bool                           `json:"scheduled,omitempty" title:"Evaluate completed windows"`
	RequireComplete            bool                           `json:"requireComplete,omitempty" title:"Require complete, good-quality windows"`
	Decision                   *platform.ObservationDecision  `json:"decision,omitempty" type:"json"`
	Intervals                  *platform.ObservationIntervals `json:"intervals,omitempty" type:"json" title:"State interval analysis"`
	State                      string                         `json:"state" field:"readonly" choices:"draft,published,retired"`
	Publisher                  string                         `json:"publisher,omitempty" field:"readonly" source:"record"`
	PublisherScopes            string                         `json:"publisherScopes" field:"readonly" type:"json" title:"Publisher scopes"`
	DefinitionVersion          int                            `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published                  string                         `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed" source:"record"`
	Versions                   []string                       `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// EnterpriseTarget lands a pipeline's rows in the enterprise model
// as elements of one stereotype owned on an external system's behalf: SAP
// HR/OM organisational units become ActualOrganizations under the org tree,
// AD departments posts, MES equipment ActualResources at locations. Columns
// name what each row gives; a value written "=literal" is a constant instead.
type EnterpriseTarget struct {
	Source     string `json:"source"`     // the system's name, e.g. sap-hr: element ids are <source>:<id>
	Stereotype string `json:"stereotype"` // element stereotype, e.g. ActualOrganization
	ID         string `json:"id"`         // the column with the system's stable id
	Name       string `json:"name"`
	ShortName  string `json:"shortName,omitempty"`
	Kind       string `json:"kind,omitempty"` // column or =literal, e.g. =plant
	// Parent is the column with the parent's id in the same system; Root the element the top ones sit
	// under (the tenant's own, e.g. the company); In the relationship kind the placement is made in.
	Parent   string `json:"parent,omitempty"`
	Root     string `json:"root,omitempty"`
	In       string `json:"in,omitempty"`
	Relation string `json:"relation,omitempty"` // e.g. part of
	// At places a resource or post at an element given by column (ids of the same system or =literal).
	At         string   `json:"at,omitempty"`
	AtKind     string   `json:"atKind,omitempty"` // the relationship stereotype towards At; default ResponsibleFor (owner → resource)
	From       string   `json:"from,omitempty"`
	Until      string   `json:"until,omitempty"`
	Properties []string `json:"properties,omitempty"` // columns copied as tagged values
}

var ValueSourceName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Check prevents new slice jobs; retained definitions keep their original shape.
func (e EnterpriseTarget) Check() error {
	return fmt.Errorf("Enterprise slice targets are retired; use explicit directory actions")
}

// Writebacks. A writeback declares that an accepted action on an
// object is sent to an external system through a Connection: an HTTP request
// whose body is mapped from the action's payload. The sending is an
// effect - at least once, in order per connection, with the decision's change
// id as the idempotency key - so an outage queues it and the receiver can
// deduplicate retries. The answer can write fields back onto the record (the document number
// SAP assigned, say) through the object's own edit.
const (
	WritebackType            = "build.writeback"
	SchemaWritebackAnswered  = "build.writeback.answered"
	ValueWritebackAnswerKept = 20
	// WritebackEndpoint prefixes the effect endpoint of a connection: "connection:<id>".
	WritebackEndpoint = "connection:"
)

type Writeback struct {
	platform.Record
	Name  string `json:"name" field:"required,search"`
	Title string `json:"title" field:"required,search"`
	// Connection is where it goes (http or odata); Object and On name the accepted action that sends it:
	// On is the action's verb - create, edit, or a declared action's name.
	Connection string `json:"connection" field:"required" ref:"build.connection"`
	Object     string `json:"object" field:"required" title:"Object"`
	On         string `json:"on" field:"required" title:"After action"`
	// Method and Path form the request; Path is relative to the connection's address and may use
	// {id} and {field} placeholders from the payload.
	Method    string `json:"method" title:"Method"`
	Transport string `json:"transport,omitempty" choices:"http,a2a" title:"Transport"`
	Path      string `json:"path" title:"Path"`
	// Mapping is payload field → body key; empty sends the payload with its id.
	Mapping []SourceField `json:"mapping,omitempty" title:"Body mapping"`
	// Result is answer key → record field, written onto the record when the system answers.
	Result    []SourceField  `json:"result,omitempty" title:"Answer mapping"`
	State     string         `json:"state" field:"readonly"`
	Publisher string         `json:"publisher,omitempty" field:"readonly"`
	Sent      int            `json:"sent,omitempty" field:"readonly" title:"Delivered"`
	Rejected  int            `json:"rejected,omitempty" field:"readonly"`
	Failed    int            `json:"failed,omitempty" field:"readonly"`
	Unknown   int            `json:"unknown,omitempty" field:"readonly"`
	Answers   []WritebackAns `json:"answers,omitempty" field:"readonly" type:"json" title:"Recent answers"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// WritebackAns is one settled attempt as the builder sees it.
type WritebackAns struct {
	At     time.Time `json:"at"`
	Effect string    `json:"effect"`
	Target string    `json:"target"`
	Result string    `json:"result"` // delivered, rejected, failed
	Detail string    `json:"detail,omitempty"`
	Answer string    `json:"answer,omitempty"` // first bytes of the body
}

// Schema is the full action name a writeback listens for.
func (w Writeback) Schema() string { return w.Object + "." + w.On }

// WritebackRequest is the frozen request an effect carries: everything the
// dispatcher needs without reading records, the secret only by name.
type WritebackRequest struct {
	Connection        string         `json:"connection,omitempty"`
	Confirmation      string         `json:"confirmation,omitempty"`
	Dispatch          string         `json:"dispatch,omitempty"`
	DispatchKey       string         `json:"dispatchKey,omitempty"`
	SafeRetry         bool           `json:"safeRetry,omitempty"`
	Party             string         `json:"party,omitempty"`
	Compensates       string         `json:"compensates,omitempty"`
	Version           int            `json:"version,omitempty"`
	Object            string         `json:"object,omitempty"`
	DefinitionVersion int            `json:"definitionVersion,omitempty"`
	Result            []SourceField  `json:"result,omitempty"`
	Writeback         string         `json:"writeback"`
	Method            string         `json:"method"`
	Transport         string         `json:"transport,omitempty"`
	Path              string         `json:"path,omitempty"`
	URL               string         `json:"url"`
	Secret            string         `json:"secret,omitempty"`
	AllowPrivate      bool           `json:"allowPrivate,omitempty"`
	Body              map[string]any `json:"body"`
}

// Request freezes the request for one accepted decision: the payload mapped to
// the body (or sent whole with its id), the path's placeholders filled.
func (w Writeback) Request(conn Connection, id string, payload json.RawMessage) (WritebackRequest, error) {
	if err := CheckWritebackTransport(w.Transport, w.Method); err != nil {
		return WritebackRequest{}, err
	}
	if w.Transport == "a2a" && conn.Kind != "http" {
		return WritebackRequest{}, fmt.Errorf("A2A uses an HTTP connection")
	}
	ValueDefinitionVersion := 0
	if w.Published != "" {
		var asset platform.ReleaseAsset
		if err := ValueDecodeIntegration([]byte(w.Published), &asset); err != nil {
			return WritebackRequest{}, err
		}
		if asset.Ref != (platform.AssetRef{App: ID, Kind: platform.AssetWriteback, Name: w.Name}) {
			return WritebackRequest{}, fmt.Errorf("writeback publication identity differs")
		}
		if err := ValueValidateIntegrationDescriptor(asset); err != nil {
			return WritebackRequest{}, err
		}
		version, err := ValueIntegrationOrdinal(asset)
		if err != nil {
			return WritebackRequest{}, err
		}
		ValueDefinitionVersion = version
	} else if w.DefinitionVersion != 0 {
		return WritebackRequest{}, fmt.Errorf("versioned writeback has no accepted definition")
	}
	var in map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &in); err != nil {
			return WritebackRequest{}, fmt.Errorf("the payload is not an object")
		}
	}
	if in == nil {
		in = map[string]any{}
	}
	in["id"] = id
	body := in
	if len(w.Mapping) > 0 {
		body = map[string]any{}
		for _, m := range w.Mapping {
			v, err := platform.IntegrationConvert(in[m.From], m.Convert)
			if err != nil {
				return WritebackRequest{}, fmt.Errorf("%s: %v", m.From, err)
			}
			body[m.To] = v
		}
	}
	path := w.Path
	for k, v := range in {
		path = strings.ReplaceAll(path, "{"+k+"}", platform.IntegrationScalar(v))
	}
	method := w.Method
	if method == "" {
		method = "POST"
	}
	request := WritebackRequest{Connection: conn.ID, Version: 1, Object: w.Object, DefinitionVersion: ValueDefinitionVersion, Result: slices.Clone(w.Result), Writeback: w.ID, Method: method, Transport: w.Transport, Path: path, URL: conn.Resolve(path), Secret: conn.Secret, AllowPrivate: conn.AllowPrivate, Body: body}
	return request, nil
}

func CheckWritebackTransport(transport, method string) error {
	if transport != "" && transport != "http" && transport != "a2a" {
		return fmt.Errorf("A writeback transport is HTTP or A2A")
	}
	if transport == "a2a" && method != "" && method != "POST" {
		return fmt.Errorf("A2A uses POST SendMessage")
	}
	return nil
}

// Fields are what the answer writes onto the record, by the Result mapping.
func (w Writeback) Fields(answer []byte) map[string]any {
	var in map[string]any
	if json.Unmarshal(answer, &in) != nil {
		// OData v2 wraps in d; a bare value is nothing to map.
		return nil
	}
	// OData v2 wraps the entity in d. A mapping written against the raw
	// answer (d.MaterialDocument) and one written against the entity
	// (MaterialDocument) both find it (UX-14).
	unwrapped := in
	if d, ok := in["d"].(map[string]any); ok && len(in) == 1 {
		unwrapped = d
	}
	out := map[string]any{}
	for _, m := range w.Result {
		v, ok := platform.IntegrationLookupPath(in, m.From)
		if !ok {
			if v, ok = platform.IntegrationLookupPath(unwrapped, m.From); !ok {
				continue
			}
		}
		if c, err := platform.IntegrationConvert(v, m.Convert); err == nil {
			out[m.To] = c
		}
	}
	return out
}

// Pipelines (I-D): a declared, closed set of steps from
// datasets to a dataset or to an object. No script: every step is data the
// builder can read back, every row that fails an expectation is quarantined
// with its reason, and a run is an ordinary journaled input like a pull.
const (
	PipelineType      = "build.pipeline"
	SchemaPipelineRan = "build.pipeline.ran"
)

type Pipeline struct {
	platform.Record
	Name  string `json:"name" field:"required,search"`
	Title string `json:"title" field:"required,search"`
	// Input is the dataset the rows start from; Steps transform them in order.
	Input string                   `json:"input" field:"required" ref:"build.dataset"`
	Steps []platform.TransformStep `json:"steps" type:"json"`
	// Expectations are checked on the final rows; failing rows are quarantined, not written.
	Expectations []platform.TransformExpectation `json:"expectations,omitempty" type:"json"`
	// Output is either a dataset (raw rows) or an object with the column that is the record id.
	OutputDataset string `json:"outputDataset,omitempty" ref:"build.dataset" title:"Output dataset"`
	OutputObject  string `json:"outputObject,omitempty" title:"Output object"`
	Key           string `json:"key,omitempty" title:"Record id column"`
	// Marking is the highest marking an input carried when it ran; kept so lineage and the object gate see it.
	Marking string `json:"marking,omitempty" field:"readonly" title:"Marking"`
	// OutputEnterprise lands the rows in the enterprise model instead: one element per row.
	OutputEnterprise *EnterpriseTarget `json:"outputEnterprise,omitempty" type:"json" title:"Enterprise model"`
	// Every is a period, or empty: run when an input gains a version, or on request.
	Every        string       `json:"every,omitempty" title:"Run every"`
	State        string       `json:"state" field:"readonly"`
	Runner       string       `json:"runner,omitempty" field:"readonly" title:"Runs as"`
	RunnerScopes string       `json:"runnerScopes,omitempty" field:"readonly" type:"json"`
	Requested    bool         `json:"requested,omitempty" field:"readonly"`
	Last         *PipelineRun `json:"last,omitempty" field:"readonly" type:"json" title:"Last run"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// PipelineRun is what one run did.
type PipelineRun struct {
	Execution         *IntegrationExecution     `json:"execution,omitempty"`
	DefinitionVersion string                    `json:"definitionVersion,omitempty"`
	At                time.Time                 `json:"at"`
	Input             int                       `json:"inputVersion"`
	Rows              int                       `json:"rows"`              // rows after the steps
	Written           int                       `json:"written"`           // rows written to the output
	Marking           string                    `json:"marking,omitempty"` // the highest marking of the inputs
	Merged            int                       `json:"merged,omitempty"`  // rows merged into matched records
	Quarantined       int                       `json:"quarantined"`
	Failed            int                       `json:"failed"` // rows the output refused
	Error             string                    `json:"error,omitempty"`
	Quarantine        []platform.QuarantinedRow `json:"quarantine,omitempty"`
	Failures          []SourceFailure           `json:"failures,omitempty"`
	Output            int                       `json:"outputVersion,omitempty"`
}

// Due is whether a published pipeline should run at now: asked for, its period
// passed, or its input has a version newer than the last run's.
func (p Pipeline) Due(now time.Time, inputVersion int) bool {
	if p.State != "published" {
		return false
	}
	if p.Last != nil && p.Last.Execution != nil && p.Last.Execution.Pending {
		return true
	}
	if p.Last != nil && p.Last.DefinitionVersion != p.ExecutionVersion() {
		return true
	}
	if p.Requested {
		return true
	}
	if p.Last == nil {
		return inputVersion > 0
	}
	if inputVersion > p.Last.Input {
		return true
	}
	if p.Every != "" {
		if every, err := time.ParseDuration(p.Every); err == nil && !now.Before(p.Last.At.Add(every)) {
			return true
		}
	}
	return false
}

// ObjectRows turns final rows into the output object's rows: every column is
// a payload field (the key column names the record). The host freezes operation
// identities with the execution before any output is written.
func (p Pipeline) ObjectRows(rows []map[string]any) []SourceRow {
	out := make([]SourceRow, 0, len(rows))
	for _, r := range rows {
		row := SourceRow{ID: platform.IntegrationScalar(r[p.Key])}
		payload := map[string]any{}
		for k, v := range r {
			if k != p.Key && v != nil && !strings.Contains(k, ".") {
				payload[k] = v
			}
		}
		row.Payload, _ = json.Marshal(payload)
		if row.ID == "" {
			row.Error = "row has no id"
		}
		out = append(out, row)
	}
	return out
}

// StepDatasets are the other datasets steps look up or join.
func (p Pipeline) StepDatasets() []string {
	var ids []string
	for _, s := range p.Steps {
		if s.Dataset != "" {
			ids = append(ids, s.Dataset)
		}
	}
	return ids
}

func (p *Pipeline) NormalizeFormulaRules() error {
	for i := range p.Steps {
		s := &p.Steps[i]
		if s.Kind != "compute" {
			continue
		}
		if s.FormulaExpr != nil {
			if s.Formula != "" {
				return fmt.Errorf("one formula expression required")
			}
			continue
		}
		used, err := platform.FormulaFields(s.Formula)
		if err != nil {
			return err
		}
		types := map[string]string{}
		for name := range used {
			types[name] = "decimal"
		}
		e, err := platform.LowerFormula(s.Formula, types)
		if err != nil {
			return err
		}
		s.FormulaExpr = &e
		s.Formula = ""
	}
	return nil
}

type Match struct {
	platform.Record
	Name   string `json:"name" field:"required,search"`
	Title  string `json:"title" field:"required,search"`
	Object string `json:"object" field:"required" title:"Object"`
	// Keys: a row that agrees with an existing record on any key is that record.
	Keys []MatchKey `json:"keys" type:"json" title:"Keys"`
	// Prefer: for a field, the pipeline or source (by name) whose value wins; others fill it only while empty.
	Prefer    []Preference `json:"prefer,omitempty" type:"json" title:"Preferred producers"`
	State     string       `json:"state" field:"readonly"`
	Publisher string       `json:"publisher,omitempty" field:"readonly"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// MatchKey is one way to say "the same": the fields compared, and how they are
// normalised first - exact; folded (trim, case, inner spaces); digits (only
// the digits, without leading zeros, so 000123 = 123 = M-123).
type MatchKey struct {
	Fields    []string `json:"fields"`
	Normalize string   `json:"normalize,omitempty"`
}

type Preference struct {
	Field    string `json:"field"`
	Producer string `json:"producer"`
}

// KeyOf is the normalised value of key k over the fields of a row, or "" when
// any field is missing - a half-known key never matches.
func (k MatchKey) KeyOf(fields map[string]any) string {
	parts := make([]string, 0, len(k.Fields))
	for _, f := range k.Fields {
		v := platform.IntegrationScalar(fields[f])
		if v == "" {
			return ""
		}
		parts = append(parts, platform.IntegrationNormalize(v, k.Normalize))
	}
	return strings.Join(parts, "\x1f")
}

// Resolver applies one published rule to rows landing from producer: rows
// that match an existing record take its id (an edit instead of a second
// record), rows that match each other converge on the first, and fields a
// different producer owns are dropped while the record already has a value.
type Resolver struct {
	Rule     Match
	Producer string
	index    map[string]string         // key → record id
	fields   map[string]map[string]any // record id → fields
	Merged   int
}

func NewResolver(rule Match, producer string, records []map[string]any) *Resolver {
	r := &Resolver{Rule: rule, Producer: producer, index: map[string]string{}, fields: map[string]map[string]any{}}
	for _, rec := range records {
		id := platform.IntegrationScalar(rec["id"])
		if id == "" {
			continue
		}
		r.fields[id] = rec
		r.Learn(id, rec)
	}
	return r
}

func (r *Resolver) Learn(id string, fields map[string]any) {
	for i, k := range r.Rule.Keys {
		if v := k.KeyOf(fields); v != "" {
			key := k.Normalize + ":" + ValueItoa(i) + ":" + v
			if _, taken := r.index[key]; !taken {
				r.index[key] = id
			}
		}
	}
}

// Resolve rewrites the row in place and tells whether it was merged into another id.
func (r *Resolver) Resolve(row *SourceRow) bool {
	if row.Error != "" || row.ID == "" {
		return false
	}
	var fields map[string]any
	if json.Unmarshal(row.Payload, &fields) != nil {
		return false
	}
	merged := false
	if _, known := r.fields[row.ID]; !known {
		for i, k := range r.Rule.Keys {
			if v := k.KeyOf(fields); v != "" {
				if id, ok := r.index[k.Normalize+":"+ValueItoa(i)+":"+v]; ok && id != row.ID {
					row.Key, row.ID, merged = row.Key+":as:"+id, id, true
					break
				}
			}
		}
	}
	if existing := r.fields[row.ID]; existing != nil {
		for _, p := range r.Rule.Prefer {
			if p.Producer != r.Producer && platform.IntegrationScalar(existing[p.Field]) != "" {
				delete(fields, p.Field)
			}
		}
		for k, v := range fields {
			existing[k] = v
		}
	} else {
		r.fields[row.ID] = fields
	}
	r.Learn(row.ID, fields)
	row.Payload, _ = json.Marshal(fields)
	if merged {
		r.Merged++
	}
	return merged
}

func ValueItoa(i int) string { return string(rune('a' + i)) }

// Data sources (block C): a builder names an external JSON
// endpoint, the array inside its answer, the field that identifies a row and
// how fields map onto one of the tenant's objects. The host pulls it when due
// or asked, outside any decision - like a CSV import: every row becomes the
// object's own generated create or edit as the publisher, under a key made of
// a durably prepared execution and row position; retries reuse that execution.
// The pull's summary is decided on the source (SchemaSourcePulled).
const (
	SourceType              = "build.source"
	SchemaSourcePulled      = "build.source.pulled"
	SourceBody              = 16 << 20 // the most bytes an answer may hold
	ValueSourceRows         = 5000
	ValueSourceFailuresKept = 20
)

type Source struct {
	platform.Record
	Name  string `json:"name" field:"required,search"`
	Title string `json:"title" field:"required,search"`
	// Connection is the system the source reads through; empty uses a bare JSON URL.
	Connection string `json:"connection,omitempty" ref:"build.connection"`
	// Profile is how rows are read: json (default), csv, odata (an entity set), table (a database table).
	Profile string `json:"profile,omitempty" choices:"json,csv,odata,table"`
	// URL answers JSON or CSV over https (http only to a private address when allowed); relative to the connection's address when one is set.
	URL string `json:"url,omitempty" title:"URL"`
	// Entity is the OData entity set or the schema.table a table profile reads; Filter narrows it ($filter, or a WHERE fragment).
	Entity string `json:"entity,omitempty" title:"Entity or table"`
	Filter string `json:"filter,omitempty" title:"Filter" help:"OData $filter, or a WHERE fragment of simple comparisons"`
	// Since is the timestamp or sequence property an incremental pull orders by; Cursor the last value pulled.
	Since        string `json:"since,omitempty" title:"Incremental column" help:"A timestamp or sequence property; each pull reads rows past the cursor"`
	Cursor       string `json:"cursor,omitempty" field:"readonly"`
	AllowPrivate bool   `json:"allowPrivate,omitempty" title:"Allow private address" help:"Also accept http and private networks, for on-premise systems"`
	// Header is one request header, "Name: value", typically Authorization.
	Header string `json:"header,omitempty" title:"Request header" help:"One header such as Authorization: Bearer …"`
	// Path is the dotted path to the array of rows inside the answer; empty: the answer is the array.
	Path string `json:"path,omitempty" title:"Rows at" help:"Dotted path to the array, such as data.items; empty when the answer is the array"`
	// Dataset keeps the rows as they came; a pipeline maps them later. Otherwise Object is the
	// tenant's type the rows become directly, Key the row field that is the record's id.
	Dataset string        `json:"dataset,omitempty" ref:"build.dataset" title:"Target dataset"`
	Object  string        `json:"object,omitempty" title:"Target object"`
	Key     string        `json:"key,omitempty" title:"Row id field"`
	Mapping []SourceField `json:"mapping,omitempty" title:"Field mapping"`
	// Stream sources are read by the original continuous instance. They never
	// materialize machine-rate rows into ordinary object or dataset records.
	Stream bool `json:"stream,omitempty" title:"Continuous Flow source"`
	// Every is a period such as 15m or 24h; empty: pulled only when asked.
	Every string `json:"every,omitempty" title:"Pull every" help:"A period such as 15m, 1h or 24h; empty: only when asked"`
	State string `json:"state" field:"readonly"`
	// The declaration publisher authorizes background status reads independently
	// of an explicit pull request.
	Publisher       string `json:"publisher,omitempty" field:"readonly"`
	PublisherScopes string `json:"publisherScopes,omitempty" field:"readonly" type:"json" title:"Publisher scopes"`
	// Puller and its token scopes belong to the explicit run request.
	Puller       string      `json:"puller,omitempty" field:"readonly" title:"Pulls as"`
	PullerScopes string      `json:"pullerScopes,omitempty" field:"readonly" type:"json"`
	Requested    bool        `json:"requested,omitempty" field:"readonly" title:"Pull requested"`
	Last         *SourcePull `json:"last,omitempty" field:"readonly" type:"json" title:"Last pull"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// SourceField maps one row field onto one object field, optionally converted.
type SourceField struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Convert string `json:"convert,omitempty"` // "", string, number, boolean, date
}

// IntegrationExecution fixes one invocation independently of its input content.
// Snapshot is the digest of its immutable preparation in the original file store.
// Completed invocations keep the reference for historical reading.
type IntegrationExecution struct {
	ID       string         `json:"id"`
	Member   string         `json:"member"`
	Scopes   []string       `json:"scopes,omitempty"`
	Pending  bool           `json:"pending,omitempty"`
	Snapshot string         `json:"snapshot"`
	Inputs   map[string]int `json:"inputs,omitempty"`
}

// SourcePull is what one pull did: counted, with the first failures kept.
type SourcePull struct {
	Execution         *IntegrationExecution `json:"execution,omitempty"`
	DefinitionVersion string                `json:"definitionVersion,omitempty"`
	At                time.Time             `json:"at"`
	Rows              int                   `json:"rows"`
	Applied           int                   `json:"applied"`
	Failed            int                   `json:"failed"`
	Merged            int                   `json:"merged,omitempty"` // rows that became an edit of a matched record
	Error             string                `json:"error,omitempty"`
	Failures          []SourceFailure       `json:"failures,omitempty"`
	// Cursor is the last incremental value the pull reached; kept on the source.
	Cursor string `json:"cursor,omitempty"`
}

type SourceFailure struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
}

// Due is whether a published source should be pulled at now: asked for, or
// its period has passed since the last pull.
func (s Source) Due(now time.Time) bool {
	if s.State != "published" || s.Stream {
		return false
	}
	if s.Last != nil && s.Last.Execution != nil && s.Last.Execution.Pending {
		return true
	}
	if s.Last != nil && s.Last.DefinitionVersion != s.ExecutionVersion() {
		return true
	}
	if s.Requested {
		return true
	}
	every, err := time.ParseDuration(s.Every)
	if s.Every == "" || err != nil {
		return false
	}
	return s.Last == nil || !now.Before(s.Last.At.Add(every))
}

// SourceRow is one mapped row. Preparation fixes its operation and execution key.
type SourceRow struct {
	ID      string
	Payload json.RawMessage
	Key     string
	Verb    string
	Error   string
}

// Decode turns an answer's body into rows for the json and csv profiles.
func (s Source) Decode(body []byte) ([]map[string]any, error) {
	if s.Profile == "csv" {
		return platform.IntegrationCSVRows(body)
	}
	return platform.IntegrationRowsAt(body, s.Path)
}

// MapRows turns decoded rows into the target's rows; the host decides each as
// the publisher, outside any decision of the builder.
func (s Source) MapRows(rows []map[string]any) ([]SourceRow, error) {
	if len(rows) > ValueSourceRows {
		return nil, fmt.Errorf("the answer holds %d rows; a pull takes at most %d", len(rows), ValueSourceRows)
	}
	out := make([]SourceRow, 0, len(rows))
	for _, row := range rows {
		r := SourceRow{ID: platform.IntegrationScalar(row[s.Key])}
		payload := map[string]any{}
		for _, m := range s.Mapping {
			v, convErr := platform.IntegrationConvert(row[m.From], m.Convert)
			if convErr != nil {
				r.Error = m.To + ": " + convErr.Error()
				break
			}
			if v != nil {
				payload[m.To] = v
			}
		}
		if r.ID == "" && r.Error == "" {
			r.Error = "row has no id"
		}
		r.Payload, _ = json.Marshal(payload)
		out = append(out, r)
	}
	return out, nil
}

// Fail counts one failed row, keeping the first few.
func (p *SourcePull) Fail(id, outcome string) {
	p.Failed++
	if len(p.Failures) < ValueSourceFailuresKept {
		p.Failures = append(p.Failures, SourceFailure{ID: id, Outcome: outcome})
	}
}

// Advance is the cursor after rows: the greatest Since value seen, as text.
func (s Source) Advance(rows []map[string]any) string {
	if s.Since == "" {
		return ""
	}
	best := s.Cursor
	for _, row := range rows {
		if v := platform.IntegrationScalar(row[s.Since]); v != "" {
			a, anum := new(big.Rat).SetString(v)
			b, bnum := new(big.Rat).SetString(best)
			at, aerr := time.Parse(time.RFC3339Nano, v)
			bt, berr := time.Parse(time.RFC3339Nano, best)
			if best == "" || anum && bnum && a.Cmp(b) > 0 || aerr == nil && berr == nil && at.After(bt) || (!anum || !bnum) && (aerr != nil || berr != nil) && v > best {
				best = v
			}
		}
	}
	return best
}
