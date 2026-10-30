package platform

import (
	"platformserver/internal/rules"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type propertyPolicy struct {
	always, never bool
	program       *rules.RowProgram
}

type roleSource []string

func (r roleSource) Ref(name string) (rules.Value, bool) {
	if name != "roles" {
		return rules.Value{}, false
	}
	return rules.SetValue(r), true
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
	text := rules.Type{Kind: "text"}
	expr := rules.Expr{Op: "overlaps", Kind: "boolean", Args: []rules.Expr{
		rules.RefOf("set", "roles"),
		{Kind: "set", Lit: &rules.Literal{Value: rules.Raw(items), Element: "text"}},
	}}
	program, err := rules.CompileRow(expr, rules.Schema{Refs: map[string]rules.Type{"roles": {Kind: "set", Element: &text}}})
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
