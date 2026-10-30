package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type money struct {
	amount   *big.Rat
	currency string
}

var decimalText = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)$`)

func decode(raw json.RawMessage) (any, error) {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	err := d.Decode(&v)
	return v, err
}
func rat(v any) (*big.Rat, error) {
	var text string
	switch x := v.(type) {
	case string:
		text = x
	case json.Number:
		text = x.String()
	default:
		return nil, fail("type", "$", "numeric value required")
	}
	if len(text) > 128 {
		return nil, fail("budget", "$", "numeric byte budget exceeded")
	}
	n, ok := new(big.Rat).SetString(text)
	if !ok || n.Num().BitLen() > 256 || n.Denom().BitLen() > 256 {
		return nil, fail("number", "$", "finite bounded number required")
	}
	return n, nil
}
func validateValue(v Value, at string) (any, error) {
	if v.frozenNumber != nil {
		return *v.frozenNumber, nil
	}
	if v.native != nil {
		if f, ok := v.native.(float64); ok && canonicalKind(v.Kind) == "decimal" {
			return decimalFloat(f), nil
		}
		return v.native, nil
	}
	if v.Status == "missing" || v.Status == "null" {
		return nil, nil
	}
	if v.Status != "value" {
		return nil, fail(v.Status, at, "value is "+v.Status)
	}
	x, err := decode(v.Data)
	if err != nil {
		return nil, fail("value", at, "malformed typed value")
	}
	if x == nil {
		return nil, nil
	}
	switch canonicalKind(v.Kind) {
	case "json":
		if err := BoundedPredicateValue(x); err != nil {
			return nil, fail("predicate", at, err.Error())
		}
		return x, nil
	case "decimal", "integer":
		n, err := rat(x)
		if err != nil {
			return nil, fail("number", at, err.Error())
		}
		if v.Kind == "integer" && (!n.IsInt() || !n.Num().IsInt64()) {
			return nil, fail("integer", at, "bounded integer required")
		}
		return n, nil
	case "money":
		fields, ok := x.(map[string]any)
		if !ok {
			return nil, fail("money", at, "amount and currency required")
		}
		n, err := rat(fields["amount"])
		c, ok := fields["currency"].(string)
		if err != nil || !ok || len(c) != 3 || !n.IsInt() || !n.Num().IsInt64() {
			return nil, fail("money", at, "bounded minor-unit amount and currency required")
		}
		return money{n, c}, nil
	case "text", "choice", "reference":
		s, ok := x.(string)
		if !ok || len(s) > MaxText {
			return nil, fail("text", at, "bounded text required (4096 bytes)")
		}
		return s, nil
	case "boolean":
		b, ok := x.(bool)
		if !ok {
			return nil, fail("type", at, "boolean required")
		}
		return b, nil
	case "date", "datetime":
		s, ok := x.(string)
		if !ok {
			return nil, fail("time", at, "date text required")
		}
		layout := time.DateOnly
		if v.Kind == "datetime" {
			layout = time.RFC3339Nano
		}
		t, err := time.Parse(layout, s)
		if err != nil || t.Year() < 1 || t.Year() > 9999 {
			return nil, fail("time", at, "invalid date or datetime")
		}
		return t.UTC(), nil
	case "set":
		items, ok := x.([]any)
		if !ok || len(items) > MaxSet {
			return nil, fail("set", at, "bounded set required")
		}
		return items, nil
	}
	return nil, fail("type", at, "unsupported value type")
}

func BoundedPredicateValue(v any) error {
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
			if err := BoundedPredicateValue(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range value {
			if err := BoundedPredicateValue(item); err != nil {
				return err
			}
		}
	}
	return nil
}
func numericValue(kind string, n *big.Rat, currency, at string) (Value, error) {
	if n.Num().BitLen() > 256 || n.Denom().BitLen() > 256 {
		return Value{}, fail("budget", at, "numeric result budget exceeded")
	}
	if kind == "integer" || kind == "money" {
		if !n.IsInt() || !n.Num().IsInt64() {
			return Value{}, fail("integer", at, "result is not a bounded integer")
		}
	}
	if kind == "money" {
		if len(currency) != 3 {
			return Value{}, fail("currency", at, "declared currency required for money result")
		}
		return Present(kind, map[string]string{"amount": n.Num().String(), "currency": currency}), nil
	}
	if kind == "integer" {
		return Present(kind, n.Num().String()), nil
	}
	text := n.FloatString(decimalPlaces(n))
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	if text == "" || text == "-0" {
		text = "0"
	}
	return Present(kind, text), nil
}
func numericParts(x any) (*big.Rat, string) {
	if m, ok := x.(money); ok {
		return m.amount, m.currency
	}
	return x.(*big.Rat), ""
}
func compare(a, b any, at string) (int, error) {
	switch x := a.(type) {
	case *big.Rat:
		y, ok := b.(*big.Rat)
		if !ok {
			return 0, fail("type", at, "comparison types differ")
		}
		return x.Cmp(y), nil
	case money:
		y, ok := b.(money)
		if !ok || x.currency != y.currency {
			return 0, fail("currency", at, "comparison currencies differ")
		}
		return x.amount.Cmp(y.amount), nil
	case string:
		y, ok := b.(string)
		if !ok {
			return 0, fail("type", at, "comparison types differ")
		}
		return strings.Compare(x, y), nil
	case bool:
		y, ok := b.(bool)
		if !ok {
			return 0, fail("type", at, "comparison types differ")
		}
		if x == y {
			return 0, nil
		}
		if x {
			return 1, nil
		}
		return -1, nil
	case time.Time:
		y, ok := b.(time.Time)
		if !ok {
			return 0, fail("type", at, "comparison types differ")
		}
		return x.Compare(y), nil
	}
	return 0, fail("type", at, "value is not comparable")
}
func temporalValue(kind string, t time.Time) Value {
	format := time.DateOnly
	if kind == "datetime" {
		format = time.RFC3339Nano
	}
	return Present(kind, t.Format(format))
}
func round(n *big.Rat, digits int) *big.Rat {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(absInt(digits))), nil)
	factor := new(big.Rat).SetInt(scale)
	x := new(big.Rat).Set(n)
	if digits >= 0 {
		x.Mul(x, factor)
	} else {
		x.Quo(x, factor)
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(x.Num(), x.Denom(), remainder)
	if new(big.Int).Lsh(new(big.Int).Abs(remainder), 1).Cmp(x.Denom()) >= 0 {
		if x.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	out := new(big.Rat).SetInt(quotient)
	if digits >= 0 {
		return out.Quo(out, factor)
	}
	return out.Mul(out, factor)
}
func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func integerArg(v any, at string) (int, error) {
	n, ok := v.(*big.Rat)
	if !ok || !n.IsInt() || !n.Num().IsInt64() {
		return 0, fail("integer", at, "integer operand required")
	}
	value := n.Num().Int64()
	if value > 1000000 || value < -1000000 {
		return 0, fail("budget", at, "integer operand exceeds operation budget")
	}
	return int(value), nil
}
func ParseDecimalText(s string) (string, error) {
	if len(s) > 128 || !decimalText.MatchString(s) {
		return "", fail("number", "$", "invalid explicit numeric text")
	}
	n, err := rat(s)
	if err != nil {
		return "", err
	}
	v, err := numericValue("decimal", n, "", "$")
	if err != nil {
		return "", err
	}
	var text string
	_ = json.Unmarshal(v.Data, &text)
	return text, nil
}
func LiteralOf(kind string, value any) Expr {
	return Expr{Kind: kind, Lit: &Literal{Value: Raw(value)}}
}
func RefOf(kind, name string) Expr { return Expr{Kind: kind, Ref: name} }

func decimalPlaces(n *big.Rat) int {
	d := new(big.Int).Set(n.Denom())
	counts := []int{0, 0}
	for i, p := range []int64{2, 5} {
		prime := big.NewInt(p)
		for new(big.Int).Mod(d, prime).Sign() == 0 {
			d.Quo(d, prime)
			counts[i]++
		}
	}
	if d.Cmp(big.NewInt(1)) == 0 {
		return max(counts[0], counts[1])
	}
	return 18
}

func valueKey(x any) (string, bool) {
	switch v := x.(type) {
	case string:
		return "s" + v, true
	case *big.Rat:
		return "n" + v.RatString(), true
	case bool:
		if v {
			return "b1", true
		}
		return "b0", true
	case time.Time:
		return "t" + strconv.FormatInt(v.Unix(), 10) + "." + strconv.Itoa(v.Nanosecond()), true
	}
	return "", false
}
