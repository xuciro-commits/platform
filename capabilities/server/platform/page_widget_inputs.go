package platform

import (
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

// page_boolean_input.go

func (d *PageDocument) checkBooleanInput(s Section) error {
	if s.Widget != "boolean-input" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if s.BooleanVariable == "" || s.BooleanLabel != nil && len(*s.BooleanLabel) > 1024 {
		return fmt.Errorf("boolean input needs an original page or overlay boolean state and bounded label")
	}
	if s.BooleanVariant != "" && (!PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.BooleanInput.RequiredUIProfile) || !slices.Contains(pageWidgets.Runtime.BooleanInput.Variants, s.BooleanVariant)) {
		return fmt.Errorf("boolean presentation needs its supported variant profile")
	}
	return nil
}

// page_choice_input.go

type PageChoiceInput struct {
	Clearable    bool     `json:"clearable,omitempty"`
	Variant      string   `json:"variant"`
	Options      []string `json:"options"`
	OptionLabels []string `json:"optionLabels,omitempty"`
	Label        *string  `json:"label,omitempty"`
}

func (d *PageDocument) checkChoiceInput(s Section) error {
	if s.Widget != "choice-input" {
		return nil
	}
	c := s.ChoiceInput
	limits := pageWidgets.Runtime.ChoiceInput
	if c == nil || !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || !slices.Contains(limits.Variants, c.Variant) || c.Options == nil || len(c.Options) > limits.MaxOptions || c.Label != nil && len(*c.Label) > 1024 {
		return fmt.Errorf("choice input needs scoped text state, bounded static options and a supported presentation")
	}
	indexed := c.Variant == "steps" || c.Variant == "tabs"
	if indexed {
		if !PageUIProfileSupports(d.UIProfile, limits.IndexedRequiredUIProfile) || len(c.Options) == 0 || len(c.OptionLabels) != len(c.Options) {
			return fmt.Errorf("indexed choice needs its profile and one label for each original index")
		}
	} else if c.OptionLabels != nil {
		return fmt.Errorf("choice option labels need the indexed presentation")
	}
	if c.Clearable && (c.Variant != "multiple" || !PageUIProfileSupports(d.UIProfile, limits.ClearRequiredUIProfile)) {
		return fmt.Errorf("clearing a choice set needs its multiple presentation and supported profile")
	}
	variable, typ := s.ChoiceVariable, "string"
	if c.Variant == "multiple" {
		if !PageUIProfileSupports(d.UIProfile, limits.MultipleRequiredUIProfile) || s.ChoiceVariable != "" {
			return fmt.Errorf("multiple choices need their string-set profile and port")
		}
		variable, typ = s.ChoiceSetVariable, "string-set"
	} else if s.ChoiceSetVariable != "" {
		return fmt.Errorf("single choice cannot bind a string-set port")
	}
	v := d.Variables[variable]
	if variable == "" || v.Type != typ || v.Mode != "state" && !(indexed && v.Mode == "constant") || v.Scope != "page" && v.Scope != "overlay" {
		return fmt.Errorf("choice needs its original scoped state type")
	}
	seen := map[string]bool{}
	for index, option := range c.Options {
		if option == "" || seen[option] || len(option) > limits.MaxOptionBytes {
			return fmt.Errorf("choice options must be unique nonempty bounded strings")
		}
		seen[option] = true
		if indexed && (option != strconv.Itoa(index) || len(c.OptionLabels[index]) > limits.MaxOptionBytes) {
			return fmt.Errorf("indexed choices need canonical original indices and bounded labels")
		}
	}
	return nil
}

// page_input.go

func (d *PageDocument) checkInputPresentation(s Section) error {
	if s.InputKind == "" {
		return nil
	}
	// A scan input is a search input read by a barcode scanner or camera
	//the same text state drives the same query search.
	required := pageWidgets.Runtime.Input.SearchRequiredUIProfile
	if s.InputKind == "scan" {
		required = pageWidgets.Runtime.Input.ScanRequiredUIProfile
	}
	if s.Widget != "input" || s.InputKind != "search" && s.InputKind != "scan" || !PageUIProfileSupports(d.UIProfile, required) {
		return fmt.Errorf("search presentation needs its original input and profile")
	}
	id := ""
	for _, node := range d.Nodes {
		if node.Section == s.ID {
			id = node.ValueVariable
		}
	}
	v := d.Variables[id]
	if id == "" || v.Type != "string" || v.Mode != "state" || v.Scope != "page" && v.Scope != "overlay" {
		return fmt.Errorf("search input needs original scoped text state")
	}
	for _, q := range d.Queries {
		if q.Search != nil && q.Search.Variable == id && q.Owner == v.Owner && q.ItemOwner == "" {
			return nil
		}
	}
	return fmt.Errorf("search input needs a same-owner original query search")
}

// page_range.go

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

// page_record_picker.go

type PageRecordPicker struct {
	LabelField string  `json:"labelField"`
	Label      *string `json:"label,omitempty"`
}

func (d *PageDocument) checkRecordPicker(s Section) error {
	if s.Widget != "record-picker" {
		return nil
	}
	if s.Model != nil {
		if !PageUIProfileSupports(d.UIProfile, PageModelProfile) {
			return fmt.Errorf("model selector requires its model binding profile")
		}
		if len(s.Fields) > 0 {
			return fmt.Errorf("model selector retains its original selection output")
		}
		if s.PickerValueVariable != "" {
			v := d.Variables[s.PickerValueVariable]
			if v.Type != "string" || v.Mode != "state" || !slices.Contains([]string{"page", "overlay"}, v.Scope) {
				return fmt.Errorf("model picker ID output requires its original scoped text state")
			}
		}
		return nil
	}
	v := d.Variables[s.CollectionVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordPicker.RequiredUIProfile) || s.RecordPicker == nil || s.RecordPicker.Label != nil && len(*s.RecordPicker.Label) > 1024 || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) {
		return fmt.Errorf("record picker needs an original scoped query window and bounded label")
	}
	if s.PickerValueVariable != "" {
		value := d.Variables[s.PickerValueVariable]
		if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordPicker.ValueRequiredUIProfile) || value.Type != "string" || value.Mode != "state" || value.Scope != v.Scope || value.Owner != v.Owner {
			return fmt.Errorf("picker ID output needs its profile and original same-owner text state")
		}
	}
	q := d.Queries[v.Source.Query]
	if q.Offset != 0 || q.Limit != pageWidgets.Runtime.RecordPicker.MaxCandidates || !slices.Equal(q.Sort, []string{"id"}) {
		return fmt.Errorf("record picker needs its fixed ID ordered candidate window")
	}
	return nil
}

// CheckRecordPicker retains the public caller while field rules live in the manifest.
func (s Section) CheckRecordPicker(info EntityInfo) error {
	if s.Widget != "record-picker" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}

func (p Page) CheckRecordPickerQuery(id string, named *Definition) error {
	if p.Document == nil {
		return nil
	}
	for _, s := range p.Sections {
		v := p.Document.Variables[s.CollectionVariable]
		if s.Widget == "record-picker" && v.Source != nil && v.Source.Query == id && named != nil && named.Query != nil && len(named.Query.Sort) > 0 && !slices.Equal(named.Query.Sort, []string{"id"}) {
			return fmt.Errorf("named query owns an incompatible record picker ordering")
		}
	}
	return nil
}
