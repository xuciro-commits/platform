package authoring

import (
	"fmt"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func processSubject(object string, run *platform.Run) string {
	if id, ok := strings.CutPrefix(run.Subject, object+"/"); ok {
		return id
	}
	return run.Key
}

func (o *Owner) automationOwnerRefusal(c platform.Caller, id string, p Process, now time.Time) *kernel.Error {
	if p.Kind != "automation" && p.Scheduler == "" {
		return nil
	}
	member, ok := o.env.Host().Member(p.Scheduler)
	if !ok {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The automation owner is no longer available")
	}
	owner := o.env.Host().Caller(c, member, o.env.AppID())
	decision := owner.Authorize(platform.AuthzRequest{Principal: owner.ID, Tenant: owner.Tenant, Kind: "action", Type: ProcessType, Record: id, Action: SchemaProcessRun}, now)
	if !decision.Allow {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The automation owner is no longer authorized")
	}
	return nil
}

func (o *Owner) pauseAutomation(c platform.Caller, id, reason string, now time.Time) {
	if c.Replaying {
		return
	}
	c = o.env.Host().Automation(c, o.env.AppID())
	current, ok := platform.Get[Process](c, id)
	if !ok || current.Paused {
		return
	}
	_, refusal := platform.Decide(c, o.env.Application(), &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: o.env.AppID(),
		Schema: &pb.SchemaRef{Name: SchemaProcessPause, Version: 1}, Target: &pb.EntityRef{Type: ProcessType, Id: id},
		IdempotencyKey: fmt.Sprintf("automation-pause:%s:%d", id, current.Revision), Payload: platform.Raw(map[string]string{"reason": reason})}, now)
	if refusal != nil {
		c.FailDecision(refusal)
	}
}

func (o *Owner) Run(c platform.Caller, now time.Time) *kernel.Error {
	if c.Replaying {
		return nil
	}
	processes, err := o.Inventory()
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	for _, live := range processes {
		p, ok := readPublished[Process](live.Published)
		if !ok || live.Archived || live.Paused || p.Scheduler == "" {
			continue
		}
		if denied := o.automationOwnerRefusal(c, live.ID, p, now); denied != nil {
			o.pauseAutomation(c, live.ID, denied.Message, now)
		}
	}
	return nil
}

func (o *Owner) Handles(s *pb.Submission) bool {
	return s.GetTarget().GetType() == ProcessType || strings.HasPrefix(s.GetSchema().GetName(), ProcessType+".")
}

func (o *Owner) Decide(ledger *platform.Ledger, c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error, bool) {
	if !o.Handles(s) {
		return nil, nil, false
	}
	if s.GetSchema().GetName() == ProcessType+".archive" && !c.Replaying {
		if !c.Holds(c.App, Builder) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}, true
		}
		if record, _ := platform.Get[Process](c, s.GetTarget().GetId()); record.Published != "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published definition cannot be archived; its installed version must be retained"), true
		}
	}
	record, err, ok := ledger.Generated(c, s, now, nil, o.Entities()...)
	return record, err, ok || o.Handles(s)
}
