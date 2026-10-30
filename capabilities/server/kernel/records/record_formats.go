package records

import (
	"fmt"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
)

type compiledFormat struct {
	filter *RowFilter
	reads  []string
	format platform.FieldFormat
}

type formattedField struct {
	name  string
	rules []compiledFormat
}

type FormatSet struct {
	fields []formattedField
}

func CompileFormats(info platform.EntityInfo) (*FormatSet, error) {
	set := &FormatSet{}
	for _, field := range info.Fields {
		if len(field.Format) == 0 {
			continue
		}
		formatted := formattedField{name: field.Name}
		for i, rule := range field.Format {
			filter, err := CompileRowFilter(info, rule.When)
			if err != nil {
				return nil, fmt.Errorf("field %s format rule %d does not compile against %s: %w", field.Name, i+1, info.Type, err)
			}
			formatted.rules = append(formatted.rules, compiledFormat{filter: filter, reads: formatReads(rule.When), format: rule.Format()})
		}
		set.fields = append(set.fields, formatted)
	}
	if len(set.fields) == 0 {
		return nil, nil
	}
	return set, nil
}

func formatReads(expr platform.Expr) []string {
	var names []string
	var visit func(platform.Expr)
	visit = func(e platform.Expr) {
		if e.Ref != "" {
			name, _, _ := strings.Cut(e.Ref, "#")
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
		for _, arg := range e.Args {
			visit(arg)
		}
	}
	visit(expr)
	return names
}

func (s *FormatSet) Evaluate(record any, hidden []platform.FieldInfo) map[string]platform.FieldFormat {
	if s == nil {
		return nil
	}
	v := reflect.ValueOf(record)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return nil
	}
	unreadable := func(name string) bool {
		return slices.ContainsFunc(hidden, func(f platform.FieldInfo) bool { return f.Name == name })
	}
	var out map[string]platform.FieldFormat
	for _, field := range s.fields {
		if unreadable(field.name) {
			continue
		}
	rules:
		for _, rule := range field.rules {
			for _, name := range rule.reads {
				if unreadable(name) {
					continue rules
				}
			}
			if rule.filter.Match(v) {
				if out == nil {
					out = map[string]platform.FieldFormat{}
				}
				out[field.name] = rule.format
				break
			}
		}
	}
	return out
}

func (et *Type) Formats() *FormatSet { return et.formats }
