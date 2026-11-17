package platform

import (
	"fmt"
	"reflect"
	"slices"
)

func (info EntityInfo) CompileEnterpriseUse(engines ...RuleEngine) (ExprProgram, error) {
	if info.EnterpriseUse == nil {
		return nil, nil
	}
	if info.EnterpriseUse.Kind != "boolean" {
		return nil, fmt.Errorf("current enterprise use must return boolean")
	}
	refs := map[string]ExprType{"archived": {Kind: "boolean"}}
	for _, field := range info.Fields {
		refs[field.Name] = ExprType{Kind: RuleFieldKind(field.Type), Object: field.Ref}
	}
	return RuleEngineOf(engines).CompileExpr(*info.EnterpriseUse, ExprSchema{Refs: refs})
}

func ApplyFieldAliases(info EntityInfo, value reflect.Value) {
	for _, field := range info.Fields {
		if field.AliasOf != "" {
			target, _ := info.Field(field.AliasOf)
			value.FieldByIndex(field.Index).Set(value.FieldByIndex(target.Index))
		}
	}
}

func (info EntityInfo) HasReadSources() bool {
	return len(info.Derived) > 0 || slices.ContainsFunc(info.Fields, func(field FieldInfo) bool { return field.AliasOf != "" })
}

func (f FieldInfo) Reads(role string) bool { return f.ReadsAny([]string{role}) }

func (f FieldInfo) Writes(role string) bool { return f.WritesAny([]string{role}) }

func (f FieldInfo) ReadsAny(roles []string, engines ...RuleEngine) bool {
	return propertyPolicyOf("read", f, engines...).allows(roles)
}

func (f FieldInfo) WritesAny(roles []string, engines ...RuleEngine) bool {
	if len(roles) == 0 {
		roles = []string{""}
	}
	return propertyPolicyOf("write", f, engines...).allows(roles)
}
