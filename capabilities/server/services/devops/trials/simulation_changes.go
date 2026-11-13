package trials

import (
	"encoding/json"
	"strings"

	"platformkernel/kernel"
	"platformserver/platform"
	branches "platformserver/services/resources/merge"
)

func SimulatedChanges(refs []string, read func(typ string, q platform.Query) ([]any, *kernel.Error)) []SimulatedChange {
	out := []SimulatedChange{}
	for _, ref := range refs {
		typ, id, _ := strings.Cut(ref, "/")
		domain, _ := json.Marshal([]any{[]any{"id", "=", id}})
		page, err := read(typ, platform.Query{Domain: domain, Limit: 1, Archived: true})
		if err != nil || len(page) == 0 {
			continue
		}
		raw, _ := json.Marshal(page[0])
		out = append(out, SimulatedChange{Type: typ, ID: id, Record: raw})
	}
	return out
}

func (s CandidateSimulation) Summary() *branches.BranchSimulationCheck {
	steps := make([]branches.StepOutcome, len(s.Steps))
	for i, step := range s.Steps {
		steps[i] = branches.StepOutcome{Unmatched: step.Matched != nil && !*step.Matched || step.ComputeMatched != nil && !*step.ComputeMatched || step.FunctionMatched != nil && !*step.FunctionMatched, Refusal: step.Refusal}
	}
	return branches.SimulationSummary(s.CandidateID, s.Passed, steps)
}
