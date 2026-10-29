package authz

import (
	"slices"
	"strings"
)

const AnyMember = "*"

type Engine interface {
	Decide(Request, ...Policy) Verdict
}

type Standard struct{}

func (Standard) Decide(r Request, policies ...Policy) Verdict { return Decide(r, policies...) }

type Subject struct {
	ID         string
	App        string
	Roles      []string
	Agent      bool
	Automation bool
	Replaying  bool
}

type Request struct {
	Kind       string
	Subject    Subject
	Permission string
	Resource   string
	Allowed    []string
	Attributes map[string]string
	Context    *Context
}

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

type Verdict struct {
	Allow      bool     `json:"allow"`
	Rule       string   `json:"rule"`
	Reason     string   `json:"reason"`
	Role       string   `json:"role,omitempty"`
	Policy     string   `json:"policy,omitempty"`
	Layers     []Layer  `json:"layers"`
	Withheld   []string `json:"withheld,omitempty"`
	Definition string   `json:"definition,omitempty"`
}

type Policy struct {
	ID         string
	Effect     string
	Permission string
	When       func(Request) bool
}

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

func Decide(r Request, policies ...Policy) Verdict {
	if r.Subject.Replaying {
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
