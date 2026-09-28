package platformserver

import (
	"encoding/json"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/work"
	"platformserver/platform"
)

func (d *stagedDecision) requestApproval(a platform.ResultApp, m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	// Permission/attribute/rule checks occur privately. Even a rule that uses
	// another app cannot leave anything in the actual request transaction.
	probe := d.tenant.newStagedDecision()
	probe.probing = true
	if _, err := platform.Decide(platform.NewCaller(probe, m, a.Manifest().ID, false, false), a, s, now); err != nil {
		return nil, err
	}
	id := a.Manifest().ID + "." + s.GetIdempotencyKey()
	held, err := protojson.Marshal(s)
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	payload, err := json.Marshal(map[string]any{"requester": m.ID, "submission": json.RawMessage(held)})
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	c := platform.NewCaller(d, platform.Member{ID: "app:" + work.ID, Tenant: d.tenant.ID}, work.ID, false, true)
	request := &pb.Submission{TenantId: d.tenant.ID, PrincipalId: c.ID, Authority: work.ID,
		IdempotencyKey: "approval:" + id, Target: &pb.EntityRef{Type: work.ApprovalType, Id: id},
		Schema: &pb.SchemaRef{Name: work.SchemaRequest, Version: 1}, Payload: payload}
	return platform.Decide(c, d.tenant.app(work.ID), request, now)
}
