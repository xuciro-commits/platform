package ontology

import (
	ruleprotocol "platformserver/kernel/protocol"
	"reflect"

	"platformserver/kernel/records"
	"platformserver/platform"
)

func CheckObjectConstraints(o Object, entity platform.RecordType, lookup func(string) (platform.RecordTypeInfo, bool), checkCollections func(platform.RuleReadSpec, string, func(string) (platform.RecordTypeInfo, bool)) error, engines ...platform.RuleEngine) error {
	if len(o.Constraints) == 0 {
		return nil
	}
	info, err := records.Describe(AppID, entity, func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	lookup = RuleLookup(o, AppID, BuilderRole, lookup)
	if err := ruleprotocol.CheckConstraints(info, lookup, true, engines...); err != nil {
		return err
	}
	for _, constraint := range o.Constraints {
		if err := checkCollections(ruleprotocol.ConstraintSpec(info, constraint), "constraint "+constraint.Name, lookup); err != nil {
			return err
		}
	}
	return nil
}
