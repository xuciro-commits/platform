package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"platformserver/internal/rules"
	"slices"
	"strconv"
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
		if err := rules.BoundedPredicateValue(v); err != nil {
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

type Predicate struct {
	Op    string      `json:"op" enum:"eq,neq,gt,gte,lt,lte,contains,exists,all,any,not"`
	Left  *Binding    `json:"left,omitempty"`
	Right *Binding    `json:"right,omitempty"`
	Terms []Predicate `json:"terms,omitempty"`
}

func (p Predicate) Check() error { return p.check(0) }

func (p Predicate) check(depth int) error {
	if depth > 16 {
		return fmt.Errorf("predicate nesting exceeds its bound")
	}
	switch p.Op {
	case "all", "any", "not":
		if p.Left != nil || p.Right != nil || len(p.Terms) == 0 || len(p.Terms) > 32 || p.Op == "not" && len(p.Terms) != 1 {
			return fmt.Errorf("logical predicate needs bounded terms")
		}
		for _, term := range p.Terms {
			if err := term.check(depth + 1); err != nil {
				return err
			}
		}
	case "eq", "neq", "gt", "gte", "lt", "lte", "contains", "exists":
		if p.Left == nil || len(p.Terms) > 0 || p.Op != "exists" && p.Right == nil || p.Op == "exists" && p.Right != nil {
			return fmt.Errorf("predicate needs matching operands")
		}
		if err := p.Left.Check(); err != nil {
			return err
		}
		if p.Right != nil {
			return p.Right.Check()
		}
	default:
		return fmt.Errorf("unknown predicate operator %s", p.Op)
	}
	return nil
}

type predicateBindings struct {
	left, right Binding
	exists      bool
}

type predicateValues struct {
	run      *Run
	subject  json.RawMessage
	groups   []predicateBindings
	resolved map[int]bool
	values   map[string]rules.Value
	failure  error
}

func (v *predicateValues) Ref(name string) (rules.Value, bool) {
	indexText, _, found := strings.Cut(name, ".")
	index, err := strconv.Atoi(indexText)
	if !found || err != nil || index < 0 || index >= len(v.groups) {
		return rules.Value{}, false
	}
	group := v.groups[index]
	kind := "json"
	if group.exists {
		kind = "boolean"
	}
	if !v.resolved[index] {
		v.resolved[index] = true
		left, err := group.left.Resolve(v.run, v.subject)
		if group.exists {
			v.values[indexText+".left"] = rules.Present("boolean", err == nil && !bytes.Equal(bytes.TrimSpace(left), []byte("null")))
		} else {
			if err == nil {
				var right json.RawMessage
				right, err = group.right.Resolve(v.run, v.subject)
				if err == nil {
					v.values[indexText+".left"] = rules.PresentRaw("json", left)
					v.values[indexText+".right"] = rules.PresentRaw("json", right)
				}
			}
			if err != nil {
				v.failure = err
			}
		}
	}
	if value, ok := v.values[name]; ok {
		return value, true
	}
	if v.failure != nil {
		return rules.Value{Kind: kind, Status: "binding-error"}, true
	}
	return rules.Missing(kind), true
}

func (p Predicate) lower(groups *[]predicateBindings, schema *rules.Schema) rules.Expr {
	if p.Op == "all" || p.Op == "any" || p.Op == "not" {
		parts := make([]rules.Expr, len(p.Terms))
		for i, term := range p.Terms {
			parts[i] = term.lower(groups, schema)
		}
		if len(parts) == 1 && p.Op != "not" {
			return parts[0]
		}
		op := map[string]string{"all": "and", "any": "or", "not": "not"}[p.Op]
		return rules.Expr{Op: op, Kind: "boolean", Args: parts}
	}
	index := strconv.Itoa(len(*groups))
	group := predicateBindings{left: *p.Left, exists: p.Op == "exists"}
	if p.Right != nil {
		group.right = *p.Right
	}
	*groups = append(*groups, group)
	if group.exists {
		schema.Refs[index+".left"] = rules.Type{Kind: "boolean"}
		return rules.RefOf("boolean", index+".left")
	}
	schema.Refs[index+".left"] = rules.Type{Kind: "json"}
	schema.Refs[index+".right"] = rules.Type{Kind: "json"}
	op := map[string]string{"eq": "=", "neq": "!=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=", "contains": "contains"}[p.Op]
	return rules.Expr{Op: op, Kind: "boolean", Args: []rules.Expr{rules.RefOf("json", index+".left"), rules.RefOf("json", index+".right")}}
}

func (p Predicate) Test(r *Run, subject json.RawMessage) (bool, error) {
	if err := p.Check(); err != nil {
		return false, err
	}
	groups := []predicateBindings{}
	schema := rules.Schema{Refs: map[string]rules.Type{}}
	expr := p.lower(&groups, &schema)
	program, err := rules.CompilePredicate(expr, schema)
	if err != nil {
		return false, err
	}
	values := &predicateValues{run: r, subject: subject, groups: groups, resolved: map[int]bool{}, values: map[string]rules.Value{}}
	answer, err := program.EvalSource(values)
	if values.failure != nil {
		return false, values.failure
	}
	if issue, ok := err.(*rules.Issue); ok {
		if issue.Message == "malformed typed value" {
			return false, fmt.Errorf("predicate operands are invalid JSON")
		}
		return false, fmt.Errorf("%s", issue.Message)
	}
	return err == nil && string(answer.Value.Data) == "true", err
}
