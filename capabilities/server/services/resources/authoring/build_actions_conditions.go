package build

import (
	"fmt"
	"platformserver/platform"
	"slices"
	"strings"
)

func checkActionCondition(o Object, a Action, cond Condition, lookup func(string) (platform.EntityInfo, bool), requireMessage bool) error {
	if cond.Expr != nil {
		return nil
	}
	left, err := conditionField(o, a, cond.Field, lookup)
	if err != nil {
		return fmt.Errorf("action condition: %w", err)
	}
	switch {
	case !slices.Contains(Operators, cond.Operator):
		return fmt.Errorf("unsupported condition operator %q", cond.Operator)
	case cond.Value != "" && cond.ValueField != "":
		return fmt.Errorf("use a value or a comparison field, not both")
	case cond.Operator != "empty" && cond.Operator != "not empty" && cond.Value == "" && cond.ValueField == "":
		return fmt.Errorf("%s %s compares with nothing", cond.Field, cond.Operator)
	case requireMessage && strings.TrimSpace(cond.Message) == "":
		return fmt.Errorf("the condition on %s says nothing to a person it stops", cond.Field)
	}
	if cond.ValueField != "" {
		if cond.Operator == "empty" || cond.Operator == "not empty" {
			return fmt.Errorf("an emptiness check needs no comparison field")
		}
		right, err := conditionField(o, a, cond.ValueField, lookup)
		if err != nil {
			return fmt.Errorf("action condition: %w", err)
		}
		if err := checkConditionFields(left, right, cond.Operator); err != nil {
			return fmt.Errorf("action condition: %w", err)
		}
	} else if err := checkCondition(left.Type, left.Choices, cond); err != nil {
		return fmt.Errorf("the condition on %s: %w", cond.Field, err)
	}
	return nil
}

func conditionField(o Object, a Action, name string, lookup func(string) (platform.EntityInfo, bool)) (platform.FieldInfo, error) {
	if input, ok := strings.CutPrefix(name, "input."); ok {
		for _, in := range a.Inputs {
			if in.Name == input {
				return platform.FieldInfo{Name: name, Type: in.Type, Ref: in.Ref, Choices: ValueChoices(in.Choices)}, nil
			}
		}
		return platform.FieldInfo{}, fmt.Errorf("condition input %q is not declared", input)
	}
	return platform.RecordPathField(TypeOf(o.Name), strings.Split(name, "."), func(typ string) (platform.EntityInfo, bool) {
		if typ != TypeOf(o.Name) {
			if lookup == nil {
				return platform.EntityInfo{}, false
			}
			return lookup(typ)
		}
		info := platform.EntityInfo{Type: typ}
		for _, f := range o.Fields {
			info.Fields = append(info.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type, Ref: f.Ref, Choices: ValueChoices(f.Choices)})
		}
		if len(o.States) > 0 {
			state := platform.FieldInfo{Name: "state", Type: "choice"}
			for _, s := range o.States {
				state.Choices = append(state.Choices, s.Name)
			}
			info.Fields = append(info.Fields, state)
		}
		return info, true
	})
}

func checkConditionFields(left, right platform.FieldInfo, op string) error {
	if left.Type == "money" || right.Type == "money" {
		return fmt.Errorf("money needs a currency-aware comparison rule")
	}
	numeric := func(kind string) bool { return kind == "integer" || kind == "decimal" }
	if left.Type != right.Type && !(numeric(left.Type) && numeric(right.Type)) || left.Type == "reference" && left.Ref != right.Ref {
		return fmt.Errorf("condition fields have incompatible types %s and %s", left.Type, right.Type)
	}
	if op != "=" && op != "!=" && !numeric(left.Type) && left.Type != "date" && left.Type != "datetime" {
		return fmt.Errorf("%s does not support ordered comparison", left.Type)
	}
	return nil
}

func inputFits(input, field string) bool {
	switch field {
	case "text", "longtext":
		return input == "text" || input == "longtext" || input == "choice"
	case "decimal":
		return input == "decimal" || input == "integer"
	case "choice":
		return input == "choice"
	default:
		return input == field
	}
}

func checkCondition(kind string, options []string, c Condition) error {
	if c.Operator == "empty" || c.Operator == "not empty" {
		return nil
	}
	if kind == "money" {
		return fmt.Errorf("money needs a currency-aware comparison rule")
	}
	if c.Value == "$me" {
		if kind != "text" && kind != "longtext" {
			return fmt.Errorf("$me only compares with a text field")
		}
		return nil
	}
	if c.Operator != "=" && c.Operator != "!=" && kind != "integer" && kind != "decimal" && kind != "date" && kind != "datetime" {
		return fmt.Errorf("%s does not support ordered comparison", kind)
	}
	if _, err := platform.ConditionValue(kind, c.Value); err != nil {
		return fmt.Errorf("%q does not fit %s: %w", c.Value, kind, err)
	}
	if kind == "choice" && !slices.Contains(options, c.Value) {
		return fmt.Errorf("%q is not one of this field's choices", c.Value)
	}
	return nil
}
