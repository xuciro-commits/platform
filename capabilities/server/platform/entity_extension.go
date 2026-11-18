package platform

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type ExtensionSource struct {
	Base   AssetBinding `json:"base"`
	Fields []FieldInfo  `json:"fields"`
}

func (s ExtensionSource) Check(typ string) error {
	if s.Base.Ref.Check() != nil || s.Base.Ref.Kind != AssetObject || s.Base.Ref.Name != typ || s.Base.SourceVersion == "" || len(s.Fields) == 0 {
		return fmt.Errorf("extension %s needs its original base package version and shape", typ)
	}
	seen := map[string]bool{}
	for _, field := range s.Fields {
		if field.Name == "" || field.Type == "" || seen[field.Name] {
			return fmt.Errorf("extension %s has an invalid original base shape", typ)
		}
		seen[field.Name] = true
	}
	return nil
}

type Interface struct {
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description,omitempty"`
	Fields      []InterfaceField `json:"fields"`
}

type InterfaceField struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

var interfaceName = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9]*$`)

func (i Interface) Check(app string) error {
	if !interfaceName.MatchString(i.Name) || app == "" || !strings.HasPrefix(i.Name, app+".") {
		return fmt.Errorf("interface %q: not <%s>.<name> in lower-case letters and digits", i.Name, app)
	}
	if i.Title == "" || i.Description == "" || len(i.Fields) == 0 {
		return fmt.Errorf("interface %s lacks a title, a description or fields", i.Name)
	}
	for k, f := range i.Fields {
		if f.Name == "" || f.Type == "" || slices.ContainsFunc(i.Fields[:k], func(x InterfaceField) bool { return x.Name == f.Name }) {
			return fmt.Errorf("interface %s: field %q unnamed, untyped or repeated", i.Name, f.Name)
		}
	}
	return nil
}

func (i Interface) Implements(info RecordTypeInfo) error {
	for _, want := range i.Fields {
		k := slices.IndexFunc(info.Fields, func(f FieldInfo) bool { return f.Name == want.Name })
		if k < 0 {
			return fmt.Errorf("%s implements %s but has no field %s", info.Type, i.Name, want.Name)
		}
		if info.Fields[k].Type != want.Type {
			return fmt.Errorf("%s implements %s but its field %s is %s, not %s", info.Type, i.Name, want.Name, info.Fields[k].Type, want.Type)
		}
	}
	return nil
}
