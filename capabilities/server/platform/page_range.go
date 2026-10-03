package platform

import (
	"fmt"
	"math/big"
	"strings"
)

// PageRangeInput presents original string drafts on an exact decimal tick grid.
// An empty draft remains an absent optional query condition.
type PageRangeInput struct {
	Min   string `json:"min"`
	Max   string `json:"max"`
	Step  string `json:"step"`
	Label string `json:"label,omitempty"`
	Unit  string `json:"unit,omitempty"`
}

func (d *PageDocument) checkRangeInput(s Section) error {
	if s.Widget != "range-input" {
		if s.RangeInput != nil || s.RangeMinVariable != "" || s.RangeMaxVariable != "" {
			return fmt.Errorf("range bindings need their widget")
		}
		return nil
	}
	r := s.RangeInput
	if r == nil || !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RangeInput.RequiredUIProfile) || s.RangeMinVariable == "" || s.RangeMaxVariable == "" || s.RangeMinVariable == s.RangeMaxVariable || len(r.Label) > 1024 || len(r.Unit) > 64 || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != "" {
		return fmt.Errorf("range needs two distinct string states and bounded presentation")
	}
	lower, upper := d.Variables[s.RangeMinVariable], d.Variables[s.RangeMaxVariable]
	if lower.Mode != "state" || upper.Mode != "state" || lower.Type != "string" || upper.Type != "string" || lower.Scope != upper.Scope || lower.Owner != upper.Owner || lower.Scope != "page" && lower.Scope != "overlay" {
		return fmt.Errorf("range drafts must share their original page or overlay state owner")
	}
	min, e1 := ParseDecimal(r.Min)
	max, e2 := ParseDecimal(r.Max)
	step, e3 := ParseDecimal(r.Step)
	if e1 != nil || e2 != nil || e3 != nil || min.Value != r.Min || max.Value != r.Max || step.Value != r.Step || step.Rat().Sign() <= 0 || min.Rat().Cmp(max.Rat()) >= 0 {
		return fmt.Errorf("range bounds and step need ordered canonical decimals")
	}
	ticks := new(big.Rat).Quo(new(big.Rat).Sub(max.Rat(), min.Rat()), step.Rat())
	if !ticks.IsInt() || ticks.Cmp(new(big.Rat).SetInt64(int64(pageWidgets.Runtime.RangeInput.MaxTicks))) > 0 {
		return fmt.Errorf("range needs an integral bounded tick grid")
	}
	// Every emitted draft must fit the original decimal input budget, including
	// fractional values between long integer endpoints.
	scale := 0
	for _, text := range []string{r.Min, r.Max, r.Step} {
		if _, fraction, ok := strings.Cut(text, "."); ok && len(fraction) > scale {
			scale = len(fraction)
		}
	}
	value := min.Rat()
	for i := int64(1); i < ticks.Num().Int64(); i++ {
		value.Add(value, step.Rat())
		text := value.FloatString(scale)
		if scale > 0 {
			text = strings.TrimSuffix(strings.TrimRight(text, "0"), ".")
		}
		if len(text) > pageWidgets.Runtime.Decimal.MaxBytes {
			return fmt.Errorf("range ticks exceed the original decimal draft budget")
		}
	}
	return nil
}
