package platform

import (
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// A decision owner may refuse requests. Caller must preserve its refusal
// instead of selecting another mutation route when a capability is absent.
type refusedOperationRuntime struct {
	Runtime
	refusal *kernel.Error
	seen    Caller
	keys    []string
	issues  []FieldIssue
}

func (r *refusedOperationRuntime) RequestOperation(Caller, *pb.ChangeRecord, OperationRequest) (OperationCall, *kernel.Error) {
	return OperationCall{}, r.refusal
}
func (r *refusedOperationRuntime) CancelOperation(Caller, *pb.ChangeRecord, string) *kernel.Error {
	return r.refusal
}
func (r *refusedOperationRuntime) RequestCompilation(Caller, *pb.ChangeRecord, string, string, CodeBuildRequest) (OperationCall, *kernel.Error) {
	return OperationCall{}, r.refusal
}
func (r *refusedOperationRuntime) RequestFunction(Caller, *pb.ChangeRecord, FunctionRequest) (FunctionCall, *kernel.Error) {
	return FunctionCall{}, r.refusal
}
func (r *refusedOperationRuntime) RequestApproval(Caller, string, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return nil, r.refusal
}
func (r *refusedOperationRuntime) ControlDecision(Caller, *pb.Submission, time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	return nil, r.refusal
}
func (r *refusedOperationRuntime) Seen(c Caller, keys ...string) { r.seen, r.keys = c, keys }
func (r *refusedOperationRuntime) InputRefused(_ *kernel.Error, issues []FieldIssue) {
	r.issues = issues
}

func TestCallerKeepsRuntimeRefusalsAndPrivateMutationOwner(t *testing.T) {
	owner := &refusedOperationRuntime{refusal: Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "owner refused")}
	caller := NewCaller(owner, Member{ID: "member", Tenant: "tenant"}, "application", false, false)
	_, operationErr := caller.RequestOperation(nil, OperationRequest{})
	_, compilationErr := caller.RequestCompilation(nil, "key", "target", CodeBuildRequest{})
	_, functionErr := caller.RequestFunction(nil, FunctionRequest{})
	_, approvalErr := caller.RequestApproval("other", nil, time.Time{})
	_, controlErr := ControlDecision(caller, nil, time.Time{})
	for _, refusal := range []*kernel.Error{operationErr, compilationErr, functionErr, approvalErr, controlErr, caller.CancelOperation(nil, "operation")} {
		if refusal != owner.refusal {
			t.Fatalf("Caller replaced its runtime refusal: %v", refusal)
		}
	}
	MarkSeen(caller, "notice")
	caller.RefuseFields([]FieldIssue{{Path: []string{"field"}}})
	if owner.seen.rt != owner || owner.seen.App != caller.App || owner.seen.ID != caller.ID || len(owner.keys) != 1 || owner.keys[0] != "notice" || len(owner.issues) != 1 || owner.issues[0].Path[0] != "field" {
		t.Fatal("notification or field diagnostic escaped its decision owner")
	}
}

func TestCallerWithoutHostRefusesAcceptedRequests(t *testing.T) {
	caller := As("application", Member{ID: "member", Tenant: "tenant"})
	_, operationErr := caller.RequestOperation(nil, OperationRequest{})
	_, compilationErr := caller.RequestCompilation(nil, "key", "target", CodeBuildRequest{})
	_, functionErr := caller.RequestFunction(nil, FunctionRequest{})
	_, approvalErr := caller.RequestApproval("other", nil, time.Time{})
	_, controlErr := ControlDecision(caller, nil, time.Time{})
	for _, refusal := range []*kernel.Error{operationErr, compilationErr, functionErr, approvalErr, controlErr, caller.CancelOperation(nil, "operation")} {
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
			t.Fatalf("unhosted Caller accepted a host-owned request: %v", refusal)
		}
	}
}
