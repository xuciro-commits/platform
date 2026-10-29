package rules

import (
	"fmt"

	"strconv"
	"strings"
	"unicode"
)

type formulaNode struct {
	op          byte
	value       string
	field       string
	left, right *formulaNode
}

func parseFormula(text string) (*formulaNode, error) {
	p := &formulaParser{src: strings.TrimSpace(text)}
	if p.src == "" {
		return nil, fmt.Errorf("the formula is empty")
	}
	node, err := p.Expr()
	if err != nil {
		return nil, err
	}
	p.Skip()
	if p.pos != len(p.src) {
		return nil, fmt.Errorf("unexpected %q in the formula", p.src[p.pos:])
	}
	return node, nil
}

type formulaParser struct {
	src string
	pos int
}

func (p *formulaParser) Skip() {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
}

func (p *formulaParser) Peek() byte {
	p.Skip()
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *formulaParser) Expr() (*formulaNode, error) {
	left, err := p.Term()
	if err != nil {
		return nil, err
	}
	for {
		op := p.Peek()
		if op != '+' && op != '-' {
			return left, nil
		}
		p.pos++
		right, err := p.Term()
		if err != nil {
			return nil, err
		}
		left = &formulaNode{op: op, left: left, right: right}
	}
}

func (p *formulaParser) Term() (*formulaNode, error) {
	left, err := p.Factor()
	if err != nil {
		return nil, err
	}
	for {
		op := p.Peek()
		if op != '*' && op != '/' {
			return left, nil
		}
		p.pos++
		right, err := p.Factor()
		if err != nil {
			return nil, err
		}
		left = &formulaNode{op: op, left: left, right: right}
	}
}

func (p *formulaParser) Factor() (*formulaNode, error) {
	switch c := p.Peek(); {
	case c == '(':
		p.pos++
		node, err := p.Expr()
		if err != nil {
			return nil, err
		}
		if p.Peek() != ')' {
			return nil, fmt.Errorf("a parenthesis is not closed")
		}
		p.pos++
		return node, nil
	case c == '-':
		p.pos++
		node, err := p.Factor()
		if err != nil {
			return nil, err
		}
		return &formulaNode{op: '~', left: node}, nil
	case c >= '0' && c <= '9' || c == '.':
		start := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
			p.pos++
		}
		_, err := strconv.ParseFloat(p.src[start:p.pos], 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", p.src[start:p.pos])
		}
		return &formulaNode{op: 'n', value: p.src[start:p.pos]}, nil
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

func (n *formulaNode) Fields(into map[string]bool) {
	if n == nil {
		return
	}
	if n.op == 'f' {
		into[n.field] = true
	}
	n.left.Fields(into)
	n.right.Fields(into)
}

func (n *formulaNode) Expr(fields map[string]string) (Expr, error) {
	switch n.op {
	case 'n':
		return LiteralOf("decimal", n.value), nil
	case 'f':
		kind, ok := fields[n.field]
		if !ok {
			return Expr{}, fmt.Errorf("undeclared formula field %s", n.field)
		}
		return RefOf(kind, n.field), nil
	}
	a, err := n.left.Expr(fields)
	if err != nil {
		return a, err
	}
	op := string(n.op)
	args := []Expr{a}
	if n.op == '~' {
		op = "negate"
	} else {
		b, err := n.right.Expr(fields)
		if err != nil {
			return b, err
		}
		args = append(args, b)
	}
	return Expr{Op: op, Kind: "decimal", Args: args}, nil
}

func LowerFormula(text string, fields map[string]string) (Expr, error) {
	node, err := parseFormula(text)
	if err != nil {
		return Expr{}, err
	}
	return node.Expr(fields)
}

func FormulaFields(text string) (map[string]bool, error) {
	node, err := parseFormula(text)
	if err != nil {
		return nil, err
	}
	fields := map[string]bool{}
	node.Fields(fields)
	return fields, nil
}
