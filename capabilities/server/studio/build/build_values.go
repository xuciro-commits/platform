package build

import (
	"fmt"
	"reflect"
	"slices"

	"platformserver/platform"
)

func fieldFormats(fields []Field) map[string][]platform.FormatRule {
	out := map[string][]platform.FormatRule{}
	for _, f := range fields {
		if len(f.Format) > 0 {
			out[f.Name] = f.Format
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func valueTypeBindings(fields []Field) map[string]platform.AssetBinding {
	out := map[string]platform.AssetBinding{}
	for _, f := range fields {
		if f.ValueType != nil {
			out[f.Name] = *f.ValueType
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (b *Build) checkValueTypeFields(fields []Field) error {
	for _, f := range fields {
		if f.ValueType == nil {
			continue
		}
		if b.host == nil || f.ValueType.Ref.App != ID {
			return fmt.Errorf("value type needs its canonical tenant owner")
		}
		v, ok := b.host.ResolveValueType(*f.ValueType)
		if !ok {
			return fmt.Errorf("value type version is unavailable")
		}
		if f.Choices != "" || f.Ref != "" || f.Inverse != "" || f.Property != nil || f.Formula != "" || f.FormulaExpr != nil {
			return fmt.Errorf("a field bound to a value type cannot declare another field shape")
		}
		if err := v.CheckField(platform.FieldInfo{Name: f.Name, Type: f.Type, Title: f.Title}); err != nil {
			return err
		}
	}
	return nil
}

func (b *Build) checkPresentation(o Object) error {
	if o.Display != "" {
		i := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == o.Display })
		if i < 0 || !slices.Contains([]string{"text", "longtext", "choice"}, o.Fields[i].Type) {
			return fmt.Errorf("the title field %q must be a text-like field of this object", o.Display)
		}
	}
	if o.Icon == "" && o.Color == "" && fieldFormats(o.Fields) == nil {
		return nil
	}
	info, err := platform.Describe(ID, Entity(o), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	if b.host == nil {
		return nil
	}
	return b.host.ValidateObjectPresentation(info)
}

const SchemaValueType = ValueTypeType + ".publish"

const ValueTypeType = "build.valuetype"

type ValueType struct {
	platform.Record
	Name        string                    `json:"name" field:"required,search"`
	Title       string                    `json:"title" field:"required,search"`
	Description string                    `json:"description" field:"required" type:"longtext"`
	Base        string                    `json:"base" field:"required" choices:"text,longtext,integer,decimal,date,datetime"`
	Constraints platform.InputConstraints `json:"constraints,omitzero" type:"json" field:"aside"`
	Pattern     string                    `json:"pattern,omitempty" help:"An RE2 regular expression the text must match; anchor it with ^ and $ for the whole value"`
	Choices     []string                  `json:"choices,omitempty" field:"aside" title:"Listed values"`
	State       string                    `json:"state" field:"readonly" choices:"draft,published"`
	Version     int                       `json:"version,omitempty" field:"readonly"`
	Published   string                    `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions    []string                  `json:"versions,omitempty" field:"readonly"`
}

func (f ValueType) Definition() platform.ValueType {
	return platform.ValueType{Name: f.Name, Title: f.Title, Description: f.Description, Base: f.Base, Constraints: f.Constraints, Pattern: f.Pattern, Choices: slices.Clone(f.Choices)}
}

var valueKind = resourceKind[ValueType, platform.ValueType]{
	asset: platform.AssetValueType, typ: ValueTypeType, prefix: "value", noun: "valueType", title: "Value type", plural: "Value types", capability: "valuetypes",
	description: "A reusable scalar rule set: base type, bounds, pattern and listed values that fields bind to by version.",
	transition:  "Install this value type as its next retained version.",
	immutable:   "A published value type keeps its identity and base type",
	records:     func(b *Build) map[string]ValueType { return b.valueTypes },
	validate: func(_ *Build, f ValueType) (ValueType, error) {
		if !Named(f.Name) {
			return f, fmt.Errorf("Value type name must use lower-case letters and digits")
		}
		return f, f.Definition().Check()
	},
	identity: func(a, b ValueType) bool { return a.Name == b.Name && a.Base == b.Base },
	fromDeclaration: func(v platform.ValueType) ValueType {
		return ValueType{Name: v.Name, Title: v.Title, Description: v.Description, Base: v.Base, Constraints: v.Constraints, Pattern: v.Pattern, Choices: slices.Clone(v.Choices)}
	},
	install: func(h platform.BuildHost, c platform.Caller, d platform.ValueType, version int) error {
		return h.InstallValueType(c, d, version)
	},
	validateInstall: func(h platform.BuildHost, d platform.ValueType) error { return h.ValidateInstallValueType(d) },
}
