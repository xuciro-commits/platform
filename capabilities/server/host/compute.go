package host

import records "platformserver/kernel/records"

import "platformserver/services/functions/runtime"

import (
	"context"
	"encoding/json"
	acceptedformat "platformserver/kernel/acceptedformat"
	controlowner "platformserver/services/security/control"

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
	var input *platform.OperationInputArtifact
	if len(sealed) == 1 {
		input = sealed[0]
	}
	return compute.OperationPlan{Tenant: t.ID, Member: t.Member, App: t.App, Recording: t.Recording(), Queued: t.queuedOperations(), Entity: t.Entity, Rules: t.services.Rules,
		Authorize: func(applicant platform.Caller, owner string, op platform.Operation, at time.Time, sources []string, definition string) *kernel.Error {
			return authorizationRefusal(t.authorizeCapability(applicant, owner, op.Name, op.Roles, op.Check() == nil, authorizationDetails{Store: store, At: at, Sources: sources, Definition: definition}))
		},
		Closure: func(owner string, op platform.Operation, version int, release *string) (string, string, error) {
			closure, bound, err := t.closures().Operation(owner, op, version, release)
			return closure.ID, bound, err
		},
		InputKey: func(input platform.OperationInputArtifact) error { _, err := t.staged.InputKey(input); return err },
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
func (t *Tenant) operationResult(c platform.Caller, id string) (platform.OperationResult, *kernel.Error) {
	return compute.ReadableBy(t.ID, c, id, t.operationEffect, t.Member, func(member platform.Member, app string, b compute.OperationBinding) *kernel.Error {
		return authorizationRefusal(t.operationAuthorization(member, app, b, time.Now().UTC()))
	})
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
	permission, allowed, declared := binding.Permission()
	c := platform.NewCaller(runtime{t: t, scoped: true}, member, owner, false, false)
	return t.authorizeCapability(c, owner, permission, allowed, declared, authorizationDetails{At: at, Sources: binding.Call.Sources, Definition: binding.Call.Definition})
}

func (d *stagedDecision) RequestCompilation(c platform.Caller, r *pb.ChangeRecord, key, target string, q platform.CodeBuildRequest) (platform.OperationCall, *kernel.Error) {
	call, intent, err := compute.PlanCompilation(d.tenant.ID, c, r, key, target, q, d.tenant.queuedOperations(), d.Intents)
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
		Authorised: func(ctx context.Context, x platform.Effect, b compute.OperationBinding, now time.Time) (platform.Member, *kernel.Error) {
			return recordRead(t, ctx, func(t *Tenant) (platform.Member, *kernel.Error) {
				member, ok := t.Member(b.Member)
				if !ok || member.Tenant != t.ID {
					return member, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation member is no longer authorised")
				}
				if !t.operationCurrent(x.ID, x.Generation) {
					return member, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation was cancelled")
				}
				return member, authorizationRefusal(t.operationAuthorization(member, x.App, b, now))
			})
		},
		StillAuthorised: func(ctx context.Context, x platform.Effect, b compute.OperationBinding, now time.Time) (bool, *kernel.Error) {
			return recordRead(t, ctx, func(t *Tenant) (bool, *kernel.Error) {
				latest, ok := t.Member(b.Member)
				return ok && t.operationAuthorization(latest, x.App, b, now).Allow && t.operationCurrent(x.ID, x.Generation), nil
			})
		},
		RulesRevoked: func(ctx context.Context, member platform.Member, rules platform.RuleTablePlan) (string, *kernel.Error) {
			return recordRead(t, ctx, func(t *Tenant) (string, *kernel.Error) {
				return compute.RulesRevoked(member, rules, func(ref platform.AssetRef, version int) (platform.NamedQuery, bool) {
					query, _, ok := t.memberQueryVersion(member, ref, version)
					return query, ok
				}, t.Entity, t.services.Authorization), nil
			})
		},
		BuildCode: func(ctx context.Context, q platform.CodeBuildRequest) (platform.CodeBuildResult, error) {
			return t.artifacts().Build(ctx, q)
		},
		ReadInput: func(ctx context.Context, ref platform.OperationInputArtifact) ([]byte, error) {
			return t.staged.ReadInput(ctx, ref)
		},
		StageOutput: func(op platform.Operation, call string, output []byte) (json.RawMessage, *platform.StagedResult, error) {
			return t.staged.Output(op, call, output)
		},
		App: t.App,
		ReadArtifact: func(ctx context.Context, digest string) ([]byte, error) {
			return t.artifacts().Read(ctx, digest)
		},
		Rules: func() platform.RuleEngine { return t.services.Rules },
	}, t.ID, x, b, now)
}

func (t *Tenant) queuedOperations() int {
	return len(t.outbound.Select(func(x actions.Queued) bool {
		return x.Endpoint == acceptedformat.OperationEndpoint && !platform.EffectSettled(x.State)
	}))
}
