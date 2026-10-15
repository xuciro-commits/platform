package platform

import (
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// RefusalEscalation asks the existing Work owner to open one review task when
// distinct decisions reach Count in a window starting at its first refusal.
// Repeating an idempotency key never contributes another refusal.
type RefusalEscalation struct {
	Count         int    `json:"count"`
	WindowSeconds int    `json:"windowSeconds"`
	ReviewerRole  string `json:"reviewerRole"`
}

func (e *RefusalEscalation) UnmarshalJSON(raw []byte) error {
	type plain RefusalEscalation
	return strictTxn(raw, (*plain)(e))
}

func (e RefusalEscalation) Check() error {
	if e.Count < 2 || e.Count > 100 || e.WindowSeconds < 1 || e.WindowSeconds > 604800 || e.ReviewerRole == "" || len(e.ReviewerRole) > 64 {
		return fmt.Errorf("Refusal escalation needs 2–100 distinct requests, a bounded window and a reviewer role")
	}
	return nil
}

// RefusalEscalationRequest is an immutable business result projected into a
// normal Work task. It contains no submitted inputs or original field values.
type RefusalEscalationRequest struct {
	Key        string    `json:"key"`
	App        string    `json:"app"`
	Subject    string    `json:"subject"`
	Action     string    `json:"action"`
	Code       string    `json:"code"`
	Message    string    `json:"message"`
	Count      int       `json:"count"`
	First      time.Time `json:"first"`
	Last       time.Time `json:"last"`
	Candidates []string  `json:"candidates"`
}

func (r RefusalEscalationRequest) TaskID() string { return r.App + ":refusal:" + r.Key }

type RefusalEscalationRuntime interface {
	PendingRefusalEscalations(Caller, time.Time) ([]RefusalEscalationRequest, *kernel.Error)
}

func PendingRefusalEscalations(c Caller, at time.Time) ([]RefusalEscalationRequest, *kernel.Error) {
	rt, ok := c.rt.(RefusalEscalationRuntime)
	if !ok || !c.Staging() || c.Replaying || !c.Automation || c.App != WorkApp || c.ID != "app:"+WorkApp {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Business refusal escalation requires the private Work owner")
	}
	return rt.PendingRefusalEscalations(c, at)
}
