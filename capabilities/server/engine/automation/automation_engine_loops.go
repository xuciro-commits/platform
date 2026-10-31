package automation

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/platform"
	"slices"
)

func (ss *session) sources(x *FlowInstance, sources []string) {
	x.Sources = slices.Compact(slices.Sorted(slices.Values(append(x.Sources, sources...))))
}

func (ss *session) output(x *FlowInstance, token int, raw json.RawMessage) bool {
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	if len(raw) > 48<<10 || !json.Valid(raw) {
		return false
	}
	tok := ss.token(x, token)
	if tok.Outputs == nil {
		tok.Outputs = map[string]json.RawMessage{}
	}
	if x.Outputs == nil {
		x.Outputs = map[string]json.RawMessage{}
	}
	proposed := maps.Clone(x.Outputs)
	proposed[tok.Step] = raw
	encoded, _ := json.Marshal(proposed)
	if len(encoded) > 60<<10 {
		return false
	}
	tok.Outputs[tok.Step], x.Outputs[tok.Step] = slices.Clone(raw), slices.Clone(raw)
	return true
}

func (ss *session) outputRefused(x *FlowInstance, token int) {
	tok := ss.token(x, token)
	step := ss.def(x).steps[tok.Step]
	var canceled []Token
	for _, candidate := range x.Tokens {
		if candidate.ID != token && ss.descendant(x, candidate.ID, token) {
			canceled = append(canceled, candidate)
		}
	}
	ss.stopPaths(&FlowInstance{ID: x.ID, Flow: x.Flow, Version: x.Version, OnBehalf: x.OnBehalf, Tokens: canceled})
	ids := map[int]bool{}
	for _, candidate := range canceled {
		ids[candidate.ID] = true
	}
	x.Tokens = slices.DeleteFunc(x.Tokens, func(candidate Token) bool { return ids[candidate.ID] })
	tok = ss.token(x, token)
	tok.Attempts = stepAttempts
	ss.failed(x, token, step, "flow output exceeds its bounded saved-result budget")
}

func (ss *session) beginLoop(x *FlowInstance, token int, step *platform.Step, app platform.Caller, run *platform.Run) {
	tok := ss.token(x, token)
	frame := &LoopFrame{Results: map[int]json.RawMessage{}, While: step.Loop.While != nil, Outer: slices.Collect(maps.Keys(tok.Outputs))}
	if frame.While && step.Loop.Initial != nil {
		state, err := step.Loop.Initial(app, run)
		if err != nil {
			ss.failed(x, token, step, err.Error())
			return
		}
		frame.State = state
	}
	if step.Loop.Items != nil {
		items, err := step.Loop.Items(app, run)
		if err != nil {
			ss.failed(x, token, step, err.Error())
			return
		}
		if len(items) > step.Loop.MaxIterations {
			ss.failed(x, token, step, "collection exceeds maximum iterations")
			return
		}
		rawItems, _ := json.Marshal(items)
		if len(rawItems) > 48<<10 {
			ss.outputRefused(x, token)
			return
		}
		frame.Items = items
	}
	tok.Waits, tok.Loop = "loop", frame
	ss.fillLoop(x, token)
}

func (ss *session) fillLoop(x *FlowInstance, token int) {
	parent := ss.token(x, token)
	if parent.ID < 0 || parent.Loop == nil {
		return
	}
	step := ss.def(x).steps[parent.Step]
	loop, frame := step.Loop, parent.Loop
	active := 0
	for _, tok := range x.Tokens {
		if tok.Parent == token {
			active++
		}
	}
	for active < loop.Concurrency {
		parent = ss.token(x, token)
		if frame.While {
			run := ss.run(x, token)
			run.Frames = append(run.Frames, platform.Frame{Node: step.Name, Index: frame.Next, Item: frame.State})
			holds, err := loop.While(ss.app(x), run)
			if err != nil {
				ss.failed(x, token, step, err.Error())
				return
			}
			if !holds {
				break
			}
			if frame.Next >= loop.MaxIterations {
				ss.failed(x, token, step, "while exceeded maximum iterations")
				return
			}
		} else if frame.Next >= len(frame.Items) {
			break
		}
		index := frame.Next
		frame.Next++
		var item json.RawMessage
		if !frame.While {
			item = frame.Items[index]
		} else {
			item = frame.State
		}
		child := Token{ID: ss.tokenID(x), Step: loop.Body, Parent: token, Waits: "ready", Frames: append(slices.Clone(parent.Frames), platform.Frame{Node: step.Name, Index: index, Item: item}), Input: slices.Clone(parent.Input), Outputs: maps.Clone(parent.Outputs)}
		x.Tokens = append(x.Tokens, child)
		active++
	}
	if active == 0 {
		result := make([]json.RawMessage, frame.Next)
		for i := range result {
			result[i] = frame.Results[i]
		}
		if !ss.output(x, token, platform.Raw(map[string]any{"items": result, "count": frame.Next})) {
			ss.outputRefused(x, token)
			return
		}
		ss.trace(x, parent.Step, "iterated", fmt.Sprintf("%d saved results", frame.Next), "")
		ss.next(x, token, "")
	}
}

func (ss *session) controlLoop(x *FlowInstance, token int, step *platform.Step) {
	current := ss.token(x, token)
	ancestor := current.Parent
	for ancestor != 0 {
		parent := ss.token(x, ancestor)
		if parent.ID < 0 {
			break
		}
		if parent.Waits == "loop" {
			break
		}
		ancestor = parent.Parent
	}
	parent := ss.token(x, ancestor)
	if ancestor == 0 || parent.Loop == nil {
		ss.stuck(x, token, "loop control has no enclosing scope")
		return
	}
	iteration := token
	for ss.token(x, iteration).Parent != ancestor {
		iteration = ss.token(x, iteration).Parent
	}
	if step.Output != nil {
		raw, err := step.Output(ss.app(x), ss.run(x, token))
		if err != nil {
			ss.failed(x, token, step, err.Error())
			return
		}
		if !ss.output(x, token, raw) {
			ss.outputRefused(x, token)
			return
		}
	}
	value := maps.Clone(ss.token(x, token).Outputs)
	var stop []Token
	for _, candidate := range x.Tokens {
		if candidate.ID != ancestor && (step.LoopControl == "break" && ss.descendant(x, candidate.ID, ancestor) || step.LoopControl == "continue" && candidate.ID != iteration && ss.descendant(x, candidate.ID, iteration)) {
			stop = append(stop, candidate)
		}
	}
	ss.stopPaths(&FlowInstance{ID: x.ID, Flow: x.Flow, Version: x.Version, OnBehalf: x.OnBehalf, Tokens: stop})
	ids := map[int]bool{}
	for _, candidate := range stop {
		ids[candidate.ID] = true
	}
	x.Tokens = slices.DeleteFunc(x.Tokens, func(candidate Token) bool { return ids[candidate.ID] })
	if step.LoopControl == "continue" {
		root := ss.token(x, iteration)
		root.Outputs = value
		root.Step = step.Name
		ss.trace(x, step.Name, "continued", "the enclosing iteration", "")
		ss.endPath(x, iteration)
		return
	}
	parent = ss.token(x, ancestor)
	results := []json.RawMessage{}
	for _, index := range slices.Sorted(maps.Keys(parent.Loop.Results)) {
		results = append(results, parent.Loop.Results[index])
	}
	if !ss.output(x, ancestor, platform.Raw(map[string]any{"items": results, "count": len(results), "stopped": true})) {
		ss.outputRefused(x, ancestor)
		return
	}
	ss.trace(x, step.Name, "broke", "pending iterations canceled; accepted actions retained", "")
	ss.next(x, ancestor, "")
}

func (ss *session) raceFailure(x *FlowInstance, token int, why string) bool {
	current := ss.token(x, token)
	ancestor := current.Parent
	for ancestor != 0 {
		parent := ss.token(x, ancestor)
		if parent.ID < 0 {
			return false
		}
		if parent.Waits == "join" && len(ss.def(x).steps[parent.Step].Any) > 0 {
			break
		}
		ancestor = parent.Parent
	}
	if ancestor == 0 {
		return false
	}
	branch := token
	for ss.token(x, branch).Parent != ancestor {
		branch = ss.token(x, branch).Parent
	}
	parent := ss.token(x, ancestor)
	if parent.Results == nil {
		parent.Results = map[string]json.RawMessage{}
	}
	parent.Results[ss.token(x, branch).Branch] = platform.Raw(map[string]string{"error": why})
	var canceled []Token
	for _, candidate := range x.Tokens {
		if ss.descendant(x, candidate.ID, branch) {
			canceled = append(canceled, candidate)
		}
	}
	ss.stopPaths(&FlowInstance{ID: x.ID, Flow: x.Flow, Version: x.Version, OnBehalf: x.OnBehalf, Tokens: canceled})
	ids := map[int]bool{}
	for _, candidate := range canceled {
		ids[candidate.ID] = true
	}
	x.Tokens = slices.DeleteFunc(x.Tokens, func(candidate Token) bool { return ids[candidate.ID] })
	if !slices.ContainsFunc(x.Tokens, func(candidate Token) bool { return candidate.Parent == ancestor }) {
		parent = ss.token(x, ancestor)
		parent.Attempts = stepAttempts
		ss.failed(x, ancestor, ss.def(x).steps[parent.Step], "all race paths failed: "+why)
	}
	return true
}
