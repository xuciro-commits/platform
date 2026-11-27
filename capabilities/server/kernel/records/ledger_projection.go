package records

import (
	"encoding/json"
	"fmt"
	"math/big"
	"platformserver/platform"
	"strings"
	"time"
)

// ledgerProjection is an immutable, disposable read image. Facts remain in
// ordinary accepted record rows; this index contains only derived totals.
// Copying it shares immutable index roots, as with record and reference indexes.
type ledgerProjection struct {
	current      Index[ledgerTotals]
	months       Index[ledgerTotals]
	days         Index[ledgerTotals]
	credits      Index[ledgerTotals]
	debitMonths  Index[ledgerTotals]
	creditMonths Index[ledgerTotals]
	reversed     Index[string]
}

type ledgerPlan struct {
	balances     []ledgerBalancePlan
	closedBefore string
}

func (p ledgerPlan) replayPlan() ledgerPlan {
	p.closedBefore = ""
	p.balances = append([]ledgerBalancePlan(nil), p.balances...)
	for i := range p.balances {
		p.balances[i].constraints = nil
		p.balances[i].nonnegative = nil
	}
	return p
}

type ledgerBalancePlan struct {
	name        string
	keys        []string
	measures    []ledgerMeasurePlan
	nonnegative []string
	constraints []ledgerCheck
}
type ledgerCheck struct {
	schema  platform.ExprSchema
	program platform.ExprProgram
	message string
}

type ledgerMeasurePlan struct{ name, kind string }
type ledgerFact struct {
	id, on, reverses string
	fields           map[string]json.RawMessage
}
type ledgerTotals map[string]json.RawMessage

func ledgerJSON(v any) json.RawMessage { return platform.Raw(v) }

func ledgerKey(name string, keys []json.RawMessage) string {
	if keys == nil {
		keys = []json.RawMessage{}
	}
	return name + "/" + string(ledgerJSON(keys))
}

func (p ledgerProjection) balance(name string, keys []json.RawMessage) ledgerTotals {
	return cloneLedgerTotals(p.current.Get(ledgerKey(name, keys)))
}

func cloneLedgerTotals(in ledgerTotals) ledgerTotals {
	out := make(ledgerTotals, len(in))
	for name, value := range in {
		out[name] = append(json.RawMessage(nil), value...)
	}
	return out
}

// apply computes the successor image without mutating the predecessor. The
// caller may discard it for preflight or publish it with the accepted fact.
// An invalid measure or constraint leaves all balances unchanged.
func (p ledgerProjection) apply(plan ledgerPlan, f ledgerFact) (ledgerProjection, error) {
	if f.id == "" {
		return p, fmt.Errorf("ledger fact has no id")
	}
	if _, err := time.Parse(time.DateOnly, f.on); err != nil {
		return p, fmt.Errorf("ledger fact needs a business date")
	}
	if plan.closedBefore != "" && f.on < plan.closedBefore {
		return p, fmt.Errorf("ledger is closed before %s", plan.closedBefore)
	}
	next := p
	if f.reverses != "" {
		if f.reverses == f.id || p.reversed.Has(f.reverses) {
			return p, fmt.Errorf("ledger fact already reversed")
		}
		next.reversed.Set(f.reverses, f.id)
	}
	for _, b := range plan.balances {
		keys := make([]json.RawMessage, 0, len(b.keys))
		for _, name := range b.keys {
			v := f.fields[name]
			if len(v) == 0 || string(v) == "null" || string(v) == `""` {
				return p, fmt.Errorf("ledger balance %s requires key %s", b.name, name)
			}
			keys = append(keys, v)
		}
		key := ledgerKey(b.name, keys)
		current := cloneLedgerTotals(next.current.Get(key))
		monthlyKey := f.on[:7] + "/" + key
		monthly := cloneLedgerTotals(next.months.Get(monthlyKey))
		dayKey := key + "/" + f.on
		monthKey := key + "/" + f.on[:7]
		daily := cloneLedgerTotals(next.days.Get(dayKey))
		credit := cloneLedgerTotals(next.credits.Get(dayKey))
		debitMonth := cloneLedgerTotals(next.debitMonths.Get(monthKey))
		creditMonth := cloneLedgerTotals(next.creditMonths.Get(monthKey))
		for _, m := range b.measures {
			var err error
			current[m.name], err = addLedgerAmount(m.kind, current[m.name], f.fields[m.name])
			if err != nil {
				return p, fmt.Errorf("ledger balance %s measure %s: %w", b.name, m.name, err)
			}
			monthly[m.name], err = addLedgerAmount(m.kind, monthly[m.name], f.fields[m.name])
			if err != nil {
				return p, fmt.Errorf("ledger monthly balance %s measure %s: %w", b.name, m.name, err)
			}
			delta := f.fields[m.name]
			amount, _, err := ledgerNumber(delta)
			if err != nil {
				return p, err
			}
			if amount.Sign() >= 0 {
				daily[m.name], err = addLedgerAmount(m.kind, daily[m.name], delta)
				if err != nil {
					return p, err
				}
				debitMonth[m.name], err = addLedgerAmount(m.kind, debitMonth[m.name], delta)
				if err != nil {
					return p, err
				}
			} else {
				delta, err = negateLedgerAmount(m.kind, delta)
				if err != nil {
					return p, err
				}
				credit[m.name], err = addLedgerAmount(m.kind, credit[m.name], delta)
				if err != nil {
					return p, err
				}
				creditMonth[m.name], err = addLedgerAmount(m.kind, creditMonth[m.name], delta)
				if err != nil {
					return p, err
				}
			}
		}
		for _, name := range b.nonnegative {
			v, _, err := ledgerNumber(current[name])
			if err != nil || v.Sign() < 0 {
				return p, fmt.Errorf("ledger balance %s: %s must not be negative", b.name, name)
			}
		}
		for _, rule := range b.constraints {
			ctx := platform.ExprContext{Refs: map[string]platform.ExprValue{}}
			for name, typ := range rule.schema.Refs {
				raw := current[name]
				if len(raw) == 0 {
					raw = f.fields[name]
				}
				ctx.Refs[name] = platform.ExprValue{Kind: typ.Kind, Status: "value", Data: raw}
			}
			result, err := rule.program.Eval(ctx)
			if err != nil || string(result.Value.Data) != "true" {
				return p, fmt.Errorf("ledger balance %s: %s", b.name, rule.message)
			}
		}
		next.current.Set(key, current)
		next.months.Set(monthlyKey, monthly)
		next.days.Set(dayKey, daily)
		next.credits.Set(dayKey, credit)
		next.debitMonths.Set(monthKey, debitMonth)
		next.creditMonths.Set(monthKey, creditMonth)
	}
	return next, nil
}

func ledgerNumber(raw json.RawMessage) (*big.Rat, string, error) {
	if len(raw) == 0 {
		return new(big.Rat), "", nil
	}
	if raw[0] == '{' {
		var m platform.Money
		if err := json.Unmarshal(raw, &m); err != nil || m.Currency == "" {
			return nil, "", fmt.Errorf("invalid money amount")
		}
		return new(big.Rat).SetInt64(m.Amount), m.Currency, nil
	}
	if len(raw) > 1024 || !json.Valid(raw) || strings.ContainsAny(string(raw), `"{}[]`) {
		return nil, "", fmt.Errorf("invalid numeric amount")
	}
	// Accepted decimal fields are finite IEEE-754 numbers. Bound exponents so
	// arbitrary raw input cannot create enormous big integers during validation.
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, "", fmt.Errorf("invalid numeric amount")
	}
	if _, err := n.Float64(); err != nil {
		return nil, "", fmt.Errorf("amount outside finite decimal range")
	}
	v, ok := new(big.Rat).SetString(string(n))
	if !ok {
		return nil, "", fmt.Errorf("invalid numeric amount")
	}
	return v, "", nil
}

func addLedgerAmount(kind string, old, delta json.RawMessage) (json.RawMessage, error) {
	if len(delta) == 0 || string(delta) == "null" {
		return nil, fmt.Errorf("missing measure")
	}
	a, ac, err := ledgerNumber(old)
	if err != nil {
		return nil, err
	}
	b, bc, err := ledgerNumber(delta)
	if err != nil {
		return nil, err
	}
	if kind == "money" {
		if bc == "" || (ac != "" && ac != bc) {
			return nil, fmt.Errorf("incompatible currencies")
		}
	} else if ac != "" || bc != "" {
		return nil, fmt.Errorf("unexpected currency")
	}
	sum := new(big.Rat).Add(a, b)
	switch kind {
	case "integer", "money":
		if !a.IsInt() || !b.IsInt() || !sum.IsInt() || !sum.Num().IsInt64() {
			return nil, fmt.Errorf("integer measure overflow or fraction")
		}
		if kind == "money" {
			return ledgerJSON(platform.Money{Amount: sum.Num().Int64(), Currency: bc}), nil
		}
		return ledgerJSON(sum.Num().Int64()), nil
	case "decimal":
		return finiteLedgerDecimal(sum)
	default:
		return nil, fmt.Errorf("unsupported measure type %s", kind)
	}
}

func finiteLedgerDecimal(v *big.Rat) (json.RawMessage, error) {
	den := new(big.Int).Set(v.Denom())
	two, five, one := big.NewInt(2), big.NewInt(5), big.NewInt(1)
	var a, b int
	for new(big.Int).Mod(den, two).Sign() == 0 {
		den.Quo(den, two)
		a++
	}
	for new(big.Int).Mod(den, five).Sign() == 0 {
		den.Quo(den, five)
		b++
	}
	if den.Cmp(one) != 0 || max(a, b) > 1024 {
		return nil, fmt.Errorf("non-finite decimal measure")
	}
	return json.RawMessage(v.FloatString(max(a, b))), nil
}

func checkLedgerReversal(plan ledgerPlan, original, reversal ledgerFact) error {
	if original.reverses != "" {
		return fmt.Errorf("a reversal cannot itself be reversed")
	}
	for _, b := range plan.balances {
		for _, name := range b.keys {
			if string(original.fields[name]) != string(reversal.fields[name]) {
				return fmt.Errorf("reversal key %s differs", name)
			}
		}
		for _, m := range b.measures {
			a, ac, err := ledgerNumber(original.fields[m.name])
			if err != nil {
				return err
			}
			z, zc, err := ledgerNumber(reversal.fields[m.name])
			if err != nil {
				return err
			}
			if ac != zc || new(big.Rat).Add(a, z).Sign() != 0 {
				return fmt.Errorf("reversal measure %s is not the opposite fact", m.name)
			}
		}
	}
	return nil
}

func negateLedgerAmount(kind string, raw json.RawMessage) (json.RawMessage, error) {
	v, c, err := ledgerNumber(raw)
	if err != nil {
		return nil, err
	}
	v.Neg(v)
	if kind == "decimal" {
		return finiteLedgerDecimal(v)
	}
	if !v.IsInt() || !v.Num().IsInt64() {
		return nil, fmt.Errorf("reversal amount overflow")
	}
	if kind == "money" {
		return ledgerJSON(platform.Money{Amount: v.Num().Int64(), Currency: c}), nil
	}
	return ledgerJSON(v.Num().Int64()), nil
}
