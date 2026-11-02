package work

import (
	"encoding/json"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const schemaRedirectApproval = ApprovalType + ".redirect"

func redirectAction() platform.Action {
	return platform.Action{Schema: schemaRedirectApproval, Target: ApprovalType, Capability: "approvals", Title: "Start redirected approval", Description: "Reauthorize the original requester and open the declared target-version approval", Automation: true, Payload: []platform.Field{}}
}

func (w *Work) runRedirects(c platform.Caller, now time.Time) *kernel.Error {
	rows, _, err := platform.Find[ApprovalRequest](c, platform.Query{Domain: platform.Raw([]any{[]any{"state", "=", "redirected"}, []any{"redirectTo", "=", ""}, []any{"redirectRelease", "=", w.host.ActiveRelease()}}), Sort: []string{"id"}, Limit: 100})
	if err != nil {
		return err
	}
	for _, a := range rows {
		if a.RedirectAction == "" || a.RedirectTo != "" || a.RedirectRelease != w.host.ActiveRelease() {
			continue
		}
		_, refusal := platform.Decide(c, w, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "redirect:" + a.ID + ":" + a.RedirectRelease, Target: &pb.EntityRef{Type: ApprovalType, Id: a.ID}, Schema: &pb.SchemaRef{Name: schemaRedirectApproval, Version: 1}, Payload: []byte("{}")}, now)
		if refusal != nil {
			return refusal
		}
	}
	return nil
}

func (w *Work) redirectApproval(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	a, found := platform.Get[ApprovalRequest](c, s.GetTarget().GetId())
	if !found || !c.Automation || c.ID != "app:"+ID || a.State != "redirected" || a.RedirectTo != "" || a.RedirectAction == "" || a.RedirectRelease != w.host.ActiveRelease() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval has no active declared redirect")
	}
	held := &pb.Submission{}
	if protojson.Unmarshal([]byte(a.Submission), held) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The original approval submission cannot be decoded")
	}
	held.Schema = &pb.SchemaRef{Name: a.RedirectAction, Version: 1}
	held.IdempotencyKey = "redirect:" + a.ID + ":" + a.RedirectRelease
	held.ExpectedRevision = nil
	requester, exists := w.host.Member(a.Requester)
	var refusal *kernel.Error
	if !exists || !requester.Active() {
		refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The original approval requester is no longer active")
	}
	caller := w.host.Caller(c, requester, a.App)
	if refusal == nil {
		refusal = platform.ProbeDecision(caller, func(probe platform.Caller) *kernel.Error { _, err := w.host.Attempt(probe, held, now); return err })
	}
	var open func(*pb.ChangeRecord)
	newID := a.App + "." + held.GetIdempotencyKey()
	if refusal == nil {
		raw, err := protojson.Marshal(held)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The target approval submission cannot be encoded")
		}
		request := &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "approval:" + newID, Target: &pb.EntityRef{Type: ApprovalType, Id: newID}, Schema: &pb.SchemaRef{Name: SchemaRequest, Version: 1}, Payload: platform.Raw(map[string]any{"requester": a.Requester, "submission": json.RawMessage(raw)})}
		open, refusal = w.request(c, request, now)
	}
	return func(r *pb.ChangeRecord) {
		if refusal != nil {
			a.State, a.Outcome = "refused", refusal.Error()
		} else {
			open(r)
			a.RedirectTo, a.Outcome = newID, "The declared target action has a new approval request"
		}
		if err := c.Put(r, a); err != nil {
			c.FailDecision(err)
		}
		c.Notify(platform.Notification{Title: "Approval redirect", Body: a.Outcome, Ref: ApprovalType + "/" + a.ID, Key: "redirect:" + a.ID}, now, platform.Recipient{Member: a.Requester})
	}, nil
}
