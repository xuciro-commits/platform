package host

import records "platformserver/kernel/records"

import "platformserver/services/functions/runtime"

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	acceptedformat "platformserver/kernel/acceptedformat"
	controlowner "platformserver/services/security/control"

	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
)

func pendingOperationInputs(inputs map[string][]*preparedOperationInput) int {
	pending := 0
	for _, prepared := range inputs {
		pending += len(prepared)
	}
	return pending
}

func (d *stagedDecision) RequestOperation(c platform.Caller, r *pb.ChangeRecord, q platform.OperationRequest) (platform.OperationCall, *kernel.Error) {
	var input *platform.OperationInputArtifact
	hash, _ := platform.CanonicalDigest(q)
	if pending := d.operationInputs[hash]; len(pending) > 0 {
		prepared := pending[0]
		platformOwner := c.App == controlowner.PlatformApp && c.ID == prepared.ref.Member
		flowOwner := c.Automation && c.App == "flow" && c.ID == "app:flow" && q.OnBehalf == prepared.ref.Member
		if !platformOwner && !flowOwner || hash != prepared.requestHash {
			d.Failure = platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The prepared input belongs to another operation request")
			return platform.OperationCall{}, d.Failure
		}
		input = &prepared.ref
		if len(pending) == 1 {
			delete(d.operationInputs, hash)
		} else {
			d.operationInputs[hash] = pending[1:]
		}
	}
	call, intent, err := d.tenant.planOperation(c, r, q, d.Records, d.Intents, input)
	if err != nil {
		if d.Failure == nil {
			d.Failure = err
		}
		return platform.OperationCall{}, err
	}
	if !d.ProbeOnly {
		d.Intents = append(d.Intents, intent)
	}
	return call, nil
}
func (t *Tenant) planOperation(c platform.Caller, r *pb.ChangeRecord, q platform.OperationRequest, store *records.Store, pending []platform.Effect, sealed ...*platform.OperationInputArtifact) (platform.OperationCall, platform.Effect, *kernel.Error) {
	refuse := func(code pb.ErrorCode, msg string) (platform.OperationCall, platform.Effect, *kernel.Error) {
		return platform.OperationCall{}, platform.Effect{}, platform.Refuse(code, msg)
	}
	owner := q.App
	if owner == "" {
		owner = c.App
	}
	if c.Tenant != t.ID || r == nil || r.GetSubmission().GetTenantId() != t.ID || r.GetSubmission().GetAuthority() != c.App || q.Key == "" || len(q.Key) > 256 || q.Version < 0 || owner != c.App && c.App != controlowner.PlatformApp && (!c.Automation || c.App != "flow") || !c.Automation && (q.OnBehalf != "" && q.OnBehalf != c.ID || q.Release != nil) {
		return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation needs an accepted owner or delegated Flow decision")
	}
	member := c.Member
	if c.Automation {
		var ok bool
		member, ok = t.Member(q.OnBehalf)
		if !ok || member.Agent {
			return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation needs an explicit authorised member")
		}
	}
	app := t.App(owner)
	if app == nil {
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation owner is not installed")
	}
	op, version, ok := compute.OperationDefinition(app, q.Name, q.Version)
	if !ok {
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation is not declared")
	}
	definition, err := platform.CanonicalDigest([]any{owner, op, version})
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation definition cannot be bound")
	}
	applicant := platform.ActingCaller(c, member, owner, false)
	decision := t.authorizeCapability(applicant, owner, op.Name, op.Roles, op.Check() == nil, authorizationDetails{Store: store, At: r.GetRecordedTime().AsTime(), Sources: q.Sources, Definition: definition})
	if refusal := authorizationRefusal(decision); refusal != nil {
		return platform.OperationCall{}, platform.Effect{}, refusal
	}
	var input *platform.OperationInputArtifact
	if len(sealed) == 1 {
		input = sealed[0]
	}
	if input == nil {
		if err := op.Input.Validate(q.Inputs, op.Limits.MaxInputBytes); err != nil {
			return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	closure, release, err := t.operationClosure(owner, op, version, q.Release)
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation dependencies cannot be bound")
	}
	hash, _ := platform.CanonicalDigest(q.Inputs)
	id := fmt.Sprintf("%s:%s:operation:%s:%s", t.ID, owner, q.Key, acceptedformat.OperationEndpoint)
	if input != nil {
		if input.Tenant != t.ID || input.Member != member.ID || input.Call != id || input.Definition != definition || op.Binding.ABI != platform.WasmDataABI || input.Size > op.Limits.DataInputBytes {
			return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The operation changed while its input was prepared")
		}
		if _, err := t.staged.InputKey(*input); err != nil {
			return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		hash = input.InputHash
	}
	call := platform.OperationCall{ID: id, Definition: definition, InputHash: hash, Version: version, Module: op.Binding.Module, Sources: slices.Clone(q.Sources), Dependencies: closure.ID, Release: release}
	if !c.Replaying && (!t.recordsAcceptedResults() || c.Staging()) {
		call.OwnerVersion = app.Manifest().Version
	}
	for _, intent := range pending {
		if intent.ID == id {
			return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation request key is already planned")
		}
	}
	_, exists := t.outbound.Get(id)
	queued := t.queuedOperations()
	for _, x := range pending {
		if x.Endpoint == acceptedformat.OperationEndpoint {
			queued++
		}
	}
	if exists {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation request key is already accepted")
	}
	if queued >= 256 {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation queue is at capacity")
	}
	if op.Binding.Kind == "wasm" && !t.files().Exists(context.Background(), artifactKey(t.ID, op.Binding.Module)) {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation module is not retained")
	}
	target := q.Target
	if target == "" {
		target = r.GetSubmission().GetTarget().GetType() + "/" + r.GetSubmission().GetTarget().GetId()
	}
	applications, err := t.bindApplications(platform.AssetRef{App: owner, Kind: platform.AssetCompute, Name: op.Name}, call.Release, call.Dependencies)
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The operation dependencies cannot be bound")
	}
	call.Applications = applications
	binding := compute.OperationBinding{Definition: op, Call: call, Member: member.ID, Inputs: slices.Clone(q.Inputs), SealedInput: input}
	if op.Rules != nil {
		if input != nil {
			return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Rule tables use captured scalar inputs")
		}
		ruleCaller := platform.ActingCaller(c, member, owner, false)
		var values map[string]any
		decoder := json.NewDecoder(strings.NewReader(string(q.Inputs)))
		decoder.UseNumber()
		if decoder.Decode(&values) != nil {
			return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Rule table inputs are invalid")
		}
		aggregate := platform.Expr{}
		for _, row := range op.Rules.Rows {
			schema, err := platform.RuleSchema(row.Condition, op.Rules.Reads, t.Entity, t.services.Rules)
			if err != nil {
				return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			binding.RuleSchemas = append(binding.RuleSchemas, schema)
			aggregate.Args = append(aggregate.Args, row.Condition)
		}
		facts, err := ruleCaller.RuleContext(aggregate, op.Rules.Reads, t.Entity, nil, values, r.GetRecordedTime().AsTime())
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
	at := r.GetRecordedTime().AsTime()
	return call, platform.Effect{Applications: applications, ID: id, Endpoint: acceptedformat.OperationEndpoint, Event: owner + "/operation", App: owner, Key: q.Key, Target: target, At: at, Due: at, State: "pending", Body: string(body)}, nil
}
func (t *Tenant) operationResult(c platform.Caller, id string) (platform.OperationResult, *kernel.Error) {
	if found, ok := t.outbound.Get(id); ok && found.Endpoint == acceptedformat.OperationEndpoint {
		x := found.Effect
		var b compute.OperationBinding
		if json.Unmarshal([]byte(x.Body), &b) != nil {
			return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation input is unavailable")
		}
		if c.Tenant != t.ID || !c.Automation && c.ID != b.Member || c.Automation && c.App != x.App && c.App != "flow" {
			return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation is not readable")
		}
		member, ok := t.Member(b.Member)
		if !ok {
			return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation permission was revoked")
		}
		if refusal := authorizationRefusal(t.operationAuthorization(member, x.App, b, time.Now().UTC())); refusal != nil {
			return platform.OperationResult{}, refusal
		}
		return compute.ResultOf(x), nil
	}
	return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation call was not found")
}
func (r runtime) OperationResult(c platform.Caller, id string) (platform.OperationResult, *kernel.Error) {
	return r.t.operationResult(c, id)
}
func (d *stagedDecision) OperationResult(c platform.Caller, id string) (platform.OperationResult, *kernel.Error) {
	if x, ok := d.OperationAnswers[id]; ok {
		return x, nil
	}
	return d.tenant.operationResult(c, id)
}

func (t *Tenant) operationAllowed(x platform.Effect, at time.Time) bool {
	var b compute.OperationBinding
	if json.Unmarshal([]byte(x.Body), &b) != nil {
		return false
	}
	member, ok := t.Member(b.Member)
	if !ok || member.Tenant != t.ID {
		return false
	}
	return t.operationAuthorization(member, x.App, b, at).Allow
}

func (t *Tenant) operationAuthorization(member platform.Member, owner string, binding compute.OperationBinding, at time.Time) platform.AuthzDecision {
	permission, allowed, declared := binding.Definition.Name, binding.Definition.Roles, binding.Definition.Check() == nil
	if binding.Build != nil {
		permission, allowed, declared = "build.code.compile", []string{"builder"}, binding.Build.Input.Check() == nil && binding.Build.Output.Check() == nil
	}
	c := platform.NewCaller(runtime{t: t, scoped: true}, member, owner, false, false)
	return t.authorizeCapability(c, owner, permission, allowed, declared, authorizationDetails{At: at, Sources: binding.Call.Sources, Definition: binding.Call.Definition})
}

func (d *stagedDecision) RequestCompilation(c platform.Caller, r *pb.ChangeRecord, key, target string, q platform.CodeBuildRequest) (platform.OperationCall, *kernel.Error) {
	refuse := func(message string) (platform.OperationCall, *kernel.Error) {
		err := platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)
		if d.Failure == nil {
			d.Failure = err
		}
		return platform.OperationCall{}, err
	}
	if c.App != "build" || c.Automation || r.GetSubmission().GetAuthority() != c.App || key == "" || len(q.Source) == 0 || len(q.Source) > 256<<10 || q.Language != "go" && q.Language != "tinygo" || q.Input.Check() != nil || q.Output.Check() != nil {
		return refuse("Compilation needs a bounded Go/TinyGo code asset")
	}
	queued := d.tenant.queuedOperations()
	for _, x := range d.Intents {
		if x.Endpoint == acceptedformat.OperationEndpoint {
			queued++
		}
	}
	if queued >= 256 {
		return refuse("Operation queue is at capacity")
	}
	hash, _ := platform.CanonicalDigest(q)
	id := fmt.Sprintf("%s:build:operation:%s:%s", d.tenant.ID, key, acceptedformat.OperationEndpoint)
	call := platform.OperationCall{ID: id, InputHash: hash, Definition: hash}
	body, _ := json.Marshal(compute.OperationBinding{Build: &q, Call: call, Member: c.ID})
	at := r.GetRecordedTime().AsTime()
	if !d.ProbeOnly {
		d.Intents = append(d.Intents, platform.Effect{ID: id, Endpoint: acceptedformat.OperationEndpoint, Event: "build/operation", App: "build", Key: key, Target: target, At: at, Due: at, State: "pending", Body: string(body)})
	}
	return call, nil
}

func artifactKey(tenant, digest string) string { return tenant + "/artifacts/wasm/" + digest }
func (t *Tenant) readArtifact(ctx context.Context, digest string) ([]byte, error) {
	r, n, err := t.files().Get(ctx, artifactKey(t.ID, digest))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if n <= 0 || n > 16<<20 {
		return nil, fmt.Errorf("module exceeds its byte bound")
	}
	raw, err := io.ReadAll(io.LimitReader(r, (16<<20)+1))
	hash := sha256.Sum256(raw)
	if err != nil || len(raw) > 16<<20 || hex.EncodeToString(hash[:]) != digest {
		return nil, fmt.Errorf("module digest differs from its retained bytes")
	}
	return raw, nil
}

func (t *Tenant) buildCode(ctx context.Context, q platform.CodeBuildRequest) (platform.CodeBuildResult, error) {
	result, module, err := compute.EnvironmentCompiler().Compile(ctx, q)
	if err == nil {
		err = compute.ValidateWasm(ctx, module, q.ABI)
		if err == nil {
			hash := sha256.Sum256(module)
			result.Module = hex.EncodeToString(hash[:])
			err = t.files().Put(ctx, artifactKey(t.ID, result.Module), module, "application/wasm")
		}
	}
	if err != nil {
		result.Diagnostics = err.Error()
	}
	return result, err
}

func (t *Tenant) artifactStored(ctx context.Context, digest string) bool {
	r, n, err := t.files().Get(ctx, artifactKey(t.ID, digest))
	if err != nil {
		return false
	}
	r.Close()
	return n > 0 && n <= 16<<20
}

func (t *Tenant) executeOperation(x platform.Effect, now time.Time) platform.Outcome {
	var b compute.OperationBinding
	if json.Unmarshal([]byte(x.Body), &b) != nil {
		return platform.Outcome{Effect: x.ID, Result: "rejected", Generation: x.Generation, Detail: "Invalid accepted operation input"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.Timeout())
	defer cancel()
	t.cancels.Add(x.ID, cancel)
	defer t.cancels.Remove(x.ID)
	return compute.Execute(ctx, operationExecution{t}, t.ID, x, b, now)
}

func (t *Tenant) queuedOperations() int {
	return len(t.outbound.Select(func(x actions.Queued) bool {
		return x.Endpoint == acceptedformat.OperationEndpoint && !platform.EffectSettled(x.State)
	}))
}
