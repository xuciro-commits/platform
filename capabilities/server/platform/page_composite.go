package platform

import (
	"fmt"
	"slices"
)

const PageCompositeProfile = "platform.page.v2.111"

func CompositeLimits() (records, cells, bytes int) {
	limits := pageWidgets.Runtime.Composite
	return limits.MaxRecords, limits.MaxCells, limits.MaxBytes
}

func CompositePortRoles(widget, port string) []string {
	keys := []string{}
	if contract := pageWidget(widget); contract != nil {
		for _, source := range contract.ModelPorts {
			if source.ID == port {
				for key := range source.Roles {
					keys = append(keys, key)
				}
				break
			}
		}
	}
	slices.Sort(keys)
	return keys
}

// ModelViewBinding assigns visual roles to declared fields. Expressions use
// the same pure rule language as model labels and action conditions.
type ModelViewBinding struct {
	Model       ModelBinding        `json:"model"`
	Roles       map[string]FieldRef `json:"roles,omitempty"`
	Expressions map[string]Expr     `json:"expressions,omitempty"`
}

type PageComposite struct {
	Bindings  map[string]ModelViewBinding `json:"bindings"`
	Value     *Expr                       `json:"value,omitempty"`
	Tones     map[string]string           `json:"tones,omitempty"`
	Direction string                      `json:"direction,omitempty" enum:"right,down"`
	Acyclic   bool                        `json:"acyclic,omitempty"`
}

func (s Section) PrimaryModel() *ModelBinding {
	if s.Model != nil {
		return s.Model
	}
	if s.Composite != nil {
		key := "nodes"
		if s.Widget == "model-matrix" {
			key = "rows"
		}
		if source, ok := s.Composite.Bindings[key]; ok {
			return &source.Model
		}
	}
	return nil
}

func (s Section) ModelBindings() []*ModelBinding {
	result := []*ModelBinding{}
	if s.Model != nil {
		result = append(result, s.Model)
	}
	if s.Composite != nil {
		for _, key := range compositeKeys(s.Composite) {
			binding := s.Composite.Bindings[key]
			result = append(result, &binding.Model)
		}
	}
	return result
}

func compositeKeys(c *PageComposite) []string {
	keys := make([]string, 0, len(c.Bindings))
	for key := range c.Bindings {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func CompositeRole(binding ModelViewBinding, role string, info EntityInfo) *FieldRef {
	if field, ok := binding.Roles[role]; ok {
		return &field
	}
	if _, ok := binding.Expressions[role]; ok {
		return nil
	}
	if role == "label" {
		if binding.Model.Label != nil {
			return nil
		}
		name := info.Display
		// Nodes and axes already fall back to their record identity. An
		// unnamed relation should not fill the graph with opaque edge IDs.
		if name == "" || name == "id" {
			return nil
		}
		return &FieldRef{Name: name}
	}
	if role == "state" && info.Lifecycle != nil {
		return &FieldRef{Name: info.Lifecycle.Field}
	}
	return nil
}

func MatrixRuleReads(c *PageComposite) RuleReadSpec {
	return RuleReadSpec{Inputs: map[string]ExprType{
		"row":    {Kind: "reference", Object: c.Bindings["rows"].Model.Type},
		"column": {Kind: "reference", Object: c.Bindings["columns"].Model.Type},
	}}
}

func (p Page) CheckCompositeBindings(lookup func(string) (EntityInfo, bool), criteria func(AssetRef) (*RuleCriteria, bool)) error {
	for _, s := range p.Sections {
		contract := pageWidget(s.Widget)
		composite := contract != nil && len(contract.ModelPorts) > 0
		if s.Composite == nil {
			if composite {
				return fmt.Errorf("section %s: composite model bindings are required", s.ID)
			}
			continue
		}
		c := s.Composite
		if !composite || s.Model != nil || s.Object.Name != "" || len(s.Fields) > 0 || s.Query.Name != "" || s.Relation != "" || s.ParentSelection != "" || p.Document == nil || !PageUIProfileSupports(p.Document.UIProfile, PageCompositeProfile) || len(c.Bindings) > len(contract.ModelPorts) || len(c.Tones) > 64 || c.Direction != "" && c.Direction != "right" && c.Direction != "down" {
			return fmt.Errorf("section %s: invalid composite contract or profile", s.ID)
		}
		for value, tone := range c.Tones {
			if len(value) > 256 || !slices.Contains([]string{"neutral", "info", "success", "warning", "danger"}, tone) {
				return fmt.Errorf("section %s: invalid declared tone", s.ID)
			}
		}
		for _, port := range contract.ModelPorts {
			binding, exists := c.Bindings[port.ID]
			if !exists {
				if port.Required {
					return fmt.Errorf("section %s: model port %s is required", s.ID, port.ID)
				}
				continue
			}
			if binding.Model.Options != nil || binding.Model.Relation != nil || len(binding.Roles) > len(port.Roles) || len(binding.Expressions) > len(port.Roles) {
				return fmt.Errorf("section %s port %s: unsupported binding", s.ID, port.ID)
			}
			// Validate one source while retaining the original page's producers,
			// so declared sets can resolve their actual typed record variable.
			if err := p.checkModelSources([]Section{{ID: s.ID, Widget: "table", Model: &binding.Model}}, lookup, criteria); err != nil {
				return err
			}
			info, _ := lookup(binding.Model.Type)
			for role, declaration := range port.Roles {
				field := CompositeRole(binding, role, info)
				expression, computed := binding.Expressions[role]
				if _, explicit := binding.Roles[role]; explicit && computed {
					return fmt.Errorf("section %s role %s: use one field or expression", s.ID, role)
				}
				if field == nil && !computed {
					if declaration.Required {
						return fmt.Errorf("section %s port %s: role %s is required", s.ID, port.ID, role)
					}
					continue
				}
				kind := expression.Kind
				if field != nil {
					if field.Name != "" && len(field.Path) > 0 || field.Slot != "" || field.Transform != "" {
						return fmt.Errorf("section %s: invalid visual field reference", s.ID)
					}
					f, err := RecordPathField(binding.Model.Type, field.Segments(), func(typ string) (EntityInfo, bool) { v, ok := lookup(typ); return ConstraintEntity(v), ok })
					if err != nil {
						return fmt.Errorf("section %s port %s role %s: %w", s.ID, port.ID, role, err)
					}
					kind = RuleFieldKind(f.Type)
					if target := declaration.Target; target != "" && f.Type == "reference" && f.Ref != c.Bindings[target].Model.Type {
						return fmt.Errorf("section %s role %s: reference target differs from %s", s.ID, role, target)
					}
				} else if err := CheckRuleExpression(expression, RuleReadSpec{Object: binding.Model.Type}, lookup); err != nil {
					return fmt.Errorf("section %s role %s: %w", s.ID, role, err)
				} else if target := declaration.Target; target != "" {
					// Ask the original compiler to compare against the declared
					// reference type. Matching ID text cannot change its owner.
					schema, err := RuleSchema(expression, RuleReadSpec{Object: binding.Model.Type}, lookup)
					if err != nil {
						return err
					}
					schema.Refs["$visualTarget"] = ExprType{Kind: "reference", Object: c.Bindings[target].Model.Type}
					_, err = CompileExpr(Expr{Op: "=", Kind: "boolean", Args: []Expr{expression, ExprReference("reference", "$visualTarget")}}, schema)
					if err != nil {
						return fmt.Errorf("section %s role %s: reference target differs from %s: %w", s.ID, role, target, err)
					}
				}
				if !slices.Contains(declaration.Kinds, kind) {
					return fmt.Errorf("section %s role %s: incompatible value kind %s", s.ID, role, kind)
				}
			}
			for key := range binding.Roles {
				if _, ok := port.Roles[key]; !ok {
					return fmt.Errorf("section %s: undeclared visual role %s", s.ID, key)
				}
			}
			for key := range binding.Expressions {
				if _, ok := port.Roles[key]; !ok {
					return fmt.Errorf("section %s: undeclared visual expression %s", s.ID, key)
				}
			}
		}
		for key := range c.Bindings {
			if !slices.ContainsFunc(contract.ModelPorts, func(port pageModelPort) bool { return port.ID == key }) {
				return fmt.Errorf("section %s: undeclared model port %s", s.ID, key)
			}
		}
		if s.Widget == "model-matrix" {
			if c.Value == nil || !slices.Contains([]string{"text", "boolean", "integer", "number", "decimal"}, c.Value.Kind) {
				return fmt.Errorf("section %s: matrix needs a scalar cell expression", s.ID)
			}
			if err := CheckRuleExpression(*c.Value, MatrixRuleReads(c), lookup); err != nil {
				return fmt.Errorf("section %s cell: %w", s.ID, err)
			}
		} else if c.Value != nil {
			return fmt.Errorf("section %s: cell expressions belong to matrices", s.ID)
		}
	}
	return nil
}
