package ontology

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"platformserver/platform"
)

const BaseField = "base"

var fieldTypes = []string{"text", "longtext", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference", "attachment"}

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

func ValueChoices(s string) []string {
	var out []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func CheckObjectShape(o Object, app, objectType string, model platform.Entity, declared []platform.Interface, known func(string) bool, lookup func(string) (platform.EntityInfo, bool)) error {
	if len(o.Implements) > 0 {
		info, err := platform.Describe(app, model, func(reflect.Type) string { return "" })
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
		if o.Extends == objectType || (known == nil || !known(o.Extends)) {
			return fmt.Errorf("the object cannot extend %s: not an object type this tenant has", o.Extends)
		}
		k := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == BaseField })
		if k < 0 || o.Fields[k].Type != "reference" || o.Fields[k].Ref != o.Extends || !o.Fields[k].Required {
			return fmt.Errorf("an extension of %s needs a required reference field %q pointing at it", o.Extends, BaseField)
		}
		if base, exists := lookup(o.Extends); exists {
			info, err := platform.Describe(app, model, func(reflect.Type) string { return "" })
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

func CheckPropertyType(f PropertyType) error {
	if !Named(f.Name) {
		return fmt.Errorf("Property name must use lower-case letters and digits")
	}
	return f.Definition().Check()
}

func CheckValueType(f ValueType) error {
	if !Named(f.Name) {
		return fmt.Errorf("Value type name must use lower-case letters and digits")
	}
	return f.Definition().Check()
}

func CheckObjectTypeGroup(g ObjectTypeGroup) error {
	if !Named(g.Name) {
		return fmt.Errorf("Object type group name must use lower-case letters and digits")
	}
	return g.Definition().Check()
}

func CheckLinkType(name, forward, reverse string, definition platform.LinkType, parent, child platform.EntityInfo) error {
	if !Named(name) || !Named(forward) || !Named(reverse) {
		return fmt.Errorf("Link names must use lower-case letters and digits")
	}
	return definition.CheckSchema(parent, child)
}

func CheckScope(o Object) error {
	field := func(name string) *Field {
		if i := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == name }); i >= 0 {
			return &o.Fields[i]
		}
		return nil
	}
	sc := o.Scope
	if sc == nil {
		sc = &ObjectScope{}
	}
	if sc.Owner != "" {
		if f := field(sc.Owner); f == nil || f.Type != "text" && f.Type != "reference" {
			return fmt.Errorf("the owner field %q is not a text or reference field of this object", sc.Owner)
		}
	}
	if sc.Unit != "" {
		if f := field(sc.Unit); f == nil || f.Type != "text" && f.Type != "reference" {
			return fmt.Errorf("the unit field %q is not a text or reference field of this object", sc.Unit)
		}
	}
	for _, a := range o.Access {
		if (a.Read == "unit" || a.Read == "below") && (sc.Unit == "" || sc.Structure == "") {
			return fmt.Errorf("the role %q reads by unit, so the object needs a unit field and a structure under Scope", a.Role)
		}
	}
	return nil
}

func CheckAccess(o Object, builderRole, userRole string) error {
	roles := map[string]bool{builderRole: true}
	for _, a := range o.Access {
		switch {
		case !Named(a.Role):
			return fmt.Errorf("the role %q is not lower-case letters and digits", a.Role)
		case a.Role == builderRole:
			return fmt.Errorf("the role %q always does everything with what it builds", builderRole)
		case roles[a.Role]:
			return fmt.Errorf("the role %q is given access twice", a.Role)
		case readLevels[a.Read] == "":
			return fmt.Errorf("the role %q reads %q; it reads all, below, unit, own or none", a.Role, a.Read)
		case a.Read == "none" && (a.Create || a.Edit || a.Archive):
			return fmt.Errorf("the role %q may not read the object, so it may not create, edit or archive it either", a.Role)
		}
		roles[a.Role] = true
	}
	known := func(role string) bool { return roles[role] || len(o.Access) == 0 && role == userRole }
	for _, act := range o.Actions {
		for _, r := range act.Roles {
			if !known(r) {
				return fmt.Errorf("the action %q is for %q, which is not a role of this object", act.Name, r)
			}
		}
	}
	for _, f := range o.Fields {
		for _, r := range append(slices.Clone(f.Read), f.Write...) {
			if !known(r) {
				return fmt.Errorf("the field %q names %q, which is not a role of this object", f.Name, r)
			}
		}
		if len(f.Read) > 0 {
			for _, r := range f.Write {
				if !slices.Contains(f.Read, r) {
					return fmt.Errorf("the field %q is set by %q, which may not read it", f.Name, r)
				}
			}
		}
	}
	return nil
}

var readLevels = map[string]string{"all": platform.ScopeTenant, "below": platform.ScopeBelow, "unit": platform.ScopeUnit, "own": platform.ScopeOwn, "none": platform.ScopeNone}
