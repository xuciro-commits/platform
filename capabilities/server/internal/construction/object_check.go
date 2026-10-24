package construction

import (
	"fmt"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// BaseField is the required record reference of an extension object.
const BaseField = "base"

func CheckEnterpriseUse(o Object, engines ...platform.RuleEngine) error {
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
	var engine platform.RuleEngine = platform.NativeRules{}
	if len(engines) != 0 {
		engine = engines[0]
	}
	_, err := engine.CompileExpr(*o.EnterpriseUse, platform.ExprSchema{Refs: refs})
	return err
}

// checkFields refuses a field the platform has no type for, a choice without
// values, or a reference to an object this tenant has not.
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

// checkConditions reads every "Only when" as the kit will (field=value,value)
// and refuses one that names no other unconditional choice or boolean field,
// or a value it never takes.
func CheckConditions(fields []Field) error {
	for _, f := range fields {
		if f.When == "" {
			continue
		}
		name, values, ok := strings.Cut(f.When, "=")
		if !ok || name == "" || values == "" {
			return fmt.Errorf("the field %q applies \"only when\" %q, which is not field=value,value", f.Name, f.When)
		}
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
		for _, v := range strings.Split(values, ",") {
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
			// An editable draft may explicitly rebind its base at publication.
			// Installed declarations retain their source and are checked at upgrade.
			info.ExtensionSource = nil
			if err := platform.CheckExtension(info, base); err != nil {
				return err
			}
		}
	}
	return nil
}
