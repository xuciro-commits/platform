// Package authz owns the ordered authorization decision. Owners resolve facts;
// neither a role nor an allow policy can widen row, field or provenance limits.
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
	Kind       string
	Subject    Subject
	Permission string   // e.g. "mes.order.release", "mes.order.read", "mes.order.cost.write"
	Resource   string   // "<type>/<id>" when about a record; "" otherwise
	Allowed    []string // the roles the permission names (AnyMember for every member)
	Attributes map[string]string
	Context    *Context
}

// Context contains resolved owner facts, not caller-supplied policy overrides.
// A nil context is the original role-only question used by static catalogues.
type Context struct {
	Tenant     Gate
	Principal  Gate
	Verb       *Gate
	Row        Gate
	Fields     Gate
	Derived    Gate
	Markings   Gate
	Withheld   []string
	Definition string
}

type Gate struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

type Layer struct {
	Name    string `json:"name"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
	Role    string `json:"role,omitempty"`
	Policy  string `json:"policy,omitempty"`
}

// Verdict is the answer, with the rule that decided it, for the UI, the
// audit trail and the explain endpoint.
type Verdict struct {
	Allow      bool     `json:"allow"`
	Rule       string   `json:"rule"`   // bypass, role, policy, none
	Reason     string   `json:"reason"` // one sentence a person can read
	Role       string   `json:"role,omitempty"`
	Policy     string   `json:"policy,omitempty"`
	Layers     []Layer  `json:"layers"`
	Withheld   []string `json:"withheld,omitempty"`
	Definition string   `json:"definition,omitempty"`
}

// Policy is a tenant rule over requests (ADR-0044 §3.4 ABAC): the first
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
		// Only the legacy replay decoder uses this branch. Modern recovery never
		// enters the engine; it applies the recorded authorization evidence.
		return Verdict{Allow: true, Rule: "bypass", Reason: "replaying an accepted decision", Layers: []Layer{{Name: "recorded", Allowed: true, Reason: "replaying an accepted decision"}}}
	}
	if r.Kind == "" {
		r.Kind = "action"
	}
	v := Verdict{Rule: "none", Layers: []Layer{}}
	check := func(name string, gate Gate, role, policy string) bool {
		v.Layers = append(v.Layers, Layer{Name: name, Allowed: gate.Allowed, Reason: gate.Reason, Role: role, Policy: policy})
		if !gate.Allowed {
			v.Reason = gate.Reason
		}
		return gate.Allowed
	}
	context := r.Context
	if context == nil {
		pass := Gate{Allowed: true, Reason: "not restricted by this owner"}
		context = &Context{Tenant: pass, Principal: pass, Row: pass, Fields: pass, Derived: pass, Markings: pass}
	}
	v.Definition = context.Definition
	v.Withheld = slices.Clone(context.Withheld)
	if !check("tenant", context.Tenant, "", "") || !check("principal", context.Principal, "", "") {
		return v
	}
	if !slices.Contains([]string{"read", "write", "create", "archive", "action", "derive", "export", "protocol"}, r.Kind) {
		check("deny", Gate{Reason: "Unknown authorization kind"}, "", "")
		return v
	}
	var allow *Policy
	for i := range policies {
		p := &policies[i]
		if p.Effect == "deny" && p.Matches(r) {
			v.Rule, v.Policy = "policy", p.ID
			check("deny", Gate{Reason: "An explicit policy denies this operation"}, "", p.ID)
			return v
		}
		if allow == nil && p.Effect == "allow" && p.Matches(r) {
			allow = p
		}
	}
	check("deny", Gate{Allowed: true, Reason: "no explicit deny matched"}, "", "")
	role := ""
	if slices.Contains(r.Allowed, AnyMember) {
		role = AnyMember
	}
	for _, held := range r.Subject.Roles {
		if role == "" && held != "" && slices.Contains(r.Allowed, held) {
			role = held
		}
	}
	// A matching allow may supply the verb grant, preserving ADR-0044. It
	// remains pending until every narrowing layer below has passed.
	verb := Gate{Allowed: role != "" || allow != nil || r.Subject.Automation, Reason: "No role or policy grants this operation"}
	if role != "" {
		verb.Reason = "An assigned role grants this operation"
	}
	if r.Subject.Automation {
		verb.Reason = "the host's declared operation"
	}
	if allow != nil && role == "" {
		verb.Reason = "allow policy supplies the operation grant"
	}
	if context.Verb != nil && !context.Verb.Allowed {
		verb = *context.Verb
	}
	if !check("verb", verb, role, "") {
		return v
	}
	for _, layer := range []struct {
		name string
		gate Gate
	}{{"row", context.Row}, {"fields", context.Fields}, {"derived", context.Derived}, {"markings", context.Markings}} {
		if !check(layer.name, layer.gate, "", "") {
			return v
		}
	}
	final := Gate{Allowed: true, Reason: verb.Reason}
	v.Rule, v.Role = "role", role
	if r.Subject.Automation {
		v.Rule = "automation"
	}
	if allow != nil && role == "" {
		v.Rule, v.Policy, final.Reason = "policy", allow.ID, "An allow policy grants this operation"
	}
	check("allow", final, "", v.Policy)
	v.Allow, v.Reason = true, final.Reason
	return v
}

// Widest is the widest of the scope levels several roles give (ADR-0044
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
