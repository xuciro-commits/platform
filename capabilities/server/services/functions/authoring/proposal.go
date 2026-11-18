package authoring

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/kernel/protocol"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type Proposal struct {
	run        protocol.FunctionRun
	binding    platform.ProposalAdoption
	amendments json.RawMessage
}

func (o *Owner) PrepareProposal(c platform.Caller, r *pb.ChangeRecord, txn *platform.BusinessTxn, entity platform.Entity, now time.Time) (platform.TransactionProposal, *platform.Error) {
	p, err := o.prepareProposal(c, r, txn, entity, now)
	if err != nil {
		return platform.TransactionProposal{}, err
	}
	return platform.TransactionProposal{Apply: p.Apply, Finish: p.Finish}, nil
}

func (o *Owner) prepareProposal(c platform.Caller, r *pb.ChangeRecord, txn *platform.BusinessTxn, entity platform.Entity, now time.Time) (*Proposal, *platform.Error) {
	if entity.Lifecycle == nil {
		return nil, nil
	}
	name := strings.TrimPrefix(r.GetSubmission().GetSchema().GetName(), txn.Subject+".")
	var action *platform.ActionType
	for i := range entity.Lifecycle.Actions {
		if entity.Lifecycle.Actions[i].Name == name {
			action = &entity.Lifecycle.Actions[i]
			break
		}
	}
	if action == nil || action.Adoption == nil {
		return nil, nil
	}
	fail := func(message string) (*Proposal, *platform.Error) {
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
	visible, problem := c.ReadRecord(protocol.FunctionCallType, id, now)
	var readable protocol.FunctionRun
	if problem != nil || json.Unmarshal(visible, &readable) != nil || readable.Withheld || readable.Output == "" {
		return fail("The proposal and its current sources must be readable")
	}
	run, ok := platform.Get[protocol.FunctionRun](c, id)
	if !ok || run.State != "ready" || run.Contract.Proposal == nil || run.Decision != "pending" || run.Source != txn.Subject+"/"+r.GetSubmission().GetTarget().GetId() || run.Function != binding.Function.Name || cmp.Or(run.App, o.env.App()) != cmp.Or(binding.Function.App, o.env.App()) || binding.Function.Version != 0 && run.Version != binding.Function.Version {
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
	if NeedsHandover(run) && !c.Holds(o.env.App(), policy.HandoffRole) {
		checkpoint := action.Approval != nil && slices.ContainsFunc(action.Approval.Levels, func(level platform.ApprovalLevel) bool {
			return level.AppRole == policy.HandoffRole
		})
		if !checkpoint {
			return fail("This proposal needs its designated reviewer or approval checkpoint")
		}
	}
	if err := binding.CheckMapping(run.Contract, action.Parameters); err != nil {
		return fail(err.Error())
	}
	amendments := map[string]json.RawMessage{}
	for _, mapping := range binding.Mapping {
		if string(values[mapping.Input]) != string(output[mapping.Output]) {
			amendments[mapping.Input] = values[mapping.Input]
		}
	}
	return &Proposal{run: run, binding: binding, amendments: platform.Raw(amendments)}, nil
}

func (p *Proposal) Finish(c platform.Caller, r *pb.ChangeRecord, now time.Time) *platform.Error {
	if p == nil {
		return nil
	}
	run := p.run
	run.Decision = "adopted"
	run.DecidedBy = c.ID
	run.DecidedAt = now
	run.AdoptedAction = r.GetSubmission().GetSchema().GetName()
	run.AdoptedChange = r.GetChangeId()
	authorization, _ := platform.CanonicalDigest([]any{c.ID, c.Tenant, c.Roles, c.Grants, c.Scopes, c.Status})
	if run.TrainingAuthorization != authorization {
		run.TrainingAuthorization = ""
	}
	run.TrainingSources = map[string]uint32{}
	for _, ref := range run.Sources {
		typ, id, ok := strings.Cut(strings.Split(ref, "#")[0], "/")
		if !ok {
			continue
		}
		raw, problem := c.ReadRecord(typ, id, now)
		if problem != nil {
			continue
		}
		var header struct {
			Revision uint32 `json:"revision"`
		}
		_ = json.Unmarshal(raw, &header)
		run.TrainingSources[strings.Split(ref, "#")[0]] = header.Revision
	}

	run.Amendments = p.amendments
	if NeedsHandover(run) {
		if err := c.Assign(r, platform.Assignment{Key: "proposal:" + run.ID, Close: true}); err != nil {
			return err
		}
	}
	return c.Put(r, run)
}

func (p *Proposal) Apply(entity platform.Entity, info platform.EntityInfo, changed map[string]any, extra []string) (platform.Entity, map[string]any) {
	if p == nil {
		return entity, changed
	}
	payload := maps.Clone(changed)
	generated := map[string]any{}
	for name, value := range changed {
		if field, ok := info.Field(name); ok && field.ReadOnly && field.SourceType == "generated" {
			generated[name] = value
			delete(payload, name)
		}
	}
	compute := entity.ComputeAt
	entity.ComputeAt = func(c platform.Caller, record any, at time.Time) *platform.Error {
		reflectRecord := reflect.ValueOf(record).Elem()
		for name, value := range generated {
			field, _ := info.Field(name)
			if err := assignProposalField(reflectRecord.FieldByIndex(field.Index), value); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
		}
		if compute != nil {
			if err := compute(c, record, at); err != nil {
				return err
			}
		}
		sources := append(slices.Clone(p.run.Sources), extra...)
		sources = append(sources, protocol.FunctionCallType+"/"+p.run.ID+"#output")
		slices.Sort(sources)
		sources = slices.Compact(sources)
		for field := range changed {
			destination := reflectRecord.FieldByName("AISources" + proposalGoName(field))
			if destination.IsValid() && destination.CanSet() {
				destination.Set(reflect.ValueOf(sources))
			}
		}
		return nil
	}
	return entity, payload
}

func assignProposalField(field reflect.Value, value any) error {
	if !field.IsValid() {
		return fmt.Errorf("no such field")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	target := reflect.New(field.Type())
	if err := json.Unmarshal(raw, target.Interface()); err != nil {
		return fmt.Errorf("%v does not fit a %s", value, field.Type())
	}
	field.Set(target.Elem())
	return nil
}

func proposalGoName(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}
