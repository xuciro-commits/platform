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
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	r := s.RangeInput
	if r == nil || !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RangeInput.RequiredUIProfile) || len(r.Label) > 1024 || len(r.Unit) > 64 {
		return fmt.Errorf("range needs two distinct string states and bounded presentation")
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
