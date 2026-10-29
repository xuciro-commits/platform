package platformserver

import (
	"platformserver/engine/business"

	"encoding/json"
	acceptedformat "platformserver/kernel/acceptedformat"

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
	var events []business.RefusalEvent
	for key, r := range d.tenant.committed.AllRefusals("business") {
		if r.Business != nil {
			events = append(events, business.RefusalEvent{Key: key, Fact: *r.Business})
		}
	}
	for key, raw := range d.tenant.committed.AllAnswers("business") {
		var envelope struct{ Kind string }
		if json.Unmarshal(raw, &envelope) != nil || envelope.Kind != "record-batch" {
			continue
		}
		r, _, err := acceptedformat.DecodeBatch(raw)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The accepted refusal history cannot be read")
		}
		for _, fact := range r.Business {
			events = append(events, business.RefusalEvent{Key: key, Fact: fact})
		}
	}
	return business.RefusalEscalations(events, at, func(id string) bool {
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
