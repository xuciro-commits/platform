package platform

import (
	ruleexpr "platformserver/expr"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type propertyPolicy struct {
	always, never bool
	program       *ruleexpr.RowProgram
}

type roleSource []string

func (r roleSource) Ref(name string) (ruleexpr.Value, bool) {
	if name != "roles" {
		return ruleexpr.Value{}, false
	}
	return ruleexpr.SetValue(r), true
}

var propertyPolicies sync.Map

func propertyPolicyOf(kind string, f FieldInfo) *propertyPolicy {
	key := kind + "\x00" + strconv.Itoa(len(f.Read)) + "\x00" + strings.Join(f.Read, "\x01") + "\x00" + strconv.Itoa(len(f.Write)) + "\x00" + strings.Join(f.Write, "\x01")
	if cached, ok := propertyPolicies.Load(key); ok {
		return cached.(*propertyPolicy)
	}
	policy := lowerPropertyPolicy(kind, f)
	propertyPolicies.Store(key, policy)
	return policy
}

func lowerPropertyPolicy(kind string, f FieldInfo) *propertyPolicy {
	if slices.Contains(f.Read, "-") {
		return &propertyPolicy{never: true}
	}
	var allowed []string
	switch {
	case kind == "read" && len(f.Read) == 0, len(f.Read) == 0 && len(f.Write) == 0:
		return &propertyPolicy{always: true}
	case kind == "read", len(f.Write) == 0:
		allowed = f.Read
	case len(f.Read) == 0:
		allowed = f.Write
	default:
		for _, role := range f.Read {
			if slices.Contains(f.Write, role) {
				allowed = append(allowed, role)
			}
		}
	}
	allowed = slices.Compact(slices.Sorted(slices.Values(allowed)))
	if len(allowed) == 0 {
		return &propertyPolicy{never: true}
	}
	items := make([]any, len(allowed))
	for i, role := range allowed {
		items[i] = role
	}
	text := ruleexpr.Type{Kind: "text"}
	expr := ruleexpr.Expr{Op: "overlaps", Kind: "boolean", Args: []ruleexpr.Expr{
		ruleexpr.RefOf("set", "roles"),
		{Kind: "set", Lit: &ruleexpr.Literal{Value: ruleexpr.Raw(items), Element: "text"}},
	}}
	program, err := ruleexpr.CompileRow(expr, ruleexpr.Schema{Refs: map[string]ruleexpr.Type{"roles": {Kind: "set", Element: &text}}})
	if err != nil {
		return &propertyPolicy{never: true}
	}
	return &propertyPolicy{program: program}
}

func (p *propertyPolicy) allows(roles []string) bool {
	switch {
	case p.always:
		return true
	case p.never:
		return false
	}
	return p.program.Match(roleSource(roles))
}
