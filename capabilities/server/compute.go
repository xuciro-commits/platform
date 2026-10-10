package platformserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const operationEndpoint = "operation"

// Both compilation and execution use the original effect outbox. They are
// internal, owned computations: no endpoint configuration or external webhook.
type operationBinding struct {
	Definition  platform.Operation         `json:"definition"`
	Call        platform.OperationCall     `json:"call"`
	Member      string                     `json:"member"`
	Inputs      json.RawMessage            `json:"inputs,omitempty"`
	SealedInput *operationInputArtifact    `json:"sealedInput,omitempty"`
	Build       *platform.CodeBuildRequest `json:"build,omitempty"`
}

func (d *stagedDecision) RequestOperation(c platform.Caller, r *pb.ChangeRecord, q platform.OperationRequest) (platform.OperationCall, *kernel.Error) {
	var input *operationInputArtifact
	if p := d.operationInput; p != nil {
		hash, _ := canonicalDigest(q)
		platformOwner := c.App == PlatformApp && c.ID == p.ref.Member
		flowOwner := c.Automation && c.App == "flow" && c.ID == "app:flow" && q.OnBehalf == p.ref.Member
		if !platformOwner && !flowOwner || hash != p.requestHash {
			d.failure = platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The prepared input belongs to another operation request")
			return platform.OperationCall{}, d.failure
		}
		input = &p.ref
		d.operationInput = nil
	}
	call, intent, err := d.tenant.planOperation(c, r, q, d.records, d.intents, input)
	if err != nil {
		if d.failure == nil {
			d.failure = err
		}
		return platform.OperationCall{}, err
	}
	if !d.probing {
		d.intents = append(d.intents, intent)
	}
	return call, nil
}
func (t *Tenant) planOperation(c platform.Caller, r *pb.ChangeRecord, q platform.OperationRequest, store *recordStore, pending []platform.Effect, sealed ...*operationInputArtifact) (platform.OperationCall, platform.Effect, *kernel.Error) {
	refuse := func(code pb.ErrorCode, msg string) (platform.OperationCall, platform.Effect, *kernel.Error) {
		return platform.OperationCall{}, platform.Effect{}, platform.Refuse(code, msg)
	}
	owner := q.App
	if owner == "" {
		owner = c.App
	}
	if c.Tenant != t.ID || r == nil || r.GetSubmission().GetTenantId() != t.ID || r.GetSubmission().GetAuthority() != c.App || q.Key == "" || len(q.Key) > 256 || q.Version < 0 || owner != c.App && c.App != PlatformApp && (!c.Automation || c.App != "flow") || !c.Automation && (q.OnBehalf != "" && q.OnBehalf != c.ID || q.Release != nil) {
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
	app := t.app(owner)
	if app == nil {
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation owner is not installed")
	}
	op, version, ok := operationDefinition(app, q.Name, q.Version)
	if !ok {
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation is not declared")
	}
	if op.Check() != nil || !slices.Contains(op.Roles, member.Roles[owner]) {
		return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This member cannot call the operation")
	}
	var input *operationInputArtifact
	if len(sealed) == 1 {
		input = sealed[0]
	}
	if input == nil {
		if err := op.Input.Validate(q.Inputs, op.Limits.MaxInputBytes); err != nil {
			return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	for _, source := range q.Sources {
		if !t.readableIn(store, member, strings.Split(source, "#")[0], r.GetRecordedTime().AsTime()) {
			return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation source is not readable")
		}
		if strings.Contains(source, "#") && !t.mayRead(member, r.GetRecordedTime().AsTime(), false)(source) {
			return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation source field is not readable")
		}
	}
	definition, err := canonicalDigest([]any{owner, op, version})
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation definition cannot be bound")
	}
	closure, release, err := t.operationClosure(owner, op, version, q.Release)
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation dependencies cannot be bound")
	}
	hash, _ := canonicalDigest(q.Inputs)
	id := fmt.Sprintf("%s:%s:operation:%s:%s", t.ID, owner, q.Key, operationEndpoint)
	if input != nil {
		if input.Tenant != t.ID || input.Member != member.ID || input.Call != id || input.Definition != definition || op.Binding.ABI != platform.WasmDataABI || input.Size > op.Limits.DataInputBytes {
			return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The operation changed while its input was prepared")
		}
		if _, err := t.staged.inputKey(*input); err != nil {
			return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		hash = input.InputHash
	}
	call := platform.OperationCall{ID: id, Definition: definition, InputHash: hash, Version: version, Module: op.Binding.Module, Sources: slices.Clone(q.Sources), Dependencies: closure.ID, Release: release}
	if !c.Replaying && (t.Record == nil || c.Staging()) {
		call.OwnerVersion = app.Manifest().Version
	}
	for _, intent := range pending {
		if intent.ID == id {
			return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation request key is already planned")
		}
	}
	t.opsMu.Lock()
	exists := slices.ContainsFunc(t.outbound, func(x *effect) bool { return x.ID == id })
	queued := 0
	for _, x := range t.outbound {
		if x.Endpoint == operationEndpoint && !settled(x.State) {
			queued++
		}
	}
	for _, x := range pending {
		if x.Endpoint == operationEndpoint {
			queued++
		}
	}
	t.opsMu.Unlock()
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
	binding := operationBinding{Definition: op, Call: call, Member: member.ID, Inputs: slices.Clone(q.Inputs), SealedInput: input}
	if input != nil {
		binding.Inputs = nil
	}
	body, _ := json.Marshal(binding)
	at := r.GetRecordedTime().AsTime()
	return call, platform.Effect{ID: id, Endpoint: operationEndpoint, Event: owner + "/operation", App: owner, Key: q.Key, Target: target, At: at, Due: at, State: "pending", Body: string(body)}, nil
}
func operationDefinition(app platform.App, name string, version int) (platform.Operation, int, bool) {
	if owner, ok := app.(interface {
		OperationDefinition(string, int) (platform.Operation, int, bool)
	}); ok {
		return owner.OperationDefinition(name, version)
	}
	if version == 0 {
		for _, o := range app.Manifest().Operations {
			if o.Name == name {
				return o, 0, true
			}
		}
	}
	return platform.Operation{}, 0, false
}
func (t *Tenant) operationResult(c platform.Caller, id string) (platform.OperationResult, *kernel.Error) {
	t.opsMu.Lock()
	var found *platform.Effect
	for _, x := range t.outbound {
		if x.ID != id || x.Endpoint != operationEndpoint {
			continue
		}
		copy := x.Effect
		found = &copy
		break
	}
	t.opsMu.Unlock()
	if found != nil {
		x := *found
		var b operationBinding
		if json.Unmarshal([]byte(x.Body), &b) != nil {
			return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation input is unavailable")
		}
		if c.Tenant != t.ID || !c.Automation && c.ID != b.Member || c.Automation && c.App != x.App && c.App != "flow" {
			return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation is not readable")
		}
		member, ok := t.Member(b.Member)
		if !ok || b.Build != nil && member.Roles["build"] != "builder" || b.Build == nil && !slices.Contains(b.Definition.Roles, member.Roles[x.App]) {
			return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation permission was revoked")
		}
		for _, source := range b.Call.Sources {
			if !t.mayRead(member, time.Now().UTC(), false)(source) {
				return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation source is no longer readable")
			}
		}
		return operationResultOf(x), nil
	}
	return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation call was not found")
}
func operationResultOf(x platform.Effect) platform.OperationResult {
	r := platform.OperationResult{ID: x.ID, State: "pending", Error: x.Error, Generation: x.Generation, Millis: x.Millis}
	var binding operationBinding
	if json.Unmarshal([]byte(x.Body), &binding) == nil && binding.Build == nil {
		r.Ref = &platform.AssetRef{App: x.App, Kind: platform.AssetCompute, Name: binding.Definition.Name}
		r.OwnerVersion = binding.Call.OwnerVersion
		r.Definition = binding.Call.Definition
		r.Version = binding.Call.Version
		r.Module = binding.Call.Module
		r.Dependencies = binding.Call.Dependencies
		r.Release = binding.Call.Release
	}
	switch x.State {
	case "delivered":
		r.State = "completed"
		r.Output = slices.Clone(x.Output)
	case "rejected", "failed":
		r.State = "failed"
	case "discarded":
		r.State = "cancelled"
	}
	if x.Generation > 0 && r.State == "pending" {
		r.State = "running"
	}
	return r
}
func (r runtime) OperationResult(c platform.Caller, id string) (platform.OperationResult, *kernel.Error) {
	return r.t.operationResult(c, id)
}
func (d *stagedDecision) OperationResult(c platform.Caller, id string) (platform.OperationResult, *kernel.Error) {
	if x, ok := d.operationAnswers[id]; ok {
		return x, nil
	}
	return d.tenant.operationResult(c, id)
}

func (t *Tenant) operationAllowed(x platform.Effect, at time.Time) bool {
	var b operationBinding
	if json.Unmarshal([]byte(x.Body), &b) != nil {
		return false
	}
	member, ok := t.Member(b.Member)
	if !ok || member.Tenant != t.ID {
		return false
	}
	if b.Build != nil {
		return member.Roles["build"] == "builder"
	}
	if !slices.Contains(b.Definition.Roles, member.Roles[x.App]) {
		return false
	}
	for _, source := range b.Call.Sources {
		if !t.mayRead(member, at, false)(source) {
			return false
		}
	}
	return true
}

func (d *stagedDecision) RequestCompilation(c platform.Caller, r *pb.ChangeRecord, key, target string, q platform.CodeBuildRequest) (platform.OperationCall, *kernel.Error) {
	refuse := func(message string) (platform.OperationCall, *kernel.Error) {
		err := platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)
		if d.failure == nil {
			d.failure = err
		}
		return platform.OperationCall{}, err
	}
	if c.App != "build" || c.Automation || r.GetSubmission().GetAuthority() != c.App || key == "" || len(q.Source) == 0 || len(q.Source) > 256<<10 || q.Language != "go" && q.Language != "tinygo" || q.Input.Check() != nil || q.Output.Check() != nil {
		return refuse("Compilation needs a bounded Go/TinyGo code asset")
	}
	d.tenant.opsMu.Lock()
	queued := 0
	for _, x := range d.tenant.outbound {
		if x.Endpoint == operationEndpoint && !settled(x.State) {
			queued++
		}
	}
	d.tenant.opsMu.Unlock()
	for _, x := range d.intents {
		if x.Endpoint == operationEndpoint {
			queued++
		}
	}
	if queued >= 256 {
		return refuse("Operation queue is at capacity")
	}
	hash, _ := canonicalDigest(q)
	id := fmt.Sprintf("%s:build:operation:%s:%s", d.tenant.ID, key, operationEndpoint)
	call := platform.OperationCall{ID: id, InputHash: hash, Definition: hash}
	body, _ := json.Marshal(operationBinding{Build: &q, Call: call, Member: c.ID})
	at := r.GetRecordedTime().AsTime()
	if !d.probing {
		d.intents = append(d.intents, platform.Effect{ID: id, Endpoint: operationEndpoint, Event: "build/operation", App: "build", Key: key, Target: target, At: at, Due: at, State: "pending", Body: string(body)})
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

func (t *Tenant) executeOperation(x platform.Effect, now time.Time) platform.Outcome {
	out := platform.Outcome{Effect: x.ID, Result: "rejected", Generation: x.Generation}
	var b operationBinding
	if json.Unmarshal([]byte(x.Body), &b) != nil {
		out.Detail = "Invalid accepted operation input"
		return out
	}
	member, ok := t.Member(b.Member)
	if !ok || member.Tenant != t.ID {
		out.Detail = "Operation member is no longer authorised"
		return out
	}
	start := time.Now()
	timeout := 180 * time.Second
	if b.Build == nil {
		timeout = time.Duration(b.Definition.Limits.TimeoutMillis) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	t.mu.Lock()
	if !t.operationCurrent(x.ID, x.Generation) {
		t.mu.Unlock()
		out.Detail = "Operation was cancelled"
		return out
	}
	t.cancels.add(x.ID, cancel)
	t.mu.Unlock()
	defer t.cancels.remove(x.ID)
	var output json.RawMessage
	var err error
	if b.Build != nil {
		if member.Roles["build"] != "builder" {
			out.Detail = "Compilation permission was revoked"
			return out
		}
		compiler := t.Compiler
		if compiler == nil {
			compiler = EnvironmentCompiler()
		}
		var result platform.CodeBuildResult
		var module []byte
		result, module, err = compiler.Compile(ctx, *b.Build)
		if err == nil {
			err = ValidateWasm(ctx, module, b.Build.ABI)
			if err == nil {
				hash := sha256.Sum256(module)
				result.Module = hex.EncodeToString(hash[:])
				err = t.files().Put(ctx, artifactKey(t.ID, result.Module), module, "application/wasm")
			}
		}
		if err != nil {
			result.Diagnostics = err.Error()
		}
		output, _ = json.Marshal(result)
		out.Result = "delivered"
	} else {
		op := b.Definition
		hash, _ := canonicalDigest(b.Inputs)
		if b.SealedInput != nil {
			hash = b.SealedInput.InputHash
		}
		definition, _ := canonicalDigest([]any{x.App, op, b.Call.Version})
		if op.Check() != nil || hash != b.Call.InputHash || definition != b.Call.Definition || !slices.Contains(op.Roles, member.Roles[x.App]) {
			out.Detail = "Operation definition or permission differs"
			return out
		}
		for _, source := range b.Call.Sources {
			if !t.mayRead(member, now, false)(source) {
				out.Detail = "Operation source permission was revoked"
				return out
			}
		}
		inputs := b.Inputs
		if ref := b.SealedInput; ref != nil {
			if len(inputs) != 0 || ref.Tenant != t.ID || ref.Call != x.ID || ref.Member != b.Member || ref.Definition != b.Call.Definition || ref.InputHash != b.Call.InputHash || ref.Size > op.Limits.DataInputBytes || op.Binding.ABI != platform.WasmDataABI {
				out.Detail = "The accepted input artifact belongs to another operation"
				return out
			}
			inputs, err = t.staged.readInput(ctx, *ref)
			if err == nil {
				err = op.Input.Validate(inputs, op.Limits.DataInputBytes)
			}
			if err == nil {
				actual, digestErr := canonicalDigest(json.RawMessage(inputs))
				if digestErr != nil || actual != b.Call.InputHash {
					err = fmt.Errorf("the sealed input differs from its accepted input hash")
				}
			}
			if err != nil {
				out.Detail = err.Error()
				return out
			}
			// Reading a file is outside the submission lock. Recheck the live
			// grants and cancellation before disclosing its bytes to a guest.
			t.mu.Lock()
			latest, authorised := t.Member(b.Member)
			authorised = authorised && t.admits(latest) == nil && slices.Contains(op.Roles, latest.Roles[x.App]) && t.operationCurrent(x.ID, x.Generation)
			for _, source := range b.Call.Sources {
				authorised = authorised && t.mayRead(latest, now, false)(source)
			}
			t.mu.Unlock()
			if !authorised {
				out.Detail = "Operation permission or generation changed while its input was read"
				return out
			}
		}
		if op.Binding.Kind == "native" {
			executor, ok := t.app(x.App).(platform.OperationExecutor)
			if !ok {
				err = fmt.Errorf("Native operation executor is not available")
			} else {
				output, err = executor.Compute(ctx, op.Name, inputs)
			}
		} else {
			var module []byte
			module, err = t.readArtifact(ctx, op.Binding.Module)
			if err == nil {
				worker := t.ComputeWorker
				if worker == nil {
					worker = EnvironmentWasmWorker()
				}
				var result WasmResponse
				request := WasmRequest{ABI: op.Binding.ABI, Module: module, Digest: op.Binding.Module, Input: inputs, Limits: op.Limits}
				if op.Limits.DataInputBytes > 0 {
					sum := sha256.Sum256(inputs)
					request.Input = nil
					request.Data = &WasmDataInput{Tenant: t.ID, Call: x.ID, Digest: hex.EncodeToString(sum[:]), Bytes: inputs}
				}
				result, err = worker.Execute(ctx, request)
				output = result.Output
			}
		}
		if err == nil {
			// Inline while it fits; above that, the operation's declared
			// per-call channel seals the bytes and the result references them
			// (ADR-0047 §13.3). A result over every budget is refused, never
			// trimmed.
			answer, staged, stageErr := t.staged.output(op, x.ID, output)
			err = stageErr
			if stageErr == nil {
				output, out.Staged = answer, staged
			}
		}
		if err != nil {
			out.Detail = err.Error()
			return out
		}
		out.Result = "delivered"
	}
	out.Answer = output
	out.Millis = time.Since(start).Milliseconds()
	digest := sha256.Sum256(output)
	out.Digest = hex.EncodeToString(digest[:])
	return out
}
