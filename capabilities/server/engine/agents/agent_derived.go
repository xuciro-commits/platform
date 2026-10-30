package agents

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const (
	derivedObjects   = 24
	derivedActions   = 40
	derivedFunctions = 8
	derivedFields    = 40
	toolNameLimit    = 64
)

func (a *Agents) Compiled(run platform.AgentRunRecord, d *platform.CompiledAgent) *platform.CompiledAgent {
	if d == nil {
		return nil
	}
	derived := a.derived(run, d)
	if len(derived) == 0 {
		return d
	}
	out := *d
	out.Toolset = maps.Clone(d.Toolset)
	for name, tool := range derived {
		if _, declared := out.Toolset[name]; !declared {
			out.Toolset[name] = tool
		}
	}
	return &out
}

func (a *Agents) tool(run platform.AgentRunRecord, d *platform.CompiledAgent, name string) (platform.CompiledAgentTool, bool) {
	if tool, ok := d.Toolset[name]; ok {
		return tool, true
	}
	tool, ok := a.derived(run, d)[name]
	return tool, ok
}

func (a *Agents) derived(run platform.AgentRunRecord, d *platform.CompiledAgent) map[string]platform.CompiledAgentTool {
	reader := a.readsAs(run)
	if reader == nil {
		return nil
	}
	surface := a.host.AgentSurface(*reader, d.App)
	out := map[string]platform.CompiledAgentTool{}
	add := func(tool platform.CompiledAgentTool) {
		tool.Tool.Name = toolName(tool.Tool.Name)
		out[tool.Tool.Name] = tool
	}
	focus, _, _ := strings.Cut(run.Ref, "/")
	entities := map[string]platform.EntityInfo{}
	var objects []platform.EntityInfo
	for _, info := range surface.Entities {
		entities[info.Type] = info
		if !platform.StaticControlAction(platform.Action{Target: info.Type}) {
			objects = append(objects, info)
		}
	}
	slices.SortFunc(objects, func(x, y platform.EntityInfo) int {
		if (x.Type == focus) != (y.Type == focus) {
			if x.Type == focus {
				return -1
			}
			return 1
		}
		return strings.Compare(x.Type, y.Type)
	})
	for _, info := range objects[:min(len(objects), derivedObjects)] {
		add(objectTool(info, "load"))
		add(objectTool(info, "aggregate"))
	}
	actions := slices.DeleteFunc(slices.Clone(surface.Actions), func(x platform.AgentAction) bool {
		return platform.StaticControlAction(x.Action) || x.Action.ConfirmationOnly
	})
	slices.SortFunc(actions, func(x, y platform.AgentAction) int {
		if (x.Action.Target == focus) != (y.Action.Target == focus) {
			if x.Action.Target == focus {
				return -1
			}
			return 1
		}
		return strings.Compare(x.Action.Schema, y.Action.Schema)
	})
	for _, x := range actions[:min(len(actions), derivedActions)] {
		add(actionTool(x, entities))
	}
	functions := slices.Clone(surface.Functions)
	slices.SortFunc(functions, func(x, y platform.AgentFunction) int { return strings.Compare(x.Function.Name, y.Function.Name) })
	for _, f := range functions[:min(len(functions), derivedFunctions)] {
		add(functionTool(f))
	}
	return out
}

func toolName(name string) string {
	name = agentToolName(name)
	if len(name) <= toolNameLimit {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return name[:toolNameLimit-9] + "_" + hex.EncodeToString(sum[:4])
}

func fieldList(info platform.EntityInfo) string {
	var parts []string
	for _, f := range info.Fields[:min(len(info.Fields), derivedFields)] {
		part := f.Name + " (" + f.Type
		if len(f.Choices) > 0 {
			part += ": " + strings.Join(f.Choices, "|")
		}
		if f.Ref != "" {
			part += " of " + f.Ref
		}
		parts = append(parts, part+")")
	}
	return strings.Join(parts, ", ")
}

func objectTool(info platform.EntityInfo, op string) platform.CompiledAgentTool {
	properties := map[string]any{
		"domain": map[string]any{"type": "array", "items": map[string]any{"type": "array"}, "description": "Filters in the prefix form, like [[\"field\",\"=\",\"value\"]]"},
		"search": map[string]any{"type": "string", "description": "Words to find"},
	}
	kind, verb := "objects", "Load a page of"
	if op == "aggregate" {
		kind, verb = "aggregate", "Count, group and total"
		properties["groups"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Fields to group by"}
		properties["measures"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "count, or sum:field, avg:field, min:field, max:field"}
	} else {
		properties["sort"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Fields to order by; -field for descending"}
		properties["limit"] = map[string]any{"type": "integer", "description": "Records in the page, at most 50"}
		properties["offset"] = map[string]any{"type": "integer", "description": "Records to skip"}
	}
	properties["rationale"] = rationale()
	plural := cmp.Or(info.Plural, info.Title, info.Type)
	return platform.CompiledAgentTool{Kind: kind, Schema: info.Type, Tool: platform.Tool{
		Name:        op + "_" + info.Type,
		Description: fmt.Sprintf("%s %s (%s) you may read. Fields: %s.", verb, plural, info.Type, fieldList(info)),
		Properties:  properties, Required: []string{"rationale"}}}
}

func actionTool(x platform.AgentAction, entities map[string]platform.EntityInfo) platform.CompiledAgentTool {
	action := x.Action
	var info *platform.EntityInfo
	if entity, ok := entities[action.Target]; ok {
		info = &entity
	}
	description := action.Title + ": " + action.Description
	if len(x.Conditions) > 0 {
		description += " Refused unless: " + strings.Join(x.Conditions, "; ") + "."
	}
	if action.NeedsApproval {
		description += " Needs approval."
	}
	description += " Drafted for a person to confirm."
	target := "The ID of the " + cmp.Or(action.Target, "record") + " the action is about"
	if action.New {
		target = "The ID of the new " + cmp.Or(action.Target, "record") + ", short and unique"
	}
	return platform.CompiledAgentTool{Kind: "action", Schema: action.Schema, Target: action.Target, Owner: x.Owner, Tool: platform.Tool{
		Name:        action.Schema,
		Description: description,
		Properties: map[string]any{"target": map[string]any{"type": "string", "description": target}, "rationale": rationale(),
			"payload": platform.ActionInputSchema(action, info, nil)},
		Required: []string{"target", "rationale", "payload"}}}
}

func functionTool(f platform.AgentFunction) platform.CompiledAgentTool {
	properties := map[string]any{
		"source":    map[string]any{"type": "string", "description": "The ID of the " + f.Function.Object + " record it reads"},
		"rationale": rationale(),
	}
	required := []string{"source", "rationale"}
	if f.Function.Conversation {
		properties["question"] = map[string]any{"type": "string", "description": "The question to answer"}
		required = append(required, "question")
	}
	return platform.CompiledAgentTool{Kind: "function", Schema: f.Call.Action.Schema, Target: f.Call.Action.Target, Owner: f.Call.Owner,
		Preset: map[string]any{"name": f.Function.Name, "app": f.Owner}, Tool: platform.Tool{
			Name:        "call_" + f.Owner + "_" + f.Function.Name,
			Description: fmt.Sprintf("%s: %s Asks the AI function over a %s record and keeps its suggestion for review. Drafted for a person to confirm.", f.Function.Title, f.Function.Description, f.Function.Object),
			Properties:  properties, Required: required}}
}

func (a *Agents) objects(run platform.AgentRunRecord, tool platform.CompiledAgentTool, args map[string]any, now time.Time) (platform.AgentObservation, *kernel.Error) {
	reader := a.readsAs(run)
	if reader == nil {
		return platform.AgentObservation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A read needs a reader")
	}
	set := map[string]any{"object": tool.Schema}
	for _, key := range []string{"domain", "search"} {
		if value, ok := args[key]; ok && value != nil {
			set[key] = value
		}
	}
	request := map[string]any{"set": set}
	op := "load"
	if tool.Kind == "aggregate" {
		op = "aggregate"
		for _, key := range []string{"groups", "measures"} {
			if value, ok := args[key]; ok && value != nil {
				request[key] = value
			}
		}
	} else {
		for _, key := range []string{"sort", "limit", "offset"} {
			if value, ok := args[key]; ok && value != nil {
				request[key] = value
			}
		}
	}
	raw, _ := json.Marshal(request)
	return a.host.AgentObjects(*reader, op, raw, now)
}
