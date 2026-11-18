package platform

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"time"
)

type Refusal struct {
	When         *Expr              `json:"when"`
	Level        string             `json:"level"`
	Code         string             `json:"code"`
	Message      string             `json:"message"`
	Translations map[string]string  `json:"translations,omitempty"`
	Route        *TxnRef            `json:"route,omitempty"`
	Record       *TxnStep           `json:"record,omitempty"`
	Escalation   *RefusalEscalation `json:"escalation,omitempty"`
}

func (r *Refusal) UnmarshalJSON(raw []byte) error {
	type plain Refusal
	return strictTxn(raw, (*plain)(r))
}

type BusinessRefusalFact struct {
	Level        string                     `json:"level"`
	Code         string                     `json:"code"`
	Message      string                     `json:"message"`
	Translations map[string]string          `json:"translations,omitempty"`
	Step         string                     `json:"step,omitempty"`
	Subject      string                     `json:"subject"`
	Action       string                     `json:"action"`
	Member       string                     `json:"member"`
	At           time.Time                  `json:"at"`
	Original     json.RawMessage            `json:"original"`
	Inputs       map[string]json.RawMessage `json:"inputs,omitempty"`
	Record       string                     `json:"record,omitempty"`
	Route        *TxnRef                    `json:"route,omitempty"`
	Escalation   *RefusalEscalation         `json:"escalation,omitempty"`
}

func (c Caller) RefuseBusiness(fact BusinessRefusalFact) *Error {
	owner := c.StagedDecision()
	if owner == nil || c.Replaying {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal facts need the accepted decision owner / 业务拒绝事实须经接受决策归属方")
	}
	answer := Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{message}", fact.Message)
	owner.BusinessRefused(answer, fact)
	return answer
}

func (c Caller) CaptureBusinessRefusal(receipt *pb.ChangeRecord, fact BusinessRefusalFact) *Error {
	owner := c.StagedDecision()
	if owner == nil || c.Replaying {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal facts need the accepted decision owner / 业务拒绝事实须经接受决策归属方")
	}
	return owner.CaptureBusinessRefusal(c, receipt, fact)
}

func (c Caller) RefuseBusinessCondition(code, message string, record any, inputs map[string]any, at time.Time) *Error {
	if owner := c.StagedDecision(); owner != nil && !c.Replaying {
		return owner.RefuseBusinessCondition(c, code, message, record, inputs, at)
	}
	return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{message}", message)
}

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

type RefusalCodeCount struct {
	Category string `json:"category"`
	Code     string `json:"code"`
	Type     string `json:"type"`
	Count    int    `json:"count"`
}
