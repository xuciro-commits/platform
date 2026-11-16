package pages

import (
	"fmt"
	"slices"

	"platformserver/platform"
)

func compositeRole(view platform.ModelViewBinding, role string, info platform.EntityInfo) *platform.FieldRef {
	if field, ok := view.Roles[role]; ok {
		return &field
	}
	if _, ok := view.Expressions[role]; ok {
		return nil
	}
	if role == "label" {
		if view.Model.Label != nil {
			return nil
		}
		if name := info.Display; name != "" && name != "id" {
			return &platform.FieldRef{Name: name}
		}
		return nil
	}
	if role == "state" && info.Lifecycle != nil {
		return &platform.FieldRef{Name: info.Lifecycle.Field}
	}
	return nil
}

func checkComposite(b platform.PageDataBinding, lookup func(string) (platform.EntityInfo, bool), engines ...platform.RuleEngine) error {
	widget := b.Ports["widget"]
	if widget == "" {
		for _, candidate := range platform.PageWidgets.Widgets {
			fits := len(candidate.ModelPorts) > 0 && len(b.Models) > 0
			for key := range b.Models {
				found := false
				for _, port := range candidate.ModelPorts {
					if port.ID == key {
						found = true
						break
					}
				}
				fits = fits && found
			}
			for _, port := range candidate.ModelPorts {
				_, present := b.Models[port.ID]
				fits = fits && (present || !port.Required)
			}
			if fits {
				widget = candidate.ComponentID
				break
			}
		}
	}
	contract := platform.PageWidget(widget)
	if contract == nil || len(contract.ModelPorts) == 0 || b.Model != nil || len(b.Models) > len(contract.ModelPorts) {
		return fmt.Errorf("binding %s: invalid composite contract", b.ID)
	}
	entity := func(typ string) (platform.EntityInfo, bool) {
		v, ok := lookup(typ)
		return platform.ConstraintEntity(v), ok
	}
	for _, port := range contract.ModelPorts {
		view, exists := b.Models[port.ID]
		if !exists {
			if port.Required {
				return fmt.Errorf("binding %s: model port %s is required", b.ID, port.ID)
			}
			continue
		}
		if view.Model.Options != nil || view.Model.Relation != nil || len(view.Roles) > len(port.Roles) || len(view.Expressions) > len(port.Roles) {
			return fmt.Errorf("binding %s port %s: unsupported binding", b.ID, port.ID)
		}
		info, _ := lookup(view.Model.Type)
		for role, declaration := range port.Roles {
			field := compositeRole(view, role, info)
			expression, computed := view.Expressions[role]
			if _, explicit := view.Roles[role]; explicit && computed {
				return fmt.Errorf("binding %s role %s: use one field or expression", b.ID, role)
			}
			if field == nil && !computed {
				if declaration.Required {
					return fmt.Errorf("binding %s port %s: role %s is required", b.ID, port.ID, role)
				}
				continue
			}
			kind := expression.Kind
			if field != nil {
				if field.Name != "" && len(field.Path) > 0 || field.Slot != "" || field.Transform != "" {
					return fmt.Errorf("binding %s: invalid field reference", b.ID)
				}
				f, err := platform.RecordPathField(view.Model.Type, fieldSegments(*field), entity)
				if err != nil {
					return fmt.Errorf("binding %s port %s role %s: %w", b.ID, port.ID, role, err)
				}
				kind = platform.RuleFieldKind(f.Type)
				if target := declaration.Target; target != "" && f.Type == "reference" && f.Ref != b.Models[target].Model.Type {
					return fmt.Errorf("binding %s role %s: reference target differs from %s", b.ID, role, target)
				}
			} else if err := checkRuleExpression(expression, platform.RuleReadSpec{Object: view.Model.Type}, lookup, engines...); err != nil {
				return fmt.Errorf("binding %s role %s: %w", b.ID, role, err)
			} else if target := declaration.Target; target != "" {
				schema, err := platform.RuleSchema(expression, platform.RuleReadSpec{Object: view.Model.Type}, lookup, engines...)
				if err != nil {
					return err
				}
				schema.Refs["$visualTarget"] = platform.ExprType{Kind: "reference", Object: b.Models[target].Model.Type}
				if _, err = platform.RuleEngineOf(engines).CompileExpr(platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{expression, platform.ExprReference("reference", "$visualTarget")}}, schema); err != nil {
					return fmt.Errorf("binding %s role %s: reference target differs from %s: %w", b.ID, role, target, err)
				}
			}
			if !slices.Contains(declaration.Kinds, kind) {
				return fmt.Errorf("binding %s role %s: incompatible value kind %s", b.ID, role, kind)
			}
		}
		for key := range view.Roles {
			if _, ok := port.Roles[key]; !ok && key != "label" && key != "state" {
				return fmt.Errorf("binding %s: undeclared role %s", b.ID, key)
			}
		}
		for key := range view.Expressions {
			if _, ok := port.Roles[key]; !ok {
				return fmt.Errorf("binding %s: undeclared expression %s", b.ID, key)
			}
		}
	}
	for key := range b.Models {
		found := false
		for _, port := range contract.ModelPorts {
			if port.ID == key {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("binding %s: undeclared model port %s", b.ID, key)
		}
	}
	if widget == "model-matrix" {
		if b.Value == nil || !slices.Contains([]string{"text", "boolean", "integer", "number", "decimal"}, b.Value.Kind) {
			return fmt.Errorf("binding %s: matrix needs a scalar cell expression", b.ID)
		}
		if err := checkRuleExpression(*b.Value, matrixRuleReads(b), lookup, engines...); err != nil {
			return fmt.Errorf("binding %s cell: %w", b.ID, err)
		}
	} else if b.Value != nil {
		return fmt.Errorf("binding %s: cell expressions belong to matrices", b.ID)
	}
	return nil
}

func compositeLimits() (records, cells, bytes int) {
	limits := platform.PageWidgets.Runtime.Composite
	return limits.MaxRecords, limits.MaxCells, limits.MaxBytes
}

func matrixRuleReads(c platform.PageDataBinding) platform.RuleReadSpec {
	return platform.RuleReadSpec{Inputs: map[string]platform.ExprType{"row": {Kind: "reference", Object: c.Models["rows"].Model.Type}, "column": {Kind: "reference", Object: c.Models["columns"].Model.Type}}}
}
