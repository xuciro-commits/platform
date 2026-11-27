package actions

import (
	"encoding/json"
	"platformserver/kernel/records"
	"platformserver/platform"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

func reverseLedgerAction(c platform.Caller, e platform.RecordType, s *pb.Submission, existing any, now time.Time) (func(*pb.ChangeRecord), *platform.Error) {
	if !c.Staging() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Ledger reversal requires a private accepted decision")
	}
	var input struct {
		On string `json:"on"`
	}
	if len(s.GetPayload()) > 0 && json.Unmarshal(s.GetPayload(), &input) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid reversal date")
	}
	if input.On == "" {
		input.On = c.Today(now)
	}
	if e.ReverseCheck != nil && !c.Replaying && !c.SourceProjection() {
		if refusal := e.ReverseCheck(c, existing, input.On, now); refusal != nil {
			return nil, refusal
		}
	}
	probe, err := records.ReverseLedgerValue(e, existing, "_reverse_probe", input.On)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	if refusal := c.Check(probe); refusal != nil {
		return nil, refusal
	}
	return func(r *pb.ChangeRecord) {
		revID := "reverse-" + r.GetChangeId()
		if s.GetTarget().GetType() != r.GetSubmission().GetTarget().GetType() || s.GetTarget().GetId() != r.GetSubmission().GetTarget().GetId() {
			revID += "-" + s.GetTarget().GetId()
		}
		fact, err := records.ReverseLedgerValue(e, existing, revID, input.On)
		if err != nil {
			c.FailDecision(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error()))
			return
		}
		if refusal := c.Check(fact); refusal != nil {
			c.FailDecision(refusal)
			return
		}
		if refusal := c.Put(r, fact); refusal != nil {
			c.FailDecision(refusal)
			return
		}
		if e.ReverseFollow != nil && !c.Replaying && !c.SourceProjection() {
			if refusal := e.ReverseFollow(c, r, existing, input.On, now); refusal != nil {
				c.FailDecision(refusal)
				return
			}
		}
	}, nil
}
