package platformserver

import (
	"encoding/json"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

// ReadCapabilityAI projects the original Build call for native function users
// who do not hold an editor role. It never bypasses source or member checks.
func (t *Tenant) ReadCapabilityAI(m platform.Member, id string, now time.Time) (platform.OperationResult, *kernel.Error) {
	answer := platform.OperationResult{ID: id}
	if refusal := t.admits(m); refusal != nil {
		return answer, refusal
	}
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
		contract, _, exists = functionDefinition(t.app(owner), run.Function, run.Version)
		if !exists {
			return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Retained function definition is unavailable")
		}
	}
	if run.Member != m.ID && m.Roles[build.ID] != build.Builder || !slices.Contains(contract.Roles, m.Roles[owner]) {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Function call is unavailable to this member")
	}
	readable := t.mayRead(m, now, false)
	for _, source := range run.Sources {
		if !readable(source) {
			return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Function source permission was revoked")
		}
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
