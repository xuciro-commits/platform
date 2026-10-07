// Package authz is the platform's one authorization engine (ADR-0078 §3.4):
// every check — an action submitted, a read, a field shown, a page offered,
// an agent acting for someone — asks Decide and gets a Verdict that says yes
// or no and why. Nothing else in the platform compares roles by hand.
//
// The order is fixed: a bypass the host grants (replay, automation), then
// what the roles allow (RBAC), then what a relation to the record allows
// (ReBAC: owner, participant, belongs-to), then the tenant's policies
// (ABAC, deny first). The engine knows no manifests and no records: callers
// hand it the roles the permission names and whether the relation holds, so
// it stays a few dozen lines any reader can verify.
package authz

import (
	"slices"
	"strings"
)

// AnyMember as an allowed role lets every member of the tenant through.
const AnyMember = "*"

// Subject is who asks: a member with the roles they hold in the app the
// permission belongs to, today.
type Subject struct {
	ID         string
	App        string
	Roles      []string
	Agent      bool
	Automation bool // the host's own work: rules, jobs, deliveries
	Replaying  bool // the journal replaying an accepted decision
}

// Request is one question: may Subject exercise Permission on Resource?
type Request struct {
	Subject    Subject
	Permission string   // e.g. "mes.order.release", "mes.order.read", "mes.order.cost.write"
	Resource   string   // "<type>/<id>" when about a record; "" otherwise
	Allowed    []string // the roles the permission names (AnyMember for every member)
	Related    bool     // the subject stands in a relation to the resource that reads it
	Attributes map[string]string
}

// Verdict is the answer, with the rule that decided it, for the UI, the
// audit trail and the explain endpoint.
type Verdict struct {
	Allow  bool   `json:"allow"`
	Rule   string `json:"rule"`   // bypass, role, relation, policy, none
	Reason string `json:"reason"` // one sentence a person can read
	Role   string `json:"role,omitempty"`
	Policy string `json:"policy,omitempty"`
}

// Policy is a tenant rule over requests (ADR-0078 §3.4 ABAC): the first
// matching deny wins over any allow; an allow lets through what roles do not.
type Policy struct {
	ID         string
	Effect     string // deny or allow
	Permission string // exact permission, or prefix ending in "*"
	When       func(Request) bool
}

// Matches reports whether the policy speaks to the request.
func (p Policy) Matches(r Request) bool {
	if strings.HasSuffix(p.Permission, "*") {
		if !strings.HasPrefix(r.Permission, strings.TrimSuffix(p.Permission, "*")) {
			return false
		}
	} else if p.Permission != "" && p.Permission != r.Permission {
		return false
	}
	return p.When == nil || p.When(r)
}

// Decide answers the request.
func Decide(r Request, policies ...Policy) Verdict {
	if r.Subject.Replaying {
		return Verdict{Allow: true, Rule: "bypass", Reason: "replaying an accepted decision"}
	}
	if r.Subject.Automation {
		return Verdict{Allow: true, Rule: "bypass", Reason: "the host's own work"}
	}
	for _, p := range policies {
		if p.Effect == "deny" && p.Matches(r) {
			return Verdict{Allow: false, Rule: "policy", Policy: p.ID, Reason: "policy " + p.ID + " denies " + r.Permission}
		}
	}
	if slices.Contains(r.Allowed, AnyMember) {
		return Verdict{Allow: true, Rule: "role", Role: AnyMember, Reason: "every member may " + r.Permission}
	}
	for _, role := range r.Subject.Roles {
		if role != "" && slices.Contains(r.Allowed, role) {
			return Verdict{Allow: true, Rule: "role", Role: role, Reason: "the role " + role + " in " + r.Subject.App + " may " + r.Permission}
		}
	}
	if r.Related {
		return Verdict{Allow: true, Rule: "relation", Reason: r.Subject.ID + " is related to " + r.Resource}
	}
	for _, p := range policies {
		if p.Effect == "allow" && p.Matches(r) {
			return Verdict{Allow: true, Rule: "policy", Policy: p.ID, Reason: "policy " + p.ID + " allows " + r.Permission}
		}
	}
	v := Verdict{Rule: "none"}
	switch {
	case len(r.Subject.Roles) == 0:
		v.Reason = r.Subject.ID + " holds no role in " + r.Subject.App + ", so may not " + r.Permission
	default:
		v.Reason = "the role " + strings.Join(r.Subject.Roles, ", ") + " in " + r.Subject.App + " may not " + r.Permission
	}
	return v
}

// Widest is the widest of the scope levels several roles give (ADR-0078
// §3.3: roles add up): tenant > below > unit > own > none.
func Widest(levels []string) string {
	order := []string{"tenant", "below", "unit", "own", "none"}
	best := len(order)
	for _, l := range levels {
		if i := slices.Index(order, l); i >= 0 && i < best {
			best = i
		}
	}
	if best == len(order) {
		return "none"
	}
	return order[best]
}
