package buildapi

import (
	"fmt"

	"strconv"
	"strings"
	"unicode"
)

type ValueFormulaNode struct {
	op          byte // 'n' number, 'f' field, or + - * / ~ (negate)
	value       float64
	field       string
	left, right *ValueFormulaNode
}

func ValueParseFormula(text string) (*ValueFormulaNode, error) {
	p := &ValueFormulaParser{src: strings.TrimSpace(text)}
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

type ValueFormulaParser struct {
	src string
	pos int
}

func (p *ValueFormulaParser) Skip() {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
}

func (p *ValueFormulaParser) Peek() byte {
	p.Skip()
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *ValueFormulaParser) Expr() (*ValueFormulaNode, error) {
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
		left = &ValueFormulaNode{op: op, left: left, right: right}
	}
}

func (p *ValueFormulaParser) Term() (*ValueFormulaNode, error) {
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
		left = &ValueFormulaNode{op: op, left: left, right: right}
	}
}

func (p *ValueFormulaParser) Factor() (*ValueFormulaNode, error) {
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
		return &ValueFormulaNode{op: '~', left: node}, nil
	case c >= '0' && c <= '9' || c == '.':
		start := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
			p.pos++
		}
		n, err := strconv.ParseFloat(p.src[start:p.pos], 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", p.src[start:p.pos])
		}
		return &ValueFormulaNode{op: 'n', value: n}, nil
	case c >= 'a' && c <= 'z':
		start := p.pos
		for p.pos < len(p.src) && (unicode.IsLower(rune(p.src[p.pos])) || unicode.IsDigit(rune(p.src[p.pos]))) {
			p.pos++
		}
		return &ValueFormulaNode{op: 'f', field: p.src[start:p.pos]}, nil
	case c == 0:
		return nil, fmt.Errorf("the formula ends too early")
	}
	return nil, fmt.Errorf("unexpected %q in the formula", string(p.src[p.pos]))
}

func (n *ValueFormulaNode) Fields(into map[string]bool) {
	if n == nil {
		return
	}
	if n.op == 'f' {
		into[n.field] = true
	}
	n.left.Fields(into)
	n.right.Fields(into)
}

func (n *ValueFormulaNode) Eval(value func(string) float64) float64 {
	switch n.op {
	case 'n':
		return n.value
	case 'f':
		return value(n.field)
	case '~':
		return -n.left.Eval(value)
	case '+':
		return n.left.Eval(value) + n.right.Eval(value)
	case '-':
		return n.left.Eval(value) - n.right.Eval(value)
	case '*':
		return n.left.Eval(value) * n.right.Eval(value)
	case '/':
		d := n.right.Eval(value)
		if d == 0 {
			return 0
		}
		return n.left.Eval(value) / d
	}
	return 0
}
