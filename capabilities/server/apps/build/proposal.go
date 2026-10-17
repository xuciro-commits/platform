package build

import (
	"cmp"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const SchemaProposalReview = FunctionCallType + ".review"

type proposalConsumption struct {
	run        FunctionRun
	binding    platform.ProposalAdoption
	amendments json.RawMessage
}

func checkActionAdoption(a Action) error {
	if a.Adoption == nil {
		return nil
	}
	if a.Transaction == nil {
		return fmt.Errorf("Proposal adoption needs the original business transaction")
	}
	if err := a.Adoption.Check(); err != nil {
		return err
	}
	if a.Adoption.Function.App != "" && a.Adoption.Function.App != ID {
		return fmt.Errorf("Proposal adoption cannot cross record authorities")
	}
	input := slices.IndexFunc(a.Inputs, func(in Input) bool { return in.Name == a.Adoption.Input })
	if input < 0 || a.Inputs[input].Type != "reference" || a.Inputs[input].Ref != FunctionCallType || !a.Inputs[input].Required {
		return fmt.Errorf("Adoption needs a required reference input to the retained function call")
	}
	for _, mapping := range a.Adoption.Mapping {
		if !slices.ContainsFunc(a.Inputs, func(in Input) bool { return in.Name == mapping.Input }) {
			return fmt.Errorf("Proposal mapping names an undeclared action input")
		}
	}
	return nil
}

func (b *Build) prepareProposal(c platform.Caller, r *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time) (*proposalConsumption, *kernel.Error) {
	entity := b.installed[txn.Subject]
	if entity.Lifecycle == nil {
		return nil, nil
	}
	name := strings.TrimPrefix(r.GetSubmission().GetSchema().GetName(), txn.Subject+".")
	var action *platform.Transition
	for i := range entity.Lifecycle.Transitions {
		if entity.Lifecycle.Transitions[i].Name == name {
			action = &entity.Lifecycle.Transitions[i]
			break
		}
	}
	if action == nil || action.Adoption == nil {
		return nil, nil
	}
	fail := func(message string) (*proposalConsumption, *kernel.Error) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, message)
	}
	if c.Simulating() {
		return fail("Proposal adoption does not execute in isolated trials")
	}
	if c.Agent && action.Approval != nil {
		return fail("A checkpointed proposal must be adopted through its human approval path")
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(r.GetSubmission().GetPayload(), &values) != nil {
		return fail("Adoption inputs are invalid")
	}
	binding := *action.Adoption
	var id string
	if json.Unmarshal(values[binding.Input], &id) != nil || id == "" {
		return fail("Choose a retained proposal for this action")
	}
	visible, problem := c.ReadRecord(FunctionCallType, id, now)
	var readable FunctionRun
	if problem != nil || json.Unmarshal(visible, &readable) != nil || readable.Withheld || readable.Output == "" {
		return fail("The proposal and its current sources must be readable")
	}
	run, ok := platform.Get[FunctionRun](c, id)
	if !ok || run.State != "ready" || run.Contract.Proposal == nil || run.Decision != "pending" || run.Source != txn.Subject+"/"+r.GetSubmission().GetTarget().GetId() || run.Function != binding.Function.Name || cmp.Or(run.App, ID) != cmp.Or(binding.Function.App, ID) || binding.Function.Version != 0 && run.Version != binding.Function.Version {
		return fail("The proposal is not pending for this action and source")
	}
	if run.ExpiresAt.IsZero() || !now.Before(run.ExpiresAt) {
		return fail("The proposal has expired")
	}
	if len(run.SourceValues) != len(run.Contract.Fields) {
		return fail("The proposal has no accepted input evidence")
	}
	for _, field := range run.Contract.Fields {
		current, _, problem := c.ReadRecordPath(txn.Subject, r.GetSubmission().GetTarget().GetId(), []string{field}, now)
		if problem != nil || string(current) != string(run.SourceValues[field]) {
			return fail("The proposal source changed; request a new proposal")
		}
	}

	var output map[string]json.RawMessage
	if run.Contract.ValidateOutput([]byte(run.Output)) != nil || json.Unmarshal([]byte(run.Output), &output) != nil {
		return fail("The retained proposal output is invalid")
	}
	policy := run.Contract.Proposal
	if proposalNeedsHandover(run) && !c.Holds(ID, policy.HandoffRole) {
		checkpoint := action.Approval != nil && slices.ContainsFunc(action.Approval.Levels, func(level platform.ApprovalLevel) bool { return level.AppRole == policy.HandoffRole })
		if !checkpoint {
			return fail("This proposal needs its designated reviewer or approval checkpoint")
		}
	}
	if err := binding.CheckMapping(run.Contract, action.Payload); err != nil {
		return fail(err.Error())
	}
	amendments := map[string]json.RawMessage{}
	for _, mapping := range binding.Mapping {
		if string(values[mapping.Input]) != string(output[mapping.Output]) {
			amendments[mapping.Input] = values[mapping.Input]
		}
	}
	return &proposalConsumption{run: run, binding: binding, amendments: platform.Raw(amendments)}, nil
}
func proposalNeedsHandover(run FunctionRun) bool {
	p := run.Contract.Proposal
	if p == nil || p.HandoffRole == "" {
		return false
	}
	if p.ConfidenceField == "" {
		return true
	}
	var values map[string]json.RawMessage
	var confidence float64
	return json.Unmarshal([]byte(run.Output), &values) != nil || json.Unmarshal(values[p.ConfidenceField], &confidence) != nil || confidence < p.MinimumConfidence
}
func (b *Build) finishProposal(c platform.Caller, r *pb.ChangeRecord, p *proposalConsumption, now time.Time) *kernel.Error {
	if p == nil {
		return nil
	}
	run := p.run
	run.Decision = "adopted"
	run.DecidedBy = c.ID
	run.DecidedAt = now
	run.AdoptedAction = r.GetSubmission().GetSchema().GetName()
	run.AdoptedChange = r.GetChangeId()
	run.Amendments = p.amendments
	if proposalNeedsHandover(run) {
		if err := c.Assign(r, platform.Assignment{Key: "proposal:" + run.ID, Close: true}); err != nil {
			return err
		}
	}
	return c.Put(r, run)
}

// Read provenance uses the original entity compute and derivation paths.
// It is accepted with each business write, before its original validation.
func proposalEntity(e platform.Entity, changed map[string]any, p *proposalConsumption, extra []string) platform.Entity {
	if p == nil {
		return e
	}
	compute := e.ComputeAt
	e.ComputeAt = func(c platform.Caller, record any, at time.Time) *kernel.Error {
		if compute != nil {
			if err := compute(c, record, at); err != nil {
				return err
			}
		}
		rv := reflect.ValueOf(record).Elem()
		sources := append(slices.Clone(p.run.Sources), extra...)
		sources = append(sources, FunctionCallType+"/"+p.run.ID+"#output")
		slices.Sort(sources)
		sources = slices.Compact(sources)
		for field := range changed {
			destination := rv.FieldByName("AISources" + goName(field))
			if destination.IsValid() && destination.CanSet() {
				destination.Set(reflect.ValueOf(sources))
			}
		}
		return nil
	}
	return e
}

func (b *Build) submitProposalReview(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		raw, err := c.ReadRecord(FunctionCallType, s.GetTarget().GetId(), now)
		var visible FunctionRun
		run, ok := platform.Get[FunctionRun](c, s.GetTarget().GetId())
		if err != nil || json.Unmarshal(raw, &visible) != nil || visible.Withheld || !ok || run.Contract.Proposal == nil || run.State != "ready" || run.Decision != "pending" || c.ID != run.Member && !c.Holds(ID, run.Contract.Proposal.HandoffRole) && !c.Holds(ID, Builder) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The pending proposal is not available for review")
		}
		var input struct {
			Decision string `json:"decision"`
		}
		if json.Unmarshal(s.GetPayload(), &input) != nil || input.Decision != "rejected" && input.Decision != "expired" || input.Decision == "expired" && (run.ExpiresAt.IsZero() || now.Before(run.ExpiresAt)) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose rejection, or expiry after the proposal deadline")
		}
		return func(r *pb.ChangeRecord) {
			if input.Decision == "rejected" {
				fact := platform.BusinessRefusalFact{Level: "rejected", Code: "AI_PROPOSAL_REJECTED", Message: "The reviewer rejected the AI proposal", Translations: map[string]string{"zh-CN": "审核人驳回了 AI 提议"}, Subject: FunctionCallType + "/" + run.ID, Action: s.GetSchema().GetName(), Member: c.ID, At: now.UTC(), Original: raw, Inputs: map[string]json.RawMessage{"decision": platform.Raw(input.Decision)}, Record: FunctionCallType + "/" + run.ID}
				if err := c.CaptureBusinessRefusal(r, fact); err != nil {
					c.FailDecision(err)
					return
				}
			}
			run.Decision = input.Decision
			run.DecidedBy = c.ID
			run.DecidedAt = now
			if proposalNeedsHandover(run) {
				if err := c.Assign(r, platform.Assignment{Key: "proposal:" + run.ID, Close: true}); err != nil {
					return
				}
			}
			c.Put(r, run)
		}, nil
	})
}

func checkAdoptionSources(o Object, a Action, lookup func(string) (platform.EntityInfo, bool)) error {
	if a.Adoption == nil || a.Transaction == nil {
		return nil
	}
	mapped := map[string]bool{}
	for _, m := range a.Adoption.Mapping {
		mapped[m.Input] = true
	}
	taintedSteps := map[string]bool{}
	consumesRef := func(ref string) bool {
		if strings.HasPrefix(ref, "input.") {
			return mapped[strings.TrimPrefix(ref, "input.")]
		}
		if step, ok := strings.CutPrefix(ref, "step."); ok {
			name, _, _ := strings.Cut(step, ".")
			return taintedSteps[name]
		}
		return mapped[ref]
	}
	var consumes func(platform.Expr) bool
	consumes = func(e platform.Expr) bool {
		if consumesRef(e.Ref) {
			return true
		}
		for _, arg := range e.Args {
			if consumes(arg) {
				return true
			}
		}
		return e.In != nil && consumes(*e.In)
	}
	var problem error
	walkBusinessTxn(a.Transaction, func(step platform.TxnStep) {
		for _, change := range step.Changes {
			if !consumesRef(change.From) && (change.Expr == nil || !consumes(*change.Expr)) {
				continue
			}
			taintedSteps[step.Name] = true
			source := ""
			if step.Target.Type == TypeOf(o.Name) {
				for _, field := range o.Fields {
					if field.Name == change.Field {
						source = field.SourceType
						break
					}
				}
			} else if info, ok := lookup(step.Target.Type); ok {
				if field, ok := info.Field(change.Field); ok {
					source = field.SourceType
				}
			}
			if source != "generated" {
				problem = fmt.Errorf("AI proposal destinations must declare generated provenance")
			}
		}
	})
	return problem
}
