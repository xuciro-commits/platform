package host

import (
	"platformserver/services/actions"

	"slices"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (d *stagedDecision) PendingRefusalEscalations(c platform.Caller, at time.Time) ([]platform.RefusalEscalationRequest, *kernel.Error) {
	if !c.Staging() || !c.Automation || c.App != platform.WorkApp || c.ID != "app:"+platform.WorkApp || c.Replaying || c.Tenant != d.tenant.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Business refusal escalation requires the private Work owner")
	}
	events, err := actions.BusinessRefusalEvents(d.tenant.Committed.AllRefusals("business"), d.tenant.Committed.AllAnswers("business"))
	if err != nil {
		return nil, err
	}
	return actions.RefusalEscalations(events, at, func(id string) bool {
		_, exists := platform.Get[platform.WorkTask](c, id)
		return exists
	}, func(request platform.RefusalEscalationRequest, role string) []string {
		caller := platform.NewCaller(d, platform.Member{ID: "app:" + request.App, Tenant: c.Tenant}, request.App, false, true)
		candidates := d.recipients(caller, at, []platform.Recipient{{AppRole: role}})
		return slices.DeleteFunc(candidates, func(id string) bool {
			member, known := (hostView{t: d.tenant, decision: d}).Member(id)
			return !known || !member.Active() || !d.Readable(platform.NewCaller(d, member, request.App, false, false), request.Subject)
		})
	})
}
