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
	probe, err := records.ReverseLedgerValue(e, existing, "_reverse_probe", input.On)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	if refusal := c.Check(probe); refusal != nil {
		return nil, refusal
	}
	return func(r *pb.ChangeRecord) {
		fact, err := records.ReverseLedgerValue(e, existing, "reverse-"+r.GetChangeId(), input.On)
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
		}
	}, nil
}
