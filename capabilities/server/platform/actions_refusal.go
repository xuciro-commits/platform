package platform

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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

func (c Caller) RefuseBusinessCondition(code, message string, record any, inputs map[string]any, at time.Time) *kernel.Error {
	if owner, ok := c.rt.(BusinessConditionRuntime); ok && c.Staging() && !c.Replaying {
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

type BusinessInputChange struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

type BusinessRefusalEntry struct {
	Key          string                `json:"key"`
	App          string                `json:"app"`
	Type         string                `json:"type"`
	ID           string                `json:"id"`
	Action       string                `json:"action"`
	Member       string                `json:"member"`
	At           time.Time             `json:"at"`
	Category     string                `json:"category"`
	Code         string                `json:"code"`
	Message      string                `json:"message"`
	State        string                `json:"state"`
	Inputs       json.RawMessage       `json:"inputs,omitempty"`
	Issues       []FieldIssue          `json:"issues,omitempty"`
	Facts        []BusinessRefusalFact `json:"facts,omitempty"`
	CorrectionOf string                `json:"correctionOf,omitempty"`
	Changes      []BusinessInputChange `json:"changes,omitempty"`
}

type BusinessRefusalPage struct {
	Records []BusinessRefusalEntry `json:"records"`
	Total   int                    `json:"total"`
}

type RefusalCodeCount struct {
	Category string `json:"category"`
	Code     string `json:"code"`
	Type     string `json:"type"`
	Count    int    `json:"count"`
}

type Delivery struct {
	At         time.Time `json:"at"`
	App        string    `json:"app"`
	Action     string    `json:"action"`
	Target     string    `json:"target"`
	Subscriber string    `json:"subscriber"`
	Outcome    string    `json:"outcome"`
	Attempt    int       `json:"attempt,omitempty"`
}

type AuditEntry struct {
	At     time.Time `json:"at"`
	Member string    `json:"member"`
	App    string    `json:"app"`
	Action string    `json:"action"`
	Target string    `json:"target,omitempty"`
}

type PersonalRead struct {
	At     time.Time `json:"at"`
	Member string    `json:"member"`
	Type   string    `json:"type"`
	IDs    []string  `json:"ids"`
	Fields []string  `json:"fields"`
}

func EffectSettled(state string) bool {
	return state == "delivered" || state == "rejected" || state == "failed" || state == "discarded" || state == "compensated"
}
