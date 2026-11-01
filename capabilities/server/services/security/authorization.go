package security

import (
	"platformserver/platform"
	"slices"
	"strings"
)

type Standard struct{}

func (Standard) Decide(r platform.AuthorizationInput, policies ...platform.AuthorizationPolicy) platform.AuthzDecision {
	return Decide(r, policies...)
}
func (Standard) Widest(levels []string) string { return Widest(levels) }

func policyMatches(p platform.AuthorizationPolicy, r platform.AuthorizationInput) bool {
	if strings.HasSuffix(p.Permission, "*") {
		if !strings.HasPrefix(r.Permission, strings.TrimSuffix(p.Permission, "*")) {
			return false
		}
	} else if p.Permission != "" && p.Permission != r.Permission {
		return false
	}
	return p.When == nil || p.When(r)
}

func Decide(r platform.AuthorizationInput, policies ...platform.AuthorizationPolicy) platform.AuthzDecision {
	if r.Subject.Replaying {
		return platform.AuthzDecision{Allow: true, Rule: "bypass", Reason: "replaying an accepted decision", Layers: []platform.AuthzLayer{{Name: "recorded", Allowed: true, Reason: "replaying an accepted decision"}}}
	}
	if r.Kind == "" {
		r.Kind = "action"
	}
	v := platform.AuthzDecision{Rule: "none", Layers: []platform.AuthzLayer{}}
	check := func(name string, gate platform.AuthorizationGate, role, policy string) bool {
		v.Layers = append(v.Layers, platform.AuthzLayer{Name: name, Allowed: gate.Allowed, Reason: gate.Reason, Role: role, Policy: policy})
		if !gate.Allowed {
			v.Reason = gate.Reason
		}
		return gate.Allowed
	}
	context := r.Context
	if context == nil {
		pass := platform.AuthorizationGate{Allowed: true, Reason: "not restricted by this owner"}
		context = &platform.AuthorizationContext{Tenant: pass, Principal: pass, Row: pass, Fields: pass, Derived: pass, Markings: pass}
	}
	v.Definition = context.Definition
	v.Withheld = slices.Clone(context.Withheld)
	if !check("tenant", context.Tenant, "", "") || !check("principal", context.Principal, "", "") {
		return v
	}
	if !slices.Contains([]string{"read", "write", "create", "archive", "action", "derive", "export", "protocol"}, r.Kind) {
		check("deny", platform.AuthorizationGate{Reason: "Unknown authorization kind"}, "", "")
		return v
	}
	var allow *platform.AuthorizationPolicy
	for i := range policies {
		p := &policies[i]
		if p.Effect == "deny" && policyMatches(*p, r) {
			v.Rule, v.Policy = "policy", p.ID
			check("deny", platform.AuthorizationGate{Reason: "An explicit policy denies this operation"}, "", p.ID)
			return v
		}
		if allow == nil && p.Effect == "allow" && policyMatches(*p, r) {
			allow = p
		}
	}
	check("deny", platform.AuthorizationGate{Allowed: true, Reason: "no explicit deny matched"}, "", "")
	role := ""
	if slices.Contains(r.Allowed, platform.AnyMember) {
		role = platform.AnyMember
	}
	for _, held := range r.Subject.Roles {
		if role == "" && held != "" && slices.Contains(r.Allowed, held) {
			role = held
		}
	}
	verb := platform.AuthorizationGate{Allowed: role != "" || allow != nil || r.Subject.Automation, Reason: "No role or policy grants this operation"}
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
		gate platform.AuthorizationGate
	}{{"row", context.Row}, {"fields", context.Fields}, {"derived", context.Derived}, {"markings", context.Markings}} {
		if !check(layer.name, layer.gate, "", "") {
			return v
		}
	}
	final := platform.AuthorizationGate{Allowed: true, Reason: verb.Reason}
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
