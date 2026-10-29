package platformserver

import (
	"context"
	"encoding/json"
	agentowner "platformserver/engine/agents"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
)

// ReadCapabilityAI projects the original Build call for native function users
// who do not hold an editor role. It never bypasses source or member checks.
func (t *Tenant) ReadCapabilityAI(m platform.Member, id string, now time.Time) (platform.OperationResult, *kernel.Error) {
	return recordRead(t, context.Background(), func() (platform.OperationResult, *kernel.Error) {
		return t.readCapabilityAI(m, id, now)
	})
}

func (t *Tenant) readCapabilityAI(m platform.Member, id string, now time.Time) (platform.OperationResult, *kernel.Error) {
	answer := platform.OperationResult{ID: id}
	if refusal := t.admits(m); refusal != nil {
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
		contract, _, exists = agentowner.FunctionDefinition(t.app(owner), run.Function, run.Version)
		if !exists {
			return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Retained function definition is unavailable")
		}
	}
	if run.Member != m.ID && !m.Holds(build.ID, build.Builder) {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Function call is unavailable to this member")
	}
	caller := platform.NewCaller(runtime{t}, m, owner, false, false)
	if refusal := authorizationRefusal(t.authorizeCapability(caller, owner, contract.Name, contract.Roles, true, authorizationDetails{At: now, Sources: run.Sources})); refusal != nil {
		return answer, refusal
	}
	answer.State = "pending"
	if run.State == "ready" {
		answer.State, answer.Output = "completed", json.RawMessage(run.Output)
	}
	if run.State == "rejected" {
		answer.State, answer.Error = "failed", run.Reason
	}
	return answer, nil
}
