package build

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

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Pipelines (ADR-0071, ADR-0069 I-D): a declared, closed set of steps from
// datasets to a dataset or to an object. No script: every step is data the
// builder can read back, every row that fails an expectation is quarantined
// with its reason, and a run is an ordinary journaled input like a pull.
const (
	PipelineType      = "build.pipeline"
	SchemaPipelineRan = "build.pipeline.ran"
)

// StepKinds are the transformations a pipeline may declare, in the order a
// builder usually needs them.
var StepKinds = []string{"select", "rename", "cast", "filter", "compute", "lookup", "join", "dedupe", "aggregate", "sort"}

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
	// Every is a period, or empty: run when an input gains a version, or on request.
	Every     string       `json:"every,omitempty" title:"Run every"`
	State     string       `json:"state" field:"readonly"`
	Runner    string       `json:"runner,omitempty" field:"readonly" title:"Runs as"`
	Requested bool         `json:"requested,omitempty" field:"readonly"`
	Last      *PipelineRun `json:"last,omitempty" field:"readonly" type:"json" title:"Last run"`
}

// Step is one transformation. The fields used depend on Kind:
//
//	select     Columns
//	rename     From → To
//	cast       Column, Type (string, number, boolean, date)
//	filter     Column, Op (=, !=, <, <=, >, >=, contains, empty, notempty), Value
//	compute    To, Formula (the ADR-0064 arithmetic over number columns)
//	lookup     Dataset, Column (here) = Match (there), take Columns (prefixed with As)
//	join       Dataset, Column = Match, inner unless Outer; the other side's columns prefixed with As
//	dedupe     Columns (the key; first row wins after Sort)
//	aggregate  Columns (group by), Measures (sum/min/max/count/avg of a column into To)
//	sort       Column, Desc
type Step struct {
	Kind     string    `json:"kind"`
	Columns  []string  `json:"columns,omitempty"`
	From     string    `json:"from,omitempty"`
	To       string    `json:"to,omitempty"`
	Column   string    `json:"column,omitempty"`
	Type     string    `json:"type,omitempty"`
	Op       string    `json:"op,omitempty"`
	Value    string    `json:"value,omitempty"`
	Formula  string    `json:"formula,omitempty"`
	Dataset  string    `json:"dataset,omitempty"`
	Match    string    `json:"match,omitempty"`
	As       string    `json:"as,omitempty"`
	Outer    bool      `json:"outer,omitempty"`
	Desc     bool      `json:"desc,omitempty"`
	Measures []Measure `json:"measures,omitempty"`
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
	At          time.Time        `json:"at"`
	Input       int              `json:"inputVersion"`
	Rows        int              `json:"rows"`    // rows after the steps
	Written     int              `json:"written"` // rows written to the output
	Quarantined int              `json:"quarantined"`
	Failed      int              `json:"failed"` // rows the output refused
	Error       string           `json:"error,omitempty"`
	Quarantine  []QuarantinedRow `json:"quarantine,omitempty"`
	Failures    []SourceFailure  `json:"failures,omitempty"`
	Output      int              `json:"outputVersion,omitempty"`
}

type QuarantinedRow struct {
	Reason string         `json:"reason"`
	Row    map[string]any `json:"row"`
}

func (b *Build) pipelineEntity() platform.Entity {
	return platform.Entity{Type: PipelineType, Title: "Pipeline", Plural: "Pipelines", Model: Pipeline{}, Display: "title",
		Description: "Declared steps from a dataset to a dataset or an object, with expectations that quarantine bad rows.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Check the pipeline and let the host run it.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishPipeline},
				{Name: "run", Title: "Run now", Description: "Ask for one run at the next tick.", From: []string{"published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "integrations", Payload: []platform.Field{}, Do: requestRun},
				{Name: "pause", Title: "Pause", Description: "Stop running; the definition stays.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{Builder}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func pipelineActions() []platform.Action {
	return []platform.Action{{Schema: SchemaPipelineRan, Target: PipelineType, Capability: "integrations", Title: "Keep run result", Description: "Retain what one run of a pipeline did.", Roles: []string{Builder},
		Payload: []platform.Field{{Name: "run", Type: "json", Required: true, Description: "The run's counts, quarantine and failures"}}}}
}

func requestRun(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	p, ok := record.(*Pipeline)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	p.Requested = true
	return nil
}

func (b *Build) publishPipeline(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	p, ok := record.(*Pipeline)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkPipeline(c, *p); err != nil {
		return err
	}
	p.Runner, p.Requested = c.ID, true
	return nil
}

func (b *Build) checkPipeline(c platform.Caller, p Pipeline) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(p.Name, p.ID); err != nil {
		return refuse(err.Error())
	}
	if _, ok := platform.Get[Dataset](c, p.Input); !ok {
		return refuse("The input dataset does not exist")
	}
	for i, s := range p.Steps {
		if err := checkStep(c, s); err != nil {
			return refuse("Step {n} ({kind}): {why}", i+1, s.Kind, err.Error())
		}
	}
	for _, e := range p.Expectations {
		if e.Column == "" || !slices.Contains([]string{"notnull", "unique", "in", "matches", "range"}, e.Rule) {
			return refuse("An expectation names a column and one of notnull, unique, in, matches, range")
		}
		if e.Rule == "matches" {
			if _, err := regexp.Compile(e.Value); err != nil {
				return refuse("The expectation on {column} has an invalid pattern", e.Column)
			}
		}
	}
	switch {
	case p.OutputDataset != "" && p.OutputObject != "", p.OutputDataset == "" && p.OutputObject == "":
		return refuse("A pipeline writes either a dataset or an object")
	case p.OutputDataset != "":
		if p.OutputDataset == p.Input {
			return refuse("A pipeline does not write its own input")
		}
		if _, ok := platform.Get[Dataset](c, p.OutputDataset); !ok {
			return refuse("The output dataset does not exist")
		}
	default:
		info, ok := b.lookupEntity(p.OutputObject)
		if !ok {
			return refuse("The output object {object} is not installed", p.OutputObject)
		}
		if p.Key == "" {
			return refuse("Name the column that is the record id")
		}
		_ = info
	}
	if p.Every != "" {
		every, err := time.ParseDuration(p.Every)
		if err != nil || every < time.Minute || every > 366*24*time.Hour {
			return refuse("A pipeline runs every period between 1m and a year, such as 15m, 1h or 24h")
		}
	}
	return nil
}

func checkStep(c platform.Caller, s Step) error {
	need := func(ok bool, what string) error {
		if !ok {
			return fmt.Errorf("needs %s", what)
		}
		return nil
	}
	switch s.Kind {
	case "select", "dedupe":
		return need(len(s.Columns) > 0, "columns")
	case "rename":
		return need(s.From != "" && s.To != "", "from and to")
	case "cast":
		return need(s.Column != "" && slices.Contains([]string{"string", "number", "boolean", "date"}, s.Type), "a column and a type")
	case "filter":
		return need(s.Column != "" && slices.Contains([]string{"=", "!=", "<", "<=", ">", ">=", "contains", "empty", "notempty"}, s.Op), "a column and an operator")
	case "compute":
		if s.To == "" {
			return fmt.Errorf("needs a target column")
		}
		_, err := parseFormula(s.Formula)
		return err
	case "lookup", "join":
		if s.Dataset == "" || s.Column == "" || s.Match == "" {
			return fmt.Errorf("needs a dataset, a column here and a match column there")
		}
		if _, ok := platform.Get[Dataset](c, s.Dataset); !ok {
			return fmt.Errorf("the dataset does not exist")
		}
		if s.Kind == "lookup" && len(s.Columns) == 0 {
			return fmt.Errorf("names the columns to take")
		}
		return nil
	case "aggregate":
		if len(s.Measures) == 0 {
			return fmt.Errorf("needs measures")
		}
		for _, m := range s.Measures {
			if m.To == "" || !slices.Contains([]string{"sum", "min", "max", "count", "avg"}, m.Fn) || (m.Fn != "count" && m.Column == "") {
				return fmt.Errorf("each measure is sum, min, max, avg of a column or count, into a named column")
			}
		}
		return nil
	case "sort":
		return need(s.Column != "", "a column")
	}
	return fmt.Errorf("is not one of %s", strings.Join(StepKinds, ", "))
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
func (p Pipeline) Execute(input []map[string]any, read func(dataset string) ([]map[string]any, error)) ([]map[string]any, []QuarantinedRow, error) {
	rows := make([]map[string]any, len(input)) // steps edit rows in place; the input stays as it came
	for i, r := range input {
		rows[i] = make(map[string]any, len(r))
		for k, v := range r {
			rows[i][k] = v
		}
	}
	var err error
	for i, s := range p.Steps {
		rows, err = s.apply(rows, read)
		if err != nil {
			return nil, nil, fmt.Errorf("step %d (%s): %v", i+1, s.Kind, err)
		}
	}
	kept, quarantined := p.expect(rows)
	return kept, quarantined, nil
}

func (s Step) apply(rows []map[string]any, read func(string) ([]map[string]any, error)) ([]map[string]any, error) {
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
			v, err := convert(r[s.Column], s.Type)
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
			if matches(r[s.Column], s.Op, s.Value) {
				out = append(out, r)
			}
		}
		return out, nil
	case "compute":
		node, err := parseFormula(s.Formula)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			r[s.To] = node.eval(func(name string) float64 { return numeric(r[name]) })
		}
		return rows, nil
	case "lookup", "join":
		other, err := read(s.Dataset)
		if err != nil {
			return nil, err
		}
		index := map[string][]map[string]any{}
		for _, o := range other {
			k := scalar(o[s.Match])
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
				if hits := index[scalar(r[s.Column])]; len(hits) > 0 {
					take(r, hits[0])
				}
			}
			return rows, nil
		}
		var out []map[string]any
		for _, r := range rows {
			hits := index[scalar(r[s.Column])]
			if len(hits) == 0 {
				if s.Outer {
					out = append(out, r)
				}
				continue
			}
			for _, h := range hits {
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
			k := keyOf(r, s.Columns)
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
			k := keyOf(r, s.Columns)
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
				v := numeric(r[m.Column])
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
			if fa, fb := numeric(a), numeric(b); typeOf(a) == "number" && typeOf(b) == "number" {
				less = fa < fb
			} else {
				less = scalar(a) < scalar(b)
			}
			if s.Desc {
				return !less && scalar(a) != scalar(b)
			}
			return less
		})
		return rows, nil
	}
	return nil, fmt.Errorf("unknown step")
}

func (p Pipeline) expect(rows []map[string]any) ([]map[string]any, []QuarantinedRow) {
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
			text := scalar(v)
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
				n := numeric(v)
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
		row := SourceRow{ID: scalar(r[p.Key])}
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

func (b *Build) submitPipelineRan(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Run PipelineRun `json:"run"`
		}
		p, ok := platform.Get[Pipeline](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Run.At.IsZero() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Run result has no pipeline")
		}
		return func(r *pb.ChangeRecord) {
			run := payload.Run
			p.Last, p.Requested = &run, false
			c.Put(r, p)
		}, nil
	})
}

func matches(v any, op, want string) bool {
	text := scalar(v)
	switch op {
	case "empty":
		return v == nil || text == ""
	case "notempty":
		return v != nil && text != ""
	case "contains":
		return strings.Contains(strings.ToLower(text), strings.ToLower(want))
	case "=":
		return text == want || (typeOf(v) == "number" && numeric(v) == numeric(want))
	case "!=":
		return !(text == want || (typeOf(v) == "number" && numeric(v) == numeric(want)))
	}
	if typeOf(v) == "number" {
		a, b := numeric(v), numeric(want)
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

func numeric(v any) float64 {
	switch x := v.(type) {
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

func keyOf(r map[string]any, cols []string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = scalar(r[c])
	}
	return strings.Join(parts, "\x1f")
}
