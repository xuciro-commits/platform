package platform

import (
	"fmt"
	"math"
	"slices"
)

// ProposalPolicy places a typed function's answer in the original review path.
// A nil policy remains read-only advice. It never grants a business action.
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

// ProposalAdoption consumes a retained call record. This is not a function
// invocation: the immutable contract and version travel with that record.
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

// A trial may produce proposals, but cannot execute their adoption.
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
