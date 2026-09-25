package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// A session is one decision of the flow app: it loads instances, moves them
// until each waits, and applies what changed as that decision.
type session struct {
	f       *Flows
	c       platform.Caller // the flow app, automated
	now     time.Time
	changed map[string]*FlowInstance
	order   []string
	assigns []flowTask
	runs    []AgentRunRecord // agent runs its steps start (ADR-0021)
	signals []runSignal      // what people made of their proposals
	close   []string
	event   *platform.Event
	moves   int
}

type runSignal struct {
	run string
	Signal
}

// review keeps what a person made of the proposal of the agent step an Ask reviews.
func (ss *session) review(x *FlowInstance, step *platform.Step, kind, by, detail string) {
	if step == nil || step.Ask == nil || step.Ask.Reviews == "" || ss.f.t.agents == nil {
		return
	}
	domain, _ := json.Marshal([]any{[]any{"flow", "=", x.ID}, []any{"step", "=", step.Ask.Reviews}, []any{"state", "=", "done"}})
	runs, _, _ := platform.Find[AgentRunRecord](ss.f.t.automation(AgentApp, ss.c.Replaying), platform.Query{Domain: domain, Sort: []string{"-created"}, Limit: 1})
	if len(runs) > 0 {
		ss.signals = append(ss.signals, runSignal{run: runs[0].ID, Signal: Signal{At: ss.now, Kind: kind, By: by, Detail: detail}})
	}
}

type flowTask struct {
	app string
	platform.Assignment
}

const movesPerDecision = 1000 // a flow that loops without waiting is stopped

func (f *Flows) session(c platform.Caller, now time.Time) *session {
	return &session{f: f, c: c, now: now, changed: map[string]*FlowInstance{}}
}

func (ss *session) load(id string) *FlowInstance {
	if x := ss.changed[id]; x != nil {
		return x
	}
	x, ok := platform.Get[FlowInstance](ss.c, id)
	if !ok {
		return nil
	}
	ss.changed[id], ss.order = &x, append(ss.order, id)
	return &x
}

func (ss *session) apply(r *pb.ChangeRecord) {
	for _, id := range ss.order {
		ss.c.Put(r, *ss.changed[id])
	}
	for _, id := range ss.close {
		ss.f.t.closeTask(ss.c, r, id)
	}
	for _, x := range ss.assigns {
		ss.f.t.automation(x.app, ss.c.Replaying).Assign(r, x.Assignment)
	}
	for _, run := range ss.runs {
		ss.f.t.automation(AgentApp, ss.c.Replaying).Put(r, run)
	}
	for _, x := range ss.signals {
		ss.f.t.agents.signal(ss.c, r, x.run, x.Signal)
	}
}

// agentEnded goes on from an agent step when its run ends: done, with its
// result as the answer; stopped, to the step's fault path, or a person does
// the step instead.
func (f *Flows) agentEnded(c platform.Caller, run AgentRunRecord, now time.Time) {
	c = f.t.automation(FlowApp, c.Replaying)
	x, ok := platform.Get[FlowInstance](c, run.Flow)
	if !ok || ended(x.State) {
		return
	}
	i := slices.IndexFunc(x.Tokens, func(t Token) bool { return t.Child == run.ID && t.Waits == "agent" })
	if i < 0 {
		return // it timed out, or the instance moved on
	}
	f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
		tok := ss.token(in, run.Token)
		step := ss.def(in).steps[tok.Step]
		if run.State == "done" {
			in.Answer = run.Result
			ss.trace(in, tok.Step, "agent answered", clip(run.Result, 300), run.Agent)
			ss.next(in, tok.ID, "")
			return
		}
		ss.trace(in, tok.Step, "agent stopped", run.Stopped, run.Agent)
		if step.Fault != "" {
			ss.next(in, tok.ID, step.Fault)
			return
		}
		app, r := ss.app(in), ss.run(in)
		a := platform.Assignment{Key: fmt.Sprintf("flow:%s:%d", in.ID, in.Seq), Ref: InstanceType + "/" + in.ID, Title: step.Agent.Goal(app, r),
			To: step.Agent.To(app, r), Body: "The agent stopped (" + run.Stopped + "); do its step."}
		in.Seq++
		tok.Waits, tok.Child, tok.Task = "ask", "", ss.def(in).app+":"+a.Key
		ss.assigns = append(ss.assigns, flowTask{app: ss.def(in).app, Assignment: a})
	})
}

func (ss *session) trace(x *FlowInstance, step, what, detail, by string) {
	x.Trace = append(x.Trace, TraceLine{At: ss.now, Step: step, What: what, Detail: detail, By: by})
}

func (ss *session) token(x *FlowInstance, id int) *Token {
	i := slices.IndexFunc(x.Tokens, func(t Token) bool { return t.ID == id })
	if i < 0 {
		return &Token{ID: -1}
	}
	return &x.Tokens[i]
}

func (ss *session) def(x *FlowInstance) *flowDef { return ss.f.def(x.Flow, x.Version) }

func (ss *session) app(x *FlowInstance) platform.Caller {
	return ss.f.t.automation(ss.def(x).app, ss.c.Replaying)
}

func (ss *session) run(x *FlowInstance) *platform.Run {
	r := ss.f.run(x)
	r.Event = ss.event
	return r
}

// decide makes one flow decision about instance id; build fills the session.
func (f *Flows) decide(c platform.Caller, schema, id, key string, now time.Time, build func(ss *session) *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
	s := &pb.Submission{TenantId: f.t.ID, PrincipalId: c.ID, Authority: FlowApp, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: InstanceType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte("{}")}
	return f.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		ss := f.session(c, now)
		if err := build(ss); err != nil {
			return nil, err
		}
		return ss.apply, nil
	})
}

// step changes instance id as change says, then moves it on, as one decision.
func (f *Flows) step(c platform.Caller, id string, now time.Time, change func(*session, *FlowInstance)) *kernel.Error {
	x, ok := platform.Get[FlowInstance](c, id)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	_, err := f.decide(c, SchemaFlowStep, id, fmt.Sprintf("%s:%d", id, x.Revision+1), now, func(ss *session) *kernel.Error {
		in := ss.load(id)
		change(ss, in)
		ss.advance(in)
		return nil
	})
	return err
}

// start opens an instance unless one with its key runs, and moves it on.
func (f *Flows) start(c platform.Caller, d *flowDef, key string, data any, onBehalf string, e *platform.Event, parent string, now time.Time) *kernel.Error {
	flow := d.app + "." + d.Name
	id := flow + ":" + key
	for n := 2; ; n++ {
		x, known := platform.Get[FlowInstance](c, id)
		if !known {
			break
		}
		if !ended(x.State) {
			return nil // one running instance per key
		}
		id = fmt.Sprintf("%s:%s#%d", flow, key, n)
	}
	_, err := f.decide(c, SchemaFlowStart, id, "start:"+id, now, func(ss *session) *kernel.Error {
		ss.event = e
		ss.advance(ss.create(d, id, key, data, onBehalf, parent))
		return nil
	})
	return err
}

func ended(state string) bool {
	return state == "done" || state == "compensated" || state == "canceled"
}

func (ss *session) create(d *flowDef, id, key string, data any, onBehalf, parent string) *FlowInstance {
	raw, _ := json.Marshal(data)
	x := &FlowInstance{Record: platform.Record{ID: id}, Flow: d.app + "." + d.Name, Title: fmt.Sprintf("%s %s", d.Title, key), Version: d.Version, Key: key,
		State: "running", OnBehalf: onBehalf, Data: string(raw), Parent: parent, Tokens: []Token{{ID: 1, Step: d.Steps[0].Name, Waits: "ready", Attempts: 0}},
		Undo: []UndoEntry{}, Seq: 1}
	ss.changed[id], ss.order = x, append(ss.order, id)
	ss.trace(x, "", "started", fmt.Sprintf("version %d, key %s", d.Version, key), onBehalf)
	return x
}

// next moves a token on: to `to`, or where the step's Choose or Next says.
func (ss *session) next(x *FlowInstance, token int, to string) {
	tok := ss.token(x, token)
	if to == "" {
		if step := ss.def(x).steps[tok.Step]; step != nil {
			to = step.Next
			if step.Choose != nil { // it may keep what it read: the run's data is saved
				var reason string
				r := ss.run(x)
				to, reason = step.Choose(ss.app(x), r)
				x.Data = string(r.Data)
				ss.trace(x, tok.Step, "chose", cmpOr(to, "the end")+": "+reason, "")
			}
		}
	}
	*tok = Token{ID: tok.ID, Step: to, Branch: tok.Branch, Waits: "ready"}
	if x.State == "waiting" {
		x.State = "running"
	}
}

// advance takes every ready token's step until the instance only waits.
func (ss *session) advance(x *FlowInstance) {
	for {
		i := slices.IndexFunc(x.Tokens, func(t Token) bool { return t.Waits == "ready" })
		if i < 0 || ended(x.State) {
			break
		}
		if ss.moves++; ss.moves > movesPerDecision {
			ss.stuck(x, x.Tokens[i].ID, fmt.Sprintf("more than %d steps without waiting", movesPerDecision))
			break
		}
		ss.take(x, x.Tokens[i].ID)
	}
	if !ended(x.State) && x.State != "stuck" && x.State != "compensating" {
		x.State = "waiting"
	}
}

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
	app, run := ss.app(x), ss.run(x)
	switch {
	case step.Act != nil:
		target := step.Act.Target(app, run)
		ref, err := ss.act(d.app, step.Act.Protocol, step.Act.Action, target, payloadOf(step.Act.Payload, app, run), fmt.Sprintf("flow:%s:%s:%d", x.ID, tok.Step, x.Seq))
		if err != nil {
			ss.failed(x, token, step, err.Error())
			return
		}
		x.Seq++
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
			at := w.At(app, run)
			if !at.After(ss.now) {
				ss.trace(x, tok.Step, "time", "already reached", "")
				ss.next(x, token, "")
				return
			}
			tok.Due = at
		} else if step.Timeout > 0 {
			tok.Due = ss.now.Add(step.Timeout)
		}
		ss.trace(x, tok.Step, "waiting", waitingFor(step), "")
	case step.Agent != nil && ss.f.t.agents != nil: // the app's agent takes the step (ADR-0021); its run ends it
		ag := step.Agent
		id := fmt.Sprintf("%s:%d", x.ID, x.Seq)
		x.Seq++
		goal, ref := ag.Goal(app, run), ""
		if ag.Ref != nil {
			ref = ag.Ref(app, run)
		}
		tok.Waits, tok.Child = "agent", id
		if step.Timeout > 0 {
			tok.Due = ss.now.Add(step.Timeout)
		}
		ss.runs = append(ss.runs, ss.f.t.agents.create(id, d.app+"."+ag.Agent, goal, ref, "", x.ID, tok.Step, tok.ID))
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
		} else { // no agent app runs: a person does what the agent would
			a.Title, a.To, a.Body = step.Agent.Goal(app, run), step.Agent.To(app, run), "An agent's step, done by a person."
		}
		x.Seq++
		if step.Timeout > 0 {
			tok.Due, a.Due = ss.now.Add(step.Timeout), ss.now.Add(step.Timeout)
		}
		tok.Waits, tok.Task = "ask", d.app+":"+a.Key
		ss.assigns = append(ss.assigns, flowTask{app: d.app, Assignment: a})
		ss.trace(x, tok.Step, "asked", a.Title, "")
	case step.Call != nil:
		child, err := ss.f.version(ss.c, d.app+"."+step.Call.Flow)
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
		if step.Timeout > 0 {
			tok.Due = ss.now.Add(step.Timeout)
		}
		ss.trace(x, tok.Step, "called", child.Title+" "+id, "")
		ss.advance(ss.create(child, id, id, data, x.OnBehalf, x.ID))
	case len(step.All) > 0 || len(step.Any) > 0:
		tok.Waits = "join"
		for _, branch := range append(slices.Clone(step.All), step.Any...) {
			x.Tokens = append(x.Tokens, Token{ID: ss.tokenID(x), Step: branch, Branch: step.Name, Waits: "ready"})
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

// act submits an action as the app: its own, or a protocol's through the host.
func (ss *session) act(appID, protocol, action, target string, payload json.RawMessage, key string) (*pb.EntityRef, *kernel.Error) {
	t := ss.f.t
	c := t.automation(appID, ss.c.Replaying)
	if protocol != "" {
		ref, _, err := c.Invoke(protocol, action, target, payload, key, key, ss.now)
		return ref, err
	}
	app := t.app(appID)
	declared, ok := app.Manifest().Actions.Action(action)
	if !ok {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	authority := ""
	for _, d := range app.Declarations() {
		if d.GetDataClass() == declared.Target {
			authority = d.GetAuthorityId()
		}
	}
	s := &pb.Submission{TenantId: t.ID, PrincipalId: c.ID, Authority: authority, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: declared.Target, Id: target}, Schema: &pb.SchemaRef{Name: action, Version: 1}, Payload: payload}
	r, err := app.Submit(c, s, ss.now)
	if err != nil {
		return nil, err
	}
	return r.GetSubmission().GetTarget(), nil
}

// failed retries a step's act with backoff, then takes its fault path, then compensates.
func (ss *session) failed(x *FlowInstance, token int, step *platform.Step, why string) {
	tok := ss.token(x, token)
	tok.Attempts++
	tok.Error = why
	switch {
	case tok.Attempts < stepAttempts:
		tok.Waits, tok.Due = "retry", ss.now.Add(backoff(tok.Attempts))
		ss.trace(x, tok.Step, "retry", why, "")
	case step.Fault != "":
		ss.trace(x, tok.Step, "fault", why, "")
		ss.next(x, token, step.Fault)
	default:
		ss.compensate(x, tok.Step+": "+why)
	}
}

// compensate stops every path and undoes the completed acts, newest first.
func (ss *session) compensate(x *FlowInstance, why string) {
	ss.stopPaths(x)
	x.State = "compensating"
	x.Tokens = []Token{{ID: ss.tokenID(x), Step: "@undo", Waits: "ready"}}
	ss.trace(x, "", "compensating", why, "")
}

func (ss *session) stopPaths(x *FlowInstance) {
	for _, t := range x.Tokens {
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
	if _, err := ss.act(u.App, u.Protocol, u.Action, u.Target, u.Payload, fmt.Sprintf("undo:%s:%d", x.ID, len(x.Undo))); err != nil {
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

// stuck stops a path and asks the flow's owners (ADR-0020 D5).
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

// endPath ends a path: a branch joins its parallel step; the last main path ends the flow.
func (ss *session) endPath(x *FlowInstance, token int) {
	tok := *ss.token(x, token)
	x.Tokens = slices.DeleteFunc(x.Tokens, func(t Token) bool { return t.ID == token })
	if tok.Branch == "" {
		if len(x.Tokens) == 0 {
			ss.finish(x, "done")
		}
		return
	}
	join := slices.IndexFunc(x.Tokens, func(t Token) bool { return t.Step == tok.Branch && t.Waits == "join" })
	if join < 0 {
		return
	}
	siblings := slices.ContainsFunc(x.Tokens, func(t Token) bool { return t.Branch == tok.Branch })
	if len(ss.def(x).steps[tok.Branch].Any) > 0 {
		var others []Token
		for _, t := range x.Tokens {
			if t.Branch == tok.Branch {
				others = append(others, t)
			}
		}
		ss.stopPaths(&FlowInstance{Tokens: others})
		x.Tokens = slices.DeleteFunc(x.Tokens, func(t Token) bool { return t.Branch == tok.Branch })
		siblings = false
	}
	if !siblings {
		ss.trace(x, tok.Branch, "joined", "", "")
		ss.next(x, x.Tokens[slices.IndexFunc(x.Tokens, func(t Token) bool { return t.Step == tok.Branch && t.Waits == "join" })].ID, "")
	}
}

// finish ends an instance; a called one hands its end to its caller.
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
	ss.trace(parent, parent.Tokens[i].Step, "returned", x.ID+" "+state, "")
	ss.next(parent, parent.Tokens[i].ID, "")
	ss.advance(parent)
}

// Submit takes the administrators' decisions about instances; the flow app's
// own steps are made by the host as flows run.
func (f *Flows) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := s.GetTarget().GetId()
	schema := s.GetSchema().GetName()
	if schema == SchemaFlowStart || schema == SchemaFlowStep {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	var p struct{ Token int }
	json.Unmarshal(s.GetPayload(), &p)
	return f.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		ss := f.session(f.t.automation(FlowApp, c.Replaying), now)
		x := ss.load(id)
		if x == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if ended(x.State) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		switch schema {
		case SchemaFlowRetry:
			for i := range x.Tokens {
				if t := &x.Tokens[i]; t.Waits == "stuck" || t.Waits == "retry" || t.Waits == "undo" {
					if t.Task != "" && t.Waits == "stuck" {
						ss.close = append(ss.close, t.Task)
					}
					t.Waits, t.Attempts, t.Due, t.Task = "ready", 0, time.Time{}, ""
				}
			}
			if x.State == "stuck" {
				x.State = map[bool]string{true: "compensating", false: "running"}[slices.ContainsFunc(x.Tokens, func(t Token) bool { return t.Step == "@undo" })]
			}
			ss.trace(x, "", "retried", "", c.ID)
		case SchemaFlowSkip:
			t := ss.token(x, p.Token)
			if t.ID < 0 || t.Waits == "join" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			if t.Task != "" {
				ss.close = append(ss.close, t.Task)
			}
			ss.trace(x, t.Step, "skipped", "", c.ID)
			if t.Step == "@undo" {
				x.Undo = x.Undo[:max(0, len(x.Undo)-1)]
				*t = Token{ID: t.ID, Step: "@undo", Waits: "ready"}
				x.State = "compensating"
			} else {
				ss.next(x, t.ID, "")
				if x.State == "stuck" {
					x.State = "running"
				}
			}
		case SchemaFlowStop:
			ss.stopPaths(x)
			x.State, x.Tokens = "canceled", nil
			ss.trace(x, "", "canceled", "", c.ID)
			return ss.apply, nil
		case SchemaFlowMove:
			versions := f.defs[x.Flow]
			i := slices.IndexFunc(versions, func(d *flowDef) bool { return d.Version == x.Version })
			if i < 0 || i+1 >= len(versions) || versions[i+1].From == nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			to := versions[i+1]
			for j := range x.Tokens {
				t := &x.Tokens[j]
				if t.Step == "@undo" {
					continue
				}
				moved, ok := to.From[t.Step]
				if !ok {
					return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
				}
				t.Step = moved
			}
			ss.trace(x, "", "moved", fmt.Sprintf("version %d to %d", x.Version, to.Version), c.ID)
			x.Version = to.Version
		default:
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
		}
		ss.advance(x)
		return ss.apply, nil
	})
}
