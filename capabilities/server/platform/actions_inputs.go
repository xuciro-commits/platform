package platform

import (
	"fmt"
	"math"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/values"
	"slices"
	"time"
)

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
				return fmt.Errorf("%s does not take a date or local date-time", f.Name)
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
			if c.Earliest != "" || c.Latest != "" {
				if !slices.Contains([]string{"date", "datetime"}, f.Type) || c.DateTime {
					return fmt.Errorf("%s has nondate bounds", f.Name)
				}
				var earliest, latest time.Time
				var err error
				if c.Earliest != "" {
					earliest, err = values.AbsoluteDate(f.Type, c.Earliest)
					if err != nil {
						return fmt.Errorf("%s: %w", f.Name, err)
					}
				}
				if c.Latest != "" {
					latest, err = values.AbsoluteDate(f.Type, c.Latest)
					if err != nil {
						return fmt.Errorf("%s: %w", f.Name, err)
					}
				}
				if c.Earliest != "" && c.Latest != "" && earliest.After(latest) {
					return fmt.Errorf("%s has empty date bounds", f.Name)
				}
			}
			if (c.MinLength != nil || c.MaxLength != nil || c.Pattern != "") && f.Type != "string" {
				return fmt.Errorf("%s has nontext length or pattern", f.Name)
			}
			if c.Pattern != "" {
				if err := checkPattern(c.Pattern); err != nil {
					return fmt.Errorf("%s: %w", f.Name, err)
				}
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

func (c Caller) RefuseFields(issues []FieldIssue) *Error {
	err := Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Correct the highlighted fields.")
	if c.inputRefused != nil {
		c.inputRefused(err, issues)
	}
	if c.rt != nil {
		c.rt.InputRefused(err, issues)
	}
	return err
}
