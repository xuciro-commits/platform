package rules

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

type RefSource interface {
	Ref(name string) (Value, bool)
}

func (c Context) ref(name string) (Value, bool) {
	if c.source != nil {
		return c.source.Ref(name)
	}
	v, ok := c.Refs[name]
	return v, ok
}

type RowProgram struct {
	program *Program
	match   func(RefSource) (bool, bool)
}

func CompileRow(e Expr, s Schema) (*RowProgram, error) {
	p, err := compile(e, s, modeRecord)
	if err != nil {
		return nil, err
	}
	return &RowProgram{program: p, match: compileRowMatch(p.expr)}, nil
}

func CompilePredicate(e Expr, s Schema) (*Program, error) { return compile(e, s, modeRecord) }

func (p *Program) EvalSource(source RefSource) (Result, error) {
	return p.Eval(Context{source: source})
}

func (r *RowProgram) Match(source RefSource) bool {
	if r.match != nil {
		if matched, known := r.match(source); known {
			return matched
		}
	}
	var reasons []Issue
	v, err := r.program.eval(r.program.expr, Context{source: source}, "$", &reasons)
	return err == nil && v.Status == "value" && (v.native == true || string(v.Data) == "true")
}

func compileRowMatch(e Expr) func(RefSource) (bool, bool) {
	switch e.Op {
	case "and", "or":
		parts := make([]func(RefSource) (bool, bool), len(e.Args))
		for i, arg := range e.Args {
			parts[i] = compileRowMatch(arg)
			if parts[i] == nil {
				return nil
			}
		}
		return func(source RefSource) (bool, bool) {
			for _, part := range parts {
				matched, known := part(source)
				if !known {
					return false, false
				}
				if e.Op == "and" && !matched || e.Op == "or" && matched {
					return matched, true
				}
			}
			return e.Op == "and", true
		}
	case "not":
		if len(e.Args) != 1 {
			return nil
		}
		child := compileRowMatch(e.Args[0])
		if child == nil {
			return nil
		}
		return func(source RefSource) (bool, bool) {
			matched, known := child(source)
			return !matched, known
		}
	case "=", "!=", "<", "<=", ">", ">=":
		if len(e.Args) != 2 || e.Args[0].Ref == "" || e.Args[1].Lit == nil {
			return nil
		}
		name, kind, lit := e.Args[0].Ref, canonicalKind(e.Args[0].Kind), e.Args[1].Lit.native
		var compareValue func(Value) (int, bool)
		switch kind {
		case "text", "choice", "reference", "date":
			constant, ok := lit.(string)
			if !ok {
				return nil
			}
			compareValue = func(v Value) (int, bool) {
				value, ok := v.native.(string)
				return strings.Compare(value, constant), ok
			}
		case "boolean":
			constant, ok := lit.(bool)
			if !ok {
				return nil
			}
			compareValue = func(v Value) (int, bool) {
				value, ok := v.native.(bool)
				if !ok {
					return 0, false
				}
				if value == constant {
					return 0, true
				}
				if value {
					return 1, true
				}
				return -1, true
			}
		case "decimal", "integer":
			constant, ok := lit.(*big.Rat)
			if !ok || !constant.IsInt() || !constant.Num().IsInt64() || constant.Num().BitLen() > 52 {
				return nil
			}
			limit := float64(constant.Num().Int64())
			compareValue = func(v Value) (int, bool) {
				value, ok := v.native.(float64)
				if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
					return 0, false
				}
				if value < limit {
					return -1, true
				}
				if value > limit {
					return 1, true
				}
				return 0, true
			}
		case "datetime":
			constant, ok := lit.(time.Time)
			if !ok {
				return nil
			}
			compareValue = func(v Value) (int, bool) {
				value, ok := v.native.(time.Time)
				return value.Compare(constant), ok
			}
		default:
			return nil
		}
		return func(source RefSource) (bool, bool) {
			value, ok := source.Ref(name)
			if !ok || value.Status != "value" {
				return false, true
			}
			if !same(value.Kind, kind) {
				return false, false
			}
			comparison, known := compareValue(value)
			if !known {
				return false, false
			}
			switch e.Op {
			case "=":
				return comparison == 0, true
			case "!=":
				return comparison != 0, true
			case "<":
				return comparison < 0, true
			case "<=":
				return comparison <= 0, true
			case ">":
				return comparison > 0, true
			default:
				return comparison >= 0, true
			}
		}
	}
	if e.Lit != nil && e.Kind == "boolean" {
		if value, ok := e.Lit.native.(bool); ok {
			return func(RefSource) (bool, bool) { return value, true }
		}
	}
	return nil
}

func cacheLiterals(e *Expr) {
	if e.Lit != nil {
		if value, err := validateValue(PresentRaw(e.Kind, e.Lit.Value), "$"); err == nil && value != nil {
			e.Lit.native = value
		}
	}
	for i := range e.Args {
		cacheLiterals(&e.Args[i])
	}
	if e.In != nil {
		cacheLiterals(e.In)
	}
	if e.Op != "in" || len(e.Args) != 2 || e.Args[1].Lit == nil || e.Args[1].Kind != "set" {
		return
	}
	items, ok := e.Args[1].Lit.native.([]any)
	if !ok {
		return
	}
	index := make(map[string]struct{}, len(items))
	for _, item := range items {
		value, err := validateValue(Present(e.Args[1].Lit.Element, item), "$")
		key, keyed := valueKey(value)
		if err != nil || !keyed {
			return
		}
		index[key] = struct{}{}
	}
	e.Args[1].Lit.index = index
}

func TextValue(kind, text string) Value { return Value{Kind: kind, Status: "value", native: text} }

func FloatValue(f float64) Value {
	return Value{Kind: "decimal", Status: "value", native: f}
}

func BoolValue(b bool) Value {
	return Value{Kind: "boolean", Status: "value", native: b}
}

func decimalFloat(f float64) *big.Rat {
	r := new(big.Rat)
	if f == math.Trunc(f) && math.Abs(f) < 1<<53 {
		r.SetInt64(int64(f))
	} else {
		r.SetString(strconv.FormatFloat(f, 'g', -1, 64))
	}
	return r
}

func TimeValue(t time.Time) Value { return Value{Kind: "datetime", Status: "value", native: t.UTC()} }

func SetValue(items []string) Value {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = item
	}
	return Value{Kind: "set", Status: "value", native: out}
}

func Null(kind string) Value { return Value{Kind: kind, Status: "null"} }
