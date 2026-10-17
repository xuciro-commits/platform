package build

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// formulaExpr mechanically lowers the old grammar. Integer destinations use
// an explicit round node; saved Expr values never inherit legacy zero fallback.
func formulaExpr(fields []Field, f Field) (platform.Expr, error) {
	if f.FormulaExpr != nil {
		if f.Formula != "" {
			return platform.Expr{}, fmt.Errorf("field %s has two formulas", f.Name)
		}
		return *f.FormulaExpr, nil
	}
	node, err := ValueParseFormula(f.Formula)
	if err != nil {
		return platform.Expr{}, err
	}
	types := map[string]string{}
	for _, operand := range fields {
		if operand.Formula == "" && operand.FormulaExpr == nil && (operand.Type == "integer" || operand.Type == "decimal" || operand.Type == "money") {
			types[operand.Name] = operand.Type
			if operand.Type == "money" {
				types[operand.Name] = "decimal"
			}
		}
	}
	e, err := node.Expr(types)
	if err != nil {
		return e, err
	}
	if f.Type == "integer" {
		e = platform.Expr{Op: "round", Kind: "integer", Args: []platform.Expr{e}}
	}
	return e, nil
}
func checkFormulas(fields []Field) error {
	for _, f := range fields {
		if f.Formula != "" {
			if f.Type != "integer" && f.Type != "decimal" {
				return fmt.Errorf("field %s legacy formula needs a numeric destination", f.Name)
			}
			if _, err := formulaExpr(fields, f); err != nil {
				return fmt.Errorf("field %s: %w", f.Name, err)
			}
		}
	}
	return nil
}
func computeRulesOf(o Object, lookup func(string) (platform.EntityInfo, bool)) func(platform.Caller, any, time.Time) *kernel.Error {
	list, orderErr := formulaOrder(o.Fields)
	if len(list) == 0 {
		return nil
	}
	lookup = ruleLookup(o, lookup)
	return func(c platform.Caller, record any, now time.Time) *kernel.Error {
		if orderErr != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, orderErr.Error())
		}
		for _, f := range list {
			e, err := formulaExpr(o.Fields, f)
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			s := ruleSpec(o, Action{}, o.Reads)
			schema, err := platform.RuleSchema(e, s, lookup)
			if f.Formula != "" {
				refs := map[string]platform.ExprType{}
				for _, field := range o.Fields {
					kind := platform.RuleFieldKind(field.Type)
					if field.Type == "money" {
						kind = "decimal"
					}
					refs[field.Name] = platform.ExprType{Kind: kind}
				}
				schema = platform.ExprSchema{Refs: refs}
				_, err = platform.CompileFrozenFormula(e, schema)
			}
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			ctx, err := c.RuleContext(e, s, lookup, record, nil, now)
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, err.Error())
			}
			if f.Formula != "" {
				for _, field := range o.Fields {
					if field.Type == "money" {
						v := ctx.Refs[field.Name]
						v.Kind = "decimal"
						if v.Status == "value" {
							var amount struct {
								Amount json.RawMessage `json:"amount"`
							}
							if json.Unmarshal(v.Data, &amount) == nil {
								v.Data = amount.Amount
							}
						}
						ctx.Refs[field.Name] = v
					}
				}
			}
			var program *platform.ExprProgram
			if f.Formula != "" {
				program, err = platform.CompileFrozenFormula(e, schema)
			} else {
				program, err = platform.CompileExpr(e, schema)
			}
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			var result platform.ExprResult
			if f.Formula != "" {
				result, err = program.EvalFrozenFormula(ctx)
			} else {
				result, err = program.Eval(ctx)
			}
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Field {field}: {reason} / 字段 {field}：{reason}", f.Name, err.Error(), f.Name, err.Error())
			}
			if f.FormulaExpr != nil {
				holder := reflect.ValueOf(record).Elem().FieldByName("RuleSources" + goName(f.Name))
				if holder.IsValid() {
					holder.Set(reflect.ValueOf(ctx.Sources))
				}
			}
			value, err := platform.ExprGoValue(result.Value)
			if err == nil {
				err = assign(reflect.ValueOf(record).Elem().FieldByName(goName(f.Name)), value)
			}
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
		}
		return nil
	}
}

// validateOf is the entity hook of an extension object (ADR-0058 A3): one
// extension record per base record, so the base type's pages can merge the
// extension's fields as if they were its own.
func validateOf(o Object, model any, lookup func(string) (platform.EntityInfo, bool)) func(c platform.Caller, record any) *kernel.Error {
	if o.Extends == "" {
		return nil
	}
	typ := reflect.TypeOf(model)
	return func(c platform.Caller, record any) *kernel.Error {
		if !c.Replaying && lookup != nil {
			base, exists := lookup(o.Extends)
			if !exists {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Extension base {type} is unavailable", o.Extends)
			}
			shape := platform.EntityInfo{Type: TypeOf(o.Name), Extends: o.Extends, ExtensionSource: o.ExtensionSource}
			for _, field := range o.Fields {
				shape.Fields = append(shape.Fields, platform.FieldInfo{Name: field.Name, Type: field.Type, Ref: field.Ref, Required: field.Required, ReadOnly: field.ReadOnly, AliasOf: field.AliasOf})
			}
			if err := platform.CheckExtension(shape, base); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
		}
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

func formulaOrder(fields []Field) ([]Field, error) {
	byName := map[string]Field{}
	for _, f := range fields {
		if f.Formula != "" || f.FormulaExpr != nil {
			byName[f.Name] = f
		}
	}
	visited, visiting := map[string]bool{}, map[string]bool{}
	out := []Field{}
	var walk func(string) error
	walk = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("field %s formula dependency is cyclic", name)
		}
		f, ok := byName[name]
		if !ok {
			return nil
		}
		visiting[name] = true
		if f.FormulaExpr != nil {
			var refs func(platform.Expr) error
			refs = func(e platform.Expr) error {
				if _, ok := byName[e.Ref]; ok {
					if err := walk(e.Ref); err != nil {
						return err
					}
				}
				for _, a := range e.Args {
					if err := refs(a); err != nil {
						return err
					}
				}
				if e.In != nil {
					return refs(*e.In)
				}
				return nil
			}
			if err := refs(*f.FormulaExpr); err != nil {
				return err
			}
		}
		delete(visiting, name)
		visited[name] = true
		out = append(out, f)
		return nil
	}
	for _, f := range fields {
		if err := walk(f.Name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

type ValueFormulaNode struct {
	op          byte // 'n' number, 'f' field, or + - * / ~ (negate)
	value       string
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
		_, err := strconv.ParseFloat(p.src[start:p.pos], 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", p.src[start:p.pos])
		}
		return &ValueFormulaNode{op: 'n', value: p.src[start:p.pos]}, nil
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

// Expr lowers the old arithmetic grammar; there is no second evaluator.
func (n *ValueFormulaNode) Expr(fields map[string]string) (platform.Expr, error) {
	switch n.op {
	case 'n':
		return platform.ExprLiteralValue("decimal", n.value), nil
	case 'f':
		kind, ok := fields[n.field]
		if !ok {
			return platform.Expr{}, fmt.Errorf("undeclared formula field %s", n.field)
		}
		return platform.ExprReference(platform.RuleFieldKind(kind), n.field), nil
	}
	a, err := n.left.Expr(fields)
	if err != nil {
		return a, err
	}
	op := string(n.op)
	args := []platform.Expr{a}
	if n.op == '~' {
		op = "negate"
	} else {
		b, err := n.right.Expr(fields)
		if err != nil {
			return b, err
		}
		args = append(args, b)
	}
	return platform.Expr{Op: op, Kind: "decimal", Args: args}, nil
}
