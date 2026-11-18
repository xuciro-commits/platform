package protocol

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"time"
)

func PendingRefusalEscalations(c platform.Caller, at time.Time) ([]platform.RefusalEscalationRequest, *platform.Error) {
	rt := c.StagedDecision()
	if rt == nil || c.Replaying || !c.Automation || c.App != WorkApp || c.ID != "app:"+WorkApp {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Business refusal escalation requires the private Work owner")
	}
	return rt.PendingRefusalEscalations(c, at)
}

type BusinessInputChange struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

type BusinessRefusalEntry struct {
	Key          string                         `json:"key"`
	App          string                         `json:"app"`
	Type         string                         `json:"type"`
	ID           string                         `json:"id"`
	Action       string                         `json:"action"`
	Member       string                         `json:"member"`
	At           time.Time                      `json:"at"`
	Category     string                         `json:"category"`
	Code         string                         `json:"code"`
	Message      string                         `json:"message"`
	State        string                         `json:"state"`
	Inputs       json.RawMessage                `json:"inputs,omitempty"`
	Issues       []platform.FieldIssue          `json:"issues,omitempty"`
	Facts        []platform.BusinessRefusalFact `json:"facts,omitempty"`
	CorrectionOf string                         `json:"correctionOf,omitempty"`
	Changes      []BusinessInputChange          `json:"changes,omitempty"`
}

type BusinessRefusalPage struct {
	Records []BusinessRefusalEntry `json:"records"`
	Total   int                    `json:"total"`
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
	ID     string    `json:"id,omitempty"`
	At     time.Time `json:"at"`
	Member string    `json:"member"`
	App    string    `json:"app"`
	Action string    `json:"action"`
	Target string    `json:"target,omitempty"`
}

type PersonalRead struct {
	ID     string    `json:"id,omitempty"`
	At     time.Time `json:"at"`
	Member string    `json:"member"`
	Type   string    `json:"type"`
	IDs    []string  `json:"ids"`
	Fields []string  `json:"fields"`
}

func EffectSettled(state string) bool {
	return state == "delivered" || state == "rejected" || state == "failed" || state == "discarded" || state == "compensated"
}
