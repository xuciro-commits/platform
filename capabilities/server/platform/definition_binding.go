package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	ruleexpr "platformserver/expr"
	"slices"
	"strings"
)

type Binding struct {
	Source string          `json:"source" enum:"literal,input,subject,step,item,index,answer"`
	Step   string          `json:"step,omitempty"`
	Path   []string        `json:"path,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
}

func (b Binding) Check() error {
	if !slices.Contains([]string{"literal", "input", "subject", "step", "item", "index", "answer"}, b.Source) || len(b.Path) > 16 || len(b.Value) > 64<<10 {
		return fmt.Errorf("binding needs a supported source and bounded path/value")
	}
	if b.Source == "literal" {
		v, err := DecodeValue(b.Value, 64<<10)
		if err != nil {
			return fmt.Errorf("literal binding: %w", err)
		}
		if err := ruleexpr.BoundedPredicateValue(v); err != nil {
			return err
		}
	}
	if b.Source == "literal" && (!json.Valid(b.Value) || len(b.Path) > 0 || b.Step != "") {
		return fmt.Errorf("literal binding needs one JSON value")
	}
	if b.Source == "step" && b.Step == "" {
		return fmt.Errorf("step binding needs a node")
	}
	if b.Source != "literal" && len(b.Value) > 0 || b.Source != "step" && b.Step != "" {
		return fmt.Errorf("binding configuration does not match its source")
	}
	for _, part := range b.Path {
		if part == "" || len(part) > 128 || strings.Contains(part, "\x00") {
			return fmt.Errorf("binding path needs bounded field names")
		}
	}
	return nil
}

func (b Binding) Resolve(r *Run, subject json.RawMessage) (json.RawMessage, error) {
	if err := b.Check(); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	switch b.Source {
	case "literal":
		raw = b.Value
	case "input":
		raw = r.Data
	case "subject":
		raw = subject
	case "step":
		raw = r.Outputs[b.Step]
	case "item":
		if len(r.Frames) > 0 {
			raw = r.Frames[len(r.Frames)-1].Item
		}
	case "index":
		if len(r.Frames) > 0 {
			raw, _ = json.Marshal(r.Frames[len(r.Frames)-1].Index)
		}
	case "answer":
		raw, _ = json.Marshal(r.Answer)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("binding source %s is unavailable in this scope", b.Source)
	}
	for _, part := range b.Path {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			return nil, fmt.Errorf("binding path %s does not name an object field", strings.Join(b.Path, "."))
		}
		value, ok := object[part]
		if !ok {
			return nil, fmt.Errorf("binding field %s is unavailable", part)
		}
		raw = value
	}
	return slices.Clone(raw), nil
}

var conditionOps = map[string]string{"eq": "=", "neq": "!=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=", "contains": "contains"}

func ConditionRef(b Binding) string {
	parts := []string{b.Source}
	if b.Source == "step" {
		parts = append(parts, b.Step)
	}
	return strings.Join(append(parts, b.Path...), ".")
}

func ConditionBinding(ref string) (Binding, error) {
	parts := strings.Split(ref, ".")
	b := Binding{Source: parts[0], Path: parts[1:]}
	switch b.Source {
	case "step":
		if len(parts) < 2 {
			return Binding{}, fmt.Errorf("condition reference %s needs a node", ref)
		}
		b.Step, b.Path = parts[1], parts[2:]
	case "index", "answer":
		if len(b.Path) > 0 {
			return Binding{}, fmt.Errorf("condition reference %s has no fields", ref)
		}
	case "input", "subject", "item":
	default:
		return Binding{}, fmt.Errorf("condition reference %s names no workflow value", ref)
	}
	if len(b.Path) == 0 {
		b.Path = nil
	}
	return b, b.Check()
}

func ConditionRefs(e Expr) []string {
	var out []string
	var visit func(Expr)
	visit = func(e Expr) {
		if e.Ref != "" && !slices.Contains(out, e.Ref) {
			out = append(out, e.Ref)
		}
		for _, arg := range e.Args {
			visit(arg)
		}
		if e.In != nil {
			visit(*e.In)
		}
	}
	visit(e)
	return out
}

func ConditionBindings(e Expr) ([]Binding, error) {
	var out []Binding
	for _, ref := range ConditionRefs(e) {
		b, err := ConditionBinding(ref)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func compileCondition(e Expr, engines ...RuleEngine) (PredicateProgram, error) {
	if e.Kind != "boolean" {
		return nil, fmt.Errorf("condition must be a boolean expression")
	}
	var plain func(Expr) bool
	plain = func(e Expr) bool {
		if e.Agg != nil || e.Window != nil || e.In != nil || e.Op == "path" || strings.HasPrefix(e.Op, "$") {
			return false
		}
		return !slices.ContainsFunc(e.Args, func(arg Expr) bool { return !plain(arg) })
	}
	if !plain(e) {
		return nil, fmt.Errorf("a workflow condition compares workflow values only; it cannot read the clock, the caller or collections")
	}
	schema := ruleexpr.Schema{Refs: map[string]ruleexpr.Type{}}
	for _, ref := range ConditionRefs(e) {
		if _, err := ConditionBinding(ref); err != nil {
			return nil, err
		}
		schema.Refs[ref] = ruleexpr.Type{Kind: "json"}
	}
	return ruleEngineOf(engines).CompilePredicate(e, schema)
}

func CheckCondition(e Expr, engines ...RuleEngine) error {
	_, err := compileCondition(e, engines...)
	return err
}

type conditionValues struct {
	run     *Run
	subject json.RawMessage
}

func (v conditionValues) Ref(name string) (ruleexpr.Value, bool) {
	b, err := ConditionBinding(name)
	if err != nil {
		return ruleexpr.Value{}, false
	}
	raw, err := b.Resolve(v.run, v.subject)
	if err != nil {
		raw = json.RawMessage("null")
	}
	return ruleexpr.PresentRaw("json", raw), true
}

func TestCondition(e Expr, r *Run, subject json.RawMessage, engines ...RuleEngine) (bool, error) {
	program, err := compileCondition(e, engines...)
	if err != nil {
		return false, err
	}
	answer, err := program.EvalSource(conditionValues{run: r, subject: subject})
	if issue, ok := err.(*ruleexpr.Issue); ok {
		if issue.Message == "malformed typed value" {
			return false, fmt.Errorf("condition operands are invalid JSON")
		}
		return false, fmt.Errorf("%s", issue.Message)
	}
	return err == nil && string(answer.Value.Data) == "true", err
}

type conditionV1 struct {
	Op    string        `json:"op"`
	Left  *Binding      `json:"left,omitempty"`
	Right *Binding      `json:"right,omitempty"`
	Terms []conditionV1 `json:"terms,omitempty"`
}

func (p conditionV1) operand(b *Binding) (Expr, error) {
	if err := b.Check(); err != nil {
		return Expr{}, err
	}
	if b.Source == "literal" {
		return Expr{Kind: "json", Lit: &ruleexpr.Literal{Value: b.Value}}, nil
	}
	if slices.ContainsFunc(b.Path, func(part string) bool { return strings.Contains(part, ".") }) {
		return Expr{}, fmt.Errorf("condition path %s cannot name a field containing a dot", strings.Join(b.Path, "/"))
	}
	return ruleexpr.RefOf("json", ConditionRef(*b)), nil
}

func (p conditionV1) expr(depth int) (Expr, error) {
	if depth > 16 {
		return Expr{}, fmt.Errorf("condition nesting exceeds its bound")
	}
	switch p.Op {
	case "all", "any", "not":
		if p.Left != nil || p.Right != nil || len(p.Terms) == 0 || len(p.Terms) > 32 || p.Op == "not" && len(p.Terms) != 1 {
			return Expr{}, fmt.Errorf("logical condition needs bounded terms")
		}
		args := make([]Expr, len(p.Terms))
		for i, term := range p.Terms {
			arg, err := term.expr(depth + 1)
			if err != nil {
				return Expr{}, err
			}
			args[i] = arg
		}
		if len(args) == 1 && p.Op != "not" {
			return args[0], nil
		}
		return Expr{Op: map[string]string{"all": "and", "any": "or", "not": "not"}[p.Op], Kind: "boolean", Args: args}, nil
	}
	op, ok := conditionOps[p.Op]
	if p.Op == "exists" {
		op, ok = "is-not-null", true
	}
	if !ok {
		return Expr{}, fmt.Errorf("unknown condition operator %s", p.Op)
	}
	if p.Left == nil || len(p.Terms) > 0 || (p.Op == "exists") != (p.Right == nil) {
		return Expr{}, fmt.Errorf("condition needs matching operands")
	}
	args := []*Binding{p.Left}
	if p.Right != nil {
		args = append(args, p.Right)
	}
	e := Expr{Op: op, Kind: "boolean"}
	for _, b := range args {
		arg, err := p.operand(b)
		if err != nil {
			return Expr{}, err
		}
		e.Args = append(e.Args, arg)
	}
	return e, nil
}

func UpgradeCondition(raw json.RawMessage) (*Expr, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, err
	}
	if keys["left"] == nil && keys["right"] == nil && keys["terms"] == nil {
		var e Expr
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		return &e, nil
	}
	var legacy conditionV1
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&legacy); err != nil {
		return nil, err
	}
	e, err := legacy.expr(0)
	if err != nil {
		return nil, err
	}
	return &e, nil
}
