package work

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const (
	PolicyType         = "work.approval.policy"
	SchemaPolicySave   = PolicyType + ".save"
	SchemaPolicyRemove = PolicyType + ".remove"
)

type ApprovalPolicy struct {
	platform.Record
	Title     string `json:"title,omitempty" field:"search" title:"Why" help:"What the rule is for, for the people who read audits"`
	App       string `json:"app" field:"required,readonly" title:"App"`
	Schema    string `json:"schema" field:"required" title:"Action" help:"The action's schema, e.g. build.object.publish"`
	Approvals int    `json:"approvals" field:"required" title:"Approvals needed" help:"Distinct members who must approve, at least 1"`
}

func policyEntity() platform.Entity {
	return platform.Entity{Type: PolicyType, Title: "Approval policy", Plural: "Approval policies", Model: ApprovalPolicy{}, Display: "schema",
		Description: "How many distinct approvals an action needs in this tenant, on top of the action's own levels."}
}

func (w *Work) policyActions() []platform.Action {
	return []platform.Action{
		{Schema: SchemaPolicySave, Target: PolicyType, New: true, Capability: "approvals", Title: "Require approvals",
			Description: "Require several distinct approvals for one action.",
			Payload: []platform.Field{{Name: "app", Type: "string", Required: true, Description: "The app whose action it is"},
				{Name: "schema", Type: "string", Required: true, Description: "The action's schema"},
				{Name: "approvals", Type: "number", Required: true, Description: "Distinct members who must approve"},
				{Name: "title", Type: "string", Description: "Why the rule is there"}},
			Roles: []string{Admin}},
		{Schema: SchemaPolicyRemove, Target: PolicyType, Capability: "approvals", Title: "Drop approvals rule",
			Description: "Remove a required-approvals rule; the action's own levels stay.",
			Payload:     []platform.Field{}, Roles: []string{Admin}},
	}
}

func (w *Work) PolicyCount(c platform.Caller, app, schema string) int {
	policies, _, err := platform.Find[ApprovalPolicy](c, platform.Query{})
	if err != nil {
		return 0
	}
	count := 0
	for _, p := range policies {
		if p.Archived || p.App != app || p.Schema != schema {
			continue
		}
		if p.Approvals > count {
			count = p.Approvals
		}
	}
	return count
}

func CountFor(step ApprovalStep, all bool, policy int, approvers int) int {
	if all {
		return len(step.Approvers)
	}
	if policy < 1 {
		policy = 1
	}
	if policy > approvers {
		policy = approvers
	}
	return policy
}

func (w *Work) inputPolicy(_ platform.Caller, payload json.RawMessage, _ time.Time) *kernel.Error {
	var p struct {
		App, Schema, Title string
		Approvals          int `json:"approvals"`
	}
	if err := json.Unmarshal(payload, &p); err != nil || p.App == "" || p.Schema == "" || p.Approvals < 1 {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	return nil
}

func (w *Work) policy(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	if err := w.inputPolicy(c, s.GetPayload(), time.Now()); err != nil {
		return nil, err
	}
	id := s.GetTarget().GetId()
	existing, known := platform.Get[ApprovalPolicy](c, id)
	if s.GetSchema().GetName() == SchemaPolicyRemove {
		if !known || existing.Archived {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		existing.Archived = true
		return func(r *pb.ChangeRecord) { c.Put(r, existing) }, nil
	}
	var p struct {
		App, Schema, Title string
		Approvals          int `json:"approvals"`
	}
	if json.Unmarshal(s.GetPayload(), &p) != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	policy := ApprovalPolicy{App: p.App, Schema: p.Schema, Approvals: p.Approvals, Title: p.Title}
	policy.ID, policy.Archived = id, false
	if known {
		policy.Revision, policy.Created, policy.Changed = existing.Revision, existing.Created, existing.Changed
	}
	if err := c.Check(policy); err != nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) { c.Put(r, policy) }, nil
}
