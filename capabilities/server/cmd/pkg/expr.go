package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type exprField struct{ kind, ref string }

type exprScope struct {
	objects map[string]map[string]exprField
	self    string
	inputs  map[string]exprField
	steps   map[string]string
	sets    map[string]string
	item    string
	json    bool
}

func fieldKind(t string) string {
	switch t {
	case "longtext", "member":
		return "text"
	}
	return t
}

type exprNode struct {
	kind string
	name string
	text string
	num  float64
	list []*exprNode
	args []*exprNode
}

type exprToken struct {
	kind string
	text string
}

func lexExpr(src string) ([]exprToken, error) {
	var out []exprToken
	runes := []rune(src)
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '\'' || r == '"':
			j := i + 1
			var b strings.Builder
			for j < len(runes) && runes[j] != r {
				if runes[j] == '\\' && j+1 < len(runes) {
					j++
				}
				b.WriteRune(runes[j])
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("unterminated string")
			}
			out = append(out, exprToken{"string", b.String()})
			i = j + 1
		case unicode.IsDigit(r):
			j := i
			for j < len(runes) && (unicode.IsDigit(runes[j]) || runes[j] == '.') {
				j++
			}
			out = append(out, exprToken{"number", string(runes[i:j])})
			i = j
		case unicode.IsLetter(r) || r == '_' || r == '$':
			j := i
			for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j]) || runes[j] == '_' || runes[j] == '.' || runes[j] == '$') {
				j++
			}
			out = append(out, exprToken{"ident", string(runes[i:j])})
			i = j
		default:
			two := ""
			if i+1 < len(runes) {
				two = string(runes[i : i+2])
			}
			if two == "!=" || two == "<=" || two == ">=" {
				out = append(out, exprToken{"op", two})
				i += 2
				continue
			}
			if strings.ContainsRune("()[],=<>+-*/", r) {
				out = append(out, exprToken{"op", string(r)})
				i++
				continue
			}
			return nil, fmt.Errorf("unexpected %q", r)
		}
	}
	return out, nil
}

type exprParser struct {
	tokens []exprToken
	at     int
}

func (p *exprParser) peek() exprToken {
	if p.at < len(p.tokens) {
		return p.tokens[p.at]
	}
	return exprToken{}
}

func (p *exprParser) word(w string) bool {
	if t := p.peek(); t.kind == "ident" && t.text == w {
		p.at++
		return true
	}
	return false
}

func (p *exprParser) sym(s string) bool {
	if t := p.peek(); t.kind == "op" && t.text == s {
		p.at++
		return true
	}
	return false
}

func (p *exprParser) expect(s string) error {
	if !p.sym(s) {
		return fmt.Errorf("expected %q", s)
	}
	return nil
}

func (p *exprParser) chain(op string, next func() (*exprNode, error)) (*exprNode, error) {
	left, err := next()
	if err != nil {
		return nil, err
	}
	for p.word(op) {
		right, err := next()
		if err != nil {
			return nil, err
		}
		if left.kind == "call" && left.name == op {
			left.args = append(left.args, right)
		} else {
			left = &exprNode{kind: "call", name: op, args: []*exprNode{left, right}}
		}
	}
	return left, nil
}

func (p *exprParser) or() (*exprNode, error)  { return p.chain("or", p.and) }
func (p *exprParser) and() (*exprNode, error) { return p.chain("and", p.not) }

func (p *exprParser) not() (*exprNode, error) {
	if p.word("not") {
		inner, err := p.not()
		if err != nil {
			return nil, err
		}
		return &exprNode{kind: "call", name: "not", args: []*exprNode{inner}}, nil
	}
	return p.compare()
}

func (p *exprParser) compare() (*exprNode, error) {
	left, err := p.additive()
	if err != nil {
		return nil, err
	}
	if p.word("not") {
		if !p.word("in") {
			return nil, fmt.Errorf("expected in after not")
		}
		list, err := p.additive()
		if err != nil {
			return nil, err
		}
		return &exprNode{kind: "call", name: "not", args: []*exprNode{{kind: "call", name: "in", args: []*exprNode{left, list}}}}, nil
	}
	if p.word("in") {
		list, err := p.additive()
		if err != nil {
			return nil, err
		}
		return &exprNode{kind: "call", name: "in", args: []*exprNode{left, list}}, nil
	}
	for _, op := range []string{"=", "!=", "<=", ">=", "<", ">"} {
		if p.sym(op) {
			if p.word("empty") {
				node := &exprNode{kind: "call", name: "empty", args: []*exprNode{left}}
				if op == "=" {
					return node, nil
				}
				if op == "!=" {
					return &exprNode{kind: "call", name: "not", args: []*exprNode{node}}, nil
				}
				return nil, fmt.Errorf("empty compares only with = and !=")
			}
			right, err := p.additive()
			if err != nil {
				return nil, err
			}
			return &exprNode{kind: "call", name: op, args: []*exprNode{left, right}}, nil
		}
	}
	return left, nil
}

func (p *exprParser) binary(ops []string, next func() (*exprNode, error)) (*exprNode, error) {
	left, err := next()
	if err != nil {
		return nil, err
	}
	for {
		matched := ""
		for _, op := range ops {
			if p.sym(op) {
				matched = op
				break
			}
		}
		if matched == "" {
			return left, nil
		}
		right, err := next()
		if err != nil {
			return nil, err
		}
		left = &exprNode{kind: "call", name: matched, args: []*exprNode{left, right}}
	}
}

func (p *exprParser) additive() (*exprNode, error) {
	return p.binary([]string{"+", "-"}, p.multiplicative)
}

func (p *exprParser) multiplicative() (*exprNode, error) {
	return p.binary([]string{"*", "/"}, p.primary)
}

func (p *exprParser) primary() (*exprNode, error) {
	t := p.peek()
	switch {
	case t.kind == "op" && t.text == "(":
		p.at++
		inner, err := p.or()
		if err != nil {
			return nil, err
		}
		return inner, p.expect(")")
	case t.kind == "op" && t.text == "[":
		p.at++
		list := &exprNode{kind: "list"}
		for !p.sym("]") {
			item, err := p.primary()
			if err != nil {
				return nil, err
			}
			list.list = append(list.list, item)
			p.sym(",")
		}
		return list, nil
	case t.kind == "op" && t.text == "-":
		p.at++
		inner, err := p.primary()
		if err != nil {
			return nil, err
		}
		return &exprNode{kind: "call", name: "negate", args: []*exprNode{inner}}, nil
	case t.kind == "string":
		p.at++
		return &exprNode{kind: "string", text: t.text}, nil
	case t.kind == "number":
		p.at++
		n, err := strconv.ParseFloat(t.text, 64)
		return &exprNode{kind: "number", text: t.text, num: n}, err
	case t.kind == "ident":
		p.at++
		switch t.text {
		case "true", "false":
			return &exprNode{kind: "bool", text: t.text}, nil
		}
		if p.sym("(") {
			call := &exprNode{kind: "call", name: strings.ReplaceAll(t.text, "_", "-")}
			for !p.sym(")") {
				arg, err := p.or()
				if err != nil {
					return nil, err
				}
				call.args = append(call.args, arg)
				if !p.sym(",") && p.peek().text != ")" {
					return nil, fmt.Errorf("expected , or ) in %s()", t.text)
				}
			}
			return call, nil
		}
		return &exprNode{kind: "ref", name: t.text}, nil
	}
	return nil, fmt.Errorf("unexpected %q", t.text)
}

func parseExpr(src string) (*exprNode, error) {
	tokens, err := lexExpr(src)
	if err != nil {
		return nil, err
	}
	p := &exprParser{tokens: tokens}
	node, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.at != len(p.tokens) {
		return nil, fmt.Errorf("unexpected %q", p.peek().text)
	}
	return node, nil
}

func compileExpr(src string, s exprScope, want string) (any, error) {
	node, err := parseExpr(src)
	if err != nil {
		return nil, fmt.Errorf("expression %q: %w", src, err)
	}
	out, _, err := s.emit(node, want)
	if err != nil {
		return nil, fmt.Errorf("expression %q: %w", src, err)
	}
	return out, nil
}

func exprObject(op, kind string) *object {
	out := &object{}
	out.set("op", op)
	out.set("kind", kind)
	return out
}

func (s exprScope) literal(n *exprNode, kind string) *object {
	out := exprObject("", kind)
	lit := &object{}
	switch n.kind {
	case "number":
		lit.set("value", n.num)
	case "bool":
		lit.set("value", n.text == "true")
	default:
		lit.set("value", n.text)
	}
	out.set("lit", lit)
	return out
}

func isLiteral(n *exprNode) bool {
	return n.kind == "string" || n.kind == "number" || n.kind == "bool"
}

func numberKind(n *exprNode, want string) string {
	if want == "decimal" || strings.Contains(n.text, ".") {
		return "decimal"
	}
	return "integer"
}

func (s exprScope) resolve(name string) (exprField, string, []string, error) {
	if s.json {
		return exprField{kind: "json"}, name, nil, nil
	}
	parts := strings.Split(name, ".")
	root, rest := parts[0], parts[1:]
	var f exprField
	switch parts[0] {
	case "caller":
		if name != "caller.id" && name != "caller.tenant" {
			return exprField{}, "", nil, fmt.Errorf("%s is not a caller fact; use caller.id or caller.tenant", name)
		}
		return exprField{kind: "text"}, name, nil, nil
	case "input", "step":
		if len(parts) < 2 {
			return exprField{}, "", nil, fmt.Errorf("%s needs a name", name)
		}
		var ok bool
		if parts[0] == "input" {
			f, ok = s.inputs[parts[1]]
		} else {
			f.ref, ok = s.steps[parts[1]]
			f.kind = "reference"
		}
		if !ok && parts[0] == "input" {
			return exprField{}, "", nil, fmt.Errorf("%s is not an input of this action", name)
		}
		if !ok {
			return exprField{}, "", nil, fmt.Errorf("%s is not an earlier step", name)
		}
		root, rest = parts[0]+"."+parts[1], parts[2:]
	case "item":
		if s.item == "" || len(parts) != 2 {
			return exprField{}, "", nil, fmt.Errorf("%s is used outside an aggregate", name)
		}
		f, ok := s.objects[s.item][parts[1]]
		if !ok {
			return exprField{}, "", nil, fmt.Errorf("%s has no field %s", s.item, parts[1])
		}
		return f, name, nil, nil
	default:
		var ok bool
		f, ok = s.objects[s.self][parts[0]]
		if !ok {
			return exprField{}, "", nil, fmt.Errorf("%s has no field %s", s.self, parts[0])
		}
	}
	for _, segment := range rest {
		if f.kind != "reference" || f.ref == "" {
			return exprField{}, "", nil, fmt.Errorf("%s: %s is not a reference", name, segment)
		}
		next, ok := s.objects[f.ref][segment]
		if !ok {
			return exprField{}, "", nil, fmt.Errorf("%s has no field %s", f.ref, segment)
		}
		f = next
	}
	return f, root, rest, nil
}

func (s exprScope) emit(n *exprNode, want string) (*object, string, error) {
	switch n.kind {
	case "string", "bool", "number":
		kind := want
		if s.json {
			kind = "json"
		} else if n.kind == "bool" {
			kind = "boolean"
		} else if n.kind == "number" {
			kind = numberKind(n, want)
		} else if kind == "" {
			kind = "text"
		}
		return s.literal(n, kind), kind, nil
	case "ref":
		f, root, path, err := s.resolve(n.name)
		if err != nil {
			return nil, "", err
		}
		if len(path) == 0 {
			out := exprObject("", f.kind)
			out.set("ref", n.name)
			return out, f.kind, nil
		}
		out := exprObject("path", f.kind)
		out.set("args", []any{func() *object { r := exprObject("", "reference"); r.set("ref", root); return r }()})
		segments := []any{}
		for _, p := range path {
			segments = append(segments, p)
		}
		out.set("path", segments)
		return out, f.kind, nil
	case "list":
		return nil, "", fmt.Errorf("a list is only allowed after in")
	}
	return s.call(n, want)
}

func (s exprScope) operands(args []*exprNode, want string) ([]any, string, error) {
	kind := want
	if kind == "" || kind == "boolean" {
		kind = ""
		for _, a := range args {
			if !isLiteral(a) {
				_, k, err := s.emit(a, "")
				if err != nil {
					return nil, "", err
				}
				kind = k
				break
			}
		}
	}
	out := make([]any, 0, len(args))
	for _, a := range args {
		value, k, err := s.emit(a, kind)
		if err != nil {
			return nil, "", err
		}
		if kind == "" {
			kind = k
		}
		out = append(out, value)
	}
	return out, kind, nil
}

var resultKinds = map[string]string{
	"contains": "boolean", "starts-with": "boolean", "ends-with": "boolean", "icontains": "boolean", "matches": "boolean",
	"before": "boolean", "after": "boolean", "within-period": "boolean", "overlaps": "boolean", "not-empty": "boolean",
	"is-null": "boolean", "is-not-null": "boolean", "length": "integer", "diff-days": "integer", "diff-hours": "integer", "workdays": "integer",
	"date-part": "integer", "concat": "text", "lower": "text", "upper": "text", "trim": "text", "replace": "text",
	"start-of-day": "date", "add-days": "date", "add-months": "date", "start-of-hour": "datetime", "add-hours": "datetime",
	"parse-decimal": "decimal", "parse-number": "decimal",
}

func (s exprScope) call(n *exprNode, want string) (*object, string, error) {
	name := n.name
	switch name {
	case "today", "now", "me":
		kinds := map[string]string{"today": "date", "now": "datetime", "me": "reference"}
		return exprObject("$"+name, kinds[name]), kinds[name], nil
	case "and", "or", "not":
		args := make([]any, 0, len(n.args))
		for _, a := range n.args {
			value, _, err := s.emit(a, "boolean")
			if err != nil {
				return nil, "", err
			}
			args = append(args, value)
		}
		out := exprObject(name, "boolean")
		out.set("args", args)
		return out, "boolean", nil
	case "empty":
		if len(n.args) != 1 {
			return nil, "", fmt.Errorf("empty takes one value")
		}
		value, _, err := s.emit(n.args[0], "")
		if err != nil {
			return nil, "", err
		}
		out := exprObject("empty", "boolean")
		out.set("args", []any{value})
		return out, "boolean", nil
	case "=", "!=", "<", "<=", ">", ">=":
		args, _, err := s.operands(n.args, "")
		if err != nil {
			return nil, "", err
		}
		out := exprObject(name, "boolean")
		out.set("args", args)
		return out, "boolean", nil
	case "in":
		left, kind, err := s.emit(n.args[0], "")
		if err != nil {
			return nil, "", err
		}
		list := n.args[1]
		if list.kind != "list" {
			return nil, "", fmt.Errorf("in needs a [list]")
		}
		values := []any{}
		for _, item := range list.list {
			if !isLiteral(item) {
				return nil, "", fmt.Errorf("an in list holds literals")
			}
			values = append(values, s.literal(item, kind).values["lit"].(*object).values["value"])
		}
		set := exprObject("", "set")
		lit := &object{}
		lit.set("element", kind)
		lit.set("value", values)
		set.set("lit", lit)
		out := exprObject("in", "boolean")
		out.set("args", []any{left, set})
		return out, "boolean", nil
	case "coalesce":
		args, kind, err := s.operands(n.args, want)
		if err != nil {
			return nil, "", err
		}
		out := exprObject(name, kind)
		out.set("args", args)
		return out, kind, nil
	case "if":
		if len(n.args) != 3 {
			return nil, "", fmt.Errorf("if takes a condition and two values")
		}
		cond, _, err := s.emit(n.args[0], "boolean")
		if err != nil {
			return nil, "", err
		}
		values, kind, err := s.operands(n.args[1:], want)
		if err != nil {
			return nil, "", err
		}
		out := exprObject("if", kind)
		out.set("args", append([]any{cond}, values...))
		return out, kind, nil
	case "+", "-", "*", "/":
		var args []any
		kind := "integer"
		for _, a := range n.args {
			value, k, err := s.emit(a, "")
			if err != nil {
				return nil, "", err
			}
			if k == "money" || kind != "money" && k == "decimal" || kind == "integer" && k != "integer" {
				kind = k
			}
			args = append(args, value)
		}
		out := exprObject(name, kind)
		out.set("args", args)
		return out, kind, nil
	case "count", "sum", "min", "max", "avg":
		if len(n.args) == 0 || n.args[0].kind != "ref" {
			return nil, "", fmt.Errorf("%s needs a set", name)
		}
		source, field, _ := strings.Cut(n.args[0].name, ".")
		setType, ok := s.sets[source]
		if !ok {
			if name == "min" || name == "max" {
				break
			}
			return nil, "", fmt.Errorf("%s is not a declared set", source)
		}
		agg := &object{}
		agg.set("source", source)
		kind := "integer"
		var in *object
		switch {
		case name == "count":
		case field != "":
			f, ok := s.objects[setType][field]
			if !ok {
				return nil, "", fmt.Errorf("%s has no field %s", setType, field)
			}
			agg.set("field", []any{field})
			kind = f.kind
		case len(n.args) == 2:
			inner := s
			inner.item = setType
			value, k, err := inner.emit(n.args[1], want)
			if err != nil {
				return nil, "", err
			}
			in, kind = value, k
		default:
			return nil, "", fmt.Errorf("%s needs set.field or (set, expression)", name)
		}
		if name == "avg" && kind == "integer" {
			kind = "decimal"
		}
		out := exprObject(name, kind)
		out.set("agg", agg)
		if in != nil {
			out.set("in", in)
		}
		return out, kind, nil
	}
	if !slices.Contains([]string{"min", "max", "negate", "abs", "round"}, name) && resultKinds[name] == "" {
		return nil, "", fmt.Errorf("unknown function %s", name)
	}
	var args []any
	kind := resultKinds[name]
	for i, a := range n.args {
		value, k, err := s.emit(a, "")
		if err != nil {
			return nil, "", err
		}
		if i == 0 && kind == "" {
			kind = k
		}
		args = append(args, value)
	}
	out := exprObject(name, kind)
	out.set("args", args)
	return out, kind, nil
}
