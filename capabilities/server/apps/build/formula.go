package build

import (
	"encoding/json"
	"fmt"
	"math"
	buildapi "platformserver/internal/host/buildapi"
	"reflect"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Computed fields (ADR-0064, ADR-0057 block B1): an integer or decimal field
// may carry a Formula over the record's other number fields - `price * qty`,
// `(ordered - received) * 1.0` - evaluated by the platform each time the
// record is decided, never set by hand. Grammar: numbers, field names,
// + - * /, parentheses, unary minus; a missing operand counts as zero; a
// division by zero yields zero.

// checkFormulas: a formula sits on an integer or decimal field and names only
// the object's other plain number fields.
func checkFormulas(fields []Field) error {
	numbers := map[string]bool{}
	for _, f := range fields {
		if f.Formula == "" && (f.Type == "integer" || f.Type == "decimal" || f.Type == "money") {
			numbers[f.Name] = true
		}
	}
	for _, f := range fields {
		if f.Formula == "" {
			continue
		}
		if f.Type != "integer" && f.Type != "decimal" {
			return fmt.Errorf("the field %q has a formula, which only an integer or decimal field may", f.Name)
		}
		node, err := buildapi.ValueParseFormula(f.Formula)
		if err != nil {
			return fmt.Errorf("the field %q: %v", f.Name, err)
		}
		used := map[string]bool{}
		node.Fields(used)
		for name := range used {
			if !numbers[name] {
				return fmt.Errorf("the field %q computes from %q, which is not a number field of this object set by hand", f.Name, name)
			}
		}
	}
	return nil
}

// computeOf is the entity hook that fills every formula field of a record.
func computeOf(o Object) func(record any) {
	type computed struct {
		name string
		kind string
		node *formulaNode
	}
	var list []computed
	for _, f := range o.Fields {
		if f.Formula != "" {
			if node, err := buildapi.ValueParseFormula(f.Formula); err == nil {
				list = append(list, computed{f.Name, f.Type, node})
			}
		}
	}
	if len(list) == 0 {
		return nil
	}
	return func(record any) {
		v := reflect.ValueOf(record)
		if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
			return
		}
		v = v.Elem()
		read := func(name string) float64 {
			f := v.FieldByName(goName(name))
			if !f.IsValid() {
				return 0
			}
			if f.Kind() == reflect.Pointer {
				if f.IsNil() {
					return 0
				}
				f = f.Elem()
			}
			switch f.Kind() {
			case reflect.Int, reflect.Int64:
				return float64(f.Int())
			case reflect.Float64:
				return f.Float()
			case reflect.Struct: // money: its minor units
				if a := f.FieldByName("Amount"); a.IsValid() {
					return float64(a.Int())
				}
			}
			return 0
		}
		for _, c := range list {
			result := c.node.Eval(read)
			if math.IsNaN(result) || math.IsInf(result, 0) {
				result = 0
			}
			f := v.FieldByName(goName(c.name))
			if !f.IsValid() {
				continue
			}
			if f.Kind() == reflect.Pointer {
				if f.IsNil() {
					f.Set(reflect.New(f.Type().Elem()))
				}
				f = f.Elem()
			}
			if c.kind == "integer" {
				f.SetInt(int64(math.Round(result)))
			} else {
				f.SetFloat(result)
			}
		}
	}
}

// validateOf is the entity hook of an extension object (ADR-0058 A3): one
// extension record per base record, so the base type's pages can merge the
// extension's fields as if they were its own.
func validateOf(o Object, model any) func(c platform.Caller, record any) *kernel.Error {
	if o.Extends == "" {
		return nil
	}
	typ := reflect.TypeOf(model)
	return func(c platform.Caller, record any) *kernel.Error {
		v := reflect.ValueOf(record).Elem()
		base := v.FieldByName(goName(BaseField))
		if !base.IsValid() || base.String() == "" {
			return nil
		}
		id := v.Field(0).Interface().(platform.Record).ID
		domain, _ := json.Marshal([][]any{{BaseField, "=", base.String()}})
		found, _, err := c.FindOf(typ, platform.Query{Domain: domain})
		if err != nil {
			return nil // the base's records are not readable here; the store still checks the reference
		}
		for _, f := range found {
			if other := reflect.ValueOf(f).Field(0).Interface().(platform.Record); other.ID != id && !other.Archived {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{base} already has its {object} record", base.String(), o.Title)
			}
		}
		return nil
	}
}
