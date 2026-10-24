package build

import "platformserver/internal/construction"

import (
	"fmt"
	"platformserver/internal/host"
	"platformserver/platform"
	"reflect"
)

func (b *Build) checkRecordSets(o construction.Object, engines ...platform.RuleEngine) error {
	if len(o.Sets) == 0 {
		return nil
	}
	info, err := platform.Describe(construction.ID, Entity(o), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	lookup := func(typ string) (platform.EntityInfo, bool) {
		if typ == info.Type {
			return info, true
		}
		return b.conditionLookup(typ)
	}
	if err := platform.CheckRecordSets(info, lookup, engines...); err != nil {
		return err
	}
	for _, set := range o.Sets {
		if set.Binding != nil {
			reader, ok := b.host.(host.RecordSetHost)
			if !ok {
				return fmt.Errorf("set %s needs a typed host binding", set.Name)
			}
			if err := reader.ValidateRecordSet(info, set); err != nil {
				return fmt.Errorf("set %s: %w", set.Name, err)
			}
		}
	}
	return nil
}
