package platform

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

const (
	MaxFormatRules     = 16
	MaxFormattedFields = 32
	maxFormatLabel     = 64
)

var tones = []string{"neutral", "info", "success", "warning", "danger", "purple", "pink", "teal", "orange"}

func Tones() []string { return slices.Clone(tones) }

type FormatRule struct {
	When  Expr   `json:"when"`
	Tone  string `json:"tone" enum:"neutral,info,success,warning,danger,purple,pink,teal,orange"`
	Icon  string `json:"icon,omitempty"`
	Label string `json:"label,omitempty"`
}

type FieldFormat struct {
	Tone  string `json:"tone" enum:"neutral,info,success,warning,danger,purple,pink,teal,orange"`
	Icon  string `json:"icon,omitempty"`
	Label string `json:"label,omitempty"`
}

func (r FormatRule) Check() error {
	if r.When.Kind != "boolean" {
		return fmt.Errorf("a format rule's condition must return boolean")
	}
	if !slices.Contains(tones, r.Tone) {
		return fmt.Errorf("format tone must be one of %s", strings.Join(tones, ", "))
	}
	if r.Icon != "" && !IconName(r.Icon) {
		return fmt.Errorf("format icon %q is not an icon name", r.Icon)
	}
	if len(r.Label) > maxFormatLabel || strings.ContainsFunc(r.Label, unicode.IsControl) {
		return fmt.Errorf("format label is at most %d characters without control characters", maxFormatLabel)
	}
	return nil
}

func (r FormatRule) Format() FieldFormat {
	return FieldFormat{Tone: r.Tone, Icon: r.Icon, Label: r.Label}
}

func checkPresentation(info EntityInfo) error {
	if info.Icon != "" && !IconName(info.Icon) {
		return fmt.Errorf("entity %s: icon %q is not an icon name", info.Type, info.Icon)
	}
	if info.Color != "" && !slices.Contains(tones, info.Color) {
		return fmt.Errorf("entity %s: color must be one of %s", info.Type, strings.Join(tones, ", "))
	}
	formatted := 0
	for _, f := range info.Fields {
		if len(f.Format) == 0 {
			continue
		}
		if formatted++; formatted > MaxFormattedFields {
			return fmt.Errorf("entity %s: at most %d fields carry format rules", info.Type, MaxFormattedFields)
		}
		if len(f.Format) > MaxFormatRules {
			return fmt.Errorf("entity %s: field %s has more than %d format rules", info.Type, f.Name, MaxFormatRules)
		}
		for i, rule := range f.Format {
			if err := rule.Check(); err != nil {
				return fmt.Errorf("entity %s: field %s format rule %d: %w", info.Type, f.Name, i+1, err)
			}
		}
	}
	return nil
}

func describePresentation(info *EntityInfo, e Entity) error {
	info.Icon, info.Color = e.Icon, e.Color
	for name, rules := range e.Format {
		i := slices.IndexFunc(info.Fields, func(f FieldInfo) bool { return f.Name == name })
		if i < 0 {
			return fmt.Errorf("entity %s: format rules name an unknown field %s", e.Type, name)
		}
		info.Fields[i].Format = slices.Clone(rules)
	}
	for name, binding := range e.ValueTypeBindings {
		i := slices.IndexFunc(info.Fields, func(f FieldInfo) bool { return f.Name == name })
		if i < 0 || binding.Ref.Kind != AssetValueType || binding.Ref.Check() != nil || binding.SourceVersion == "" || info.Fields[i].Property != nil || info.Fields[i].Constraints != nil {
			return fmt.Errorf("entity %s: invalid value type field binding", e.Type)
		}
		selected := binding
		info.Fields[i].ValueType = &selected
	}
	return checkPresentation(*info)
}
