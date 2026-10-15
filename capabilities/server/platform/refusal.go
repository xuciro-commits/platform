package platform

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Refusal is a business result, separate from authorization and invariants.
// Record is an ordinary transaction step; Route names an existing transaction.
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

// BusinessRefusalFact captures what the applicant asked and why the business
// rule chose another result. Its values are re-filtered by the host on reads.
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

// BusinessRefusalRuntime attaches facts to the existing accepted-result owner.
// A refused decision saves its fact with the answer; successful flags/routes
// save facts with the ordinary atomic batch. This never grants a direct write.
type BusinessRefusalRuntime interface {
	BusinessRefused(*kernel.Error, BusinessRefusalFact)
	CaptureBusinessRefusal(Caller, *pb.ChangeRecord, BusinessRefusalFact) *kernel.Error
}

func (c Caller) RefuseBusiness(fact BusinessRefusalFact) *kernel.Error {
	owner, ok := c.rt.(BusinessRefusalRuntime)
	if !ok || !c.Staging() || c.Replaying {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal facts need the accepted decision owner / 业务拒绝事实须经接受决策归属方")
	}
	answer := Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{message}", fact.Message)
	owner.BusinessRefused(answer, fact)
	return answer
}

func (c Caller) CaptureBusinessRefusal(receipt *pb.ChangeRecord, fact BusinessRefusalFact) *kernel.Error {
	owner, ok := c.rt.(BusinessRefusalRuntime)
	if !ok || !c.Staging() || c.Replaying {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal facts need the accepted decision owner / 业务拒绝事实须经接受决策归属方")
	}
	return owner.CaptureBusinessRefusal(c, receipt, fact)
}

type BusinessConditionRuntime interface {
	RefuseBusinessCondition(Caller, string, string, any, map[string]any, time.Time) *kernel.Error
}

// A false business precondition is separate from access checks made while
// reading the condition. It retains the same original evaluator and savepoint.
func (c Caller) RefuseBusinessCondition(code, message string, record any, inputs map[string]any, at time.Time) *kernel.Error {
	if owner, ok := c.rt.(BusinessConditionRuntime); ok && c.Staging() && !c.Replaying {
		return owner.RefuseBusinessCondition(c, code, message, record, inputs, at)
	}
	return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{message}", message)
}
