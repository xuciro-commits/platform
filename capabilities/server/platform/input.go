package platform

import (
	"fmt"
	"math"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
)

var inputInstant = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d:[0-5]\d(\.\d{1,9})?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$`)

// CheckInputs validates declarations without evaluating business rules.
func CheckInputs(fields []Field) error {
	byName := map[string]Field{}
	for _, f := range fields {
		if _, ok := byName[f.Name]; ok {
			return fmt.Errorf("duplicate input %q", f.Name)
		}
		byName[f.Name] = f
	}
	seenEnds := map[string]bool{}
	for _, f := range fields {
		if len(f.Group) > 128 {
			return fmt.Errorf("%s group exceeds bounds", f.Name)
		}
		c := f.Constraints
		if c != nil {
			if c.DateTime && f.Type != "date" {
				return fmt.Errorf("%s does not take a lodging date or local time", f.Name)
			}
			numeric := f.Type == "integer" || f.Type == "number"
			if (c.Min != nil || c.Max != nil) && !numeric {
				return fmt.Errorf("%s has nonnumeric bounds", f.Name)
			}
			if c.Min != nil && (math.IsNaN(*c.Min) || math.IsInf(*c.Min, 0)) || c.Max != nil && (math.IsNaN(*c.Max) || math.IsInf(*c.Max, 0)) {
				return fmt.Errorf("%s has invalid bounds", f.Name)
			}
			if c.ExclusiveMin && c.Min == nil || c.ExclusiveMax && c.Max == nil || c.Min != nil && c.Max != nil && (*c.Min > *c.Max || *c.Min == *c.Max && (c.ExclusiveMin || c.ExclusiveMax)) {
				return fmt.Errorf("%s has empty bounds", f.Name)
			}
			if (c.MinLength != nil || c.MaxLength != nil) && f.Type != "string" {
				return fmt.Errorf("%s has nontext length", f.Name)
			}
			for _, n := range []*int{c.MinLength, c.MaxLength} {
				if n != nil && (*n < 0 || *n > 4096) {
					return fmt.Errorf("%s has invalid length", f.Name)
				}
			}
			if c.MinLength != nil && c.MaxLength != nil && *c.MinLength > *c.MaxLength {
				return fmt.Errorf("%s has empty length bounds", f.Name)
			}
			for _, name := range []string{c.Before, c.After} {
				if name != "" {
					other, ok := byName[name]
					if !ok || name == f.Name || other.Type != f.Type || !slices.Contains([]string{"date", "datetime", "integer", "number"}, f.Type) {
						return fmt.Errorf("%s has invalid comparison %q", f.Name, name)
					}
				}
			}
		}
		if f.Range != nil {
			if seenEnds[f.Range.End] {
				return fmt.Errorf("%s repeats a date range end", f.Name)
			}
			seenEnds[f.Range.End] = true
			other, ok := byName[f.Range.End]
			if !ok || other.Name == f.Name || f.Type != "date" || other.Type != "date" || other.Range != nil {
				return fmt.Errorf("%s has invalid date range", f.Name)
			}
		}
	}
	state := map[string]uint8{}
	var visit func(string) bool
	visit = func(name string) bool {
		if state[name] == 1 {
			return false
		}
		if state[name] == 2 {
			return true
		}
		state[name] = 1
		if c := byName[name].Constraints; c != nil {
			for _, dependency := range []string{c.Before, c.After} {
				if dependency != "" && !visit(dependency) {
					return false
				}
			}
		}
		state[name] = 2
		return true
	}
	for name := range byName {
		if !visit(name) {
			return fmt.Errorf("%s has cyclic input dependencies", name)
		}
	}
	return nil
}

// InputIssues uses the exact submitted values. Empty optional edit fields are
// allowed; relation checks wait for both endpoints. Replay bypasses this check.
func InputIssues(fields []Field, values map[string]any) []FieldIssue {
	issues := []FieldIssue{}
	dependencies := map[string]bool{}
	for _, f := range fields {
		if c := f.Constraints; c != nil {
			dependencies[c.Before] = true
			dependencies[c.After] = true
		}
	}
	valid := map[string]bool{}
	add := func(f Field, code, message string, related string) {
		if len(issues) >= 64 {
			return
		}
		issue := FieldIssue{Code: code, Message: message, Path: []string{f.Name}}
		if related != "" {
			issue.RelatedPaths = [][]string{{related}}
		}
		issues = append(issues, issue)
	}
	for _, f := range fields {
		v, ok := values[f.Name]
		c := f.Constraints
		// Existing unannotated owner contracts keep their original checks.
		if c == nil {
			if !dependencies[f.Name] {
				continue
			}
			c = &InputConstraints{}
		}
		text, isText := v.(string)
		empty := !ok || v == nil || isText && strings.TrimSpace(text) == ""
		if empty {
			if f.Required {
				add(f, "required", "Enter a value.", "")
			}
			continue
		}
		valid[f.Name] = true
		bad := false
		switch f.Type {
		case "string":
			bad = !isText
		case "date":
			_, err := time.Parse(time.DateOnly, text)
			bad = !isText || err != nil || strings.HasPrefix(text, "0000-")
			if bad && c.DateTime {
				_, err = time.Parse("2006-01-02T15:04", text)
				bad = !isText || err != nil || strings.HasPrefix(text, "0000-")
			}
		case "datetime":
			_, err := time.Parse(time.RFC3339Nano, text)
			bad = !isText || err != nil || !inputInstant.MatchString(text) || strings.HasPrefix(text, "0000-") || strings.HasSuffix(text, "-00:00")
		case "integer", "number":
			n, yes := v.(float64)
			bad = !yes || math.IsNaN(n) || math.IsInf(n, 0) || f.Type == "integer" && (math.Trunc(n) != n || math.Abs(n) > 9007199254740991)
		case "boolean":
			_, yes := v.(bool)
			bad = !yes
		}
		if bad {
			valid[f.Name] = false
			add(f, "type", "Enter a valid value.", "")
			continue
		}
		if len(f.Choices) > 0 && !slices.Contains(f.Choices, text) {
			add(f, "choice", "Choose a listed value.", "")
		}
		if isText {
			n := len(utf16.Encode([]rune(text)))
			if c.MinLength != nil && n < *c.MinLength {
				add(f, "minLength", fmt.Sprintf("Use at least %d characters.", *c.MinLength), "")
			}
			if c.MaxLength != nil && n > *c.MaxLength {
				add(f, "maxLength", fmt.Sprintf("Use at most %d characters.", *c.MaxLength), "")
			}
		}
		if n, yes := v.(float64); yes {
			if c.Min != nil && (n < *c.Min || c.ExclusiveMin && n == *c.Min) {
				add(f, "min", fmt.Sprintf("Value must be %s %g.", map[bool]string{false: "at least", true: "greater than"}[c.ExclusiveMin], *c.Min), "")
			}
			if c.Max != nil && (n > *c.Max || c.ExclusiveMax && n == *c.Max) {
				add(f, "max", fmt.Sprintf("Value must be %s %g.", map[bool]string{false: "at most", true: "less than"}[c.ExclusiveMax], *c.Max), "")
			}
		}
	}
	for _, f := range fields {
		c := f.Constraints
		if c == nil || !valid[f.Name] {
			continue
		}
		for _, relation := range []struct {
			name   string
			before bool
		}{{c.Before, true}, {c.After, false}} {
			if relation.name == "" || !valid[relation.name] {
				continue
			}
			v, other := values[f.Name], values[relation.name]
			cmp := 0
			switch x := v.(type) {
			case string:
				y := other.(string)
				cmp = strings.Compare(x, y)
				if f.Type == "datetime" {
					a, _ := time.Parse(time.RFC3339Nano, x)
					b, _ := time.Parse(time.RFC3339Nano, y)
					cmp = a.Compare(b)
				}
			case float64:
				y := other.(float64)
				if x < y {
					cmp = -1
				} else if x > y {
					cmp = 1
				}
			}
			if relation.before && cmp > 0 || !relation.before && cmp < 0 || cmp == 0 && !c.Inclusive {
				word := "after"
				if relation.before {
					word = "before"
				}
				if c.Inclusive {
					word += " or equal to"
				}
				add(f, "relation", fmt.Sprintf("Value must be %s %s.", word, relation.name), relation.name)
			}
		}
	}
	return issues
}

// RefuseFields attaches diagnostics to this decision's private runtime; kernel
// errors and the contract remain unchanged.
func (c Caller) RefuseFields(issues []FieldIssue) *kernel.Error {
	err := Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Correct the highlighted fields.")
	if c.inputRefused != nil {
		c.inputRefused(err, issues)
	}
	if sink, ok := c.rt.(interface {
		InputRefused(*kernel.Error, []FieldIssue)
	}); ok {
		sink.InputRefused(err, issues)
	}
	return err
}
