package host

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/protocol"
	records "platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/actions"
	automation "platformserver/services/automate/runtime"
	compute "platformserver/services/functions/runtime"
	controlowner "platformserver/services/security/control"
	"time"
)

func intend[C any](d *stagedDecision, call C, intent platform.Effect, err *platform.Error) (C, *platform.Error) {
	if err != nil {
		if d.Failure == nil {
			d.Failure = err
		}
		var none C
		return none, err
	}
	if !d.ProbeOnly {
		d.Intents = append(d.Intents, intent)
	}
	return call, nil
}

func (d *stagedDecision) RequestOperation(c platform.Caller, r *pb.ChangeRecord, q platform.OperationRequest) (platform.OperationCall, *platform.Error) {
	input, refusal := automation.TakeOperationInput(d.operationInputs, c, q, controlowner.PlatformApp)
	if refusal != nil {
		d.Failure = refusal
		return platform.OperationCall{}, refusal
	}
	call, intent, err := d.tenant.planOperation(c, r, q, d.Records, d.Intents, input)
	return intend(d, call, intent, err)
}
func (t *Tenant) planOperation(c platform.Caller, r *pb.ChangeRecord, q platform.OperationRequest, store *records.Store, pending []platform.Effect, sealed ...*protocol.OperationInputArtifact) (platform.OperationCall, platform.Effect, *platform.Error) {
	var input *protocol.OperationInputArtifact
	if len(sealed) == 1 {
		input = sealed[0]
	}
	return compute.OperationPlan{Tenant: t.ID, Member: t.Member, App: t.App, Definition: t.operationDefinition, Recording: t.Recording(), Queued: t.queuedOperations(), Entity: t.Entity, Rules: t.services.Rules,
		Authorize: func(applicant platform.Caller, owner string, op platform.Operation, at time.Time, sources []string, definition string) *platform.Error {
			return authorizationRefusal(t.authorizeCapability(applicant, owner, op.Name, op.Roles, op.Check() == nil, authorizationDetails{Store: store, At: at, Sources: sources, Definition: definition}))
		},
		Closure: func(owner string, op platform.Operation, version int, release *string) (string, string, error) {
			closure, bound, err := t.closures().Operation(owner, op, version, release)
			return closure.ID, bound, err
		},
		InputKey: func(input protocol.OperationInputArtifact) error { _, err := t.staged.InputKey(input); return err },
		Exists:   func(id string) bool { _, ok := t.outbound.Get(id); return ok },
		Retained: func(module string) bool { return t.artifacts().Exists(context.Background(), module) },
		Bind: func(ref platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
			return t.bindApplications(ref, release, dependencies)
		}}.Plan(c, r, q, pending, input)
}
func (t *Tenant) operationEffect(id string) (platform.Effect, bool) {
	queued, ok := t.outbound.Get(id)
	return queued.Effect, ok
}
func (t *Tenant) operationResult(c platform.Caller, id string) (platform.OperationResult, *platform.Error) {
	return compute.ReadableBy(t.ID, c, id, t.operationEffect, t.Member, func(member platform.Member, app string, b compute.OperationBinding) *platform.Error {
		return authorizationRefusal(t.operationAuthorization(member, app, b, time.Now().UTC()))
	})
}
func (r runtime) OperationResult(c platform.Caller, id string) (platform.OperationResult, *platform.Error) {
	return r.t.operationResult(c, id)
}
func (d *stagedDecision) OperationResult(c platform.Caller, id string) (platform.OperationResult, *platform.Error) {
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
	permission, allowed, declared := binding.Permission()
	c := platform.NewCaller(runtime{t: t, scoped: true}, member, owner, false, false)
	return t.authorizeCapability(c, owner, permission, allowed, declared, authorizationDetails{At: at, Sources: binding.Call.Sources, Definition: binding.Call.Definition})
}

func (d *stagedDecision) RequestCompilation(c platform.Caller, r *pb.ChangeRecord, key, target string, q platform.CodeBuildRequest) (platform.OperationCall, *platform.Error) {
	call, intent, err := compute.PlanCompilation(d.tenant.ID, c, r, key, target, q, d.tenant.queuedOperations(), d.Intents)
	return intend(d, call, intent, err)
}

func (t *Tenant) artifacts() compute.Artifacts {
	return compute.Artifacts{Tenant: t.ID, Files: t.files()}
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
	return compute.Execute(ctx, compute.ExecutionHost{
		Read: func(ctx context.Context, read func(compute.ExecutionReads)) *platform.Error {
			_, refusal := recordRead(t, ctx, func(t *Tenant) (struct{}, *platform.Error) {
				read(compute.ExecutionReads{Member: t.Member, Current: t.operationClaims().Current, Query: func(m platform.Member, ref platform.AssetRef, version int) (platform.NamedQuery, bool) {
					query, _, ok := t.memberQueryVersion(m, ref, version)
					return query, ok
				}, Entity: t.Entity, Authz: t.services.Authorization,
					Refusal: func(member platform.Member, app string, b compute.OperationBinding, now time.Time) *platform.Error {
						return authorizationRefusal(t.operationAuthorization(member, app, b, now))
					}})
				return struct{}{}, nil
			})
			return refusal
		},
		BuildCode: func(ctx context.Context, q platform.CodeBuildRequest) (protocol.CodeBuildResult, error) {
			return t.artifacts().Build(ctx, q)
		},
		ReadInput: func(ctx context.Context, ref protocol.OperationInputArtifact) ([]byte, error) {
			return t.staged.ReadInput(ctx, ref)
		},
		StageOutput: func(op platform.Operation, call string, output []byte) (json.RawMessage, *platform.StagedResult, error) {
			return t.staged.Output(op, call, output)
		},
		Executor: func(id string) protocol.OperationExecutor { return t.operations[id] },
		ReadArtifact: func(ctx context.Context, digest string) ([]byte, error) {
			return t.artifacts().Read(ctx, digest)
		},
		Rules: func() platform.RuleEngine { return t.services.Rules },
	}, t.ID, x, b, now)
}

func (t *Tenant) queuedOperations() int {
	return len(t.outbound.Select(func(x actions.Queued) bool {
		return x.Endpoint == acceptedformat.OperationEndpoint && !protocol.EffectSettled(x.State)
	}))
}

type preparedOperationInput = automation.OperationInput

func (h hostView) operationInstall() compute.OperationInstall {
	return compute.OperationInstall{App: h.app, Executor: h.t.operations[h.app.Manifest().ID], Rules: h.t.services.Rules, RuleQuery: h.RuleQuery, Entity: h.t.Entity,
		Retained: func(module string) bool { return h.t.artifacts().Exists(context.Background(), module) },
		Describe: func(def platform.Definition) (platform.Definition, error) {
			return h.t.describer().DescribeDefinition(def)
		},
		Definitions: func() []platform.Definition { return h.t.definitions },
		Store:       h.t.storeDefinition}
}
func (h hostView) ValidateInstallOperation(c platform.Caller, o platform.Operation) error {
	return h.operationInstall().Validate(c, o)
}
func (h hostView) InstallOperation(c platform.Caller, o platform.Operation, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	return h.operationInstall().Install(c, o, version)
}

func readCapabilityBody(w http.ResponseWriter, r *http.Request, value any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, (128<<10)+1))
	if err != nil || len(raw) > 128<<10 {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return false
	}
	if _, err = platform.DecodeValue(raw, 128<<10); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": err.Error()}})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil || decoder.Decode(new(any)) != io.EOF {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": "Use the declared request fields"}})
		return false
	}
	return true
}
