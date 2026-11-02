package automation

import (
	"cmp"
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

func (ss *session) take(x *FlowInstance, token int) {
	tok := ss.token(x, token)
	d := ss.def(x)
	if tok.Step == "@undo" {
		ss.undo(x, token)
		return
	}
	if tok.Step == "" {
		ss.endPath(x, token)
		return
	}
	if tok.Step == Compensate {
		ss.compensate(x, "the flow asked to undo")
		return
	}
	step := d.steps[tok.Step]
	app, run := ss.app(x), ss.run(x, token)
	switch {
	case step.LoopControl != "":
		ss.controlLoop(x, token, step)
	case step.End:
		if step.Output != nil {
			raw, err := step.Output(app, run)
			if err != nil {
				ss.failed(x, token, step, err.Error(), err)
				return
			}
			if !ss.output(x, token, raw) {
				ss.outputRefused(x, token)
				return
			}
			ss.sources(x, run.Sources)
		}
		ss.endPath(x, token)
	case step.Evaluate != nil:
		output, err := step.Evaluate.Run(app, run)
		if err != nil {
			ss.failed(x, token, step, err.Error(), err)
			return
		}
		if !ss.output(x, token, output) {
			ss.outputRefused(x, token)
			return
		}
		ss.sources(x, run.Sources)
		ss.trace(x, tok.Step, "evaluated", "saved typed output", "")
		ss.next(x, token, "")
	case step.Branch != nil:
		to, reason, err := step.Branch.Choose(app, run)
		if err != nil {
			ss.failed(x, token, step, err.Error(), err)
			return
		}
		x.Data = string(run.Data)
		if !ss.output(x, token, platform.Raw(map[string]string{"case": reason})) {
			ss.outputRefused(x, token)
			return
		}
		ss.sources(x, run.Sources)
		ss.trace(x, tok.Step, "chose", cmp.Or(to, "the end")+": "+reason, "")
		ss.next(x, token, to)
	case step.Loop != nil:
		ss.beginLoop(x, token, step, app, run)
	case step.Operation != nil:
		request, prepared := ss.preparedOperations[token]
		delete(ss.preparedOperations, token)
		if !prepared {
			var err *kernel.Error
			request, err = step.Operation.Request(app, run)
			if err != nil {
				ss.failed(x, token, step, err.Error(), err)
				return
			}
		}
		request.Key = fmt.Sprintf("flow:%s:%s:%d", x.ID, tok.Step, x.Seq)
		request.OnBehalf = x.OnBehalf
		request.Release = &x.Release
		request.Target = InstanceType + "/" + x.ID
		request.Sources = slices.Compact(slices.Sorted(slices.Values(append(append(slices.Clone(x.Sources), run.Sources...), request.Sources...))))
		x.Seq++
		tok.Waits = "operation"
		ss.operations = append(ss.operations, operationStart{instance: x.ID, token: token, request: request})
		ss.trace(x, tok.Step, "computing", request.Name, "")
	case step.Invoke != nil:
		a := step.Invoke.Act
		ref, err := ss.act(x, d.app, a.Protocol, a.Action, a.Target(app, run), payloadOf(a.Payload, app, run), fmt.Sprintf("flow:%s:%s:%d", x.ID, tok.Step, x.Seq))
		if err != nil {
			ss.failed(x, token, step, err.Error(), err)
			return
		}
		x.Seq++
		tok.Waits = "invocation"
		tok.Child = ref.GetId()
		ss.trace(x, tok.Step, "requested", a.Action, "")
	case step.Act != nil:
		var target string
		if step.Act.Target != nil {
			target = step.Act.Target(app, run)
		}
		payload := payloadOf(step.Act.Payload, app, run)
		if step.Act.TargetValue != nil {
			value, err := step.Act.TargetValue(app, run)
			if err != nil {
				ss.failed(x, token, step, err.Error(), err)
				return
			}
			target = value
		}
		if step.Act.Inputs != nil {
			value, err := step.Act.Inputs(app, run)
			if err != nil {
				ss.failed(x, token, step, err.Error(), err)
				return
			}
			payload = value
		}
		ss.sources(x, run.Sources)
		member := ""
		if step.Act.AsMember {
			member = x.OnBehalf
		}
		ref, err := ss.act(x, d.app, step.Act.Protocol, step.Act.Action, target, payload, fmt.Sprintf("flow:%s:%s:%d", x.ID, tok.Step, x.Seq), member)
		if err != nil {
			ss.deadLetterAct(x, tok, step, target, payload, err.Error())
			ss.failed(x, token, step, err.Error(), err)
			return
		}
		x.Seq++
		if ref.GetType() == platform.WorkApprovalType {
			tok.Waits, tok.Child = "approval", ref.GetId()
			if due, ok := ss.timeout(x, step); ok {
				tok.Due = due
			}
			ss.trace(x, tok.Step, "approval requested", ref.GetId(), x.OnBehalf)
			return
		}
		if !ss.output(x, token, platform.Raw(map[string]string{"type": ref.GetType(), "id": ref.GetId()})) {
			ss.outputRefused(x, token)
			return
		}
		ss.trace(x, tok.Step, "acted", strings.TrimPrefix(step.Act.Protocol+" ", " ")+step.Act.Action+" "+target, app.ID)
		if step.Act.Done != nil {
			step.Act.Done(app, run, ref)
			x.Data = string(run.Data)
		}
		if u := step.Undo; u != nil {
			x.Undo = append(x.Undo, UndoEntry{Step: tok.Step, App: d.app, Protocol: u.Protocol, Action: u.Action, Target: u.Target(app, run),
				Payload: payloadOf(u.Payload, app, run)})
		}
		ss.next(x, token, "")
	case step.Wait != nil:
		tok.Waits = "wait"
		w := step.Wait
		if w.Until != nil && w.Until(app, run) {
			ss.trace(x, tok.Step, "condition", "it held at once", "")
			ss.next(x, token, "")
			return
		}
		if w.At != nil {
			at, err := w.At(app, run)
			if err != nil {
				ss.failed(x, token, step, err.Error(), err)
				return
			}
			ss.sources(x, run.Sources)
			if !at.After(ss.now) {
				ss.trace(x, tok.Step, "time", "already reached", "")
				ss.next(x, token, "")
				return
			}
			tok.Due = at
		} else if due, ok := ss.timeout(x, step); ok {
			tok.Due = due
		}
		ss.trace(x, tok.Step, "waiting", waitingFor(step), "")
	case step.Agent != nil && ss.f.host.Runs() != nil:
		ag := step.Agent
		id := fmt.Sprintf("%s:%d", x.ID, x.Seq)
		x.Seq++
		goal, ref := ag.Goal(app, run), ""
		if ag.Ref != nil {
			ref = ag.Ref(app, run)
		}
		tok.Waits, tok.Child = "agent", id
		if due, ok := ss.timeout(x, step); ok {
			tok.Due = due
		}
		ss.runs = append(ss.runs, platform.RunStart{ID: id, Agent: d.app + "." + ag.Agent, Goal: goal, Ref: ref, Flow: x.ID, Step: tok.Step, Token: tok.ID})
		ss.trace(x, tok.Step, "agent", ag.Agent+": "+goal, "")
	case step.Ask != nil, step.Agent != nil:
		a := platform.Assignment{Key: fmt.Sprintf("flow:%s:%d", x.ID, x.Seq), Ref: InstanceType + "/" + x.ID}
		if ask := step.Ask; ask != nil {
			a.Title, a.To, a.Answers = ask.Title(app, run), ask.To(app, run), ask.Answers
			if ask.Body != nil {
				a.Body = ask.Body(app, run)
			}
			if ask.Ref != nil {
				a.Ref = ask.Ref(app, run)
			}
		} else {
			a.Title, a.To, a.Body = step.Agent.Goal(app, run), step.Agent.To(app, run), "An agent's step, done by a person."
		}
		x.Seq++
		if due, ok := ss.timeout(x, step); ok {
			tok.Due, a.Due = due, due
		}
		tok.Waits, tok.Task = "ask", d.app+":"+a.Key
		ss.assigns = append(ss.assigns, flowTask{app: d.app, Assignment: a})
		ss.trace(x, tok.Step, "asked", a.Title, "")
	case step.Call != nil:
		child, err := ss.f.version(ss.c, d.app+"."+step.Call.Flow)
		if step.Call.Version > 0 {
			child = ss.f.def(d.app+"."+step.Call.Flow, step.Call.Version)
			if child == nil {
				err = &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			} else {
				err = nil
			}
		}
		if err != nil {
			ss.failed(x, token, step, "calling "+step.Call.Flow+": "+err.Error())
			return
		}
		id := fmt.Sprintf("%s/%d", x.ID, x.Seq)
		x.Seq++
		var data any
		if step.Call.Data != nil {
			data = step.Call.Data(app, run)
		}
		tok.Waits, tok.Child = "call", id
		if due, ok := ss.timeout(x, step); ok {
			tok.Due = due
		}
		ss.trace(x, tok.Step, "called", child.Title+" "+id, "")
		in := ss.create(child, id, id, data, x.OnBehalf, x.ID)
		bound, bindingErr := ss.f.host.BindFlow(child.app, child.Name, child.Version, platform.FlowBinding{})
		if bindingErr != nil {
			ss.failed(x, token, step, bindingErr.Error())
			return
		}
		in.Dependencies, in.Release = bound.Dependencies, bound.Release
		ss.advance(in)
	case len(step.All) > 0 || len(step.Any) > 0:
		tok.Waits = "join"
		for _, branch := range append(slices.Clone(step.All), step.Any...) {
			x.Tokens = append(x.Tokens, Token{ID: ss.tokenID(x), Step: branch, Branch: branch, Parent: token, Frames: slices.Clone(tok.Frames), Input: slices.Clone(tok.Input), Outputs: maps.Clone(tok.Outputs), Waits: "ready"})
		}
		ss.trace(x, step.Name, "branched", strings.Join(append(slices.Clone(step.All), step.Any...), ", "), "")
	}
}

func (ss *session) tokenID(x *FlowInstance) int {
	id := 0
	for _, t := range x.Tokens {
		id = max(id, t.ID)
	}
	return id + 1
}

func waitingFor(s *platform.Step) string {
	switch w := s.Wait; {
	case w.On != "":
		return "for " + w.On
	case w.Until != nil:
		return "until a condition holds"
	}
	return "until a time"
}

func payloadOf(build func(platform.Caller, *platform.Run) any, c platform.Caller, r *platform.Run) json.RawMessage {
	if build == nil {
		return json.RawMessage("{}")
	}
	raw, _ := json.Marshal(build(c, r))
	return raw
}

func (ss *session) act(x *FlowInstance, appID, protocol, action, target string, payload json.RawMessage, key string, onBehalf ...string) (*pb.EntityRef, *kernel.Error) {
	h := ss.f.host
	if x != nil {
		h = ss.execution(x)
	}
	c := h.Automation(ss.c, appID)
	if protocol != "" {
		return h.Invoke(c, protocol, action, target, payload, key, key, ss.now)
	}
	owner, declared, ok := h.Action(action)
	if !ok || owner != appID && (len(onBehalf) == 0 || onBehalf[0] == "") {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	if len(onBehalf) > 0 && onBehalf[0] != "" {
		member, ok := h.Member(onBehalf[0])
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
		}
		c = h.Caller(ss.c, member, owner)
	}
	authority, _ := h.OwnerOf(declared.Target)
	s := &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: authority, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: declared.Target, Id: target}, Schema: &pb.SchemaRef{Name: action, Version: 1}, Payload: payload}
	r, err := platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) { return h.Submit(c, s, ss.now) })
	if err != nil {
		return nil, err
	}
	return r.GetSubmission().GetTarget(), nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func backoff(attempts int) time.Duration { return time.Second << attempts }

func target(s *pb.Submission) string { return s.GetTarget().GetType() + "/" + s.GetTarget().GetId() }

func (ss *session) timeout(x *FlowInstance, step *platform.Step) (time.Time, bool) {
	if step.WorkingDays > 0 {
		party := ""
		if x.OnBehalf != "" {
			party = "member:" + x.OnBehalf
		}
		cal := ss.f.host.Calendar(party, ss.now.UTC().Format(time.DateOnly))
		return cal.After(ss.now, step.WorkingDays), true
	}
	return ss.now.Add(step.Timeout), step.Timeout > 0
}

func (ss *session) descendant(x *FlowInstance, token, parent int) bool {
	for token != 0 {
		if token == parent {
			return true
		}
		current := ss.token(x, token)
		if current.ID < 0 {
			return false
		}
		token = current.Parent
	}
	return false
}
