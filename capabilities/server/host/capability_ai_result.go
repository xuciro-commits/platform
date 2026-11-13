package host

import (
	"context"
	agentowner "platformserver/services/ai/runtime"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	functions "platformserver/services/functions/authoring"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) ReadCapabilityAI(ctx context.Context, m platform.Member, id string, now time.Time) (platform.OperationResult, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (platform.OperationResult, *kernel.Error) {
		return t.readCapabilityAI(m, id, now)
	})
}

func (t *Tenant) readCapabilityAI(m platform.Member, id string, now time.Time) (platform.OperationResult, *kernel.Error) {
	answer := platform.OperationResult{ID: id}
	if refusal := t.Admits(m); refusal != nil {
		return answer, refusal
	}
	m = t.authorizationMember(m)
	run, ok := platform.Get[build.FunctionRun](t.automation(build.ID, false), id)
	if !ok {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Function call is unavailable")
	}
	owner := run.App
	if owner == "" {
		owner = build.ID
	}
	contract := run.Contract
	if len(contract.Roles) == 0 {
		var exists bool
		contract, _, exists = agentowner.FunctionDefinition(t.App(owner), run.Function, run.Version)
		if !exists {
			return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Retained function definition is unavailable")
		}
	}
	if run.Member != m.ID && !m.Holds(build.ID, build.Builder) {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Function call is unavailable to this member")
	}
	caller := platform.NewCaller(runtime{t: t, scoped: true}, m, owner, false, false)
	if refusal := authorizationRefusal(t.authorizeCapability(caller, owner, contract.Name, contract.Roles, true, authorizationDetails{At: now, Sources: run.Sources})); refusal != nil {
		return answer, refusal
	}
	return functions.AIAnswer(id, run.State, run.Output, run.Reason), nil
}
