package automation

import (
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func (ss *session) failed(x *FlowInstance, token int, step *platform.Step, why string) {
	tok := ss.token(x, token)
	limit := stepAttempts
	var delay time.Duration
	if step.Retry != nil {
		if step.Retry.Attempts > 0 {
			limit = step.Retry.Attempts
		}
		delay = step.Retry.Backoff
	}
	if step.NoRetry {
		tok.Attempts = limit
	}
	tok.Attempts++
	tok.Error = why
	switch {
	case tok.Attempts < limit:
		if delay <= 0 {
			delay = backoff(tok.Attempts)
		}
		tok.Waits, tok.Due = "retry", ss.now.Add(delay)
		ss.trace(x, tok.Step, "retry", why, "")
	case step.Act != nil && ss.def(x).Continuous != nil && ss.def(x).Continuous.ErrorPolicy == "deadLetter":
		ss.output(x, token, platform.Raw(map[string]string{"error": why}))
		ss.trace(x, tok.Step, "dead letter", why, "")
		ss.next(x, token, "")
	case step.Fault != "":
		ss.output(x, token, platform.Raw(map[string]string{"error": why}))
		ss.trace(x, tok.Step, "fault", why, "")
		ss.next(x, token, step.Fault)
	default:
		if ss.raceFailure(x, token, why) {
			return
		}
		ss.failedUnhandled(x, tok.Step, why)
		ss.compensate(x, tok.Step+": "+why)
	}
}

func (ss *session) failedUnhandled(x *FlowInstance, step, why string) {
	d := ss.def(x)
	a := platform.Assignment{Key: fmt.Sprintf("flow:%s:%d", x.ID, x.Seq), Ref: InstanceType + "/" + x.ID,
		Title: fmt.Sprintf("Flow %s failed at %s", x.Title, step),
		Body:  why + ". What it had done is undone; see what to do next, then mark this done."}
	x.Seq++
	for _, role := range d.Owners {
		a.To = append(a.To, platform.Recipient{AppRole: role})
	}
	if x.OnBehalf != "" && !strings.HasPrefix(x.OnBehalf, "app:") {
		a.To = append(a.To, platform.Recipient{Member: x.OnBehalf})
	}
	ss.assigns = append(ss.assigns, flowTask{app: d.app, Assignment: a})
}

func (ss *session) compensate(x *FlowInstance, why string) {
	ss.stopPaths(x)
	x.State = "compensating"
	x.Tokens = []Token{{ID: ss.tokenID(x), Step: "@undo", Waits: "ready"}}
	if runs := ss.f.host.Runs(); runs != nil {
		for _, run := range runs.Finished(ss.c, x.ID, "") {
			ss.signals = append(ss.signals, runSignal{run: run, RunSignal: platform.RunSignal{At: ss.now, Kind: "undone", By: "flow", Detail: why}})
		}
	}
	ss.trace(x, "", "compensating", why, "")
}

func (ss *session) stopPaths(x *FlowInstance) {
	for _, t := range x.Tokens {
		if t.Waits == "approval" && t.Child != "" {
			ss.withdrawals = append(ss.withdrawals, approvalWithdrawal{app: ss.def(x).app, id: t.Child, member: x.OnBehalf})
		}
		if t.Operation != "" {
			ss.cancelOperations = append(ss.cancelOperations, t.Operation)
		}
		if t.Task != "" {
			ss.close = append(ss.close, t.Task)
		}
		if t.Child != "" {
			if child := ss.load(t.Child); child != nil && !ended(child.State) {
				ss.stopPaths(child)
				child.State, child.Tokens = "canceled", nil
				ss.trace(child, "", "canceled", "its caller stopped", "")
			}
		}
	}
}

func (ss *session) undo(x *FlowInstance, token int) {
	if len(x.Undo) == 0 {
		ss.finish(x, "compensated")
		return
	}
	u := x.Undo[len(x.Undo)-1]
	if _, err := ss.act(x, u.App, u.Protocol, u.Action, u.Target, u.Payload, fmt.Sprintf("undo:%s:%d", x.ID, len(x.Undo))); err != nil {
		tok := ss.token(x, token)
		tok.Attempts++
		tok.Error = err.Error()
		if tok.Attempts < stepAttempts {
			tok.Waits, tok.Due = "undo", ss.now.Add(backoff(tok.Attempts))
			ss.trace(x, u.Step, "retry undo", err.Error(), "")
			return
		}
		ss.stuck(x, token, "undoing "+u.Step+": "+err.Error())
		return
	}
	x.Undo = x.Undo[:len(x.Undo)-1]
	ss.trace(x, u.Step, "undone", strings.TrimPrefix(u.Protocol+" ", " ")+u.Action+" "+u.Target, "")
}

func (ss *session) stuck(x *FlowInstance, token int, why string) {
	tok := ss.token(x, token)
	d := ss.def(x)
	a := platform.Assignment{Key: fmt.Sprintf("flow:%s:%d", x.ID, x.Seq), Ref: InstanceType + "/" + x.ID,
		Title: fmt.Sprintf("Flow %s is stuck", x.Title), Body: why + ". Retry, skip the step or cancel the instance in Settings."}
	x.Seq++
	for _, role := range d.Owners {
		a.To = append(a.To, platform.Recipient{AppRole: role})
	}
	tok.Waits, tok.Error, tok.Due, tok.Task = "stuck", why, time.Time{}, d.app+":"+a.Key
	x.State = "stuck"
	ss.assigns = append(ss.assigns, flowTask{app: d.app, Assignment: a})
	ss.trace(x, tok.Step, "stuck", why, "")
}

func (ss *session) endPath(x *FlowInstance, token int) {
	tok := *ss.token(x, token)
	x.Tokens = slices.DeleteFunc(x.Tokens, func(t Token) bool { return t.ID == token })
	if tok.Parent == 0 {
		if len(x.Tokens) == 0 {
			ss.finish(x, "done")
		}
		return
	}
	parent := ss.token(x, tok.Parent)
	if parent.ID < 0 {
		return
	}
	if parent.Waits == "loop" {
		frame := parent.Loop
		if frame == nil || len(tok.Frames) == 0 {
			return
		}
		index := tok.Frames[len(tok.Frames)-1].Index
		if frame.Results == nil {
			frame.Results = map[int]json.RawMessage{}
		}
		bodyOutputs := maps.Clone(tok.Outputs)
		for name := range parent.Outputs {
			delete(bodyOutputs, name)
		}
		next := platform.Raw(bodyOutputs)
		frame.Results[index] = next
		savedResults, _ := json.Marshal(frame.Results)
		if len(savedResults) > 60<<10 {
			delete(frame.Results, index)
			parent.Loop = nil
			ss.outputRefused(x, parent.ID)
			return
		}
		if frame.While {
			if ss.def(x).steps[tok.Step].Output != nil {
				frame.State = tok.Outputs[tok.Step]
			}
			parent.Outputs = maps.Clone(tok.Outputs)
		}
		ss.fillLoop(x, parent.ID)
		return
	}
	if parent.Waits != "join" {
		return
	}
	if parent.Results == nil {
		parent.Results = map[string]json.RawMessage{}
	}
	parent.Results[tok.Branch] = platform.Raw(tok.Outputs)
	if len(ss.def(x).steps[parent.Step].Any) > 0 {
		var others []Token
		for _, t := range x.Tokens {
			if t.Parent == parent.ID {
				others = append(others, t)
			}
		}
		ss.stopPaths(&FlowInstance{ID: x.ID, Flow: x.Flow, Version: x.Version, OnBehalf: x.OnBehalf, Tokens: others})
		x.Tokens = slices.DeleteFunc(x.Tokens, func(t Token) bool { return t.Parent == parent.ID })
	}
	if !slices.ContainsFunc(x.Tokens, func(t Token) bool { return t.Parent == tok.Parent }) {
		parent = ss.token(x, tok.Parent)
		if !ss.output(x, parent.ID, platform.Raw(parent.Results)) {
			ss.outputRefused(x, parent.ID)
			return
		}
		ss.trace(x, parent.Step, "joined", "saved branch results", "")
		ss.next(x, parent.ID, "")
	}
}

func (ss *session) finish(x *FlowInstance, state string) {
	ss.stopPaths(x)
	x.State, x.Tokens = state, nil
	ss.trace(x, "", "ended", state, "")
	if x.Parent == "" {
		return
	}
	parent := ss.load(x.Parent)
	if parent == nil {
		return
	}
	i := slices.IndexFunc(parent.Tokens, func(t Token) bool { return t.Child == x.ID })
	if i < 0 {
		return
	}
	parent.Answer = state
	if !ss.output(parent, parent.Tokens[i].ID, platform.Raw(map[string]any{"state": state, "outputs": x.Outputs, "data": json.RawMessage(x.Data)})) {
		ss.outputRefused(parent, parent.Tokens[i].ID)
		return
	}
	ss.trace(parent, parent.Tokens[i].Step, "returned", x.ID+" "+state, "")
	ss.next(parent, parent.Tokens[i].ID, "")
	ss.advance(parent)
}

func (f *Flows) tick(c platform.Caller, now time.Time) *kernel.Error {
	continuous := map[string]*flowDef{}
	for _, id := range slices.Sorted(maps.Keys(f.defs)) {
		d, ok := f.latest(id)
		if ok && d.Start.Continuous {
			continuous[id] = d
		}
	}
	active := map[string]bool{}
	if len(continuous) > 0 {
		err := f.eachRunning(c, func(x FlowInstance) error {
			active[x.Flow] = true
			return nil
		})
		if err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: err.Error()}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(f.defs)) {
		d, ok := f.latest(id)
		if !ok {
			continue
		}
		if d.Start.Continuous {
			if active[id] || d.Start.OnBehalf == "" || f.host == nil {
				continue
			}
			if d.Start.Enabled != nil && !d.Start.Enabled(f.host.Automation(c, d.app)) {
				continue
			}
			if _, exists := f.host.Member(d.Start.OnBehalf); !exists {
				continue
			}
			key := fmt.Sprintf("continuous-v%d", d.Version)
			if _, known := platform.Get[FlowInstance](c, id+":"+key); known {
				continue
			}
			if err := f.start(f.host.Automation(c, ID), d, key, nil, d.Start.OnBehalf, nil, "", now, true); err != nil {
				return err
			}
			active[id] = true
			continue
		}
		if d.Start.Every <= 0 {
			continue
		}
		key := now.UTC().Truncate(d.Start.Every).Format(time.RFC3339)
		if _, known := platform.Get[FlowInstance](c, id+":"+key); known {
			continue
		}
		if err := f.start(f.host.Automation(c, ID), d, key, map[string]any{"period": key}, d.Start.OnBehalf, nil, "", now); err != nil {
			return err
		}
	}
	return nil
}
