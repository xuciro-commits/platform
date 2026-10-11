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
	Name        string        `json:"name" field:"required,search"`
	Title       string        `json:"title" field:"required,search"`
	Description string        `json:"description,omitempty" field:"search"`
	Inputs      []TableColumn `json:"inputs" title:"Condition columns"`
	Outputs     []TableColumn `json:"outputs" title:"Result columns"`
	// Rows hold one condition cell per input and one result cell per output.
	Rows []TableRow `json:"rows"`
	// Default is the result when no row matches; empty: the call is refused.
	Default   []string `json:"default,omitempty" title:"Default result"`
	State     string   `json:"state" field:"readonly"`
	Version   int      `json:"version,omitempty" field:"readonly"`
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
	When []string `json:"when"`
	Then []string `json:"then"`
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
	return platform.Operation{Name: tb.Name, Title: tb.Title, Description: tb.Description, Input: ValueColumnSchema(tb.Inputs), Output: ValueColumnSchema(tb.Outputs), Roles: []string{Builder, User},
		Binding: platform.OperationBinding{Kind: "native"}, Limits: platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 1, MaxInputBytes: 64 << 10, MaxOutputBytes: 16 << 10}}
}

// Decide is the table's rule: the first row whose every condition holds, else
// the default, else a refusal naming the inputs.
func (tb Table) Decide(values map[string]any) (map[string]any, error) {
	result := func(cells []string) (map[string]any, error) {
		out := map[string]any{}
		for j, col := range tb.Outputs {
			v, err := ValueCastCell(cells[j], col.Type)
			if err != nil {
				return nil, err
			}
			out[col.Name] = v
		}
		return out, nil
	}
rows:
	for _, row := range tb.Rows {
		for j, col := range tb.Inputs {
			cond, err := ValueParseCondition(row.When[j], col.Type)
			if err != nil || !cond.Holds(values[col.Name], col.Type) {
				continue rows
			}
		}
		return result(row.Then)
	}
	if len(tb.Default) > 0 {
		return result(tb.Default)
	}
	raw, _ := json.Marshal(values)
	return nil, fmt.Errorf("no row of %s matches %s", tb.Name, raw)
}

type ValueCondition struct {
	op   string // "", =, !=, <, <=, >, >=, range, in
	a, b any
	set  []any
}

func ValueParseCondition(cell, typ string) (ValueCondition, error) {
	text := strings.TrimSpace(cell)
	if text == "" || text == "*" {
		return ValueCondition{}, nil
	}
	for _, op := range []string{">=", "<=", "!=", ">", "<", "="} {
		if rest, ok := strings.CutPrefix(text, op); ok {
			if (op == "<" || op == ">" || op == "<=" || op == ">=") && typ != "number" {
				return ValueCondition{}, fmt.Errorf("%q compares, which only a number column does", text)
			}
			v, err := ValueCastCell(rest, typ)
			return ValueCondition{op: op, a: v}, err
		}
	}
	if lo, hi, ok := strings.Cut(text, ".."); ok && typ == "number" {
		a, errA := ValueCastCell(lo, typ)
		b, errB := ValueCastCell(hi, typ)
		if errA != nil || errB != nil {
			return ValueCondition{}, fmt.Errorf("%q is not a range of numbers", text)
		}
		return ValueCondition{op: "range", a: a, b: b}, nil
	}
	if strings.Contains(text, "|") {
		c := ValueCondition{op: "in"}
		for _, part := range strings.Split(text, "|") {
			v, err := ValueCastCell(part, typ)
			if err != nil {
				return ValueCondition{}, err
			}
			c.set = append(c.set, v)
		}
		return c, nil
	}
	v, err := ValueCastCell(text, typ)
	return ValueCondition{op: "=", a: v}, err
}

func (c ValueCondition) Holds(value any, typ string) bool {
	if c.op == "" {
		return true
	}
	v, err := ValueCastValue(value, typ)
	if err != nil {
		return false
	}
	switch c.op {
	case "=":
		return v == c.a
	case "!=":
		return v != c.a
	case "in":
		for _, x := range c.set {
			if v == x {
				return true
			}
		}
		return false
	}
	n, a := v.(float64), c.a.(float64)
	switch c.op {
	case "<":
		return n < a
	case "<=":
		return n <= a
	case ">":
		return n > a
	case ">=":
		return n >= a
	case "range":
		return n >= a && n <= c.b.(float64)
	}
	return false
}

// castCell reads a table cell as its column's type.
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
