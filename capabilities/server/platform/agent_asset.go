package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// AgentReleaseDescriptor contains the original owner's declarative inputs.
// Native guards/case checks remain in their pinned Go owner; builder cases
// remain data compiled by Build.Agent.Definition, never another agent engine.
type AgentReleaseDescriptor struct {
	Name         string              `json:"name"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	Instructions string              `json:"instructions"`
	Tools        []string            `json:"tools"`
	ToolBindings map[string]AssetRef `json:"toolBindings,omitempty"`
	Budget       Budget              `json:"budget"`
	Checkpoints  []string            `json:"checkpoints,omitempty"`
	HandoffRole  string              `json:"handoffRole,omitempty"`
	Cases        json.RawMessage     `json:"cases,omitempty"`
	Native       bool                `json:"native,omitempty"`
	Guard        bool                `json:"guard,omitempty"`
	Handoff      bool                `json:"handoff,omitempty"`
}

func (a AgentReleaseDescriptor) Check() error {
	if !functionName.MatchString(a.Name) || a.Title == "" || strings.TrimSpace(a.Instructions) == "" || len(a.Instructions) > 65536 || len(a.Tools) > 1000 || a.Budget.Steps < 0 || a.Budget.Tokens < 0 || a.Budget.Actions < 0 || a.Budget.Cost < 0 || len(a.Cases) > 262144 {
		return fmt.Errorf("agent needs a valid identity, instructions and bounded tools/budget")
	}
	if len(a.Cases) > 0 && (!json.Valid(a.Cases) || a.Cases[0] != '[') {
		return fmt.Errorf("agent evaluation cases need an array")
	}
	for tool, ref := range a.ToolBindings {
		if !slices.Contains(a.Tools, tool) || ref.Check() != nil {
			return fmt.Errorf("agent tool binding must retain an original declared tool and asset")
		}
	}
	for _, tool := range a.Checkpoints {
		if !slices.Contains(a.Tools, tool) {
			return fmt.Errorf("agent checkpoint is not a declared tool")
		}
	}
	return nil
}

func (a AgentReleaseDescriptor) Dependencies(owner string) []AssetRef {
	refs := []AssetRef{}
	for _, tool := range a.Tools {
		if ref, ok := a.ToolBindings[tool]; ok {
			refs = append(refs, ref)
			continue
		}
		if a.Native && !strings.Contains(tool, ".") && !strings.HasPrefix(tool, "query:") {
			continue
		}
		if name, ok := strings.CutPrefix(tool, "query:"); ok {
			refs = append(refs, AssetRef{App: owner, Kind: AssetQuery, Name: name})
			continue
		}
		if strings.ContainsAny(tool, "#:") {
			continue
		} // native owner/version retains protocol/read/effect declarations
		refs = append(refs, AssetRef{App: owner, Kind: AssetAction, Name: tool})
	}
	slices.SortFunc(refs, compareRef)
	return slices.Compact(refs)
}
