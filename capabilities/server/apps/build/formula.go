package build

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Computed fields (ADR-0064, ADR-0057 block B1): an integer or decimal field
// may carry a Formula over the record's other number fields - `price * qty`,
// `(ordered - received) * 1.0` - evaluated by the platform each time the
// record is decided, never set by hand. Grammar: numbers, field names,
// + - * /, parentheses, unary minus; a missing operand counts as zero; a
// division by zero yields zero.

type formulaNode struct {
	op          byte // 'n' number, 'f' field, or + - * / ~ (negate)
	value       float64
	field       string
	left, right *formulaNode
}

func parseFormula(text string) (*formulaNode, error) {
	p := &formulaParser{src: strings.TrimSpace(text)}
	if p.src == "" {
		return nil, fmt.Errorf("the formula is empty")
	}
	node, err := p.expr()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.pos != len(p.src) {
		return nil, fmt.Errorf("unexpected %q in the formula", p.src[p.pos:])
	}
	return node, nil
}

type formulaParser struct {
	src string
	pos int
}

func (p *formulaParser) skip() {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
}

func (p *formulaParser) peek() byte {
	p.skip()
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *formulaParser) expr() (*formulaNode, error) {
	left, err := p.term()
	if err != nil {
		return nil, err
	}
	for {
		op := p.peek()
		if op != '+' && op != '-' {
			return left, nil
		}
		p.pos++
		right, err := p.term()
		if err != nil {
			return nil, err
		}
		left = &formulaNode{op: op, left: left, right: right}
	}
}

func (p *formulaParser) term() (*formulaNode, error) {
	left, err := p.factor()
	if err != nil {
		return nil, err
	}
	for {
		op := p.peek()
		if op != '*' && op != '/' {
			return left, nil
		}
		p.pos++
		right, err := p.factor()
		if err != nil {
			return nil, err
		}
		left = &formulaNode{op: op, left: left, right: right}
	}
}

func (p *formulaParser) factor() (*formulaNode, error) {
	switch c := p.peek(); {
	case c == '(':
		p.pos++
		node, err := p.expr()
		if err != nil {
			return nil, err
		}
		if p.peek() != ')' {
			return nil, fmt.Errorf("a parenthesis is not closed")
		}
		p.pos++
		return node, nil
	case c == '-':
		p.pos++
		node, err := p.factor()
		if err != nil {
			return nil, err
		}
		return &formulaNode{op: '~', left: node}, nil
	case c >= '0' && c <= '9' || c == '.':
		start := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
			p.pos++
		}
		n, err := strconv.ParseFloat(p.src[start:p.pos], 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", p.src[start:p.pos])
		}
		return &formulaNode{op: 'n', value: n}, nil
	case c >= 'a' && c <= 'z':
		start := p.pos
		for p.pos < len(p.src) && (unicode.IsLower(rune(p.src[p.pos])) || unicode.IsDigit(rune(p.src[p.pos]))) {
			p.pos++
		}
		return &formulaNode{op: 'f', field: p.src[start:p.pos]}, nil
	case c == 0:
		return nil, fmt.Errorf("the formula ends too early")
	}
	return nil, fmt.Errorf("unexpected %q in the formula", string(p.src[p.pos]))
}

func (n *formulaNode) fields(into map[string]bool) {
	if n == nil {
		return
	}
	if n.op == 'f' {
		into[n.field] = true
	}
	n.left.fields(into)
	n.right.fields(into)
}

func (n *formulaNode) eval(value func(string) float64) float64 {
	switch n.op {
	case 'n':
		return n.value
	case 'f':
		return value(n.field)
	case '~':
		return -n.left.eval(value)
	case '+':
		return n.left.eval(value) + n.right.eval(value)
	case '-':
		return n.left.eval(value) - n.right.eval(value)
	case '*':
		return n.left.eval(value) * n.right.eval(value)
	case '/':
		d := n.right.eval(value)
		if d == 0 {
			return 0
		}
		return n.left.eval(value) / d
	}
	return 0
}

// checkFormulas: a formula sits on an integer or decimal field and names only
// the object's other plain number fields.
func checkFormulas(fields []Field) error {
	numbers := map[string]bool{}
	for _, f := range fields {
		if f.Formula == "" && (f.Type == "integer" || f.Type == "decimal" || f.Type == "money") {
			numbers[f.Name] = true
		}
	}
	for _, f := range fields {
		if f.Formula == "" {
			continue
		}
		if f.Type != "integer" && f.Type != "decimal" {
			return fmt.Errorf("the field %q has a formula, which only an integer or decimal field may", f.Name)
		}
		node, err := parseFormula(f.Formula)
		if err != nil {
			return fmt.Errorf("the field %q: %v", f.Name, err)
		}
		used := map[string]bool{}
		node.fields(used)
		for name := range used {
			if !numbers[name] {
				return fmt.Errorf("the field %q computes from %q, which is not a number field of this object set by hand", f.Name, name)
			}
		}
	}
	return nil
}

// computeOf is the entity hook that fills every formula field of a record.
func computeOf(o Object) func(record any) {
	type computed struct {
		name string
		kind string
		node *formulaNode
	}
	var list []computed
	for _, f := range o.Fields {
		if f.Formula != "" {
			if node, err := parseFormula(f.Formula); err == nil {
				list = append(list, computed{f.Name, f.Type, node})
			}
		}
	}
	if len(list) == 0 {
		return nil
	}
	return func(record any) {
		v := reflect.ValueOf(record)
		if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
			return
		}
		v = v.Elem()
		read := func(name string) float64 {
			f := v.FieldByName(goName(name))
			if !f.IsValid() {
				return 0
			}
			if f.Kind() == reflect.Pointer {
				if f.IsNil() {
					return 0
				}
				f = f.Elem()
			}
			switch f.Kind() {
			case reflect.Int, reflect.Int64:
				return float64(f.Int())
			case reflect.Float64:
				return f.Float()
			case reflect.Struct: // money: its minor units
				if a := f.FieldByName("Amount"); a.IsValid() {
					return float64(a.Int())
				}
			}
			return 0
		}
		for _, c := range list {
			result := c.node.eval(read)
			if math.IsNaN(result) || math.IsInf(result, 0) {
				result = 0
			}
			f := v.FieldByName(goName(c.name))
			if !f.IsValid() {
				continue
			}
			if f.Kind() == reflect.Pointer {
				if f.IsNil() {
					f.Set(reflect.New(f.Type().Elem()))
				}
				f = f.Elem()
			}
			if c.kind == "integer" {
				f.SetInt(int64(math.Round(result)))
			} else {
				f.SetFloat(result)
			}
		}
	}
}

// validateOf is the entity hook of an extension object (ADR-0058 A3): one
// extension record per base record, so the base type's pages can merge the
// extension's fields as if they were its own.
func validateOf(o Object, model any) func(c platform.Caller, record any) *kernel.Error {
	if o.Extends == "" {
		return nil
	}
	typ := reflect.TypeOf(model)
	return func(c platform.Caller, record any) *kernel.Error {
		v := reflect.ValueOf(record).Elem()
		base := v.FieldByName(goName(BaseField))
		if !base.IsValid() || base.String() == "" {
			return nil
		}
		id := v.Field(0).Interface().(platform.Record).ID
		domain, _ := json.Marshal([][]any{{BaseField, "=", base.String()}})
		found, _, err := c.FindOf(typ, platform.Query{Domain: domain})
		if err != nil {
			return nil // the base's records are not readable here; the store still checks the reference
		}
		for _, f := range found {
			if other := reflect.ValueOf(f).Field(0).Interface().(platform.Record); other.ID != id && !other.Archived {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{base} already has its {object} record", base.String(), o.Title)
			}
		}
		return nil
	}
}
