package platform

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type RuleReadSpec struct {
	PriorBalance bool                      `json:"priorBalance,omitempty"`
	Object       string                    `json:"object,omitempty"`
	Inputs       map[string]ExprType       `json:"inputs,omitempty"`
	Collections  map[string]RuleCollection `json:"collections,omitempty"`
}
type RuleCollection struct {
	Set     string `json:"set,omitempty"`
	App     string `json:"app"`
	Query   string `json:"query"`
	Version int    `json:"version"`
	Object  string `json:"object"`
	Of      string `json:"of,omitempty"`
	Policy  string `json:"policy"`
}

func RuleFieldKind(kind string) string {
	switch kind {
	case "attachment":
		return "json"
	case "longtext", "text", "string", "member":
		return "text"
	case "number":
		return "number"
	default:
		return kind
	}
}

func (c Caller) ReadRuleQuery(set RuleCollection, inputs json.RawMessage, now time.Time) (json.RawMessage, *Error) {
	if c.rt == nil {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Scoped rule collections require the host")
	}
	return c.rt.ReadRuleQuery(c, set, inputs, now)
}
