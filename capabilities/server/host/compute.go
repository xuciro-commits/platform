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
			closure, bound, err := t.operationClosure(owner, op, version, release)
			return closure.ID, bound, err
		},
		InputKey: func(input platform.OperationInputArtifact) error { _, err := t.staged.InputKey(input); return err },
		Exists:   func(id string) bool { _, ok := t.outbound.Get(id); return ok },
		Retained: func(module string) bool { return t.files().Exists(context.Background(), artifactKey(t.ID, module)) },
		Bind: func(ref platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
			return t.bindApplications(ref, release, dependencies)
		}}.Plan(c, r, q, pending, input)
}
func (t *Tenant) operationResult(c platform.Caller, id string) (platform.OperationResult, *kernel.Error) {
	if found, ok := t.outbound.Get(id); ok && found.Endpoint == acceptedformat.OperationEndpoint {
		return compute.ReadableBy(t.ID, c, found.Effect, t.Member, func(member platform.Member, app string, b compute.OperationBinding) *kernel.Error {
			return authorizationRefusal(t.operationAuthorization(member, app, b, time.Now().UTC()))
		})
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

func artifactKey(tenant, digest string) string { return tenant + "/artifacts/wasm/" + digest }
func (t *Tenant) readArtifact(ctx context.Context, digest string) ([]byte, error) {
	r, n, err := t.files().Get(ctx, artifactKey(t.ID, digest))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return compute.ReadArtifact(r, n, digest)
}

func (t *Tenant) buildCode(ctx context.Context, q platform.CodeBuildRequest) (platform.CodeBuildResult, error) {
	return compute.BuildCode(ctx, q, func(ctx context.Context, digest string, module []byte) error {
		return t.files().Put(ctx, artifactKey(t.ID, digest), module, "application/wasm")
	})
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
