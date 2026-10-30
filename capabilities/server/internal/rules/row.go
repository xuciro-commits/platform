package rules

import (
	"math"
	"math/big"
	"strconv"
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

type RowProgram struct{ program *Program }

func CompileRow(e Expr, s Schema) (*RowProgram, error) {
	p, err := compile(e, s, modeRecord)
	if err != nil {
		return nil, err
	}
	return &RowProgram{p}, nil
}

func (r *RowProgram) Match(source RefSource) bool {
	var reasons []Issue
	v, err := r.program.eval(r.program.expr, Context{source: source}, "$", &reasons)
	return err == nil && v.Status == "value" && (v.native == true || string(v.Data) == "true")
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
	r := new(big.Rat)
	if f == math.Trunc(f) && math.Abs(f) < 1<<53 {
		r.SetInt64(int64(f))
	} else {
		r.SetString(strconv.FormatFloat(f, 'g', -1, 64))
	}
	return Value{Kind: "decimal", Status: "value", native: r}
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
