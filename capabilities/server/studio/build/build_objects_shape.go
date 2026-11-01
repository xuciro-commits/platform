package build

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	"platformserver/platform"
)

const BaseField = "base"

func CheckEnterpriseUse(o Object) error {
	if o.EnterpriseUse == nil {
		return nil
	}
	if o.EnterpriseUse.Kind != "boolean" {
		return fmt.Errorf("current enterprise use must return boolean")
	}
	refs := map[string]platform.ExprType{}
	for _, field := range o.Fields {
		refs[field.Name] = platform.ExprType{Kind: platform.RuleFieldKind(field.Type), Object: field.Ref}
	}
	if len(o.States) > 0 {
		refs["state"] = platform.ExprType{Kind: "choice"}
	}
	refs["archived"] = platform.ExprType{Kind: "boolean"}
	_, err := platform.CompileExpr(*o.EnterpriseUse, platform.ExprSchema{Refs: refs})
	return err
}

func CheckFields(fields []Field, known func(string) bool) error {
	seen := map[string]bool{}
	for _, f := range fields {
		switch {
		case !Named(f.Name):
			return fmt.Errorf("the field name %q is not lower-case letters and digits", f.Name)
		case seen[f.Name]:
			return fmt.Errorf("the field %q is declared twice", f.Name)
		case f.Name == "id" || f.Name == "state" || f.Name == "created" || f.Name == "changed" || f.Name == "revision" || f.Name == "archived":
			return fmt.Errorf("the field %q is the platform's own", f.Name)
		case f.Title == "":
			return fmt.Errorf("the field %q has no label", f.Name)
		case !slices.Contains(fieldTypes, f.Type):
			return fmt.Errorf("the field %q has no type %q", f.Name, f.Type)
		case f.Type == "choice" && strings.TrimSpace(f.Choices) == "":
			return fmt.Errorf("the choice field %q has no values", f.Name)
		case f.Type == "reference" && f.Ref == "":
			return fmt.Errorf("the reference field %q says nothing it refers to", f.Name)
		case f.Type == "reference" && f.Ref != "enterprise.element" && known != nil && !known(f.Ref):
			return fmt.Errorf("the field %q refers to %q, which this tenant has no object for", f.Name, f.Ref)
		case f.Ref == "enterprise.element" && f.Inverse != "":
			return fmt.Errorf("enterprise references do not declare inverse record relations")
		case f.Inverse != "" && f.Type != "reference":
			return fmt.Errorf("the field %q names how it is seen from elsewhere but refers to nothing", f.Name)
		case f.Inverse != "" && !Named(f.Inverse):
			return fmt.Errorf("the relation name %q of field %q is not lower-case letters and digits", f.Inverse, f.Name)
		case f.AliasOf != "" && (!Named(f.AliasOf) || f.AliasOf == f.Name || f.Formula != "" || f.FormulaExpr != nil):
			return fmt.Errorf("the field %q needs a direct read alias without a formula", f.Name)
		}
		seen[f.Name] = true
		if err := CheckEnterpriseReference(f.Type, f.Ref, f.Stereotype, f.Stereotypes); err != nil {
			return err
		}
	}
	return CheckConditions(fields)
}

func CheckEnterpriseReference(kind, ref, stereotype string, stereotypes []string) error {
	if ref == "enterprise.element" && kind != "reference" {
		return fmt.Errorf("enterprise element types require an enterprise reference")
	}
	if stereotype == "" && len(stereotypes) == 0 {
		return nil
	}
	if kind != "reference" || ref != "enterprise.element" {
		return fmt.Errorf("enterprise element types require an enterprise reference")
	}
	for _, value := range stereotypes {
		if value == "" {
			return fmt.Errorf("enterprise element types cannot be empty")
		}
	}
	for _, value := range append(slices.Clone(stereotypes), stereotype) {
		if value != "" && !slices.Contains([]string{"ActualOrganization", "ActualPerson", "ActualPost", "ActualLocation"}, value) {
			return fmt.Errorf("unsupported enterprise element type %q", value)
		}
	}
	return nil
}

func CheckConditions(fields []Field) error {
	for _, f := range fields {
		if f.When == "" {
			continue
		}
		condition, err := platform.ParseWhen(f.When)
		if err != nil {
			return fmt.Errorf("the field %q applies \"only when\" %q, which is not field=value,value", f.Name, f.When)
		}
		name := condition.Field
		i := slices.IndexFunc(fields, func(x Field) bool { return x.Name == name })
		switch {
		case i < 0 || name == f.Name:
			return fmt.Errorf("the field %q applies only when %q is set, which is not another field of this object", f.Name, name)
		case fields[i].When != "":
			return fmt.Errorf("the field %q depends on %q, which is conditional itself", f.Name, name)
		case fields[i].Type != "choice" && fields[i].Type != "boolean":
			return fmt.Errorf("the field %q depends on %q, which is neither a choice nor a boolean", f.Name, name)
		}
		allowed := []string{"true", "false"}
		if fields[i].Type == "choice" {
			allowed = ValueChoices(fields[i].Choices)
		}
		for _, v := range condition.In {
			if !slices.Contains(allowed, v) {
				return fmt.Errorf("the field %q depends on %q being %q, which it never is", f.Name, name, v)
			}
		}
	}
	return nil
}

var fieldTypes = []string{"text", "longtext", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference"}

func Named(s string) bool {
	if s == "" || !unicode.IsLetter(rune(s[0])) {
		return false
	}
	for _, r := range s {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func CheckObjectShape(o Object, model platform.Entity, declared []platform.Interface, known func(string) bool, lookup func(string) (platform.EntityInfo, bool)) error {
	if len(o.Implements) > 0 {
		info, err := platform.Describe(ID, model, func(reflect.Type) string { return "" })
		if err != nil {
			return err
		}
		for k, name := range o.Implements {
			if slices.Contains(o.Implements[:k], name) {
				return fmt.Errorf("the interface %s is listed twice", name)
			}
			i := slices.IndexFunc(declared, func(i platform.Interface) bool { return i.Name == name })
			if i < 0 {
				return fmt.Errorf("no app declares the interface %s", name)
			}
			if err := declared[i].Implements(info); err != nil {
				return err
			}
		}
	}
	if o.Extends != "" {
		if o.Extends == TypeOf(o.Name) || (known == nil || !known(o.Extends)) {
			return fmt.Errorf("the object cannot extend %s: not an object type this tenant has", o.Extends)
		}
		k := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == BaseField })
		if k < 0 || o.Fields[k].Type != "reference" || o.Fields[k].Ref != o.Extends || !o.Fields[k].Required {
			return fmt.Errorf("an extension of %s needs a required reference field %q pointing at it", o.Extends, BaseField)
		}
		if base, exists := lookup(o.Extends); exists {
			info, err := platform.Describe(ID, model, func(reflect.Type) string { return "" })
			if err != nil {
				return err
			}
			info.ExtensionSource = nil
			if err := platform.CheckExtension(info, base); err != nil {
				return err
			}
		}
	}
	return nil
}

func CompileObjectShape(o Object) platform.Entity {
	fields := []reflect.StructField{{Name: "Record", Type: reflect.TypeFor[platform.Record](), Anonymous: true,
		Tag: reflect.StructTag(fmt.Sprintf(`entity:"%s"`, TypeOf(o.Name)))}}
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
			tag += fmt.Sprintf(` read:"%s"`, strings.Join(append([]string{Builder}, f.Read...), ","))
		}
		if len(f.Write) > 0 {
			tag += fmt.Sprintf(` write:"%s"`, strings.Join(append([]string{Builder}, f.Write...), ","))
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
	return platform.Entity{LocalRules: localRules, Type: TypeOf(o.Name), Title: o.Title, Plural: o.Plural, Description: o.Description, Icon: o.Icon, Color: o.Color, Format: fieldFormats(o.Fields), ValueTypeBindings: valueTypeBindings(o.Fields), Model: model, Display: display, ObjectView: o.ObjectView, Constraints: o.Constraints, Sets: o.Sets, Versioning: o.Versioning, VersionPins: o.VersionPins, Implements: slices.Clone(o.Implements), Extends: o.Extends, ExtensionSource: o.ExtensionSource, Derived: derived, Withheld: withheld, EnterpriseUse: o.EnterpriseUse}
}

func goType(kind string) reflect.Type {
	switch kind {
	case "integer":
		return reflect.TypeFor[int]()
	case "decimal":
		return reflect.TypeFor[float64]()
	case "money":
		return reflect.TypeFor[platform.Money]()
	case "datetime":
		return reflect.TypeFor[time.Time]()
	case "boolean":
		return reflect.TypeFor[bool]()
	}
	return reflect.TypeFor[string]()
}

func GoName(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}

func CompileObjectPage(o Object) platform.Page {
	names := []string{}
	for _, f := range o.Fields {
		names = append(names, f.Name)
	}
	list := slices.Clone(names)
	if len(list) > 4 {
		list = list[:4]
	}
	if len(o.States) > 0 {
		list, names = append(list, "state"), append(names, "state")
	}
	typ := TypeOf(o.Name)
	actions := []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: typ + ".create"}, {App: ID, Kind: platform.AssetAction, Name: typ + ".edit"}}
	for _, a := range o.Actions {
		actions = append(actions, platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: typ + "." + a.Name})
	}
	title := o.Plural
	if title == "" {
		title = o.Title
	}
	return platform.Page{Name: o.Name, Title: title, Description: o.Description, Layout: "list-detail",
		Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: typ}, ListFields: list, DetailFields: names, Actions: actions}
}
