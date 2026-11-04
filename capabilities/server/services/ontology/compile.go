package ontology

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

func PropertyBindings(fields []Field) map[string]platform.AssetBinding {
	out := map[string]platform.AssetBinding{}
	for _, f := range fields {
		if f.Property != nil {
			out[f.Name] = *f.Property
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func FieldFormats(fields []Field) map[string][]platform.FormatRule {
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

func ValueTypeBindings(fields []Field) map[string]platform.AssetBinding {
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

func GoName(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}

func CompileObjectShape(o Object, app, builderRole string) platform.Entity {
	fields := []reflect.StructField{{Name: "Record", Type: reflect.TypeFor[platform.Record](), Anonymous: true,
		Tag: reflect.StructTag(fmt.Sprintf(`entity:"%s"`, app+"."+o.Name))}}
	for _, f := range o.Fields {
		tag := fmt.Sprintf(`json:"%s,omitempty" title:"%s"`, f.Name, f.Title)
		if f.AIInput != nil {
			if *f.AIInput {
				tag += ` ai:"allow"`
			} else {
				tag += ` ai:"deny"`
			}
		}
		if f.SourceType != "" {
			tag += fmt.Sprintf(` source:"%s"`, f.SourceType)
		}
		switch f.Type {
		case "longtext", "date", "datetime":
			tag += fmt.Sprintf(` type:"%s"`, f.Type)
		case "choice":
			tag += fmt.Sprintf(` choices:"%s"`, strings.Join(ValueChoices(f.Choices), ","))
		case "reference":
			tag += fmt.Sprintf(` ref:"%s"`, f.Ref)
			if f.Stereotype != "" {
				tag += fmt.Sprintf(` stereo:"%s"`, f.Stereotype)
			}
			if len(f.Stereotypes) > 0 {
				tag += fmt.Sprintf(` stereos:"%s"`, strings.Join(f.Stereotypes, ","))
			}
			if f.Inverse != "" {
				tag += fmt.Sprintf(` inverse:"%s"`, f.Inverse)
			}
		}
		var marks []string
		if f.Required {
			marks = append(marks, "required")
		}
		if f.Search {
			marks = append(marks, "search")
		}
		if f.Formula != "" || f.FormulaExpr != nil || f.ReadOnly || f.AliasOf != "" {
			marks = append(marks, "readonly")
		}
		if slices.ContainsFunc(o.VersionPins, func(pin platform.VersionPinBinding) bool { return pin.Version == f.Name }) && !slices.Contains(marks, "readonly") {
			marks = append(marks, "readonly")
		}
		if len(marks) > 0 {
			tag += fmt.Sprintf(` field:"%s"`, strings.Join(marks, ","))
		}
		if f.AliasOf != "" {
			tag += fmt.Sprintf(` alias:"%s"`, f.AliasOf)
		}
		if f.When != "" {
			tag += fmt.Sprintf(` when:"%s"`, f.When)
		}
		if len(f.Read) > 0 {
			tag += fmt.Sprintf(` read:"%s"`, strings.Join(append([]string{builderRole}, f.Read...), ","))
		}
		if len(f.Write) > 0 {
			tag += fmt.Sprintf(` write:"%s"`, strings.Join(append([]string{builderRole}, f.Write...), ","))
		}
		fieldType := goType(f.Type)
		if (!f.Required || f.FormulaExpr != nil) && (f.Type == "integer" || f.Type == "decimal" || f.Type == "boolean" || f.Type == "datetime" || f.Type == "money") {
			fieldType = reflect.PointerTo(fieldType)
		}
		fields = append(fields, reflect.StructField{Name: GoName(f.Name), Type: fieldType, Tag: reflect.StructTag(tag)})
	}
	var derived []platform.Derivation
	for _, f := range o.Fields {
		if f.SourceType == "generated" {
			name := "aisources" + f.Name
			fields = append(fields, reflect.StructField{Name: "AISources" + GoName(f.Name), Type: reflect.TypeFor[[]string](), Tag: reflect.StructTag(fmt.Sprintf(`json:"%s,omitempty" type:"json" field:"readonly,aside"`, name))})
			derived = append(derived, platform.Derivation{From: name, Fields: []string{f.Name}})
		}
		if f.FormulaExpr != nil {
			name := "rulesources" + f.Name
			fields = append(fields, reflect.StructField{Name: "RuleSources" + GoName(f.Name), Type: reflect.TypeFor[[]string](), Tag: reflect.StructTag(fmt.Sprintf(`json:"%s,omitempty" type:"json" field:"readonly,aside"`, name))})
			derived = append(derived, platform.Derivation{From: name, Fields: []string{f.Name}})
		}
	}
	withheld := ""
	if len(derived) > 0 {
		withheld = "rulewithheld"
		fields = append(fields, reflect.StructField{Name: "RuleWithheld", Type: reflect.TypeFor[bool](), Tag: reflect.StructTag(`json:"rulewithheld,omitempty" field:"readonly,aside"`)})
	}
	if len(o.States) > 0 {
		names := make([]string, 0, len(o.States))
		for _, st := range o.States {
			names = append(names, st.Name)
		}
		fields = append(fields, reflect.StructField{Name: "State", Type: reflect.TypeFor[string](),
			Tag: reflect.StructTag(fmt.Sprintf(`json:"state,omitempty" title:"State" field:"readonly" choices:"%s"`, strings.Join(names, ",")))})
	}
	model := reflect.New(reflect.StructOf(fields)).Elem().Interface()
	display := o.Display
	for _, f := range o.Fields {
		if display == "" && f.Search {
			display = f.Name
		}
	}
	localRules := map[string]platform.Expr{}
	for _, field := range o.Fields {
		if field.FormulaExpr != nil {
			localRules[field.Name] = *field.FormulaExpr
		}
	}
	return platform.Entity{LocalRules: localRules, Type: app + "." + o.Name, Title: o.Title, Plural: o.Plural, Description: o.Description, Icon: o.Icon, Color: o.Color, Format: FieldFormats(o.Fields), ValueTypeBindings: ValueTypeBindings(o.Fields), PropertyBindings: PropertyBindings(o.Fields), Model: model, Display: display, ObjectView: o.ObjectView, Constraints: o.Constraints, Sets: o.Sets, Versioning: o.Versioning, VersionPins: o.VersionPins, Implements: slices.Clone(o.Implements), Extends: o.Extends, ExtensionSource: o.ExtensionSource, Derived: derived, Withheld: withheld, EnterpriseUse: o.EnterpriseUse}
}

func goType(kind string) reflect.Type {
	switch kind {
	case "integer":
		return reflect.TypeFor[int]()
	case "decimal":
		return reflect.TypeFor[float64]()
	case "attachment":
		return reflect.TypeFor[*platform.Attachment]()
	case "money":
		return reflect.TypeFor[platform.Money]()
	case "datetime":
		return reflect.TypeFor[time.Time]()
	case "boolean":
		return reflect.TypeFor[bool]()
	}
	return reflect.TypeFor[string]()
}
