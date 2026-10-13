package platform

import (
	"platformserver/platform/authz"
	"time"
)

type AuthzRequest struct {
	Principal string   `json:"principal"`
	Tenant    string   `json:"tenant"`
	Kind      string   `json:"kind" enum:"read,write,create,archive,action,derive,export,protocol"`
	Type      string   `json:"type,omitempty"`
	Record    string   `json:"record,omitempty"`
	Fields    []string `json:"fields,omitempty"`
	Action    string   `json:"action,omitempty"`
	From      string   `json:"from,omitempty"`
}

type AuthzDecision = authz.Verdict
type AuthzLayer = authz.Layer

// Filtered explains a narrowing result without naming or counting hidden rows.
type AuthzCollection struct {
	Decision AuthzDecision  `json:"decision"`
	Elements []AuthzElement `json:"elements"`
	Filtered *AuthzDecision `json:"filtered,omitempty"`
}

type AuthzElement struct {
	Record   string        `json:"record"`
	Decision AuthzDecision `json:"decision"`
}

type AuthorizationRuntime interface {
	Authorize(Caller, AuthzRequest, time.Time) AuthzDecision
}

func (c Caller) Authorize(request AuthzRequest, at time.Time) AuthzDecision {
	if r, ok := c.rt.(AuthorizationRuntime); ok {
		return r.Authorize(c, request, at)
	}
	return AuthzDecision{Rule: "none", Reason: "Authorization requires the owner runtime", Layers: []AuthzLayer{{Name: "principal", Reason: "Authorization requires the owner runtime"}}}
}
