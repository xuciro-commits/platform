package protocol

import (
	"fmt"
	"math"
	valuechecks "platformserver/kernel/values"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
)

func InputIssues(fields []platform.Field, values map[string]any) []platform.FieldIssue {
	issues := []platform.FieldIssue{}
	dependencies := map[string]bool{}
	for _, f := range fields {
		if c := f.Constraints; c != nil {
			dependencies[c.Before] = true
			dependencies[c.After] = true
		}
	}
	valid := map[string]bool{}
	add := func(f platform.Field, code, message string, related string) {
		if len(issues) >= 64 {
			return
		}
		issue := platform.FieldIssue{Code: code, Message: message, Path: []string{f.Name}}
		if related != "" {
			issue.RelatedPaths = [][]string{{related}}
		}
		issues = append(issues, issue)
	}
	for _, f := range fields {
		v, ok := values[f.Name]
		text, isText := v.(string)
		empty := !ok || v == nil || isText && strings.TrimSpace(text) == ""
		if empty && f.Required {
			add(f, "required", "Enter a value.", "")
			continue
		}
		c := f.Constraints
		if c == nil {
			if !dependencies[f.Name] {
				continue
			}
			c = &platform.InputConstraints{}
		}
		if empty {
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
			bad = !isText || err != nil || !valuechecks.HasInstantFormat(text) || strings.HasPrefix(text, "0000-") || strings.HasSuffix(text, "-00:00")
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
			if c.Pattern != "" {
				if program, err := valuechecks.PatternProgram(c.Pattern); err != nil || !program.MatchString(text) {
					add(f, "pattern", "Use the expected format.", "")
				}
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
		if (f.Type == "date" || f.Type == "datetime") && (c.Earliest != "" || c.Latest != "") {
			value, _ := valuechecks.AbsoluteDate(f.Type, text)
			if c.Earliest != "" {
				earliest, _ := valuechecks.AbsoluteDate(f.Type, c.Earliest)
				if value.Before(earliest) {
					add(f, "earliest", fmt.Sprintf("Value must be on or after %s.", c.Earliest), "")
				}
			}
			if c.Latest != "" {
				latest, _ := valuechecks.AbsoluteDate(f.Type, c.Latest)
				if value.After(latest) {
					add(f, "latest", fmt.Sprintf("Value must be on or before %s.", c.Latest), "")
				}
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
