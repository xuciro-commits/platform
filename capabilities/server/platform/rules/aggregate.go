package rules

import (
	"math/big"
	"slices"
)

// NumericAggregate is the one bounded numeric fold used by Expr and host
// grouping. Selection, permissions and ordering remain with their owners.
type NumericAggregate struct {
	op, kind, currency string
	count              int64
	total              *big.Rat
}

func NewNumericAggregate(op, kind, currency string) (*NumericAggregate, error) {
	if !slices.Contains([]string{"count", "sum", "avg", "min", "max"}, op) || !numeric(kind) || op == "count" && kind != "integer" {
		return nil, fail("type", "aggregate", "supported numeric aggregate required")
	}
	return &NumericAggregate{op: op, kind: kind, currency: currency}, nil
}

func (a *NumericAggregate) Add(value Value) error {
	if a.op == "count" {
		a.count++
		return nil
	}
	x, err := validateValue(value, "aggregate")
	if err != nil {
		return err
	}
	if x == nil {
		return fail("missing", "aggregate", "aggregate operand is missing")
	}
	if !numeric(value.Kind) {
		return fail("type", "aggregate", "numeric aggregate operand required")
	}
	n, currency := numericParts(x)
	if a.total == nil {
		a.total = new(big.Rat).Set(n)
		if a.currency != "" && a.currency != currency {
			return fail("currency", "aggregate", "aggregate currencies differ")
		}
		a.currency = currency
	} else {
		if a.currency != currency {
			return fail("currency", "aggregate", "aggregate currencies differ")
		}
		switch a.op {
		case "sum", "avg":
			a.total.Add(a.total, n)
		case "min":
			if a.total.Cmp(n) > 0 {
				a.total.Set(n)
			}
		case "max":
			if a.total.Cmp(n) < 0 {
				a.total.Set(n)
			}
		}
	}
	if a.total.Num().BitLen() > 256 || a.total.Denom().BitLen() > 256 {
		return fail("budget", "aggregate", "aggregate numeric budget exceeded")
	}
	a.count++
	return nil
}

func (a *NumericAggregate) Result() (Value, error) {
	if a.op == "count" {
		return Present("integer", big.NewInt(a.count).String()), nil
	}
	if a.count == 0 {
		if a.op != "sum" {
			return Value{}, fail("empty", "aggregate", "numeric aggregate has no operands")
		}
		return numericValue(a.kind, new(big.Rat), a.currency, "aggregate")
	}
	value := new(big.Rat).Set(a.total)
	if a.op == "avg" {
		value.Quo(value, new(big.Rat).SetInt64(a.count))
	}
	return numericValue(a.kind, value, a.currency, "aggregate")
}
