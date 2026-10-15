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
			return append([]string{r.Member}, r.Reviewers...)
		}},
		Withheld: "withheld", Derived: []platform.Derivation{{From: "subject", Fields: []string{"input", "parties", "definition", "assets"}}},
	}
}
func confirmationActions() []platform.Action {
	return []platform.Action{
		{Schema: confirmationAnswer, Target: confirmationType, Title: "Record confirmation answer", Description: "Retain the original party request identity and answer.", Capability: "confirmations", Automation: true, Payload: []platform.Field{}},
		{Schema: confirmationTick, Target: confirmationType, Title: "Advance confirmation work", Description: "Apply deadlines and retain completion results through original owned work.", Capability: "confirmations", Automation: true, Payload: []platform.Field{}},
		{Schema: confirmationReview, Target: confirmationType, Title: "Review business confirmation", Description: "Resolve a party from checked evidence, or request its declared compensation.", Capability: "confirmations", Roles: []string{platform.AnyMember}, Payload: []platform.Field{{Name: "party", Type: "string", Required: true, Description: "The declared party"}, {Name: "outcome", Type: "string", Required: true, Choices: []string{"accepted", "refused", "compensate"}, Description: "The evidenced decision"}, {Name: "reason", Type: "string", Required: true, Description: "Evidence supporting the decision"}}},
	}
}

func (b *Build) checkConfirmation(spec *platform.Confirmation, object Object) error {
	if spec == nil {
		return nil
	}
	if err := spec.Check(); err != nil {
		return err
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
	return nil
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
	transport, ok := b.host.(host.ConfirmationHost)
	if !ok {
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
	for _, member := range transport.Holding(ID, spec.ReviewerRole) {
		run.Reviewers = append(run.Reviewers, member)
	}
	for _, party := range spec.Parties {
		p := platform.ConfirmationProgress{Name: party.Name, Title: party.Title, State: "pending", Key: id + ":" + party.Name, Intent: c.Tenant + ":" + ID + ":" + id + ":" + party.Name}
		prepare := func(writeback string, compensate bool) (json.RawMessage, int, error) {
			if writeback == "" {
				return nil, 0, nil
			}
			w, known := platform.Get[Writeback](c, writeback)
			if !known {
				return nil, 0, fmt.Errorf("Confirmation writeback is unavailable")
			}
			w, err = b.ExecutionWriteback(w)
			if err != nil || w.State != "published" {
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
			Answer                                                   json.RawMessage
			Difference                                               *platform.ConfirmationDifference
		}
		if json.Unmarshal(s.GetPayload(), &in) != nil {
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
		} else if !c.Automation {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Confirmation receipts belong to their original transport")
		}
		compensate := in.Outcome == "compensate"
		if in.Tag != "" {
			in.Party, _, _ = strings.Cut(in.Tag, ":")
		}
		i := slices.IndexFunc(run.Parties, func(p platform.ConfirmationProgress) bool { return p.Name == in.Party })
		if s.GetSchema().GetName() != confirmationTick && i < 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a declared confirmation party")
		}
		return func(r *pb.ChangeRecord) {
			if i >= 0 {
				p := &run.Parties[i]
				if compensate {
					if p.Compensation == "" {
						p.Compensation = p.Intent + ":compensate"
						p.CompensationState = "pending"
						run.State = "compensating"
						if err := b.host.(host.ConfirmationHost).QueueConfirmation(c, r, run, i, true, at); err != nil {
							c.FailDecision(err)
							return
						}
					}
				} else if strings.HasSuffix(in.Tag, ":compensate") {
					p.CompensationState = map[string]string{"accepted": "confirmed", "refused": "rejected", "unknown": "unknown"}[in.Outcome]
					p.Reason = in.Reason
					if p.CompensationState == "confirmed" {
						if err := b.host.(host.ConfirmationHost).ResolveConfirmationDelivery(c, run, i, true, at); err != nil {
							c.FailDecision(err)
							return
						}
					}
				} else if !slices.Contains([]string{"confirmed", "rejected"}, p.State) {
					p.State = map[string]string{"accepted": "confirmed", "refused": "rejected", "unknown": "unknown"}[in.Outcome]
					if p.State == "" {
						c.FailDecision(platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid confirmation result"))
						return
					}
					p.At, p.Reason, p.Answer = at, in.Reason, in.Answer
					if manual || in.Difference != nil {
						if err := b.host.(host.ConfirmationHost).ResolveConfirmationDelivery(c, run, i, false, at); err != nil {
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
			b.confirmationState(c, r, &run, at)
			c.Put(r, run)
		}, nil
	})
}

func (b *Build) confirmationState(c platform.Caller, r *pb.ChangeRecord, run *platform.ConfirmationRun, at time.Time) {
	all := !slices.ContainsFunc(run.Parties, func(p platform.ConfirmationProgress) bool { return p.State != "confirmed" })
	rejected := slices.ContainsFunc(run.Parties, func(p platform.ConfirmationProgress) bool { return p.State == "rejected" })
	if run.State == "compensating" {
		if !slices.ContainsFunc(run.Parties, func(p platform.ConfirmationProgress) bool {
			return p.Compensation != "" && p.CompensationState != "confirmed"
		}) {
			run.State = "compensated"
		}
		return
	}
	if all {
		run.State = "confirmed"
		c.Assign(r, platform.Assignment{Key: run.ID, Close: true})
		return
	}
	if rejected {
		run.State = "partial"
		run.Partial = true
		if run.Definition.OnPartial == "reject" {
			run.State = "rejected"
		}
	}
	if !at.Before(run.Deadline) {
		run.State = "expired"
	}
	if run.State == "partial" || run.State == "expired" || slices.ContainsFunc(run.Parties, func(p platform.ConfirmationProgress) bool { return p.State == "unknown" }) {
		c.Assign(r, platform.Assignment{Key: run.ID, Ref: confirmationType + "/" + run.ID, Title: "Review business confirmation", Body: "A party rejected, did not answer, or reached its deadline.", To: []platform.Recipient{{AppRole: run.Definition.ReviewerRole}}})
	}
}

func (b *Build) Run(c platform.Caller, job string, at time.Time) *kernel.Error {
	if job != "confirmations" {
		return nil
	}
	runs, _, err := platform.Find[platform.ConfirmationRun](c, platform.Query{Limit: 200, Sort: []string{"id"}})
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.Completion != "" || run.CompletionError != "" || run.State == "compensated" {
			continue
		}
		if !at.Before(run.Deadline) && !slices.Contains([]string{"confirmed", "expired", "compensating", "rejected"}, run.State) {
			if _, err := platform.Decide(c, b, confirmationSubmission(c, run.ID, confirmationTick, "deadline", map[string]any{}, at), at); err != nil {
				return err
			}
			run, _ = platform.Get[platform.ConfirmationRun](c, run.ID)
		}
		if run.State == "expired" && run.Definition.OnExpire == "compensate" {
			for _, p := range run.Parties {
				if (p.State == "confirmed" || p.State == "unknown") && p.Compensation == "" {
					if _, err := platform.Decide(c, b, confirmationSubmission(c, run.ID, confirmationAnswer, "compensate:"+p.Name, map[string]any{"party": p.Name, "outcome": "compensate"}, at), at); err != nil {
						return err
					}
				}
			}
		}
		action := ""
		if run.State == "confirmed" {
			action = run.Definition.OnComplete
		} else if run.Partial && run.Definition.OnPartial != "hold" {
			action = run.Definition.OnPartialAction
		}
		if action != "" {
			change, problem := b.host.(host.ConfirmationHost).CompleteConfirmation(c, nil, run, action, at)
			payload := map[string]any{"completion": change}
			if problem != nil {
				payload["completionError"] = problem.Code.String() + ": " + problem.Message
			}
			if _, err := platform.Decide(c, b, confirmationSubmission(c, run.ID, confirmationTick, "completion", payload, at), at); err != nil {
				return err
			}
		}
	}
	return nil
}

func confirmationSubmission(c platform.Caller, id, schema, key string, payload any, at time.Time) *pb.Submission {
	return &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "confirmation:" + id + ":" + key, Target: &pb.EntityRef{Type: confirmationType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
}
