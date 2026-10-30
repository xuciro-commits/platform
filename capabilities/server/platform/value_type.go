package platform

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	maxValuePattern = 256
	maxValueChoices = 64
	maxValueChoice  = 256
)

var valueBases = []string{"text", "longtext", "integer", "decimal", "date", "datetime"}

type ValueType struct {
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Base        string           `json:"base" enum:"text,longtext,integer,decimal,date,datetime"`
	Constraints InputConstraints `json:"constraints,omitzero"`
	Pattern     string           `json:"pattern,omitempty"`
	Choices     []string         `json:"choices,omitempty"`
}

func ValueBases() []string { return slices.Clone(valueBases) }

func valueInputType(base string) string {
	switch base {
	case "integer":
		return "integer"
	case "decimal":
		return "number"
	case "date", "datetime":
		return base
	}
	return "string"
}

func (v ValueType) Check() error {
	if !pageNodeID.MatchString(v.Name) || v.Title == "" || len(v.Title) > 1024 || strings.ContainsAny(v.Title, "\"\\") || v.Description == "" || len(v.Description) > 16384 || !slices.Contains(valueBases, v.Base) {
		return fmt.Errorf("value type needs an identity, base type and bounded meaning")
	}
	textual := v.Base == "text" || v.Base == "longtext"
	if v.Constraints.Before != "" || v.Constraints.After != "" || v.Constraints.DateTime {
		return fmt.Errorf("value type constraints cannot compare other fields or accept local date-times")
	}
	if err := CheckInputs([]Field{{Name: "value", Type: valueInputType(v.Base), Constraints: &v.Constraints}}); err != nil {
		return fmt.Errorf("value type constraints: %w", err)
	}
	if v.Pattern != "" {
		if !textual {
			return fmt.Errorf("only a text value type takes a pattern")
		}
		if err := checkPattern(v.Pattern); err != nil {
			return err
		}
	}
	if len(v.Choices) > 0 {
		if !textual || len(v.Choices) > maxValueChoices {
			return fmt.Errorf("only a text value type takes at most %d listed values", maxValueChoices)
		}
		for i, choice := range v.Choices {
			if strings.TrimSpace(choice) == "" || len(choice) > maxValueChoice || slices.Contains(v.Choices[:i], choice) {
				return fmt.Errorf("value type listed values must be unique, non-empty and at most %d bytes", maxValueChoice)
			}
		}
	}
	return nil
}

func (v ValueType) CheckField(f FieldInfo) error {
	if err := v.Check(); err != nil {
		return err
	}
	if f.Type != v.Base || f.Ref != "" || len(f.Choices) != 0 || f.Constraints != nil || f.Property != nil {
		return fmt.Errorf("field %s differs from its bound value type", f.Name)
	}
	return nil
}

func (v ValueType) Enforced() (*InputConstraints, []string) {
	c := v.Constraints
	c.Pattern = v.Pattern
	return &c, slices.Clone(v.Choices)
}

func checkPattern(pattern string) error {
	if len(pattern) > maxValuePattern || !utf8.ValidString(pattern) {
		return fmt.Errorf("pattern exceeds %d bytes", maxValuePattern)
	}
	if _, err := patternProgram(pattern); err != nil {
		return fmt.Errorf("pattern is not a valid RE2 expression")
	}
	return nil
}

var patternPrograms sync.Map

func patternProgram(pattern string) (*regexp.Regexp, error) {
	if cached, ok := patternPrograms.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	program, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	cached, _ := patternPrograms.LoadOrStore(pattern, program)
	return cached.(*regexp.Regexp), nil
}

func (d Definition) ValueVersion(version string) *Definition {
	if d.Version == version && d.ValueType != nil {
		return &d
	}
	v, ok := d.ValueVersions[version]
	if !ok {
		return nil
	}
	d.Version = version
	d.ValueType = &v
	d.ValueVersions = nil
	return &d
}

type ValueTypeRuntime interface {
	ResolveValueType(AssetBinding) (ValueType, bool)
}

func (c Caller) ResolveValueType(binding AssetBinding) (ValueType, bool) {
	if r, ok := c.rt.(ValueTypeRuntime); ok {
		return r.ResolveValueType(binding)
	}
	return ValueType{}, false
}

func ResolveValueTypes(fields []Field, resolve func(AssetBinding) (ValueType, bool)) ([]Field, bool) {
	if !slices.ContainsFunc(fields, func(f Field) bool { return f.ValueType != nil }) {
		return fields, true
	}
	out := slices.Clone(fields)
	for i, f := range out {
		if f.ValueType == nil {
			continue
		}
		v, ok := resolve(*f.ValueType)
		if !ok || valueInputType(v.Base) != f.Type {
			return nil, false
		}
		out[i].Constraints, out[i].Choices = v.Enforced()
	}
	return out, true
}
