package platformserver

import (
	"encoding/json"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const OperationType = "platform.operation"
const SchemaOperationCall = OperationType + ".call"

func operationActions() []platform.Action {
	return []platform.Action{{Schema: SchemaOperationCall, Target: OperationType, Capability: "compute", Title: "Call operation", Description: "Invoke a registered typed operation through its owner and accepted work.", Roles: []string{platform.AnyMember}, Payload: []platform.Field{{Name: "app", Type: "string", Required: true, Description: "The operation owner"}, {Name: "name", Type: "string", Required: true, Description: "The registered operation name"}, {Name: "version", Type: "integer", Description: "The retained owner version"}, {Name: "key", Type: "string", Required: true, Description: "The stable invocation key"}, {Name: "inputs", Type: "json", Required: true, Description: "The typed operation input"}, {Name: "sources", Type: "string[]", Description: "Protected source references"}}}}
}
func (d *Console) decideOperation(c platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var q platform.OperationRequest
	if json.Unmarshal(s.GetPayload(), &q) != nil || q.Key != s.GetIdempotencyKey() || s.GetTarget().GetId() != q.Key || q.App == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an operation and stable input key")
	}
	return func(r *pb.ChangeRecord) { _, _ = c.RequestOperation(r, q) }, nil
}

// InvokeOperation is the public compute route. The console contributes only an
// invocation receipt; the selected operation owner decides permission/schema.
func (t *Tenant) InvokeOperation(member platform.Member, q platform.OperationRequest, now time.Time) (platform.OperationCall, *kernel.Error) {
	if q.App == "" || q.Key == "" || q.Target != "" || q.OnBehalf != "" || q.Release != nil {
		return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A direct call requires an owner and stable key")
	}
	payload, err := json.Marshal(q)
	if err != nil {
		return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Operation input cannot be encoded")
	}
	s := &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: PlatformApp, IdempotencyKey: q.Key, Target: &pb.EntityRef{Type: OperationType, Id: q.Key}, Schema: &pb.SchemaRef{Name: SchemaOperationCall, Version: 1}, Payload: payload}
	if _, err := t.Submit(member, s, now); err != nil {
		return platform.OperationCall{}, err
	}
	id := fmt.Sprintf("%s:%s:operation:%s:%s", t.ID, q.App, q.Key, operationEndpoint)
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, x := range t.outbound {
		if x.ID == id {
			var binding operationBinding
			if json.Unmarshal([]byte(x.Body), &binding) == nil {
				return binding.Call, nil
			}
		}
	}
	return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Accepted operation intent is unavailable")
}
func (t *Tenant) ReadOperation(member platform.Member, id string) (platform.OperationResult, *kernel.Error) {
	return t.operationResult(platform.NewCaller(runtime{t}, member, PlatformApp, false, false), id)
}
