package platform

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func compositeRole(view ModelViewBinding, role string, info EntityInfo) *FieldRef {
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
			return &FieldRef{Name: name}
		}
		return nil
	}
	if role == "state" && info.Lifecycle != nil {
		return &FieldRef{Name: info.Lifecycle.Field}
	}
	return nil
}

func (p Page) checkComposite(b PageDataBinding, lookup func(string) (EntityInfo, bool), engines ...RuleEngine) error {
	widget := b.Ports["widget"]
	if widget == "" {
		for _, candidate := range pageWidgets.Widgets {
			fits := len(candidate.ModelPorts) > 0 && len(b.Models) > 0
			for key := range b.Models {
				fits = fits && slices.ContainsFunc(candidate.ModelPorts, func(port pageModelPort) bool { return port.ID == key })
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
	contract := pageWidget(widget)
	if contract == nil || len(contract.ModelPorts) == 0 || b.Model != nil || len(b.Models) > len(contract.ModelPorts) {
		return fmt.Errorf("binding %s: invalid composite contract", b.ID)
	}
	entity := func(typ string) (EntityInfo, bool) { v, ok := lookup(typ); return ConstraintEntity(v), ok }
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
				f, err := RecordPathField(view.Model.Type, field.Segments(), entity)
				if err != nil {
					return fmt.Errorf("binding %s port %s role %s: %w", b.ID, port.ID, role, err)
				}
				kind = RuleFieldKind(f.Type)
				if target := declaration.Target; target != "" && f.Type == "reference" && f.Ref != b.Models[target].Model.Type {
					return fmt.Errorf("binding %s role %s: reference target differs from %s", b.ID, role, target)
				}
			} else if err := CheckRuleExpression(expression, RuleReadSpec{Object: view.Model.Type}, lookup, engines...); err != nil {
				return fmt.Errorf("binding %s role %s: %w", b.ID, role, err)
			} else if target := declaration.Target; target != "" {
				schema, err := RuleSchema(expression, RuleReadSpec{Object: view.Model.Type}, lookup, engines...)
				if err != nil {
					return err
				}
				schema.Refs["$visualTarget"] = ExprType{Kind: "reference", Object: b.Models[target].Model.Type}
				if _, err = ruleEngineOf(engines).CompileExpr(Expr{Op: "=", Kind: "boolean", Args: []Expr{expression, ExprReference("reference", "$visualTarget")}}, schema); err != nil {
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
		if !slices.ContainsFunc(contract.ModelPorts, func(port pageModelPort) bool { return port.ID == key }) {
			return fmt.Errorf("binding %s: undeclared model port %s", b.ID, key)
		}
	}
	if widget == "model-matrix" {
		if b.Value == nil || !slices.Contains([]string{"text", "boolean", "integer", "number", "decimal"}, b.Value.Kind) {
			return fmt.Errorf("binding %s: matrix needs a scalar cell expression", b.ID)
		}
		if err := CheckRuleExpression(*b.Value, MatrixRuleReads(b), lookup, engines...); err != nil {
			return fmt.Errorf("binding %s cell: %w", b.ID, err)
		}
	} else if b.Value != nil {
		return fmt.Errorf("binding %s: cell expressions belong to matrices", b.ID)
	}
	return nil
}

func ValidPageImageURL(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > pageWidgets.Runtime.ContextViews.MaxURLBytes || !utf8.ValidString(raw) {
		return false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) {
		return false
	}
	for _, text := range []string{raw, decoded} {
		for _, r := range text {
			if r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
				return false
			}
		}
	}
	if strings.HasPrefix(raw, "/") {
		return !strings.HasPrefix(raw, "//") && !strings.HasPrefix(decoded, "//")
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || u.Hostname() == "" {
		return false
	}
	if strings.Contains(u.Host, "[") && net.ParseIP(u.Hostname()) == nil {
		return false
	}
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 {
			return false
		}
	}
	return true
}

func PropertyValueType(field FieldInfo) string {
	switch field.Type {
	case "text", "longtext", "choice":
		return "string"
	case "boolean":
		return "boolean"
	case "integer", "decimal":
		return "decimal"
	}
	return ""
}

func (p Page) CheckPropertySchema(variable PageVariable, object EntityInfo) error {
	source := variable.Source
	if source == nil || source.Object == nil || source.Object.Name != object.Type || source.Object.App != object.App || p.RecordVariableObject(source.Variable) != object.Type {
		return fmt.Errorf("property record object is unavailable or incompatible")
	}
	field, ok := object.Field(source.Field)
	if !ok || PropertyValueType(field) == "" || PropertyValueType(field) != variable.Type {
		return fmt.Errorf("property field is unavailable or incompatible")
	}
	return nil
}
