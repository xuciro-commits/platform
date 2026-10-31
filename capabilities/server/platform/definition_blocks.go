package platform

import (
	"fmt"
	"math"
	"slices"
)

type CapabilityDescriptor struct {
	Ref         AssetRef     `json:"ref"`
	Kind        string       `json:"kind"`
	Side        string       `json:"side"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Group       string       `json:"group"`
	Icon        string       `json:"icon"`
	Tone        string       `json:"tone"`
	Source      string       `json:"source"`
	Version     string       `json:"version"`
	Revision    int          `json:"revision,omitempty"`
	Target      string       `json:"target,omitempty"`
	Input       *ValueSchema `json:"input,omitempty"`
	Output      *ValueSchema `json:"output,omitempty"`
	Config      *ValueSchema `json:"config,omitempty"`
	Parameters  []Field      `json:"parameters,omitempty"`
	Effects     []string     `json:"effects"`
	Ports       []BlockPort  `json:"ports"`
}

type BlockPort struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Direction string `json:"direction"`
	Channel   string `json:"channel"`
	Type      string `json:"type"`
}

func ControlBlocks() []CapabilityDescriptor {
	kinds := []struct{ name, title, description, group, icon, tone string }{
		{"payload", "Input", "Start with the workflow's typed input.", "Flow", "log-in", "neutral"},
		{"transform", "Transform", "Bind fields and constants into an output object.", "Data", "braces", "info"},
		{"branch", "Branch", "Choose a true or false control path.", "Logic", "git-branch", "warning"},
		{"switch", "Switch", "Choose a named case or the default path.", "Logic", "split", "warning"},
		{"foreach", "For each", "Run a bounded body over a collection.", "Collections", "repeat", "info"},
		{"while", "While", "Repeat a scoped body within an explicit limit.", "Collections", "repeat", "info"},
		{"fork", "Parallel paths", "Start explicit paths and wait for all or the first result.", "Logic", "split", "info"},
		{"join", "Join", "Complete the current declared parallel scope.", "Logic", "merge", "info"},
		{"ask", "Human task", "Wait for a governed human answer in the existing inbox.", "People", "user-round", "warning"},
		{"wait", "Wait", "Suspend on the platform's durable timer.", "Flow", "clock", "neutral"},
		{"subflow", "Run workflow", "Call a retained native workflow.", "Flow", "workflow", "info"},
		{"break", "Break", "Finish the enclosing loop and cancel its pending iterations.", "Collections", "circle-stop", "warning"},
		{"continue", "Continue", "Finish this iteration and move to the next item.", "Collections", "step-forward", "info"},
		{"end", "Return", "Finish with an explicit output value.", "Flow", "log-out", "success"},
		{"fail", "Fail", "Stop with a clear failure.", "Flow", "circle-alert", "danger"},
	}
	out := make([]CapabilityDescriptor, 0, len(kinds))
	for _, k := range kinds {
		ports := []BlockPort{{ID: "in", Title: "In", Direction: "input", Channel: "control", Type: "flow"}, {ID: "next", Title: "Next", Direction: "output", Channel: "control", Type: "flow"}}
		if k.name == "payload" {
			ports = ports[1:]
		}
		if k.name == "end" || k.name == "fail" || k.name == "break" || k.name == "continue" {
			ports = ports[:1]
		}
		if k.name == "branch" {
			ports = append(ports[:1], BlockPort{ID: "true", Title: "True", Direction: "output", Channel: "control", Type: "flow"}, BlockPort{ID: "false", Title: "False", Direction: "output", Channel: "control", Type: "flow"})
		}
		if k.name == "foreach" || k.name == "while" {
			ports = append(ports[:1], BlockPort{ID: "body", Title: "Loop", Direction: "output", Channel: "control", Type: "flow"}, BlockPort{ID: "next", Title: "Done", Direction: "output", Channel: "control", Type: "flow"})
		}
		out = append(out, CapabilityDescriptor{Ref: AssetRef{App: "flow", Kind: AssetKind("control"), Name: k.name}, Kind: k.name, Title: k.title, Description: k.description, Group: k.group, Icon: k.icon, Tone: k.tone, Source: "platform", Version: "1", Effects: []string{}, Ports: ports})
	}
	return out
}

type ProposalPolicy struct {
	HandoffRole         string  `json:"handoffRole,omitempty"`
	ConfidenceField     string  `json:"confidenceField,omitempty"`
	MinimumConfidence   float64 `json:"minimumConfidence,omitempty"`
	ExpiresAfterSeconds int     `json:"expiresAfterSeconds"`
}

func (p ProposalPolicy) Check(output []Field) error {
	if p.ExpiresAfterSeconds < 1 || p.ExpiresAfterSeconds > 604800 || math.IsNaN(p.MinimumConfidence) || math.IsInf(p.MinimumConfidence, 0) || p.MinimumConfidence < 0 || p.MinimumConfidence > 1 {
		return fmt.Errorf("Proposal review needs an expiry of 1–604800 seconds and bounded confidence")
	}
	if p.ConfidenceField != "" {
		i := slices.IndexFunc(output, func(f Field) bool { return f.Name == p.ConfidenceField })
		if i < 0 || !output[i].Required || (output[i].Type != "decimal" && output[i].Type != "integer") || p.HandoffRole == "" {
			return fmt.Errorf("Confidence handover needs a required numeric output and reviewer role")
		}
	}
	if p.ConfidenceField == "" && p.MinimumConfidence != 0 {
		return fmt.Errorf("Choose a confidence field before setting its threshold")
	}
	return nil
}

type ProposalAdoption struct {
	Function FunctionRef     `json:"function"`
	Input    string          `json:"input"`
	Mapping  []ProposalInput `json:"mapping"`
}

type ProposalInput struct {
	Output string `json:"output"`
	Input  string `json:"input"`
}

func (p ProposalAdoption) Check() error {
	if !functionName.MatchString(p.Function.Name) || p.Function.Version < 0 || p.Function.Version > 64 || !functionFieldName.MatchString(p.Input) || len(p.Mapping) == 0 || len(p.Mapping) > 16 {
		return fmt.Errorf("Adoption needs a function, proposal input and 1–16 output mappings")
	}
	outputs, inputs := map[string]bool{}, map[string]bool{}
	for _, m := range p.Mapping {
		if !functionFieldName.MatchString(m.Output) || !functionFieldName.MatchString(m.Input) || m.Input == p.Input || outputs[m.Output] || inputs[m.Input] {
			return fmt.Errorf("Proposal output and action input mappings must be unique")
		}
		outputs[m.Output], inputs[m.Input] = true, true
	}
	return nil
}

type SimulationRuntime interface{ Simulating() bool }

func (c Caller) Simulating() bool { r, ok := c.rt.(SimulationRuntime); return ok && r.Simulating() }

func (p ProposalAdoption) CheckMapping(f AIFunction, inputs []Field) error {
	if err := p.Check(); err != nil {
		return err
	}
	if f.Proposal == nil || f.Name != p.Function.Name {
		return fmt.Errorf("Adoption needs a declared business proposal function")
	}
	for _, m := range p.Mapping {
		output := slices.IndexFunc(f.Output, func(field Field) bool { return field.Name == m.Output })
		input := slices.IndexFunc(inputs, func(field Field) bool { return field.Name == m.Input })
		if output < 0 || input < 0 || !(f.Output[output].Type == inputs[input].Type || f.Output[output].Type == "decimal" && inputs[input].Type == "number") {
			return fmt.Errorf("Proposal mapping types do not match the business action")
		}
	}
	return nil
}

func CheckApplicationBindings(bindings []AssetBinding) error {
	if len(bindings) > 1000 {
		return fmt.Errorf("application bindings exceed the owner budget")
	}
	seen := map[AssetRef]bool{}
	for _, binding := range bindings {
		if binding.Ref.Check() != nil || binding.Ref.Kind != AssetApp || binding.SourceVersion == "" || seen[binding.Ref] {
			return fmt.Errorf("application binding is invalid or repeated")
		}
		seen[binding.Ref] = true
	}
	return nil
}
