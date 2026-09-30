package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

// Binding is a typed data reference, never executable source text. Control
// edges determine scheduling; bindings only read values already in scope.
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
		if err := boundedPredicateValue(v); err != nil {
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

// Resolve reads the current token's durable frame. The definition owner must
// supply a permission-filtered subject projection, not a private record.
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

// Predicate keeps conditional meaning in structured definitions. Numeric
// comparison uses exact JSON decimals and never JavaScript coercion.
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
func (p Predicate) Test(r *Run, subject json.RawMessage) (bool, error) {
	if err := p.Check(); err != nil {
		return false, err
	}
	switch p.Op {
	case "all", "any", "not":
		result := p.Op == "all"
		for _, term := range p.Terms {
			v, err := term.Test(r, subject)
			if err != nil {
				return false, err
			}
			if p.Op == "not" {
				return !v, nil
			}
			if p.Op == "all" && !v {
				return false, nil
			}
			if p.Op == "any" && v {
				return true, nil
			}
		}
		return result, nil
	}
	left, err := p.Left.Resolve(r, subject)
	if p.Op == "exists" {
		return err == nil && !bytes.Equal(bytes.TrimSpace(left), []byte("null")), nil
	}
	if err != nil {
		return false, err
	}
	right, err := p.Right.Resolve(r, subject)
	if err != nil {
		return false, err
	}
	var a, b any
	da, db := json.NewDecoder(bytes.NewReader(left)), json.NewDecoder(bytes.NewReader(right))
	da.UseNumber()
	db.UseNumber()
	if da.Decode(&a) != nil || db.Decode(&b) != nil {
		return false, fmt.Errorf("predicate operands are invalid JSON")
	}
	if err := boundedPredicateValue(a); err != nil {
		return false, err
	}
	if err := boundedPredicateValue(b); err != nil {
		return false, err
	}
	if p.Op == "eq" || p.Op == "neq" {
		equal := jsonEqual(a, b)
		return equal == (p.Op == "eq"), nil
	}
	if p.Op == "contains" {
		switch x := a.(type) {
		case string:
			y, ok := b.(string)
			if !ok {
				return false, fmt.Errorf("contains needs two strings or an array")
			}
			return strings.Contains(x, y), nil
		case []any:
			for _, v := range x {
				if jsonEqual(v, b) {
					return true, nil
				}
			}
			return false, nil
		}
		return false, fmt.Errorf("contains needs a string or array")
	}
	var comparison int
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false, fmt.Errorf("numeric comparison needs two numbers")
		}
		an, aok := new(big.Rat).SetString(string(x))
		bn, bok := new(big.Rat).SetString(string(y))
		if !aok || !bok {
			return false, fmt.Errorf("invalid decimal comparison")
		}
		comparison = an.Cmp(bn)
	case string:
		y, ok := b.(string)
		if !ok {
			return false, fmt.Errorf("text comparison needs two strings")
		}
		comparison = strings.Compare(x, y)
	default:
		return false, fmt.Errorf("ordered comparison needs numbers or strings")
	}
	switch p.Op {
	case "gt":
		return comparison > 0, nil
	case "gte":
		return comparison >= 0, nil
	case "lt":
		return comparison < 0, nil
	case "lte":
		return comparison <= 0, nil
	}
	return false, nil
}
func jsonEqual(a, b any) bool {
	if x, ok := a.(json.Number); ok {
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		an, ao := new(big.Rat).SetString(string(x))
		bn, bo := new(big.Rat).SetString(string(y))
		return ao && bo && an.Cmp(bn) == 0
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// Exact decimal comparison has an explicit arithmetic budget. In particular,
// a short JSON string such as 1e99999999 must not allocate an enormous Rat.
func boundedPredicateValue(v any) error {
	switch value := v.(type) {
	case json.Number:
		text := string(value)
		if len(text) > 128 {
			return fmt.Errorf("predicate number exceeds 128 digits")
		}
		if index := strings.IndexAny(text, "eE"); index >= 0 {
			exponent := text[index+1:]
			if len(exponent) > 5 {
				return fmt.Errorf("predicate exponent exceeds its arithmetic bound")
			}
			n, err := strconv.Atoi(exponent)
			if err != nil || n < -308 || n > 308 {
				return fmt.Errorf("predicate exponent exceeds its arithmetic bound")
			}
		}
	case []any:
		for _, item := range value {
			if err := boundedPredicateValue(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range value {
			if err := boundedPredicateValue(item); err != nil {
				return err
			}
		}
	}
	return nil
}
