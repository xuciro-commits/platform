package authoring

import (
	"encoding/json"
	"fmt"
	"math"
	ruleprotocol "platformserver/kernel/protocol"
	"regexp"
	"strconv"
	"strings"

	ruleexpr "platformserver/expr"
	"platformserver/platform"
)

const (
	tableRows = 500
	tableCols = 16
)

var columnName = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)

func CheckTableShape(tb Table, lookup func(string) (platform.EntityInfo, bool), checkCollections func(platform.RuleReadSpec, string, func(string) (platform.EntityInfo, bool)) error, engines ...platform.RuleEngine) error {
	if strings.TrimSpace(tb.Title) == "" || len(tb.Inputs) == 0 || len(tb.Outputs) == 0 || len(tb.Inputs)+len(tb.Outputs) > tableCols || len(tb.Rows) == 0 || len(tb.Rows) > tableRows {
		return fmt.Errorf("a table needs a title, 1 to %d columns and 1 to %d rows", tableCols, tableRows)
	}
	if tb.Proposal && (len(tb.Outputs) != 1 || tb.Outputs[0].Name != "proposal" || tb.Outputs[0].Type != "text") {
		return fmt.Errorf("an edit-proposal table has one text result column named proposal")
	}
	seen := map[string]bool{}
	for _, col := range append(append([]TableColumn{}, tb.Inputs...), tb.Outputs...) {
		if !columnName.MatchString(col.Name) || seen[col.Name] || col.Type != "text" && col.Type != "number" && col.Type != "boolean" {
			return fmt.Errorf("column %q needs a unique lower-case name and a type of text, number or boolean", col.Name)
		}
		seen[col.Name] = true
	}
	plan, err := tb.RulePlan()
	if err != nil {
		return err
	}
	if err := checkCollections(plan.Reads, "table "+tb.Name, lookup); err != nil {
		return err
	}
	for i, row := range plan.Rows {
		if row.Condition.Kind != "boolean" {
			return fmt.Errorf("row %d needs boolean", i+1)
		}
		if _, err := ruleprotocol.RuleSchema(row.Condition, plan.Reads, lookup, engines...); err != nil {
			return fmt.Errorf("row %d: %w", i+1, err)
		}
	}
	for i, row := range tb.Rows {
		if (len(row.When) != len(tb.Inputs) && len(row.Expressions) != len(tb.Inputs)) || len(row.Then) != len(tb.Outputs) {
			return fmt.Errorf("row %d does not fill every column", i+1)
		}
		for j, cell := range row.When {
			if _, err := valueParseCondition(cell, tb.Inputs[j].Type); err != nil {
				return fmt.Errorf("row %d, %s: %w", i+1, tb.Inputs[j].Name, err)
			}
		}
		for j, cell := range row.Then {
			if tb.Proposal {
				if err := platform.EditProposalSchema().Validate(json.RawMessage(cell), 16<<10); err != nil {
					return fmt.Errorf("row %d, proposal: %w", i+1, err)
				}
			} else if _, err := castCell(cell, tb.Outputs[j].Type); err != nil {
				return fmt.Errorf("row %d, %s: %w", i+1, tb.Outputs[j].Name, err)
			}
		}
	}
	if len(tb.Default) > 0 {
		if len(tb.Default) != len(tb.Outputs) {
			return fmt.Errorf("the default result fills every result column")
		}
		for j, cell := range tb.Default {
			if tb.Proposal {
				if err := platform.EditProposalSchema().Validate(json.RawMessage(cell), 16<<10); err != nil {
					return fmt.Errorf("default, proposal: %w", err)
				}
			} else if _, err := castCell(cell, tb.Outputs[j].Type); err != nil {
				return fmt.Errorf("default, %s: %w", tb.Outputs[j].Name, err)
			}
		}
	}
	return nil
}

type Table struct {
	platform.Record
	Name        string                 `json:"name" field:"required,search"`
	Title       string                 `json:"title" field:"required,search"`
	Description string                 `json:"description,omitempty" field:"search"`
	Reads       *platform.RuleReadSpec `json:"reads,omitempty" type:"json" field:"aside"`
	Inputs      []TableColumn          `json:"inputs" title:"Condition columns"`
	Outputs     []TableColumn          `json:"outputs" title:"Result columns"`
	Proposal    bool                   `json:"proposal,omitempty" title:"Return edit proposals"`
	Rows        []TableRow             `json:"rows"`
	Default     []string               `json:"default,omitempty" title:"Default result"`
	State       string                 `json:"state" field:"readonly"`
	Version     int                    `json:"version,omitempty" field:"readonly"`
	Versions    []string               `json:"versions,omitempty" field:"readonly" type:"json"`
	Published   string                 `json:"published,omitempty" field:"readonly" type:"json"`
}

type TableColumn struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type"`
}

type TableRow struct {
	Expressions []platform.Expr `json:"expressions,omitempty" type:"json" field:"aside"`
	When        []string        `json:"when,omitempty"`
	Then        []string        `json:"then"`
}

func valueColumnSchema(cols []TableColumn) platform.ValueSchema {
	s := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	for _, col := range cols {
		typ := map[string]string{"text": "string", "number": "number", "boolean": "boolean"}[col.Type]
		s.Properties[col.Name] = platform.ValueSchema{Type: typ, Description: col.Title}
		s.Required = append(s.Required, col.Name)
	}
	return s
}

func (tb Table) Definition() platform.Operation {
	plan, _ := tb.RulePlan()
	output := valueColumnSchema(tb.Outputs)
	if tb.Proposal {
		output = platform.EditProposalSchema()
	}
	return platform.Operation{Rules: plan, Name: tb.Name, Title: tb.Title, Description: tb.Description, Input: valueColumnSchema(tb.Inputs), Output: output, Roles: []string{"builder", "user"},
		Binding: platform.OperationBinding{Kind: "native"}, Limits: platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 1, MaxInputBytes: 64 << 10, MaxOutputBytes: 16 << 10}}
}

type valueCondition struct {
	expr *platform.Expr
	op   string
	a, b any
	set  []any
}

func valueParseCondition(cell, typ string) (valueCondition, error) {
	text := strings.TrimSpace(cell)
	if strings.HasPrefix(text, "{") {
		var e platform.Expr
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			return valueCondition{}, err
		}
		if e.Kind != "boolean" {
			return valueCondition{}, fmt.Errorf("condition needs boolean Expr")
		}
		return valueCondition{expr: &e}, nil
	}
	if text == "" || text == "*" {
		return valueCondition{}, nil
	}
	for _, op := range []string{">=", "<=", "!=", ">", "<", "="} {
		if rest, ok := strings.CutPrefix(text, op); ok {
			if (op == "<" || op == ">" || op == "<=" || op == ">=") && typ != "number" {
				return valueCondition{}, fmt.Errorf("%q compares, which only a number column does", text)
			}
			v, err := ruleCellLiteral(rest, typ)
			return valueCondition{op: op, a: v}, err
		}
	}
	if lo, hi, ok := strings.Cut(text, ".."); ok && typ == "number" {
		a, errA := ruleCellLiteral(lo, typ)
		b, errB := ruleCellLiteral(hi, typ)
		if errA != nil || errB != nil {
			return valueCondition{}, fmt.Errorf("%q is not a range of numbers", text)
		}
		return valueCondition{op: "range", a: a, b: b}, nil
	}
	if strings.Contains(text, "|") {
		c := valueCondition{op: "in"}
		for _, part := range strings.Split(text, "|") {
			v, err := ruleCellLiteral(part, typ)
			if err != nil {
				return valueCondition{}, err
			}
			c.set = append(c.set, v)
		}
		return c, nil
	}
	v, err := ruleCellLiteral(text, typ)
	return valueCondition{op: "=", a: v}, err
}

func (c valueCondition) Expr(name, kind string) platform.Expr {
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
		if tb.Proposal {
			proposal := json.RawMessage(cells[0])
			if err := platform.EditProposalSchema().Validate(proposal, 16<<10); err != nil {
				return nil, err
			}
			return proposal, nil
		}
		out := map[string]any{}
		for i, col := range tb.Outputs {
			v, err := castCell(cells[i], col.Type)
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
				cond, err := valueParseCondition(row.When[j], col.Type)
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
			cond, err := valueParseCondition(row.When[j], col.Type)
			if err != nil {
				return err
			}
			row.Expressions = append(row.Expressions, cond.Expr(col.Name, col.Type))
		}
		row.When = nil
	}
	return nil
}

func ruleCellLiteral(raw, kind string) (any, error) {
	if kind != "number" {
		return castCell(raw, kind)
	}
	text := strings.TrimSpace(raw)
	if _, err := ruleexpr.ParseDecimal(text); err != nil {
		return nil, err
	}
	return text, nil
}

func castCell(cell, typ string) (any, error) {
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

func castValue(value any, typ string) (any, error) {
	switch x := value.(type) {
	case string:
		return castCell(x, typ)
	case float64:
		if typ == "number" {
			return x, nil
		}
		return castCell(strconv.FormatFloat(x, 'f', -1, 64), typ)
	case bool:
		if typ == "boolean" {
			return x, nil
		}
		return castCell(strconv.FormatBool(x), typ)
	case json.Number:
		return castCell(x.String(), typ)
	}
	return nil, fmt.Errorf("no value")
}

func TableInput(reads platform.RuleReadSpec, legacy bool, values map[string]any) platform.ExprContext {
	facts := platform.ExprContext{Refs: map[string]platform.ExprValue{}}
	for name, col := range reads.Inputs {
		v, ok := values[name]
		if legacy {
			v, _ = castValue(v, col.Kind)
		}
		if ok {
			facts.Refs["input."+name] = platform.ExprPresent(col.Kind, v)
			facts.Refs[name] = facts.Refs["input."+name]
		}
	}
	return facts
}
