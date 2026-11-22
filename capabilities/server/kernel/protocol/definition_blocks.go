package protocol

import (
	"platformserver/platform"
)

type CapabilityDescriptor struct {
	Ref         platform.AssetRef     `json:"ref"`
	Kind        string                `json:"kind"`
	Side        string                `json:"side"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Group       string                `json:"group"`
	Icon        string                `json:"icon"`
	Tone        string                `json:"tone"`
	Source      string                `json:"source"`
	Version     string                `json:"version"`
	Revision    int                   `json:"revision,omitempty"`
	Target      string                `json:"target,omitempty"`
	Input       *platform.ValueSchema `json:"input,omitempty"`
	Output      *platform.ValueSchema `json:"output,omitempty"`
	Config      *platform.ValueSchema `json:"config,omitempty"`
	Parameters  []platform.Field      `json:"parameters,omitempty"`
	Effects     []string              `json:"effects"`
	Ports       []BlockPort           `json:"ports"`
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
		{"notify", "Send notification", "Notify the members holding a role, or the person in a member field of the record.", "People", "bell", "info"},
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
		out = append(out, CapabilityDescriptor{Ref: platform.AssetRef{App: "flow", Kind: platform.AssetKind("control"), Name: k.name}, Kind: k.name, Title: k.title, Description: k.description, Group: k.group, Icon: k.icon, Tone: k.tone, Source: "platform", Version: "1", Effects: []string{}, Ports: ports})
	}
	return out
}
