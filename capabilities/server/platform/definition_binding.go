package platform

import (
	"encoding/json"
	"fmt"
	ruleexpr "platformserver/expr"
	"slices"
	"strings"
)

type Binding struct {
	Source string          `json:"source" enum:"literal,input,subject,step,item,index,answer"`
	Step   string          `json:"step,omitempty"`
	Path   []string        `json:"path,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
}

func (b Binding) Check() error {
	if !slices.Contains([]string{"literal", "input", "subject", "step", "item", "index", "answer"}, b.Source) || len(b.Path) > 16 || len(b.Value) > 64<<10 {
		return fmt.Errorf("binding needs a supported source and bounded path/value")
	}
	if b.Source == "literal" {
		v, err := DecodeValue(b.Value, 64<<10)
		if err != nil {
			return fmt.Errorf("literal binding: %w", err)
		}
		if err := ruleexpr.BoundedPredicateValue(v); err != nil {
			return err
		}
	}
	if b.Source == "literal" && (!json.Valid(b.Value) || len(b.Path) > 0 || b.Step != "") {
		return fmt.Errorf("literal binding needs one JSON value")
	}
	if b.Source == "step" && b.Step == "" {
		return fmt.Errorf("step binding needs a node")
	}
	if b.Source != "literal" && len(b.Value) > 0 || b.Source != "step" && b.Step != "" {
		return fmt.Errorf("binding configuration does not match its source")
	}
	for _, part := range b.Path {
		if part == "" || len(part) > 128 || strings.Contains(part, "\x00") {
			return fmt.Errorf("binding path needs bounded field names")
		}
	}
	return nil
}

func (b Binding) Resolve(r *Run, subject json.RawMessage) (json.RawMessage, error) {
	if err := b.Check(); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	switch b.Source {
	case "literal":
		raw = b.Value
	case "input":
		raw = r.Data
	case "subject":
		raw = subject
	case "step":
		raw = r.Outputs[b.Step]
	case "item":
		if len(r.Frames) > 0 {
			raw = r.Frames[len(r.Frames)-1].Item
		}
	case "index":
		if len(r.Frames) > 0 {
			raw, _ = json.Marshal(r.Frames[len(r.Frames)-1].Index)
		}
	case "answer":
		raw, _ = json.Marshal(r.Answer)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("binding source %s is unavailable in this scope", b.Source)
	}
	for _, part := range b.Path {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			return nil, fmt.Errorf("binding path %s does not name an object field", strings.Join(b.Path, "."))
		}
		value, ok := object[part]
		if !ok {
			return nil, fmt.Errorf("binding field %s is unavailable", part)
		}
		raw = value
	}
	return slices.Clone(raw), nil
}
