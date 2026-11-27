package kernel

import (
	"platformserver/kernel/protocol"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type ApprovalRequests struct {
	Tenant  string
	Runtime platform.Runtime
	App     func(string) platform.Service
	Decide  func(platform.Caller, platform.Service, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error)
	Probe   func(c platform.Caller, decide func(platform.Caller) *platform.Error) *platform.Error
	Bind    func(protocol.ApprovalOwner)
}

func (h ApprovalRequests) Request(probeOnly bool, c platform.Caller, app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	a := h.App(app)
	if a == nil || c.Automation || c.Tenant != h.Tenant || s.GetAuthority() != app || s.GetPrincipalId() != c.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval needs its original authorised member")
	}
	if probeOnly {
		if declared, _ := a.Manifest().Actions.Action(s.GetSchema().GetName()); protocol.NeedsAsk(declared, c.Member, false) {
			c.Member.AskOK = true
		}
		return h.Decide(c, a, s, now)
	}
	return h.Submit(a, c.Member, s, now)
}

func (h ApprovalRequests) Submit(a platform.Service, m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	declared, _ := a.Manifest().Actions.Action(s.GetSchema().GetName())
	ask := protocol.NeedsAsk(declared, m, false)
	probeMember := m
	if ask {
		probeMember.AskOK = true
	}
	probeCaller := platform.NewCaller(h.Runtime, probeMember, a.Manifest().ID, false, false)
	probeSub := proto.Clone(s).(*pb.Submission)
	if err := h.Probe(probeCaller, func(probe platform.Caller) *platform.Error {
		_, err := platform.Decide(probe, a, probeSub, now)
		return err
	}); err != nil {
		return nil, err
	}
	owner := h.App(protocol.WorkApp)
	if owner == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The approval owner is unavailable")
	}
	work := owner.(protocol.ApprovalOwner)
	if h.Bind != nil {
		h.Bind(work)
	}
	c := platform.NewCaller(h.Runtime, m, a.Manifest().ID, false, false)
	if ask {
		return work.RequestQuestion(c, s, now)
	}
	return work.RequestApproval(c, s, now)
}
