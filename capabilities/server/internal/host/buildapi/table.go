package buildapi

import (
	"encoding/json"
	"fmt"
	"math"

	"strconv"
	"strings"

	"platformserver/platform"
)

type Table struct {
	platform.Record
	Name        string                 `json:"name" field:"required,search"`
	Title       string                 `json:"title" field:"required,search"`
	Description string                 `json:"description,omitempty" field:"search"`
	Reads       *platform.RuleReadSpec `json:"reads,omitempty" type:"json" field:"aside"`
	Inputs      []TableColumn          `json:"inputs" title:"Condition columns"`
	Outputs     []TableColumn          `json:"outputs" title:"Result columns"`
	// Rows hold one condition cell per input and one result cell per output.
	Rows []TableRow `json:"rows"`
	// Default is the result when no row matches; empty: the call is refused.
	Default   []string `json:"default,omitempty" title:"Default result"`
	State     string   `json:"state" field:"readonly"`
	Version   int      `json:"version,omitempty" field:"readonly"`
	Versions  []string `json:"versions,omitempty" field:"readonly" type:"json"`
	Published string   `json:"published,omitempty" field:"readonly" type:"json"`
}

type TableColumn struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type"` // text, number, boolean
}

// TableRow is one rule. A condition cell is empty or "*" for any value, or
// "= v", "!= v", "< v", "<= v", "> v", ">= v", "a..b" (inclusive) or "a|b|c".
type TableRow struct {
	Expressions []platform.Expr `json:"expressions,omitempty" type:"json" field:"aside"`
	When        []string        `json:"when,omitempty"`
	Then        []string        `json:"then"`
}

func ValueColumnSchema(cols []TableColumn) platform.ValueSchema {
	s := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	for _, col := range cols {
		typ := map[string]string{"text": "string", "number": "number", "boolean": "boolean"}[col.Type]
		s.Properties[col.Name] = platform.ValueSchema{Type: typ, Description: col.Title}
		s.Required = append(s.Required, col.Name)
	}
	return s
}

// definition is the table as the native operation it installs as.
func (tb Table) Definition() platform.Operation {
	plan, _ := tb.RulePlan()
	return platform.Operation{Rules: plan, Name: tb.Name, Title: tb.Title, Description: tb.Description, Input: ValueColumnSchema(tb.Inputs), Output: ValueColumnSchema(tb.Outputs), Roles: []string{Builder, User},
		Binding: platform.OperationBinding{Kind: "native"}, Limits: platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 1, MaxInputBytes: 64 << 10, MaxOutputBytes: 16 << 10}}
}

// Decide is the table's rule: the first row whose every condition holds, else
// the default, else a refusal naming the inputs.
func (tb Table) Decide(values map[string]any) (map[string]any, error) {
	plan, err := tb.RulePlan()
	if err != nil {
		return nil, err
	}
	facts := platform.ExprContext{Refs: map[string]platform.ExprValue{}}
	for _, col := range tb.Inputs {
		v, ok := values[col.Name]
		if len(tb.Rows) > 0 && len(tb.Rows[0].Expressions) == 0 {
			v, _ = ValueCastValue(v, col.Type)
		}
		if ok {
			facts.Refs["input."+col.Name] = platform.ExprPresent(col.Type, v)
			facts.Refs[col.Name] = facts.Refs["input."+col.Name]
		}
	}
	raw, err := plan.Decide(facts, func(string) (platform.EntityInfo, bool) { return platform.EntityInfo{}, false })
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil

}

type ValueCondition struct {
	expr *platform.Expr
	op   string // "", =, !=, <, <=, >, >=, range, in
	a, b any
	set  []any
}

func ValueParseCondition(cell, typ string) (ValueCondition, error) {
	text := strings.TrimSpace(cell)
	if strings.HasPrefix(text, "{") {
		var e platform.Expr
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			return ValueCondition{}, err
		}
		if e.Kind != "boolean" {
			return ValueCondition{}, fmt.Errorf("condition needs boolean Expr")
		}
		return ValueCondition{expr: &e}, nil
	}
	if text == "" || text == "*" {
		return ValueCondition{}, nil
	}
	for _, op := range []string{">=", "<=", "!=", ">", "<", "="} {
		if rest, ok := strings.CutPrefix(text, op); ok {
			if (op == "<" || op == ">" || op == "<=" || op == ">=") && typ != "number" {
				return ValueCondition{}, fmt.Errorf("%q compares, which only a number column does", text)
			}
			v, err := ruleCellLiteral(rest, typ)
			return ValueCondition{op: op, a: v}, err
		}
	}
	if lo, hi, ok := strings.Cut(text, ".."); ok && typ == "number" {
		a, errA := ruleCellLiteral(lo, typ)
		b, errB := ruleCellLiteral(hi, typ)
		if errA != nil || errB != nil {
			return ValueCondition{}, fmt.Errorf("%q is not a range of numbers", text)
		}
		return ValueCondition{op: "range", a: a, b: b}, nil
	}
	if strings.Contains(text, "|") {
		c := ValueCondition{op: "in"}
		for _, part := range strings.Split(text, "|") {
			v, err := ruleCellLiteral(part, typ)
			if err != nil {
				return ValueCondition{}, err
			}
			c.set = append(c.set, v)
		}
		return c, nil
	}
	v, err := ruleCellLiteral(text, typ)
	return ValueCondition{op: "=", a: v}, err
}

// Expr compiles familiar cell sugar to the common language.
func (c ValueCondition) Expr(name, kind string) platform.Expr {
	if c.expr != nil {
		return *c.expr
	}
	if c.op == "" {
		return platform.ExprLiteralValue("boolean", true)
	}
	e := platform.Expr{Op: c.op, Kind: "boolean", Args: []platform.Expr{platform.ExprReference(kind, "input."+name)}}
	if c.op == "in" {
		e.Args = append(e.Args, platform.Expr{Kind: "set", Lit: &platform.ExprLiteral{Element: kind, Value: platform.Raw(c.set)}})
	} else {
		e.Args = append(e.Args, platform.ExprLiteralValue(kind, c.a))
		if c.op == "range" {
			e.Args = append(e.Args, platform.ExprLiteralValue(kind, c.b))
		}
	}
	return e
}
func (tb Table) RulePlan() (*platform.RuleTablePlan, error) {
	s := platform.RuleReadSpec{Inputs: map[string]platform.ExprType{}}
	if tb.Reads != nil {
		s.Collections = tb.Reads.Collections
	}
	for _, col := range tb.Inputs {
		s.Inputs[col.Name] = platform.ExprType{Kind: col.Type}
	}
	p := &platform.RuleTablePlan{Reads: s}
	result := func(cells []string) (json.RawMessage, error) {
		if len(cells) != len(tb.Outputs) {
			return nil, fmt.Errorf("table result columns differ")
		}
		out := map[string]any{}
		for i, col := range tb.Outputs {
			v, err := ValueCastCell(cells[i], col.Type)
			if err != nil {
				return nil, err
			}
			out[col.Name] = v
		}
		return json.Marshal(out)
	}
	for _, row := range tb.Rows {
		args := row.Expressions
		if len(args) == 0 {
			if len(row.When) != len(tb.Inputs) {
				return nil, fmt.Errorf("table condition columns differ")
			}
			for j, col := range tb.Inputs {
				cond, err := ValueParseCondition(row.When[j], col.Type)
				if err != nil {
					return nil, err
				}
				args = append(args, cond.Expr(col.Name, col.Type))
			}
		}
		if len(args) != len(tb.Inputs) {
			return nil, fmt.Errorf("table condition columns differ")
		}
		e := args[0]
		if len(args) > 1 {
			e = platform.Expr{Op: "and", Kind: "boolean", Args: args}
		}
		raw, err := result(row.Then)
		if err != nil {
			return nil, err
		}
		p.Rows = append(p.Rows, platform.RuleTableRow{Condition: e, Result: raw})
	}
	if len(tb.Default) > 0 {
		raw, err := result(tb.Default)
		if err != nil {
			return nil, err
		}
		p.Default = raw
	}
	return p, nil
}
func (tb *Table) NormalizeRules() error {
	for i := range tb.Rows {
		row := &tb.Rows[i]
		if len(row.Expressions) > 0 {
			if len(row.When) > 0 {
				return fmt.Errorf("use one table condition representation")
			}
			continue
		}
		if len(row.When) != len(tb.Inputs) {
			return fmt.Errorf("table condition columns differ")
		}
		for j, col := range tb.Inputs {
			cond, err := ValueParseCondition(row.When[j], col.Type)
			if err != nil {
				return err
			}
			row.Expressions = append(row.Expressions, cond.Expr(col.Name, col.Type))
		}
		row.When = nil
	}
	return nil
}

func ValueCastCell(cell, typ string) (any, error) {
	text := strings.TrimSpace(cell)
	switch typ {
	case "number":
		n, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("%q is not a number", text)
		}
		return n, nil
	case "boolean":
		switch strings.ToLower(text) {
		case "true", "yes":
			return true, nil
		case "false", "no":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not true or false", text)
	}
	return text, nil
}

// castValue reads a call's input value as a column's type.
func ValueCastValue(value any, typ string) (any, error) {
	switch x := value.(type) {
	case string:
		return ValueCastCell(x, typ)
	case float64:
		if typ == "number" {
			return x, nil
		}
		return ValueCastCell(strconv.FormatFloat(x, 'f', -1, 64), typ)
	case bool:
		if typ == "boolean" {
			return x, nil
		}
		return ValueCastCell(strconv.FormatBool(x), typ)
	case json.Number:
		return ValueCastCell(x.String(), typ)
	}
	return nil, fmt.Errorf("no value")
}

func ruleCellLiteral(raw, kind string) (any, error) {
	if kind != "number" {
		return ValueCastCell(raw, kind)
	}
	text := strings.TrimSpace(raw)
	e := platform.ExprLiteralValue("number", text)
	_, err := platform.CompileExpr(e, platform.ExprSchema{})
	if err != nil {
		return nil, err
	}
	return text, nil
}
