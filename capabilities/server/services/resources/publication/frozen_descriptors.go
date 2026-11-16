package publication

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

func QueryObjectDescriptor(body []byte) (platform.EntityInfo, error) {
	var shape struct {
		Type            string                       `json:"type"`
		Implements      []string                     `json:"implements"`
		Extends         string                       `json:"extends"`
		ExtensionSource *platform.ExtensionSource    `json:"extensionSource"`
		Entity          *platform.EntityInfo         `json:"entity"`
		ObjectView      *platform.ObjectView         `json:"objectView,omitempty"`
		Fields          []map[string]json.RawMessage `json:"fields"`
		Lifecycle       *platform.LifecycleInfo      `json:"lifecycle"`
		States          []platform.State             `json:"states"`
		Actions         []struct {
			Transaction *platform.BusinessTxn `json:"transaction"`
			Name        string                `json:"name"`
			Title       string                `json:"title"`
			From        []string              `json:"from"`
			Inputs      []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Required bool   `json:"required"`
				Choices  string `json:"choices"`
			} `json:"inputs"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(body, &shape); err != nil {
		return platform.EntityInfo{}, err
	}
	if shape.Entity != nil {
		if shape.Entity.Type != shape.Type {
			return platform.EntityInfo{}, fmt.Errorf("frozen native object identity differs")
		}
		return *shape.Entity, nil
	}
	var fields []platform.FieldInfo
	for _, raw := range shape.Fields {
		if choicesRaw, ok := raw["choices"]; ok {
			var source string
			if json.Unmarshal(choicesRaw, &source) == nil {
				options := []string{}
				for _, part := range strings.Split(source, ",") {
					if value := strings.TrimSpace(part); value != "" {
						options = append(options, value)
					}
				}
				raw["choices"], _ = json.Marshal(options)
			}
		}
		if whenRaw, ok := raw["when"]; ok {
			var source string
			if json.Unmarshal(whenRaw, &source) == nil {
				condition, err := platform.ParseWhen(source)
				if err != nil {
					return platform.EntityInfo{}, err
				}
				if condition == nil {
					delete(raw, "when")
				} else {
					raw["when"], _ = json.Marshal(condition)
				}
			}
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return platform.EntityInfo{}, err
		}
		var field platform.FieldInfo
		if err := json.Unmarshal(encoded, &field); err != nil {
			return platform.EntityInfo{}, err
		}
		fields = append(fields, field)
	}
	owner, _, _ := strings.Cut(shape.Type, ".")
	if shape.Lifecycle == nil && len(shape.States) > 0 {
		l := platform.LifecycleInfo{Field: "state", Initial: shape.States[0].Name, States: shape.States}
		choices := []string{}
		for _, s := range shape.States {
			choices = append(choices, s.Name)
		}
		fields = append(fields, platform.FieldInfo{Name: "state", Title: "State", Type: "choice", ReadOnly: true, Choices: choices})
		for _, a := range shape.Actions {
			inputs := make([]platform.Field, 0, len(a.Inputs))
			for _, input := range a.Inputs {
				kind := input.Type
				if kind == "choice" {
					kind = "string"
				}
				var options []string
				for _, option := range strings.Split(input.Choices, ",") {
					if value := strings.TrimSpace(option); value != "" {
						options = append(options, value)
					}
				}
				inputs = append(inputs, platform.Field{Name: input.Name, Type: kind, Required: input.Required, Choices: options})
			}
			if to, input := platform.TransactionDestination(shape.Type, "state", a.Transaction, inputs); len(to) > 0 {
				if slices.ContainsFunc(to, func(state string) bool { return !slices.Contains(choices, state) }) {
					return platform.EntityInfo{}, fmt.Errorf("frozen object transaction destination differs")
				}
				l.Transitions = append(l.Transitions, platform.TransitionInfo{Name: a.Name, Schema: shape.Type + "." + a.Name, Title: a.Title, From: a.From, To: to, ToInput: input})
				continue
			}
			l.Transitions = append(l.Transitions, platform.TransitionInfo{Name: a.Name, Schema: shape.Type + "." + a.Name, Title: a.Title, From: a.From, To: slices.Clone(a.From)})
		}
		shape.Lifecycle = &l
	}
	return platform.EntityInfo{App: owner, Type: shape.Type, ObjectView: shape.ObjectView, Standard: []string{shape.Type + ".create", shape.Type + ".edit", shape.Type + ".archive"}, Fields: fields, Lifecycle: shape.Lifecycle, Implements: shape.Implements, Extends: shape.Extends, ExtensionSource: shape.ExtensionSource}, nil
}

func ReleaseActionDescriptor(body json.RawMessage) (platform.Action, error) {
	var flat platform.Action
	if err := json.Unmarshal(body, &flat); err != nil {
		return platform.Action{}, err
	}
	var envelope struct {
		Action *platform.Action `json:"action"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return platform.Action{}, err
	}
	if envelope.Action != nil {
		if envelope.Action.Schema != flat.Schema || envelope.Action.Target != flat.Target {
			return platform.Action{}, fmt.Errorf("frozen action declaration identity differs")
		}
		return *envelope.Action, nil
	}
	return flat, nil
}
