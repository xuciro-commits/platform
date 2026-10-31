package platform

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
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

func CheckExtension(extension, base EntityInfo) error {
	if extension.Extends == "" || extension.Extends != base.Type || extension.Type == base.Type {
		return fmt.Errorf("extension %s has an unavailable base %s", extension.Type, extension.Extends)
	}
	link, ok := extension.Field("base")
	if !ok || link.Type != "reference" || link.Ref != base.Type || !link.Required {
		return fmt.Errorf("extension %s needs a required base reference to %s", extension.Type, base.Type)
	}
	for _, field := range extension.Fields {
		if field.Name == "base" || field.ReadOnly && field.AliasOf != "" {
			continue
		}
		if _, exists := base.Field(field.Name); exists {
			return fmt.Errorf("extension %s field %s conflicts with base %s; declare a migration to rename the extension field", extension.Type, field.Name, base.Type)
		}
	}
	if source := extension.ExtensionSource; source != nil {
		if err := source.Check(base.Type); err != nil {
			return err
		}
		if base.App != "" && source.Base.Ref.App != base.App {
			return fmt.Errorf("extension %s base authority differs from its source package", extension.Type)
		}
		for _, field := range source.Fields {
			next, exists := base.Field(field.Name)
			if !exists || field.Type != next.Type || field.Ref != next.Ref || field.Required != next.Required || field.ReadOnly != next.ReadOnly || field.AliasOf != next.AliasOf || !slices.Equal(field.Choices, next.Choices) || !reflect.DeepEqual(field.Property, next.Property) || !reflect.DeepEqual(field.ValueType, next.ValueType) {
				return fmt.Errorf("extension %s source %s@%s has an incompatible base field %s; review and republish the extension against the new package", extension.Type, base.Type, source.Base.SourceVersion, field.Name)
			}
		}
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
	if !interfaceName.MatchString(i.Name) || i.Name[:len(app)+1] != app+"." {
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

func (i Interface) Implements(info EntityInfo) error {
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

func CheckInterfaces(manifests []Manifest, infos map[string]EntityInfo) error {
	declared := map[string]Interface{}
	for _, m := range manifests {
		for _, i := range m.Interfaces {
			if err := i.Check(m.ID); err != nil {
				return err
			}
			if _, dup := declared[i.Name]; dup {
				return fmt.Errorf("interface %s is declared twice", i.Name)
			}
			declared[i.Name] = i
		}
	}
	for _, m := range manifests {
		for _, e := range m.Entities {
			for k, name := range e.Implements {
				i, ok := declared[name]
				if !ok {
					return fmt.Errorf("%s implements %s, which no app declares", e.Type, name)
				}
				if slices.Contains(e.Implements[:k], name) {
					return fmt.Errorf("%s implements %s twice", e.Type, name)
				}
				info, ok := infos[e.Type]
				if !ok {
					return fmt.Errorf("%s implements %s but was not described", e.Type, name)
				}
				if err := i.Implements(info); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func Interfaces(manifests []Manifest) []Interface {
	var out []Interface
	for _, m := range manifests {
		out = append(out, m.Interfaces...)
	}
	slices.SortFunc(out, func(a, b Interface) int { return compareStrings(a.Name, b.Name) })
	return out
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
