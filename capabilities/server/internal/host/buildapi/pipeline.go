package buildapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
)

// Pipelines (ADR-0024、ADR-0062 I-D): a declared, closed set of steps from
// datasets to a dataset or to an object. No script: every step is data the
// builder can read back, every row that fails an expectation is quarantined
// with its reason, and a run is an ordinary journaled input like a pull.
const (
	PipelineType      = "build.pipeline"
	SchemaPipelineRan = "build.pipeline.ran"
)

type Pipeline struct {
	platform.Record
	Name  string `json:"name" field:"required,search"`
	Title string `json:"title" field:"required,search"`
	// Input is the dataset the rows start from; Steps transform them in order.
	Input string `json:"input" field:"required" ref:"build.dataset"`
	Steps []Step `json:"steps" type:"json"`
	// Expectations are checked on the final rows; failing rows are quarantined, not written.
	Expectations []Expectation `json:"expectations,omitempty" type:"json"`
	// Output is either a dataset (raw rows) or an object with the column that is the record id.
	OutputDataset string `json:"outputDataset,omitempty" ref:"build.dataset" title:"Output dataset"`
	OutputObject  string `json:"outputObject,omitempty" title:"Output object"`
	Key           string `json:"key,omitempty" title:"Record id column"`
	// Marking (ADR-0025) is the highest marking an input carried when it ran; kept so lineage and the object gate see it.
	Marking string `json:"marking,omitempty" field:"readonly" title:"Marking"`
	// OutputEnterprise lands the rows in the enterprise model instead (ADR-0022): one element per row.
	OutputEnterprise *EnterpriseTarget `json:"outputEnterprise,omitempty" type:"json" title:"Enterprise model"`
	// Every is a period, or empty: run when an input gains a version, or on request.
	Every     string       `json:"every,omitempty" title:"Run every"`
	State     string       `json:"state" field:"readonly"`
	Runner    string       `json:"runner,omitempty" field:"readonly" title:"Runs as"`
	Requested bool         `json:"requested,omitempty" field:"readonly"`
	Last      *PipelineRun `json:"last,omitempty" field:"readonly" type:"json" title:"Last run"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// Step is one transformation. The fields used depend on Kind:
//
//	select     Columns
//	rename     From → To
//	cast       Column, Type (string, number, boolean, date)
//	filter     Column, Op (=, !=, <, <=, >, >=, contains, empty, notempty), Value
//	compute    To, Formula (the ADR-0040 arithmetic over number columns)
//	lookup     Dataset, Column (here) = Match (there), take Columns (prefixed with As)
//	join       Dataset, Column = Match, inner unless Outer; the other side's columns prefixed with As
//	dedupe     Columns (the key; first row wins after Sort)
//	aggregate  Columns (group by), Measures (sum/min/max/count/avg of a column into To)
//	sort       Column, Desc
type Step struct {
	Kind        string         `json:"kind"`
	Columns     []string       `json:"columns,omitempty"`
	From        string         `json:"from,omitempty"`
	To          string         `json:"to,omitempty"`
	Column      string         `json:"column,omitempty"`
	Type        string         `json:"type,omitempty"`
	Op          string         `json:"op,omitempty"`
	Value       string         `json:"value,omitempty"`
	FormulaExpr *platform.Expr `json:"formulaExpr,omitempty" type:"json" field:"aside"`
	Formula     string         `json:"formula,omitempty"`
	Dataset     string         `json:"dataset,omitempty"`
	Match       string         `json:"match,omitempty"`
	As          string         `json:"as,omitempty"`
	Outer       bool           `json:"outer,omitempty"`
	Desc        bool           `json:"desc,omitempty"`
	Measures    []Measure      `json:"measures,omitempty"`
}

type Measure struct {
	Fn     string `json:"fn"` // sum, min, max, count, avg
	Column string `json:"column,omitempty"`
	To     string `json:"to"`
}

// Expectation is a rule a final row must meet: notnull, unique, in (Value is
// a comma list), matches (Value is a regexp), range (Value "min..max").
type Expectation struct {
	Column string `json:"column"`
	Rule   string `json:"rule"`
	Value  string `json:"value,omitempty"`
}

// PipelineRun is what one run did.
type PipelineRun struct {
	DefinitionVersion string           `json:"definitionVersion,omitempty"`
	At                time.Time        `json:"at"`
	Input             int              `json:"inputVersion"`
	Rows              int              `json:"rows"`              // rows after the steps
	Written           int              `json:"written"`           // rows written to the output
	Marking           string           `json:"marking,omitempty"` // the highest marking of the inputs (ADR-0025)
	Merged            int              `json:"merged,omitempty"`  // rows merged into matched records (ADR-0024)
	Quarantined       int              `json:"quarantined"`
	Failed            int              `json:"failed"` // rows the output refused
	Error             string           `json:"error,omitempty"`
	Quarantine        []QuarantinedRow `json:"quarantine,omitempty"`
	Failures          []SourceFailure  `json:"failures,omitempty"`
	Output            int              `json:"outputVersion,omitempty"`
}

type QuarantinedRow struct {
	Reason string         `json:"reason"`
	Row    map[string]any `json:"row"`
}

// Due is whether a published pipeline should run at now: asked for, its period
// passed, or its input has a version newer than the last run's.
func (p Pipeline) Due(now time.Time, inputVersion int) bool {
	if p.State != "published" {
		return false
	}
	if p.Requested {
		return true
	}
	if p.Last == nil {
		return inputVersion > 0
	}
	if inputVersion > p.Last.Input {
		return true
	}
	if p.Every != "" {
		if every, err := time.ParseDuration(p.Every); err == nil && !now.Before(p.Last.At.Add(every)) {
			return true
		}
	}
	return false
}

// Execute runs the steps over rows, resolving other datasets through read,
// then applies expectations; it returns the kept rows and the quarantined ones.
func (p Pipeline) Execute(input []map[string]any, read func(dataset string) ([]map[string]any, error), rowLimit ...int) ([]map[string]any, []QuarantinedRow, error) {
	if len(rowLimit) > 0 && rowLimit[0] > 0 && len(input) > rowLimit[0] {
		return nil, nil, fmt.Errorf("pipeline input exceeds the %d row limit", rowLimit[0])
	}
	rows := make([]map[string]any, len(input)) // steps edit rows in place; the input stays as it came
	for i, r := range input {
		rows[i] = make(map[string]any, len(r))
		for k, v := range r {
			rows[i][k] = v
		}
	}
	var err error
	for i, s := range p.Steps {
		rows, err = s.Apply(rows, read, rowLimit...)
		if err != nil {
			return nil, nil, fmt.Errorf("step %d (%s): %v", i+1, s.Kind, err)
		}
	}
	kept, quarantined := p.Expect(rows)
	return kept, quarantined, nil
}

func (s Step) Apply(rows []map[string]any, read func(string) ([]map[string]any, error), rowLimit ...int) ([]map[string]any, error) {
	switch s.Kind {
	case "select":
		out := make([]map[string]any, len(rows))
		for i, r := range rows {
			n := map[string]any{}
			for _, c := range s.Columns {
				if v, ok := r[c]; ok {
					n[c] = v
				}
			}
			out[i] = n
		}
		return out, nil
	case "rename":
		for _, r := range rows {
			if v, ok := r[s.From]; ok {
				r[s.To] = v
				delete(r, s.From)
			}
		}
		return rows, nil
	case "cast":
		for _, r := range rows {
			v, err := ValueConvert(r[s.Column], s.Type)
			if err != nil {
				r[s.Column] = nil
			} else {
				r[s.Column] = v
			}
		}
		return rows, nil
	case "filter":
		out := rows[:0]
		for _, r := range rows {
			if ValueMatches(r[s.Column], s.Op, s.Value) {
				out = append(out, r)
			}
		}
		return out, nil
	case "compute":
		expr := s.FormulaExpr
		if expr == nil {
			node, err := ValueParseFormula(s.Formula)
			if err != nil {
				return nil, err
			}
			used := map[string]bool{}
			node.Fields(used)
			types := map[string]string{}
			for name := range used {
				types[name] = "decimal"
			}
			e, err := node.Expr(types)
			if err != nil {
				return nil, err
			}
			expr = &e
		}
		schema := platform.ExprSchema{Refs: map[string]platform.ExprType{}}
		var refs func(platform.Expr)
		refs = func(e platform.Expr) {
			if e.Ref != "" {
				schema.Refs[e.Ref] = platform.ExprType{Kind: e.Kind}
			}
			for _, arg := range e.Args {
				refs(arg)
			}
			if e.In != nil {
				refs(*e.In)
			}
		}
		refs(*expr)
		var program *platform.ExprProgram
		var err error
		if s.FormulaExpr == nil {
			program, err = platform.CompileFrozenFormula(*expr, schema)
		} else {
			program, err = platform.CompileExpr(*expr, schema)
		}
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			ctx := platform.ExprContext{Refs: map[string]platform.ExprValue{}}
			for name, typ := range schema.Refs {
				if s.FormulaExpr == nil {
					ctx.Refs[name] = platform.ExprPresent(typ.Kind, ValueNumeric(r[name]))
				} else if v, ok := r[name]; ok && v != nil {
					ctx.Refs[name] = platform.ExprPresent(typ.Kind, v)
				}
			}
			var result platform.ExprResult
			if s.FormulaExpr == nil {
				result, err = program.EvalFrozenFormula(ctx)
			} else {
				result, err = program.Eval(ctx)
			}
			if err != nil {
				return nil, err
			}
			v, err := platform.ExprGoValue(result.Value)
			if err != nil {
				return nil, err
			}
			r[s.To] = v
		}
		return rows, nil

	case "lookup", "join":
		other, err := read(s.Dataset)
		if err != nil {
			return nil, err
		}
		index := map[string][]map[string]any{}
		for _, o := range other {
			k := ValueScalar(o[s.Match])
			index[k] = append(index[k], o)
		}
		prefix := s.As
		if prefix == "" && s.Kind == "join" {
			prefix = s.Dataset + "."
		}
		take := func(into map[string]any, from map[string]any) {
			cols := s.Columns
			if len(cols) == 0 {
				cols = make([]string, 0, len(from))
				for k := range from {
					cols = append(cols, k)
				}
			}
			for _, c := range cols {
				if v, ok := from[c]; ok {
					into[prefix+c] = v
				}
			}
		}
		if s.Kind == "lookup" {
			for _, r := range rows {
				if hits := index[ValueScalar(r[s.Column])]; len(hits) > 0 {
					take(r, hits[0])
				}
			}
			return rows, nil
		}
		var out []map[string]any
		for _, r := range rows {
			hits := index[ValueScalar(r[s.Column])]
			if len(hits) == 0 {
				if s.Outer {
					out = append(out, r)
				}
				continue
			}
			for _, h := range hits {
				if len(rowLimit) > 0 && rowLimit[0] > 0 && len(out) >= rowLimit[0] {
					return nil, fmt.Errorf("join exceeds the %d row limit", rowLimit[0])
				}
				n := make(map[string]any, len(r)+len(h))
				for k, v := range r {
					n[k] = v
				}
				take(n, h)
				out = append(out, n)
			}
		}
		return out, nil
	case "dedupe":
		seen := map[string]bool{}
		out := rows[:0]
		for _, r := range rows {
			k := ValueKeyOf(r, s.Columns)
			if !seen[k] {
				seen[k] = true
				out = append(out, r)
			}
		}
		return out, nil
	case "aggregate":
		groups := map[string]map[string]any{}
		counts := map[string]map[string]float64{}
		var order []string
		for _, r := range rows {
			k := ValueKeyOf(r, s.Columns)
			g, ok := groups[k]
			if !ok {
				g = map[string]any{}
				for _, c := range s.Columns {
					g[c] = r[c]
				}
				groups[k], counts[k] = g, map[string]float64{}
				order = append(order, k)
			}
			for _, m := range s.Measures {
				v := ValueNumeric(r[m.Column])
				cur, has := g[m.To].(float64)
				switch m.Fn {
				case "count":
					g[m.To] = cur + 1
				case "sum", "avg":
					g[m.To] = cur + v
					counts[k][m.To]++
				case "min":
					if !has || v < cur {
						g[m.To] = v
					}
				case "max":
					if !has || v > cur {
						g[m.To] = v
					}
				}
			}
		}
		out := make([]map[string]any, 0, len(order))
		for _, k := range order {
			g := groups[k]
			for _, m := range s.Measures {
				if m.Fn == "avg" && counts[k][m.To] > 0 {
					g[m.To] = g[m.To].(float64) / counts[k][m.To]
				}
			}
			out = append(out, g)
		}
		return out, nil
	case "sort":
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i][s.Column], rows[j][s.Column]
			less := false
			if fa, fb := ValueNumeric(a), ValueNumeric(b); ValueTypeOf(a) == "number" && ValueTypeOf(b) == "number" {
				less = fa < fb
			} else {
				less = ValueScalar(a) < ValueScalar(b)
			}
			if s.Desc {
				return !less && ValueScalar(a) != ValueScalar(b)
			}
			return less
		})
		return rows, nil
	}
	return nil, fmt.Errorf("unknown step")
}

func (p Pipeline) Expect(rows []map[string]any) ([]map[string]any, []QuarantinedRow) {
	if len(p.Expectations) == 0 {
		return rows, nil
	}
	seen := map[string]map[string]bool{}
	var kept []map[string]any
	var bad []QuarantinedRow
	for _, r := range rows {
		reason := ""
		for _, e := range p.Expectations {
			v := r[e.Column]
			text := ValueScalar(v)
			switch e.Rule {
			case "notnull":
				if v == nil || text == "" {
					reason = e.Column + " is empty"
				}
			case "unique":
				if seen[e.Column] == nil {
					seen[e.Column] = map[string]bool{}
				}
				if seen[e.Column][text] {
					reason = e.Column + " repeats " + text
				}
				seen[e.Column][text] = true
			case "in":
				if !slices.Contains(strings.Split(e.Value, ","), text) {
					reason = e.Column + " is not one of " + e.Value
				}
			case "matches":
				if re, err := regexp.Compile(e.Value); err == nil && !re.MatchString(text) {
					reason = e.Column + " does not match " + e.Value
				}
			case "range":
				lo, hi, _ := strings.Cut(e.Value, "..")
				n := ValueNumeric(v)
				if l, err := strconv.ParseFloat(lo, 64); lo != "" && (err != nil || n < l) {
					reason = e.Column + " is below " + lo
				} else if h, err := strconv.ParseFloat(hi, 64); hi != "" && (err != nil || n > h) {
					reason = e.Column + " is above " + hi
				}
			}
			if reason != "" {
				break
			}
		}
		if reason != "" {
			bad = append(bad, QuarantinedRow{Reason: reason, Row: r})
		} else {
			kept = append(kept, r)
		}
	}
	return kept, bad
}

// ObjectRows turns final rows into the output object's rows: every column is
// a payload field (the key column names the record), keyed by content like a
// source pull, so a rerun over the same data decides nothing twice.
func (p Pipeline) ObjectRows(rows []map[string]any) []SourceRow {
	out := make([]SourceRow, 0, len(rows))
	for _, r := range rows {
		row := SourceRow{ID: ValueScalar(r[p.Key])}
		payload := map[string]any{}
		for k, v := range r {
			if k != p.Key && v != nil && !strings.Contains(k, ".") {
				payload[k] = v
			}
		}
		row.Payload, _ = json.Marshal(payload)
		sum := sha256.Sum256(row.Payload)
		row.Key = fmt.Sprintf("pipeline:%s:%s:%s", p.Name, row.ID, hex.EncodeToString(sum[:8]))
		if row.ID == "" {
			row.Error = "row has no id"
		}
		out = append(out, row)
	}
	return out
}

func ValueMatches(v any, op, want string) bool {
	text := ValueScalar(v)
	switch op {
	case "empty":
		return v == nil || text == ""
	case "notempty":
		return v != nil && text != ""
	case "contains":
		return strings.Contains(strings.ToLower(text), strings.ToLower(want))
	case "=":
		return text == want || (ValueTypeOf(v) == "number" && ValueNumeric(v) == ValueNumeric(want))
	case "!=":
		return !(text == want || (ValueTypeOf(v) == "number" && ValueNumeric(v) == ValueNumeric(want)))
	}
	if ValueTypeOf(v) == "number" {
		a, b := ValueNumeric(v), ValueNumeric(want)
		switch op {
		case "<":
			return a < b
		case "<=":
			return a <= b
		case ">":
			return a > b
		case ">=":
			return a >= b
		}
	}
	switch op {
	case "<":
		return text < want
	case "<=":
		return text <= want
	case ">":
		return text > want
	case ">=":
		return text >= want
	}
	return false
}

func ValueNumeric(v any) float64 {
	switch x := v.(type) {
	case json.Number:
		n, _ := x.Float64()
		return n
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return n
	}
	return 0
}

func ValueKeyOf(r map[string]any, cols []string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = ValueScalar(r[c])
	}
	return strings.Join(parts, "\x1f")
}

// StepDatasets are the other datasets steps look up or join.
func (p Pipeline) StepDatasets() []string {
	var ids []string
	for _, s := range p.Steps {
		if s.Dataset != "" {
			ids = append(ids, s.Dataset)
		}
	}
	return ids
}

func (p *Pipeline) NormalizeFormulaRules() error {
	for i := range p.Steps {
		s := &p.Steps[i]
		if s.Kind != "compute" {
			continue
		}
		if s.FormulaExpr != nil {
			if s.Formula != "" {
				return fmt.Errorf("one formula expression required")
			}
			continue
		}
		node, err := ValueParseFormula(s.Formula)
		if err != nil {
			return err
		}
		used := map[string]bool{}
		node.Fields(used)
		types := map[string]string{}
		for name := range used {
			types[name] = "decimal"
		}
		e, err := node.Expr(types)
		if err != nil {
			return err
		}
		s.FormulaExpr = &e
		s.Formula = ""
	}
	return nil
}
