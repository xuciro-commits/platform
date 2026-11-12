package compute

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const operationEndpoint = "operation"

type OperationPlan struct {
	Tenant    string
	Member    func(string) (platform.Member, bool)
	App       func(string) platform.App
	Authorize func(applicant platform.Caller, owner string, op platform.Operation, at time.Time, sources []string, definition string) *kernel.Error
	Closure   func(owner string, op platform.Operation, version int, release *string) (string, string, error)
	InputKey  func(platform.OperationInputArtifact) error
	Recording bool
	Exists    func(id string) bool
	Queued    int
	Retained  func(module string) bool
	Bind      func(ref platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error)
	Entity    func(string) (platform.EntityInfo, bool)
	Rules     platform.RuleEngine
}

func (p OperationPlan) Plan(c platform.Caller, r *pb.ChangeRecord, q platform.OperationRequest, pending []platform.Effect, input *platform.OperationInputArtifact) (platform.OperationCall, platform.Effect, *kernel.Error) {
	refuse := func(code pb.ErrorCode, msg string) (platform.OperationCall, platform.Effect, *kernel.Error) {
		return platform.OperationCall{}, platform.Effect{}, platform.Refuse(code, msg)
	}
	owner := q.App
	if owner == "" {
		owner = c.App
	}
	if c.Tenant != p.Tenant || r == nil || r.GetSubmission().GetTenantId() != p.Tenant || r.GetSubmission().GetAuthority() != c.App || q.Key == "" || len(q.Key) > 256 || q.Version < 0 || owner != c.App && c.App != "platform" && (!c.Automation || c.App != "flow") || !c.Automation && (q.OnBehalf != "" && q.OnBehalf != c.ID || q.Release != nil) {
		return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation needs an accepted owner or delegated Flow decision")
	}
	member := c.Member
	if c.Automation {
		var ok bool
		member, ok = p.Member(q.OnBehalf)
		if !ok || member.Agent {
			return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation needs an explicit authorised member")
		}
	}
	app := p.App(owner)
	if app == nil {
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation owner is not installed")
	}
	op, version, ok := OperationDefinition(app, q.Name, q.Version)
	if !ok {
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation is not declared")
	}
	definition, err := platform.CanonicalDigest([]any{owner, op, version})
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation definition cannot be bound")
	}
	at := r.GetRecordedTime().AsTime()
	if refusal := p.Authorize(platform.ActingCaller(c, member, owner, false), owner, op, at, q.Sources, definition); refusal != nil {
		return platform.OperationCall{}, platform.Effect{}, refusal
	}
	if input == nil {
		if err := op.Input.Validate(q.Inputs, op.Limits.MaxInputBytes); err != nil {
			return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	dependencies, release, err := p.Closure(owner, op, version, q.Release)
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation dependencies cannot be bound")
	}
	hash, _ := platform.CanonicalDigest(q.Inputs)
	id := fmt.Sprintf("%s:%s:operation:%s:%s", p.Tenant, owner, q.Key, operationEndpoint)
	if input != nil {
		if input.Tenant != p.Tenant || input.Member != member.ID || input.Call != id || input.Definition != definition || op.Binding.ABI != platform.WasmDataABI || input.Size > op.Limits.DataInputBytes {
			return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The operation changed while its input was prepared")
		}
		if err := p.InputKey(*input); err != nil {
			return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		hash = input.InputHash
	}
	call := platform.OperationCall{ID: id, Definition: definition, InputHash: hash, Version: version, Module: op.Binding.Module, Sources: slices.Clone(q.Sources), Dependencies: dependencies, Release: release}
	if !c.Replaying && (!p.Recording || c.Staging()) {
		call.OwnerVersion = app.Manifest().Version
	}
	queued := p.Queued
	for _, intent := range pending {
		if intent.ID == id {
			return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation request key is already planned")
		}
		if intent.Endpoint == operationEndpoint {
			queued++
		}
	}
	if p.Exists(id) {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation request key is already accepted")
	}
	if queued >= 256 {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation queue is at capacity")
	}
	if op.Binding.Kind == "wasm" && !p.Retained(op.Binding.Module) {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation module is not retained")
	}
	target := q.Target
	if target == "" {
		target = r.GetSubmission().GetTarget().GetType() + "/" + r.GetSubmission().GetTarget().GetId()
	}
	applications, err := p.Bind(platform.AssetRef{App: owner, Kind: platform.AssetCompute, Name: op.Name}, call.Release, call.Dependencies)
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The operation dependencies cannot be bound")
	}
	call.Applications = applications
	binding := OperationBinding{Definition: op, Call: call, Member: member.ID, Inputs: slices.Clone(q.Inputs), SealedInput: input}
	if op.Rules != nil {
		if input != nil {
			return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Rule tables use captured scalar inputs")
		}
		var values map[string]any
		decoder := json.NewDecoder(strings.NewReader(string(q.Inputs)))
		decoder.UseNumber()
		if decoder.Decode(&values) != nil {
			return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Rule table inputs are invalid")
		}
		aggregate := platform.Expr{}
		for _, row := range op.Rules.Rows {
			schema, err := platform.RuleSchema(row.Condition, op.Rules.Reads, p.Entity, p.Rules)
			if err != nil {
				return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			binding.RuleSchemas = append(binding.RuleSchemas, schema)
			aggregate.Args = append(aggregate.Args, row.Condition)
		}
		facts, err := platform.ActingCaller(c, member, owner, false).RuleContext(aggregate, op.Rules.Reads, p.Entity, nil, values, at)
		if err != nil {
			return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, err.Error())
		}
		binding.RuleFacts = &facts
		call.Sources = append(call.Sources, facts.Sources...)
		binding.Call = call
	}
	if input != nil {
		binding.Inputs = nil
	}
	body, _ := json.Marshal(binding)
	return call, platform.Effect{Applications: applications, ID: id, Endpoint: operationEndpoint, Event: owner + "/operation", App: owner, Key: q.Key, Target: target, At: at, Due: at, State: "pending", Body: string(body)}, nil
}
