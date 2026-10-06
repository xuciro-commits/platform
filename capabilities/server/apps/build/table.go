package build

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Decision tables (ADR-0062, ADR-0057 block E): condition columns → result
// columns, first matching row wins, a default row when none does. A published
// table is a native operation of the builder - callable from a process's
// compute step, a page's compute widget or the capability API like any other
// operation - and it is the builder that evaluates it (OperationExecutor).
// A table is operational configuration: publishing installs it at once and
// keeps the published image; drafts never change what runs.
const (
	TableType = "build.table"
	tableRows = 500
	tableCols = 16
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

func (b *Build) tableEntity() platform.Entity {
	return platform.Entity{Type: TableType, Title: "Decision table", Plural: "Decision tables", Model: Table{}, Display: "title", Description: "Condition columns to result columns; the first matching row decides. Published as a native operation.",
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "compute"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Check the table and install it as an operation at once.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "compute", Payload: []platform.Field{}, Do: b.publishTable}}}}
}

func (b *Build) publishTable(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	tb, ok := record.(*Table)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkTable(*tb); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if tb.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A table may retain at most 64 published versions")
	}
	tb.Version++
	frozen := *tb
	frozen.Published = ""
	if c.Staging() {
		if err := b.host.ValidateInstallOperation(frozen.definition()); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	} else if err := b.installTable(c, frozen); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	raw, _ := json.Marshal(frozen)
	tb.Published = string(raw)
	return nil
}

func (b *Build) installTable(c platform.Caller, tb Table) error {
	if err := b.host.InstallOperation(c, tb.definition(), tb.Version); err != nil {
		return err
	}
	b.tables[tb.Name] = tb
	return nil
}

// installTables puts every published table back after a restart or snapshot.
func (b *Build) installTables() error {
	list, err := readDefinitionInventory[Table](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return err
	}
	for _, record := range list {
		if record.Published == "" || record.Archived {
			continue
		}
		tb, ok := wasPublished[Table](record.Published)
		if !ok || tb.Name != record.Name {
			return fmt.Errorf("decision table %s has a malformed publication", record.Name)
		}
		if err := b.installTable(platform.Caller{Replaying: true}, tb); err != nil {
			return err
		}
	}
	return nil
}

var columnName = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)

func (b *Build) checkTable(tb Table) error {
	if err := b.checkName(tb.Name, tb.ID); err != nil {
		return err
	}
	return b.checkTableShape(tb)
}

// checkTableShape checks everything but the name's uniqueness.
func (b *Build) checkTableShape(tb Table) error {
	if strings.TrimSpace(tb.Title) == "" || len(tb.Inputs) == 0 || len(tb.Outputs) == 0 || len(tb.Inputs)+len(tb.Outputs) > tableCols || len(tb.Rows) == 0 || len(tb.Rows) > tableRows {
		return fmt.Errorf("a table needs a title, 1 to %d columns and 1 to %d rows", tableCols, tableRows)
	}
	seen := map[string]bool{}
	for _, col := range append(append([]TableColumn{}, tb.Inputs...), tb.Outputs...) {
		if !columnName.MatchString(col.Name) || seen[col.Name] || col.Type != "text" && col.Type != "number" && col.Type != "boolean" {
			return fmt.Errorf("column %q needs a unique lower-case name and a type of text, number or boolean", col.Name)
		}
		seen[col.Name] = true
	}
	for i, row := range tb.Rows {
		if len(row.When) != len(tb.Inputs) || len(row.Then) != len(tb.Outputs) {
			return fmt.Errorf("row %d does not fill every column", i+1)
		}
		for j, cell := range row.When {
			if _, err := parseCondition(cell, tb.Inputs[j].Type); err != nil {
				return fmt.Errorf("row %d, %s: %w", i+1, tb.Inputs[j].Name, err)
			}
		}
		for j, cell := range row.Then {
			if _, err := castCell(cell, tb.Outputs[j].Type); err != nil {
				return fmt.Errorf("row %d, %s: %w", i+1, tb.Outputs[j].Name, err)
			}
		}
	}
	if len(tb.Default) > 0 {
		if len(tb.Default) != len(tb.Outputs) {
			return fmt.Errorf("the default result fills every result column")
		}
		for j, cell := range tb.Default {
			if _, err := castCell(cell, tb.Outputs[j].Type); err != nil {
				return fmt.Errorf("default, %s: %w", tb.Outputs[j].Name, err)
			}
		}
	}
	return nil
}

func columnSchema(cols []TableColumn) platform.ValueSchema {
	s := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	for _, col := range cols {
		typ := map[string]string{"text": "string", "number": "number", "boolean": "boolean"}[col.Type]
		s.Properties[col.Name] = platform.ValueSchema{Type: typ, Description: col.Title}
		s.Required = append(s.Required, col.Name)
	}
	return s
}

// definition is the table as the native operation it installs as.
func (tb Table) definition() platform.Operation {
	return platform.Operation{Name: tb.Name, Title: tb.Title, Description: tb.Description, Input: columnSchema(tb.Inputs), Output: columnSchema(tb.Outputs), Roles: []string{Builder, User},
		Binding: platform.OperationBinding{Kind: "native"}, Limits: platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 1, MaxInputBytes: 64 << 10, MaxOutputBytes: 16 << 10}}
}

// Compute evaluates a published table: the builder's only native operations.
func (b *Build) Compute(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	tb, ok := b.tables[name]
	if !ok {
		return nil, fmt.Errorf("the builder has no native operation %s", name)
	}
	var values map[string]any
	if err := json.Unmarshal(input, &values); err != nil {
		return nil, fmt.Errorf("the input is not an object")
	}
	result, err := tb.Decide(values)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// Decide is the table's rule: the first row whose every condition holds, else
// the default, else a refusal naming the inputs.
func (tb Table) Decide(values map[string]any) (map[string]any, error) {
	result := func(cells []string) (map[string]any, error) {
		out := map[string]any{}
		for j, col := range tb.Outputs {
			v, err := castCell(cells[j], col.Type)
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
			cond, err := parseCondition(row.When[j], col.Type)
			if err != nil || !cond.holds(values[col.Name], col.Type) {
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

type condition struct {
	op   string // "", =, !=, <, <=, >, >=, range, in
	a, b any
	set  []any
}

func parseCondition(cell, typ string) (condition, error) {
	text := strings.TrimSpace(cell)
	if text == "" || text == "*" {
		return condition{}, nil
	}
	for _, op := range []string{">=", "<=", "!=", ">", "<", "="} {
		if rest, ok := strings.CutPrefix(text, op); ok {
			if (op == "<" || op == ">" || op == "<=" || op == ">=") && typ != "number" {
				return condition{}, fmt.Errorf("%q compares, which only a number column does", text)
			}
			v, err := castCell(rest, typ)
			return condition{op: op, a: v}, err
		}
	}
	if lo, hi, ok := strings.Cut(text, ".."); ok && typ == "number" {
		a, errA := castCell(lo, typ)
		b, errB := castCell(hi, typ)
		if errA != nil || errB != nil {
			return condition{}, fmt.Errorf("%q is not a range of numbers", text)
		}
		return condition{op: "range", a: a, b: b}, nil
	}
	if strings.Contains(text, "|") {
		c := condition{op: "in"}
		for _, part := range strings.Split(text, "|") {
			v, err := castCell(part, typ)
			if err != nil {
				return condition{}, err
			}
			c.set = append(c.set, v)
		}
		return c, nil
	}
	v, err := castCell(text, typ)
	return condition{op: "=", a: v}, err
}

func (c condition) holds(value any, typ string) bool {
	if c.op == "" {
		return true
	}
	v, err := castValue(value, typ)
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

// castValue reads a call's input value as a column's type.
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
