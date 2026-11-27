package platform

import (
	"reflect"
	"slices"
)

func ApplyFieldAliases(info RecordTypeInfo, value reflect.Value) {
	for _, field := range info.Fields {
		if field.AliasOf != "" {
			target, _ := info.Field(field.AliasOf)
			value.FieldByIndex(field.Index).Set(value.FieldByIndex(target.Index))
		}
	}
}

func (info RecordTypeInfo) HasReadSources() bool {
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
