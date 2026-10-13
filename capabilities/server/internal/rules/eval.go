package rules

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// EvalFrozenFormula preserves the numeric semantics of already frozen formula
// bytes. Owners select it only from a legacy immutable publication.
func (p *Program) EvalFrozenFormula(c Context) (Result, error) {
	var check func(Expr) error
	check = func(e Expr) error {
		if e.Lit != nil || e.Ref != "" {
			if !numeric(e.Kind) || e.Kind == "money" {
				return fail("type", "$", "frozen formula needs numeric leaves")
			}
			return nil
		}
		if !slices.Contains([]string{"+", "-", "*", "/", "negate", "round"}, e.Op) {
			return fail("operator", "$", "frozen compatibility is limited to old arithmetic")
		}
		for _, a := range e.Args {
			if err := check(a); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(p.expr); err != nil {
		return Result{}, err
	}
	copy := *p
	copy.frozenFormula = true
	return copy.Eval(c)
}
func (p *Program) Eval(c Context) (Result, error) {
	raw, encodeErr := json.Marshal(c)
	if encodeErr != nil || len(raw) > MaxContextBytes {
		return Result{}, fail("budget", "$", fmt.Sprintf("captured rule context is %d bytes; limit is %d", len(raw), MaxContextBytes))
	}
	reasons := []Issue{}
	v, err := p.eval(p.expr, c, "$", &reasons)
	if err == nil && p.frozenFormula {
		n := 0.0
		if v.frozenNumber != nil {
			n = *v.frozenNumber
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			n = 0
		}
		text := strconv.FormatFloat(n, 'f', -1, 64)
		if p.expr.Kind == "integer" {
			text = strconv.FormatInt(int64(n), 10)
		}
		v = Present(p.expr.Kind, text)
	}
	return Result{v, reasons}, err
}
func (p *Program) eval(e Expr, c Context, at string, reasons *[]Issue) (Value, error) {
	if e.Lit != nil {
		if p.frozenFormula {
			return frozenNumeric(PresentRaw(e.Kind, e.Lit.Value), at)
		}
		return PresentRaw(e.Kind, e.Lit.Value), nil
	}
	if e.Ref != "" {
		v, ok := c.Refs[e.Ref]
		if !ok {
			if p.frozenFormula {
				n := 0.0
				return Value{Kind: e.Kind, Status: "value", frozenNumber: &n}, nil
			}
			return Missing(e.Kind), nil
		}
		if !same(v.Kind, e.Kind) {
			return Value{}, fail("type", at, "reference runtime type differs")
		}
		if p.frozenFormula {
			return frozenNumeric(v, at)
		}
		if _, err := validateValue(v, at); err != nil {
			return Value{}, err
		}
		return v, nil
	}
	if e.Agg != nil {
		return p.aggregate(e, c, at, reasons)
	}
	if e.Op == "$me" {
		if c.Me == "" {
			return Value{}, fail("identity", at, "captured member required")
		}
		return Present("reference", c.Me), nil
	}
	if e.Op == "$now" {
		if c.Now.IsZero() {
			return Value{}, fail("clock", at, "captured input time required")
		}
		return temporalValue("datetime", c.Now.UTC()), nil
	}
	if e.Op == "$today" {
		v := Present("date", c.Today)
		_, err := validateValue(v, at)
		return v, err
	}
	if e.Op == "path" {
		key := e.Args[0].Ref + "." + strings.Join(e.Path, ".")
		if v, ok := c.Refs[key]; ok {
			if !same(v.Kind, e.Kind) {
				return Value{}, fail("type", at, "path runtime type differs")
			}
			_, err := validateValue(v, at)
			return v, err
		}
		return Missing(e.Kind), nil
	}
	values := make([]any, 0, len(e.Args))
	states := make([]Value, 0, len(e.Args))
	for i, arg := range e.Args {
		v, err := p.eval(arg, c, fmt.Sprintf("%s.args[%d]", at, i), reasons)
		if err != nil {
			return Value{}, err
		}
		states = append(states, v)
		x, err := validateValue(v, at)
		if err != nil {
			return Value{}, err
		}
		if v.Status == "missing" || x == nil {
			switch e.Op {
			case "=", "!=", "<", "<=", ">", ">=", "before", "after", "within-period", "in", "range":
				*reasons = append(*reasons, Issue{"missing", at, "comparison operand is missing or null"})
				return Present("boolean", false), nil
			case "empty", "not-empty", "is-null", "is-not-null":
				if v.Status == "missing" {
					return Value{}, fail("missing", at, "emptiness operand is missing")
				}
			default:
				return Value{}, fail("missing", fmt.Sprintf("%s.args[%d]", at, i), "required operand is missing or null")
			}
		}
		values = append(values, x)
		if e.Op == "and" && x == false {
			return Present("boolean", false), nil
		}
		if e.Op == "or" && x == true {
			return Present("boolean", true), nil
		}
	}
	out := func(v any) (Value, error) { r := Present(e.Kind, v); _, err := validateValue(r, at); return r, err }
	switch e.Op {
	case "and":
		return out(true)
	case "or":
		return out(false)
	case "not":
		return out(!values[0].(bool))
	case "matches":
		return out(p.patterns[values[1].(string)].MatchString(values[0].(string)))
	case "empty", "not-empty", "is-null", "is-not-null":
		empty := values[0] == nil
		switch x := values[0].(type) {
		case string:
			empty = x == ""
		case []any:
			empty = len(x) == 0
		}
		if e.Op == "is-null" {
			empty = values[0] == nil
		}
		if e.Op == "is-not-null" {
			return out(values[0] != nil)
		}
		if e.Op == "not-empty" {
			empty = !empty
		}
		return out(empty)
	case "=", "!=", "<", "<=", ">", ">=", "before", "after":
		left, right := values[0], values[1]
		if m, ok := left.(money); ok {
			if n, ok := right.(*big.Rat); ok {
				right = money{n, m.currency}
			}
		} else if m, ok := right.(money); ok {
			if n, ok := left.(*big.Rat); ok {
				left = money{n, m.currency}
			}
		}
		cmp, err := compare(left, right, at)
		if err != nil {
			return Value{}, err
		}
		yes := false
		switch e.Op {
		case "=":
			yes = cmp == 0
		case "!=":
			yes = cmp != 0
		case "<", "before":
			yes = cmp < 0
		case "<=":
			yes = cmp <= 0
		case ">", "after":
			yes = cmp > 0
		case ">=":
			yes = cmp >= 0
		}
		return out(yes)
	case "+", "-", "*", "/", "negate", "abs", "round", "min", "max":
		if p.frozenFormula {
			n := values[0].(float64)
			switch e.Op {
			case "negate":
				n = -n
			case "round":
				n = math.Round(n)
			case "+":
				n += values[1].(float64)
			case "-":
				n -= values[1].(float64)
			case "*":
				n *= values[1].(float64)
			case "/":
				if values[1].(float64) == 0 {
					n = 0
				} else {
					n /= values[1].(float64)
				}
			}
			return Value{Kind: e.Kind, Status: "value", frozenNumber: &n}, nil
		}
		n, currency := numericParts(values[0])
		if e.Kind == "money" && currency == "" {
			for _, x := range values {
				if m, ok := x.(money); ok {
					currency = m.currency
					break
				}
			}
		}
		n = new(big.Rat).Set(n)
		if e.Op == "negate" {
			n.Neg(n)
		}
		if e.Op == "abs" {
			n.Abs(n)
		}
		if e.Op == "round" {
			digits := 0
			if len(values) > 1 {
				var err error
				digits, err = integerArg(values[1], at)
				if err != nil {
					return Value{}, err
				}
				if digits < -18 || digits > 18 {
					return Value{}, fail("budget", at, "round precision exceeds 18 digits")
				}
			}
			n = round(n, digits)
		} else {
			for _, x := range values[1:] {
				next, other := numericParts(x)
				if currency != other && !(e.Kind == "money" && other == "") {
					return Value{}, fail("currency", at, "arithmetic currencies differ")
				}
				switch e.Op {
				case "+":
					n.Add(n, next)
				case "-":
					n.Sub(n, next)
				case "*":
					n.Mul(n, next)
				case "/":
					if next.Sign() == 0 {
						return Value{}, fail("division-zero", at, "division by zero is refused")
					}
					n.Quo(n, next)
				case "min":
					if n.Cmp(next) > 0 {
						n.Set(next)
					}
				case "max":
					if n.Cmp(next) < 0 {
						n.Set(next)
					}
				}
			}
		}
		return numericValue(e.Kind, n, currency, at)
	case "parse-decimal", "parse-number":
		s, err := ParseDecimalText(values[0].(string))
		if err != nil {
			return Value{}, fail("number", at, err.Error())
		}
		return out(s)
	case "concat":
		var b strings.Builder
		for _, x := range values {
			b.WriteString(x.(string))
			if b.Len() > MaxText {
				return Value{}, fail("budget", at, "text result budget exceeded")
			}
		}
		return out(b.String())
	case "lower":
		return out(strings.ToLower(values[0].(string)))
	case "upper":
		return out(strings.ToUpper(values[0].(string)))
	case "trim":
		return out(strings.TrimSpace(values[0].(string)))
	case "contains":
		return out(strings.Contains(values[0].(string), values[1].(string)))
	case "starts-with":
		return out(strings.HasPrefix(values[0].(string), values[1].(string)))
	case "ends-with":
		return out(strings.HasSuffix(values[0].(string), values[1].(string)))
	case "length":
		return out(utf8.RuneCountInString(values[0].(string)))
	case "replace":
		return out(strings.ReplaceAll(values[0].(string), values[1].(string), values[2].(string)))
	case "add-days", "add-months":
		n, err := integerArg(values[1], at)
		if err != nil {
			return Value{}, err
		}
		t := values[0].(time.Time)
		if e.Op == "add-days" {
			t = t.AddDate(0, 0, n)
		} else {
			first := time.Date(t.Year(), t.Month()+time.Month(n), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
			end := first.AddDate(0, 1, -1).Day()
			day := min(t.Day(), end)
			t = first.AddDate(0, 0, day-1)
		}
		if t.Year() < 1 || t.Year() > 9999 {
			return Value{}, fail("time", at, "result date is outside supported years")
		}
		return temporalValue(e.Kind, t), nil
	case "diff-days":
		return out((values[0].(time.Time).Unix() - values[1].(time.Time).Unix()) / 86400)
	case "date-part":
		t := values[0].(time.Time)
		var n int
		switch values[1].(string) {
		case "year":
			n = t.Year()
		case "month":
			n = int(t.Month())
		case "day":
			n = t.Day()
		case "weekday":
			n = int(t.Weekday())
		}
		return out(n)
	case "within-period", "range":
		a, err := compare(values[0], values[1], at)
		if err != nil {
			return Value{}, err
		}
		b, err := compare(values[0], values[2], at)
		if err != nil {
			return Value{}, err
		}
		return out(a >= 0 && b <= 0)
	case "in":
		for _, candidate := range values[1].([]any) {
			v := Present(states[0].Kind, candidate)
			x, err := validateValue(v, at)
			if err != nil {
				return Value{}, err
			}
			cmp, err := compare(values[0], x, at)
			if err != nil {
				return Value{}, err
			}
			if cmp == 0 {
				return out(true)
			}
		}
		return out(false)
	}
	return Value{}, fail("operator", at, "operator is not executable")
}
func (p *Program) aggregate(e Expr, c Context, at string, reasons *[]Issue) (Value, error) {
	decl := p.schema.Sets[e.Agg.Source]
	set, ok := c.Sets[e.Agg.Source]
	if !ok {
		return Value{}, fail("missing", at, "declared collection is unavailable")
	}
	if set.Status != "" && set.Status != "value" {
		return Value{}, fail(set.Status, at, "declared collection is "+set.Status)
	}
	if !set.Complete {
		return Value{}, fail("incomplete", at, "collection is incomplete")
	}
	if len(set.Items) > MaxSet {
		return Value{}, fail("budget", at, fmt.Sprintf("collection has %d items; limit is %d", len(set.Items), MaxSet))
	}
	if decl.Policy == "complete" && set.Withheld || set.Withheld && (e.Op == "all" || e.Op == "none") {
		return Value{}, fail("withheld", at, "quantifier or complete policy needs the full collection")
	}
	if decl.Policy == "visible" {
		*reasons = append(*reasons, Issue{"scope", at, "evaluated caller-visible collection"})
	}
	items := append([]Item(nil), set.Items...)
	slices.SortFunc(items, func(a, b Item) int { return strings.Compare(a.ID, b.ID) })
	seen := map[string]bool{}
	for _, item := range items {
		if item.ID == "" || seen[item.ID] {
			return Value{}, fail("identity", at, "collection keys must be distinct and stable")
		}
		seen[item.ID] = true
	}
	quantifier := e.Op == "all" || e.Op == "any" || e.Op == "none"
	var fold *NumericAggregate
	if !quantifier {
		currency := ""
		typ := decl.Element
		for _, part := range e.Agg.Field {
			typ = typ.Fields[part]
		}
		if e.In != nil && e.In.Ref != "" {
			typ = decl.Element.Fields[strings.TrimPrefix(e.In.Ref, "item.")]
		}
		currency = typ.Currency
		if e.In != nil && e.In.Lit != nil {
			if value, err := validateValue(PresentRaw(e.In.Kind, e.In.Lit.Value), at); err == nil {
				if money, ok := value.(money); ok {
					currency = money.currency
				}
			}
		}
		var err error
		fold, err = NewNumericAggregate(e.Op, e.Kind, currency)
		if err != nil {
			return Value{}, err
		}
	}
	for i, item := range items {
		if e.Op == "count" {
			_ = fold.Add(Value{})
			continue
		}
		child := c
		child.Refs = map[string]Value{}
		for key, value := range c.Refs {
			child.Refs[key] = value
		}
		for key, value := range item.Fields {
			child.Refs["item."+key] = value
		}
		var value Value
		var err error
		if e.In != nil {
			value, err = p.eval(*e.In, child, fmt.Sprintf("%s.items[%d]", at, i), reasons)
		} else {
			value = item.Fields[strings.Join(e.Agg.Field, ".")]
			if value.Kind == "" {
				value = Missing(e.Kind)
			}
		}
		if err != nil {
			return Value{}, err
		}
		if !quantifier {
			if err := fold.Add(value); err != nil {
				return Value{}, err
			}
			continue
		}
		x, err := validateValue(value, at)
		if err != nil {
			return Value{}, err
		}
		if x == nil {
			return Value{}, fail("missing", at, "aggregate operand is missing")
		}
		switch e.Op {
		case "all":
			if !x.(bool) {
				return Present("boolean", false), nil
			}
		case "any":
			if x.(bool) {
				return Present("boolean", true), nil
			}
		case "none":
			if x.(bool) {
				return Present("boolean", false), nil
			}
		}
	}
	if quantifier {
		return Present("boolean", e.Op != "any"), nil
	}
	return fold.Result()
}

// EvalJSON is the identical boundary used by the host and the compiled browser
// module. No browser-specific implementation of rule semantics exists.
func EvalJSON(raw []byte) ([]byte, error) {
	if len(raw) > MaxContextBytes+2*MaxBytes {
		return nil, fail("budget", "$", "evaluation request budget exceeded")
	}
	var req struct {
		Expr    Expr    `json:"expr"`
		Schema  Schema  `json:"schema"`
		Context Context `json:"context"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	p, err := Compile(req.Expr, req.Schema)
	if err != nil {
		return nil, err
	}
	result, err := p.Eval(req.Context)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func frozenNumeric(v Value, at string) (Value, error) {
	n := 0.0
	if v.Status != "missing" && v.Status != "null" {
		if v.Status != "value" {
			return Value{}, fail(v.Status, at, "value is "+v.Status)
		}
		raw, err := decode(v.Data)
		if err != nil {
			return Value{}, err
		}
		var text string
		switch x := raw.(type) {
		case nil:
		case string:
			text = x
		case json.Number:
			text = x.String()
		default:
			return Value{}, fail("type", at, "frozen formula needs a numeric operand")
		}
		if text != "" {
			n, err = strconv.ParseFloat(text, 64)
			if err != nil && !math.IsInf(n, 0) {
				return Value{}, err
			}
		}
	}
	return Value{Kind: v.Kind, Status: "value", frozenNumber: &n}, nil
}
