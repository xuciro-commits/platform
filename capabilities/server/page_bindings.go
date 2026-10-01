package platformserver

import (
	"fmt"
	"platformserver/platform"
)

func (t *Tenant) checkFormInputs(section platform.Section, target platform.EntityInfo, parentType, parentField string) error {
	if len(section.Inputs) > 64 {
		return fmt.Errorf("form inputs exceed their bound")
	}
	for name, binding := range section.Inputs {
		field, found := target.Field(name)
		if !found || field.ReadOnly || name == parentField || binding.Check() != nil {
			return fmt.Errorf("form input %s needs a writable declared field", name)
		}
		switch binding.Source {
		case "literal":
			if err := fieldValueSchema(field.Type, field.Title, field.Choices).Validate(binding.Value, 64<<10); err != nil {
				return fmt.Errorf("form input %s: %w", name, err)
			}
		case "subject":
			if section.Relation == "" {
				return fmt.Errorf("form record inputs need a declared parent relation")
			}
			source, err := platform.RecordPathField(parentType, binding.Path, t.entity)
			if err != nil {
				return fmt.Errorf("form input %s: %w", name, err)
			}
			if source.Type != field.Type && !(source.Type == "integer" && field.Type == "decimal") || source.Type == "reference" && source.Ref != field.Ref {
				return fmt.Errorf("form input %s has incompatible source type %s", name, source.Type)
			}
		default:
			return fmt.Errorf("form input %s uses a constant or declared record path", name)
		}
	}
	return nil
}
