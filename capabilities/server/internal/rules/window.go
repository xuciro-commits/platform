package rules

import (
	"fmt"
	"math/big"
	"slices"
	"time"
)

type WindowReference struct {
	Source    string `json:"source"`
	Aggregate string `json:"aggregate,omitempty"`
}
type WindowType struct {
	Unit string `json:"unit"`
}
type WindowSummary struct {
	Since   time.Time `json:"since"`
	Until   time.Time `json:"until"`
	Count   int64     `json:"count"`
	FirstAt time.Time `json:"firstAt,omitzero"`
	LastAt  time.Time `json:"lastAt,omitzero"`
	First   string    `json:"first,omitempty"`
	Last    string    `json:"last,omitempty"`
	Min     string    `json:"min,omitempty"`
	Max     string    `json:"max,omitempty"`
	Sum     string    `json:"sum,omitempty"`
	Mean    string    `json:"mean,omitempty"`
	Unit    string    `json:"unit"`
}

func (w WindowSummary) Check() error {
	if w.Since.IsZero() || w.Until.IsZero() || !w.Since.Before(w.Until) || w.Since.Year() < 1 || w.Until.Year() > 9999 || w.Until.Sub(w.Since) > 366*24*time.Hour || w.Count < 0 || w.Unit == "" || len(w.Unit) > 64 {
		return fmt.Errorf("observation window needs ordered bounds, a nonnegative count and unit")
	}
	values := []string{w.First, w.Last, w.Min, w.Max, w.Sum, w.Mean}
	if w.Count == 0 {
		if !w.FirstAt.IsZero() || !w.LastAt.IsZero() {
			return fmt.Errorf("empty observation window cannot contain sample timestamps")
		}
		for _, value := range values {
			if value != "" {
				return fmt.Errorf("empty observation window cannot contain numeric sample values")
			}
		}
		return nil
	}
	if w.FirstAt.IsZero() || w.LastAt.IsZero() || w.FirstAt.Before(w.Since) || !w.FirstAt.Before(w.Until) || w.LastAt.Before(w.FirstAt) || !w.LastAt.Before(w.Until) {
		return fmt.Errorf("observation sample timestamps must be ordered inside [since,until)")
	}
	if w.Count == 1 && !w.FirstAt.Equal(w.LastAt) || w.Count > 1 && !w.FirstAt.Before(w.LastAt) {
		return fmt.Errorf("observation count differs from its sample time span")
	}
	numbers := make([]*big.Rat, len(values))
	for i, value := range values {
		number, err := windowDecimal(value)
		if err != nil {
			return err
		}
		numbers[i] = number
	}
	minimum, maximum := numbers[2], numbers[3]
	if minimum.Cmp(maximum) > 0 {
		return fmt.Errorf("observation minimum exceeds maximum")
	}
	for _, index := range []int{0, 1, 5} {
		if numbers[index].Cmp(minimum) < 0 || numbers[index].Cmp(maximum) > 0 {
			return fmt.Errorf("observation first/last/mean lies outside its range")
		}
	}
	count := new(big.Rat).SetInt64(w.Count)
	if numbers[4].Cmp(new(big.Rat).Mul(minimum, count)) < 0 || numbers[4].Cmp(new(big.Rat).Mul(maximum, count)) > 0 {
		return fmt.Errorf("observation sum lies outside its count and range")
	}
	if w.Count == 1 {
		for _, number := range numbers {
			if number.Cmp(numbers[0]) != 0 {
				return fmt.Errorf("single observation sample has inconsistent aggregates")
			}
		}
	}
	return nil
}

func CheckWindowDecimal(value string) error { _, err := windowDecimal(value); return err }

func windowDecimal(value string) (*big.Rat, error) {
	if !decimalText.MatchString(value) {
		return nil, fmt.Errorf("observation values require finite exact decimal text")
	}
	return rat(value)
}

func (p *Program) window(e Expr, c Context, at string) (Value, error) {
	summary, exists := c.Windows[e.Window.Source]
	if !exists {
		return Value{}, fail("window", at, "declared observation window is unavailable")
	}
	if err := summary.Check(); err != nil {
		return Value{}, fail("window", at, err.Error())
	}
	if declared := p.schema.Windows[e.Window.Source]; declared.Unit != "" && declared.Unit != summary.Unit {
		return Value{}, fail("window", at, "observation unit differs from its declaration")
	}
	if e.Op == "over-window" && e.Window.Aggregate == "count" {
		return numericValue("integer", new(big.Rat).SetInt64(summary.Count), "", at)
	}
	if summary.Count == 0 {
		return Missing(e.Kind), nil
	}
	if e.Op == "over-window" {
		value := map[string]string{"first": summary.First, "last": summary.Last, "min": summary.Min, "max": summary.Max, "sum": summary.Sum, "mean": summary.Mean}[e.Window.Aggregate]
		n, err := windowDecimal(value)
		if err != nil {
			return Value{}, err
		}
		return numericValue(e.Kind, n, "", at)
	}
	first, _ := windowDecimal(summary.First)
	last, _ := windowDecimal(summary.Last)
	difference := new(big.Rat).Sub(last, first)
	if e.Op == "rate" {
		if summary.Count < 2 || !summary.FirstAt.Before(summary.LastAt) {
			return Missing(e.Kind), nil
		}
		seconds := new(big.Rat).SetFrac(big.NewInt(summary.LastAt.Sub(summary.FirstAt).Nanoseconds()), big.NewInt(int64(time.Second)))
		difference.Quo(difference, seconds)
	}
	return numericValue(e.Kind, difference, "", at)
}

func checkWindow(e Expr, s Schema, at string) (Type, error) {
	if e.Window.Source == "" || len(e.Args) > 0 || e.Lit != nil || e.Ref != "" || len(e.Path) > 0 || e.Agg != nil || e.In != nil {
		return Type{}, fail("window", at, "window operation needs exactly one declared summary source")
	}
	if _, exists := s.Windows[e.Window.Source]; !exists {
		return Type{}, fail("window", at, "observation summary source is undeclared")
	}
	if e.Op == "rate" || e.Op == "delta" {
		if e.Window.Aggregate != "" || !same(e.Kind, "decimal") {
			return Type{}, fail("window", at, "delta/rate return decimal without an aggregate selector")
		}
	} else if e.Op == "over-window" {
		if !slices.Contains([]string{"count", "first", "last", "min", "max", "sum", "mean"}, e.Window.Aggregate) || e.Window.Aggregate == "count" && e.Kind != "integer" || e.Window.Aggregate != "count" && !same(e.Kind, "decimal") {
			return Type{}, fail("window", at, "window aggregate needs its declared numeric result type")
		}
	} else {
		return Type{}, fail("window", at, "unsupported observation window operation")
	}
	return Type{Kind: e.Kind}, nil
}
