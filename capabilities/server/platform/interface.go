package platform

import (
	"fmt"
	"regexp"
	"slices"
)

// Interface is a shape several entity types share (ADR-0058 A2, Foundry's
// Ontology interfaces): named fields with their kinds. An entity type that
// lists it in Entity.Implements must carry every one of those fields with the
// same kind; the host refuses to compose otherwise. Interfaces are declared by
// the app that owns the idea (`core.coded`) and implemented by any app's
// entities, so a page or query written against the interface works for all
// of them. An interface carries no storage and no actions of its own.
type Interface struct {
	Name        string           // <app>.<name>, lower-case letters and digits
	Title       string           // what people call it
	Description string           // what implementing it means
	Fields      []InterfaceField // the fields an implementation must have
}

// InterfaceField is one field an implementation must have, by name and kind.
type InterfaceField struct {
	Name  string
	Type  string // a FieldInfo type: text, integer, decimal, money, date, datetime, boolean, choice, reference, references, tags, longtext
	Title string
}

var interfaceName = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9]*$`)

// Check says the interface is well declared and belongs to app.
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

// Implements says info carries every field of the interface with its kind;
// the error names the first field it lacks.
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

// CheckInterfaces verifies, once every app is composed, that the interfaces
// declared across manifests are distinct and that every entity implements the
// interfaces it names. infos are the described entities by type.
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

// Interfaces are the interfaces declared across manifests, by name, sorted.
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
