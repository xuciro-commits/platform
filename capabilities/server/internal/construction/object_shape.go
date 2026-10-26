package construction

import (
	"fmt"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

// CompileObjectShape builds the immutable Go model and its declarative record
// metadata. Runtime rules and installation remain with the Build application.
func CompileObjectShape(o Object) platform.Entity {
	// StructOf interns identical shapes. The entity identity keeps two named
	// objects distinct, just as two named Go record types are distinct; this
	// tag adds no JSON field and stays stable across draft/publication/restore.
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
		if len(f.Read) > 0 { // the builder always reads and sets what it builds
			tag += fmt.Sprintf(` read:"%s"`, strings.Join(append([]string{Builder}, f.Read...), ","))
		}
		if len(f.Write) > 0 {
			tag += fmt.Sprintf(` write:"%s"`, strings.Join(append([]string{Builder}, f.Write...), ","))
		}
		fieldType := goType(f.Type)
		// Optional scalar values need a nil state. Otherwise an omitted number
		// or boolean is indistinguishable from an explicitly supplied zero.
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
	if len(o.States) > 0 { // where a record stands, which only its actions move
		names := make([]string, 0, len(o.States))
		for _, st := range o.States {
			names = append(names, st.Name)
		}
		fields = append(fields, reflect.StructField{Name: "State", Type: reflect.TypeFor[string](),
			Tag: reflect.StructTag(fmt.Sprintf(`json:"state,omitempty" title:"State" field:"readonly" choices:"%s"`, strings.Join(names, ",")))})
	}
	model := reflect.New(reflect.StructOf(fields)).Elem().Interface()
	display := ""
	for _, f := range o.Fields {
		if display == "" && f.Search {
			display = f.Name
		}
	}
	return platform.Entity{Type: TypeOf(o.Name), Title: o.Title, Plural: o.Plural, Description: o.Description, Model: model, Display: display, Constraints: o.Constraints, Sets: o.Sets, Versioning: o.Versioning, VersionPins: o.VersionPins, Implements: slices.Clone(o.Implements), Extends: o.Extends, ExtensionSource: o.ExtensionSource, Derived: derived, Withheld: withheld}
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

// GoName is a field's exported Go name: the platform never shows it.
func GoName(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}

// CompileObjectPage is the list and detail page a defined object comes with: the same
// descriptor a code page has.
func CompileObjectPage(o Object) platform.Page {
	names := []string{}
	for _, f := range o.Fields {
		names = append(names, f.Name)
	}
	list := slices.Clone(names)
	if len(list) > 4 {
		list = list[:4]
	}
	if len(o.States) > 0 { // where each record stands, in the list and on its page
		list, names = append(list, "state"), append(names, "state")
	}
	typ := TypeOf(o.Name)
	actions := []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: typ + ".create"}, {App: ID, Kind: platform.AssetAction, Name: typ + ".edit"}}
	for _, a := range o.Actions {
		actions = append(actions, platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: typ + "." + a.Name})
	}
	title := o.Plural
	if title == "" { // a plural is optional; the page is never nameless (UX-10)
		title = o.Title
	}
	return platform.Page{Name: o.Name, Title: title, Description: o.Description, Layout: "list-detail",
		Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: typ}, ListFields: list, DetailFields: names, Actions: actions}
}
