package compute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type OperationBinding struct {
	Definition  platform.Operation               `json:"definition"`
	Call        platform.OperationCall           `json:"call"`
	Member      string                           `json:"member"`
	Inputs      json.RawMessage                  `json:"inputs,omitempty"`
	SealedInput *platform.OperationInputArtifact `json:"sealedInput,omitempty"`
	RuleFacts   *platform.ExprContext            `json:"ruleFacts,omitempty"`
	RuleSchemas []platform.ExprSchema            `json:"ruleSchemas,omitempty"`
	Build       *platform.CodeBuildRequest       `json:"build,omitempty"`
}

func (b OperationBinding) Permission() (string, []string, bool) {
	if b.Build != nil {
		return "build.code.compile", []string{"builder"}, b.Build.Input.Check() == nil && b.Build.Output.Check() == nil
	}
	return b.Definition.Name, b.Definition.Roles, b.Definition.Check() == nil
}

func (b OperationBinding) Timeout() time.Duration {
	if b.Build != nil {
		return 180 * time.Second
	}
	return time.Duration(b.Definition.Limits.TimeoutMillis) * time.Millisecond
}

type ExecutionHost struct {
	Read         func(ctx context.Context, read func(ExecutionReads)) *kernel.Error
	BuildCode    func(ctx context.Context, q platform.CodeBuildRequest) (platform.CodeBuildResult, error)
	ReadInput    func(ctx context.Context, ref platform.OperationInputArtifact) ([]byte, error)
	StageOutput  func(op platform.Operation, call string, output []byte) (json.RawMessage, *platform.StagedResult, error)
	App          func(id string) platform.App
	ReadArtifact func(ctx context.Context, digest string) ([]byte, error)
	Rules        func() platform.RuleEngine
}

type ExecutionReads struct {
	Member  func(id string) (platform.Member, bool)
	Current func(id string, generation uint32) bool
	Refusal func(member platform.Member, app string, b OperationBinding, now time.Time) *kernel.Error
	Query   func(member platform.Member, ref platform.AssetRef, version int) (platform.NamedQuery, bool)
	Entity  func(string) (platform.EntityInfo, bool)
	Authz   platform.AuthorizationEngine
}

func (h ExecutionHost) authorised(ctx context.Context, tenant string, x platform.Effect, b OperationBinding, now time.Time) (platform.Member, *kernel.Error) {
	var member platform.Member
	var refusal *kernel.Error
	if err := h.Read(ctx, func(r ExecutionReads) {
		var ok bool
		member, ok = r.Member(b.Member)
		switch {
		case !ok || member.Tenant != tenant:
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation member is no longer authorised")
		case !r.Current(x.ID, x.Generation):
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation was cancelled")
		default:
			refusal = r.Refusal(member, x.App, b, now)
		}
	}); err != nil {
		return member, err
	}
	return member, refusal
}

func (h ExecutionHost) stillAuthorised(ctx context.Context, x platform.Effect, b OperationBinding, now time.Time) (bool, *kernel.Error) {
	var allowed bool
	err := h.Read(ctx, func(r ExecutionReads) {
		latest, ok := r.Member(b.Member)
		allowed = ok && r.Refusal(latest, x.App, b, now) == nil && r.Current(x.ID, x.Generation)
	})
	return allowed, err
}

func (h ExecutionHost) rulesRevoked(ctx context.Context, member platform.Member, rules platform.RuleTablePlan) (string, *kernel.Error) {
	var why string
	err := h.Read(ctx, func(r ExecutionReads) {
		why = RulesRevoked(member, rules, func(ref platform.AssetRef, version int) (platform.NamedQuery, bool) {
			return r.Query(member, ref, version)
		}, r.Entity, r.Authz)
	})
	return why, err
}

func Execute(ctx context.Context, host ExecutionHost, tenant string, x platform.Effect, b OperationBinding, now time.Time) platform.Outcome {
	out := platform.Outcome{Effect: x.ID, Result: "rejected", Generation: x.Generation}
	start := time.Now()
	member, refusal := host.authorised(ctx, tenant, x, b, now)
	if refusal != nil {
		out.Detail = refusal.Message
		return out
	}
	var output json.RawMessage
	var err error
	if b.Build != nil {
		var result platform.CodeBuildResult
		result, err = host.BuildCode(ctx, *b.Build)
		output, _ = json.Marshal(result)
		out.Result = "delivered"
	} else {
		op := b.Definition
		hash, _ := platform.CanonicalDigest(b.Inputs)
		if b.SealedInput != nil {
			hash = b.SealedInput.InputHash
		}
		definition, _ := platform.CanonicalDigest([]any{x.App, op, b.Call.Version})
		if op.Check() != nil || hash != b.Call.InputHash || definition != b.Call.Definition {
			out.Detail = "Operation definition or permission differs"
			return out
		}
		inputs := b.Inputs
		if ref := b.SealedInput; ref != nil {
			if len(inputs) != 0 || ref.Tenant != tenant || ref.Call != x.ID || ref.Member != b.Member || ref.Definition != b.Call.Definition || ref.InputHash != b.Call.InputHash || ref.Size > op.Limits.DataInputBytes || op.Binding.ABI != platform.WasmDataABI {
				out.Detail = "The accepted input artifact belongs to another operation"
				return out
			}
			inputs, err = host.ReadInput(ctx, *ref)
			if err == nil {
				err = op.Input.Validate(inputs, op.Limits.DataInputBytes)
			}
			if err == nil {
				actual, digestErr := platform.CanonicalDigest(json.RawMessage(inputs))
				if digestErr != nil || actual != b.Call.InputHash {
					err = fmt.Errorf("the sealed input differs from its accepted input hash")
				}
			}
			if err != nil {
				out.Detail = err.Error()
				return out
			}
			authorised, refusal := host.stillAuthorised(ctx, x, b, now)
			if refusal != nil || !authorised {
				out.Detail = "Operation permission or generation changed while its input was read"
				return out
			}
		}
		if op.Rules != nil {
			why, refusal := host.rulesRevoked(ctx, member, *op.Rules)
			if refusal != nil {
				out.Detail = "Operation collection access could not be read"
				return out
			}
			if why != "" {
				out.Detail = why
				return out
			}
		}

		if op.Rules != nil {
			if b.RuleFacts == nil {
				err = fmt.Errorf("Rule facts are not bound")
			} else {
				output, err = op.Rules.DecideSchemas(*b.RuleFacts, b.RuleSchemas, host.Rules())
			}
		} else if op.Binding.Kind == "native" {
			executor, ok := host.App(x.App).(platform.OperationExecutor)
			if !ok {
				err = fmt.Errorf("Native operation executor is not available")
			} else {
				output, err = executor.Compute(ctx, op.Name, inputs)
			}
		} else {
			var module []byte
			module, err = host.ReadArtifact(ctx, op.Binding.Module)
			if err == nil {
				worker := EnvironmentWasmWorker()
				var result platform.WasmResponse
				request := platform.WasmRequest{ABI: op.Binding.ABI, Module: module, Digest: op.Binding.Module, Input: inputs, Limits: op.Limits}
				if op.Limits.DataInputBytes > 0 {
					sum := sha256.Sum256(inputs)
					request.Input = nil
					request.Data = &platform.WasmDataInput{Tenant: tenant, Call: x.ID, Digest: hex.EncodeToString(sum[:]), Bytes: inputs}
				}
				result, err = worker.Execute(ctx, request)
				output = result.Output
			}
		}
		if err == nil {
			answer, staged, stageErr := host.StageOutput(op, x.ID, output)
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

func OperationDefinition(app platform.App, name string, version int) (platform.Operation, int, bool) {
	if owner, ok := app.(platform.OperationDefinitions); ok {
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

func ResultOf(x platform.Effect) platform.OperationResult {
	r := platform.OperationResult{ID: x.ID, State: "pending", Error: x.Error, Generation: x.Generation, Millis: x.Millis}
	var binding OperationBinding
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

type CallSetup struct {
	Tenant      string
	Member      func(id string) (platform.Member, bool)
	App         func(id string) platform.App
	Prior       func(callID string) (string, bool)
	Quarantined func() bool
	Authorize   func(current platform.Member, op platform.Operation) *kernel.Error
}

type CallPlan struct {
	Member    platform.Member
	Operation platform.Operation
	Version   int
	ID        string
	Seal      bool
}

func (h CallSetup) Prepare(member platform.Member, q platform.OperationRequest) (CallPlan, *kernel.Error) {
	current, seated := h.Member(member.ID)
	app := h.App(q.App)
	var op platform.Operation
	var version int
	var declared bool
	if app != nil {
		op, version, declared = OperationDefinition(app, q.Name, q.Version)
	}
	callID := fmt.Sprintf("%s:%s:operation:%s:%s", h.Tenant, q.App, q.Key, operationEndpoint)
	if body, ok := h.Prior(callID); ok {
		if prior, priorVersion, sealed := PriorSealedDefinition(body); sealed {
			op, version, declared = prior, priorVersion, true
		}
	}
	seal := declared && op.Limits.DataInputBytes > 0
	if seal {
		if !seated || member.Tenant != h.Tenant || h.Quarantined() {
			return CallPlan{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This member cannot prepare the operation input")
		}
		if refusal := h.Authorize(current, op); refusal != nil {
			return CallPlan{}, refusal
		}
		var frozen platform.Operation
		_ = json.Unmarshal(platform.Raw(op), &frozen)
		op = frozen
	}
	return CallPlan{current, op, version, callID, seal}, nil
}
