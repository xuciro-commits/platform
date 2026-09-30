package build

import (
	"bytes"
	"encoding/json"
	"fmt"

	"platformserver/platform"
)

// UnmarshalJSON upgrades the old stored representation once, on result
// decoding. Every caller sees the same canonical node/binding model; there is
// no legacy validator, scheduler or compiler beside the current one.
func (s *ProcessStep) UnmarshalJSON(raw []byte) error {
	type current ProcessStep
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	legacy := false
	if kind, ok := fields["kind"]; !ok || bytes.Equal(bytes.TrimSpace(kind), []byte(`""`)) {
		legacy = true
	}
	if branches, ok := fields["branches"]; ok && len(bytes.TrimSpace(branches)) > 0 && bytes.TrimSpace(branches)[0] == '{' {
		if _, present := fields["cases"]; present {
			return fmt.Errorf("process step cannot keep both old and current answer paths")
		}
		fields["cases"] = branches
		delete(fields, "branches")
		legacy = true
	}
	if inputs, ok := fields["inputs"]; ok {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(inputs, &values); err != nil {
			return err
		}
		converted := map[string]platform.Binding{}
		for name, value := range values {
			var binding platform.Binding
			var object map[string]json.RawMessage
			if json.Unmarshal(value, &object) == nil && object["source"] != nil {
				if err := json.Unmarshal(value, &binding); err != nil {
					return err
				}
			} else if legacy {
				binding = platform.Binding{Source: "literal", Value: value}
			} else {
				return fmt.Errorf("process input %s needs a typed binding", name)
			}
			converted[name] = binding
		}
		fields["inputs"], _ = json.Marshal(converted)
	}
	canonical, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	var node current
	if err := decoder.Decode(&node); err != nil {
		return err
	}
	if node.Kind == "" && legacy {
		count := 0
		if node.Ask != "" {
			node.Kind = "ask"
			count++
		}
		if node.Act != "" {
			node.Kind = "action"
			count++
		}
		if node.Function != nil {
			node.Kind = "ai"
			count++
		}
		if count != 1 {
			return fmt.Errorf("legacy process node needs exactly one original owner operation")
		}
	}
	*s = ProcessStep(node)
	return nil
}

// Retained legacy versions keep their original semantic release bytes while
// their nodes decode into the current native compiler. New edits/publications
// always write the canonical representation.
func (p *Process) UnmarshalJSON(raw []byte) error {
	type image Process
	var saved image
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	var versions []json.RawMessage
	if json.Unmarshal(fields["steps"], &versions) == nil && saved.Version > 0 && len(saved.Versions) == 0 && saved.Published == "" {
		for _, step := range versions {
			var node map[string]json.RawMessage
			json.Unmarshal(step, &node)
			if node["kind"] == nil {
				saved.originalDefinition = append(json.RawMessage{}, raw...)
				break
			}
		}
	}
	*p = Process(saved)
	return nil
}
