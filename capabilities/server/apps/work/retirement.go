package work

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const schemaRetireApproval = ApprovalType + ".retire"

func retirementAction() platform.Action {
	return platform.Action{Schema: schemaRetireApproval, Target: ApprovalType, Capability: "approvals", Title: "Retire pending approval", Description: "Reject a held action retired by a reviewed migration and close its tasks", Automation: true, Payload: []platform.Field{{Name: "migration", Type: "string", Required: true}}}
}

func (w *Work) RetireApproval(parent platform.Caller, migration, id string, at time.Time) *kernel.Error {
	c := w.host.Automation(parent, ID)
	_, err := platform.Decide(c, w, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID,
		IdempotencyKey: "retire:" + migration + ":" + id,
		Target:         &pb.EntityRef{Type: ApprovalType, Id: id}, Schema: &pb.SchemaRef{Name: schemaRetireApproval, Version: 1},
		Payload: platform.Raw(map[string]string{"migration": migration})}, at)
	return err
}

func (w *Work) retireApproval(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var payload struct {
		Migration string `json:"migration"`
	}
	if json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Migration == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A reviewed migration is required")
	}
	actor, refusal := c.CheckApprovalRetirement(payload.Migration, s.GetTarget().GetId())
	if refusal != nil {
		return nil, refusal
	}
	a, found := platform.Get[ApprovalRequest](c, s.GetTarget().GetId())
	if !found || a.State != "pending" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The approval is no longer pending")
	}
	return func(r *pb.ChangeRecord) {
		a.State, a.RejectedBy = "rejected", actor
		a.Outcome = "The held action was retired; start a new request under the current definition"
		w.close(c, r, a, a.Level, "canceled")
		w.move(c, a, platform.ApprovalRejected, at)
		if err := c.Put(r, a); err != nil {
			c.FailDecision(err)
			return
		}
		c.Notify(platform.Notification{Title: "Approval retired", Body: a.Outcome, Ref: ApprovalType + "/" + a.ID, Key: "request:" + a.ID}, at, platform.Recipient{Member: a.Requester})
	}, nil
}
