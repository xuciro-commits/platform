package flow

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/work"
	"platformserver/internal/host"
	"platformserver/platform"
)

// A session is one decision of the flow app: it loads instances, moves them
// until each waits, and applies what changed as that decision.
type session struct {
	f                *Flows
	c                platform.Caller // the flow app, automated
	now              time.Time
	changed          map[string]*FlowInstance
	order            []string
	assigns          []flowTask
	runs             []host.RunStart // agent runs its steps start (ADR-0021)
	signals          []runSignal     // what people made of their proposals
	close            []string
	event            *platform.Event
	moves            int
	operations       []operationStart
	cancelOperations []string
	withdrawals      []approvalWithdrawal
}

type approvalWithdrawal struct{ app, id, member string }

type operationStart struct {
	instance string
	token    int
	request  platform.OperationRequest
}

type runSignal struct {
	run string
	host.RunSignal
}

// review keeps what a person made of the proposal of the agent step an Ask reviews.
func (ss *session) review(x *FlowInstance, step *platform.Step, kind, by, detail string) {
	runs := ss.f.host.Runs()
	if step == nil || step.Ask == nil || step.Ask.Reviews == "" || runs == nil {
		return
	}
	if done := runs.Finished(ss.c, x.ID, step.Ask.Reviews); len(done) > 0 {
		ss.signals = append(ss.signals, runSignal{run: done[len(done)-1], RunSignal: host.RunSignal{At: ss.now, Kind: kind, By: by, Detail: detail}})
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
	for _, withdrawal := range ss.withdrawals {
		if _, err := ss.act(withdrawal.app, "", "work.approval.withdraw", withdrawal.id, json.RawMessage("{}"), "flow-withdraw:"+withdrawal.id, withdrawal.member); err != nil {
			return
		}
	}
	for _, id := range ss.cancelOperations {
		if err := ss.c.CancelOperation(r, id); err != nil {
			return
		}
	}
	for _, op := range ss.operations {
		token := ss.token(ss.changed[op.instance], op.token)
		if token.ID < 0 || token.Waits != "operation" {
			continue
		}
		call, err := ss.c.RequestOperation(r, op.request)
		if err != nil {
			return
		}
		ss.token(ss.changed[op.instance], op.token).Operation = call.ID
	}
	for _, id := range ss.order {
		ss.c.Put(r, *ss.changed[id])
	}
	if tasks := ss.f.host.Tasks(); tasks != nil {
		for _, id := range ss.close {
			tasks.Close(ss.c, r, id)
		}
	}
	for _, x := range ss.assigns {
		ss.f.host.Automation(ss.c, x.app).Assign(r, x.Assignment)
	}
	if runs := ss.f.host.Runs(); runs != nil {
		for _, run := range ss.runs {
			runs.Start(ss.c, r, run, ss.now)
		}
		for _, x := range ss.signals {
			runs.Signal(ss.c, r, x.run, x.RunSignal, ss.now)
		}
	}
}

// RunEnded goes on from an agent step when its run ends: done, with its
// result as the answer; stopped, to the step's fault path, or a person does
// the step instead.
func (f *Flows) RunEnded(c platform.Caller, run host.RunEnd, now time.Time) {
	c = f.host.Automation(c, ID)
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
	return ss.f.host.Automation(ss.c, ss.def(x).app)
}

func (ss *session) run(x *FlowInstance, token ...int) *platform.Run {
	r := ss.f.run(x)
	r.Event = ss.event
	r.Now = ss.now
	if len(token) > 0 {
		tok := ss.token(x, token[0])
		r.Outputs = maps.Clone(tok.Outputs)
		r.Frames = slices.Clone(tok.Frames)
	}
	return r
}

// decide makes one flow decision about instance id; build fills the session.
func (f *Flows) decide(c platform.Caller, schema, id, key string, now time.Time, build func(ss *session) *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
	s := &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: key,
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
	return f.update(c, id, now, func(ss *session, in *FlowInstance) *kernel.Error {
		change(ss, in)
		return nil
	})
}

// update keeps fallible frame changes inside the original ledger decision.
// A refused change cannot advance tokens or publish partial instance images.
func (f *Flows) update(c platform.Caller, id string, now time.Time, change func(*session, *FlowInstance) *kernel.Error) *kernel.Error {
	x, ok := platform.Get[FlowInstance](c, id)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if err := f.checkBinding(x); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: err.Error()}
	}
	_, err := f.decide(c, SchemaFlowStep, id, fmt.Sprintf("%s:%d", id, x.Revision+1), now, func(ss *session) *kernel.Error {
		in := ss.load(id)
		if err := change(ss, in); err != nil {
			return err
		}
		ss.advance(in)
		return nil
	})
	return err
}

// start opens an instance unless one with its key runs, and moves it on.
func (f *Flows) start(c platform.Caller, d *flowDef, key string, data any, onBehalf string, e *platform.Event, parent string, now time.Time, deferAdvance ...bool) *kernel.Error {
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
	bound, bindingErr := f.host.BindFlow(d.app, d.Name, d.Version, host.FlowBinding{})
	if bindingErr != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: bindingErr.Error()}
	}
	_, err := f.decide(c, SchemaFlowStart, id, "start:"+id, now, func(ss *session) *kernel.Error {
		ss.event = e
		x := ss.create(d, id, key, data, onBehalf, parent)
		x.Dependencies, x.Release = bound.Dependencies, bound.Release
		if len(deferAdvance) > 0 && deferAdvance[0] {
			x.Tokens[0].Waits = "yield"
			x.Tokens[0].Due = now
			x.State = "waiting"
		} else {
			ss.advance(x)
		}
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
		Undo: []UndoEntry{}, Seq: 1, Outputs: map[string]json.RawMessage{}}
	if d.Subject != "" {
		x.Subject = d.Subject + "/" + key
		x.Sources = []string{x.Subject}
	}
	ss.changed[id], ss.order = x, append(ss.order, id)
	ss.trace(x, "", "started", fmt.Sprintf("version %d, key %s", d.Version, key), onBehalf)
	return x
}

// next moves a token on: to `to`, or where the step's Choose or Next says.
func (ss *session) next(x *FlowInstance, token int, to string) {
	tok := ss.token(x, token)
	if to == "" {
		if step := ss.def(x).steps[tok.Step]; step != nil {
			if step.Output != nil {
				output, err := step.Output(ss.app(x), ss.run(x, token))
				if err != nil {
					ss.failed(x, token, step, err.Error())
					return
				}
				if !ss.output(x, token, output) {
					ss.outputRefused(x, token)
					return
				}
			}
			to = step.Next
			if step.Choose != nil { // it may keep what it read: the run's data is saved
				var reason string
				r := ss.run(x, token)
				to, reason = step.Choose(ss.app(x), r)
				x.Data = string(r.Data)
				ss.trace(x, tok.Step, "chose", cmp.Or(to, "the end")+": "+reason, "")
			}
		}
	}
	*tok = Token{ID: tok.ID, Step: to, Branch: tok.Branch, Parent: tok.Parent, Frames: tok.Frames, Outputs: tok.Outputs, Waits: "ready"}
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
			for j := range x.Tokens {
				if x.Tokens[j].Waits == "ready" {
					x.Tokens[j].Waits = "yield"
					x.Tokens[j].Due = ss.now
				}
			}
			ss.trace(x, "", "yielded", "the next owned timer continues saved tokens", "")
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
	app, run := ss.app(x), ss.run(x, token)
	switch {
	case step.LoopControl != "":
		ss.controlLoop(x, token, step)
	case step.End:
		if step.Output != nil {
			raw, err := step.Output(app, run)
			if err != nil {
				ss.failed(x, token, step, err.Error())
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
			ss.failed(x, token, step, err.Error())
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
			ss.failed(x, token, step, err.Error())
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
		request, err := step.Operation.Request(app, run)
		if err != nil {
			ss.failed(x, token, step, err.Error())
			return
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
		ref, err := ss.act(d.app, a.Protocol, a.Action, a.Target(app, run), payloadOf(a.Payload, app, run), fmt.Sprintf("flow:%s:%s:%d", x.ID, tok.Step, x.Seq))
		if err != nil {
			ss.failed(x, token, step, err.Error())
			return
		}
		x.Seq++
		tok.Waits = "invocation"
		tok.Child = ref.GetId()
		ss.trace(x, tok.Step, "requested", a.Action, "")
	case step.Act != nil:
		target := step.Act.Target(app, run)
		payload := payloadOf(step.Act.Payload, app, run)
		if step.Act.TargetValue != nil {
			value, err := step.Act.TargetValue(app, run)
			if err != nil {
				ss.failed(x, token, step, err.Error())
				return
			}
			target = value
		}
		if step.Act.Inputs != nil {
			value, err := step.Act.Inputs(app, run)
			if err != nil {
				ss.failed(x, token, step, err.Error())
				return
			}
			payload = value
		}
		ss.sources(x, run.Sources)
		member := ""
		if step.Act.AsMember {
			member = x.OnBehalf
		}
		ref, err := ss.act(d.app, step.Act.Protocol, step.Act.Action, target, payload, fmt.Sprintf("flow:%s:%s:%d", x.ID, tok.Step, x.Seq), member)
		if err != nil {
			ss.failed(x, token, step, err.Error())
			return
		}
		x.Seq++
		if ref.GetType() == work.ApprovalType {
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
			at := w.At(app, run)
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
	case step.Agent != nil && ss.f.host.Runs() != nil: // the app's agent takes the step (ADR-0021); its run ends it
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
		ss.runs = append(ss.runs, host.RunStart{ID: id, Agent: d.app + "." + ag.Agent, Goal: goal, Ref: ref, Flow: x.ID, Step: tok.Step, Token: tok.ID})
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
		bound, bindingErr := ss.f.host.BindFlow(child.app, child.Name, child.Version, host.FlowBinding{})
		if bindingErr != nil {
			ss.failed(x, token, step, bindingErr.Error())
			return
		}
		in.Dependencies, in.Release = bound.Dependencies, bound.Release
		ss.advance(in)
	case len(step.All) > 0 || len(step.Any) > 0:
		tok.Waits = "join"
		for _, branch := range append(slices.Clone(step.All), step.Any...) {
			x.Tokens = append(x.Tokens, Token{ID: ss.tokenID(x), Step: branch, Branch: branch, Parent: token, Frames: slices.Clone(tok.Frames), Outputs: maps.Clone(tok.Outputs), Waits: "ready"})
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
func (ss *session) act(appID, protocol, action, target string, payload json.RawMessage, key string, onBehalf ...string) (*pb.EntityRef, *kernel.Error) {
	h := ss.f.host
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
	r, err := h.Submit(c, s, ss.now)
	if err != nil {
		return nil, err
	}
	return r.GetSubmission().GetTarget(), nil
}

// attemptAct is ss.act whose refusal the flow itself records: the nested
// decision is isolated in the parent's savepoint, so the retry, the backoff or
// the dead letter the flow writes about it stays in the same decision. An
// accepted attempt that was not caught would be discarded with the refused
// changes (a step's act keeps the plain route: a held approval is its answer).
func (ss *session) attemptAct(appID, action, target string, payload json.RawMessage, key string) (*pb.EntityRef, *kernel.Error) {
	var ref *pb.EntityRef
	_, err := platform.Attempt(ss.c, func() (*pb.ChangeRecord, *kernel.Error) {
		r, err := ss.act(appID, "", action, target, payload, key)
		ref = r
		if err != nil {
			return nil, err
		}
		return nil, nil
	})
	return ref, err
}

// failed retries a step's act with backoff, then takes its fault path, then compensates.
func (ss *session) failed(x *FlowInstance, token int, step *platform.Step, why string) {
	tok := ss.token(x, token)
	if step.NoRetry {
		tok.Attempts = stepAttempts
	}
	tok.Attempts++
	tok.Error = why
	switch {
	case tok.Attempts < stepAttempts:
		tok.Waits, tok.Due = "retry", ss.now.Add(backoff(tok.Attempts))
		ss.trace(x, tok.Step, "retry", why, "")
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

// failedUnhandled tells the flow's owners, and whoever started the instance,
// that a step failed with no fault path of its own: the flow undoes what it
// did, and the business it was doing needs a person (#118). No flow ends
// silently.
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

// compensate stops every path and undoes the completed acts, newest first.
func (ss *session) compensate(x *FlowInstance, why string) {
	ss.stopPaths(x)
	x.State = "compensating"
	x.Tokens = []Token{{ID: ss.tokenID(x), Step: "@undo", Waits: "ready"}}
	if runs := ss.f.host.Runs(); runs != nil { // what its agents did is undone with the rest (ADR-0022 D9)
		for _, run := range runs.Finished(ss.c, x.ID, "") {
			ss.signals = append(ss.signals, runSignal{run: run, RunSignal: host.RunSignal{At: ss.now, Kind: "undone", By: "flow", Detail: why}})
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
	// Every branch has a stable first-step key. Parallel completion order is
	// not an output order or a reason to repeat an already accepted action.
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
		child := Token{ID: ss.tokenID(x), Step: loop.Body, Parent: token, Waits: "ready", Frames: append(slices.Clone(parent.Frames), platform.Frame{Node: step.Name, Index: index, Item: item}), Outputs: maps.Clone(parent.Outputs)}
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
	if !ss.output(parent, parent.Tokens[i].ID, platform.Raw(map[string]any{"state": state, "outputs": x.Outputs, "data": json.RawMessage(x.Data)})) {
		ss.outputRefused(parent, parent.Tokens[i].ID)
		return
	}
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
	if schema == SchemaFlowStart || schema == SchemaFlowStep || schema == SchemaFlowBatch {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	var p struct{ Token int }
	json.Unmarshal(s.GetPayload(), &p)
	return f.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		ss := f.session(f.host.Automation(c, ID), now)
		x := ss.load(id)
		if x == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if ended(x.State) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		if schema != SchemaFlowStop {
			if err := f.checkBinding(*x); err != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: err.Error()}
			}
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
		case SchemaFlowReplay:
			// Deliver the named dead letters' signals again, in this same
			// instance decision (ADR-0047 §13.3): the letters keep their
			// numbers, the batch they form is a fresh successor of the frame's
			// cursor, and a signal the window still retains is absorbed as a
			// duplicate rather than applied twice.
			var in struct {
				Seqs []int64 `json:"seqs"`
			}
			if json.Unmarshal(s.GetPayload(), &in) != nil || len(in.Seqs) == 0 || len(in.Seqs) > 256 || len(in.Seqs) != len(slices.Compact(slices.Clone(in.Seqs))) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A replay names 1 to 256 distinct dead-letter numbers")
			}
			if x.Batch == nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The instance has no dead letters to replay")
			}
			d := f.def(x.Flow, x.Version)
			if d == nil || d.Continuous == nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "This instance's flow is not continuous")
			}
			retained := map[int64]DeadLetter{}
			for _, letter := range x.Batch.DeadLetters {
				retained[letter.Seq] = letter
			}
			signals := make([]Signal, 0, len(in.Seqs))
			for _, seq := range in.Seqs {
				letter, ok := retained[seq]
				if !ok {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, fmt.Sprintf("Dead letter %d is not retained on this instance", seq))
				}
				signals = append(signals, Signal{Key: letter.Key, Partition: letter.Partition, At: letter.At, Value: json.RawMessage(letter.Value)})
			}
			batch := Batch{ID: fmt.Sprintf("replay:%d:%d", x.Revision+1, len(in.Seqs)), Predecessor: x.Batch.Cursor, Signals: signals}
			if _, _, refusal := foldDecision(x, *d.Continuous, batch, now); refusal != nil {
				return nil, refusal
			}
			// The letters were delivered; a refusal above left them as they
			// were, so nothing is marked that did not happen.
			for i := range x.Batch.DeadLetters {
				if slices.Contains(in.Seqs, x.Batch.DeadLetters[i].Seq) {
					x.Batch.DeadLetters[i].Replayed = true
				}
			}
			ss.trace(x, "", "replayed", fmt.Sprintf("%d dead letters", len(in.Seqs)), c.ID)
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

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// backoff is the wait before a failed act's next attempt: 2 s, 4 s, 8 s, 16 s.
func backoff(attempts int) time.Duration { return time.Second << attempts }

func target(s *pb.Submission) string { return s.GetTarget().GetType() + "/" + s.GetTarget().GetId() }

// timeout is when a step times out: after its Timeout, or after its working
// days in the calendar of whom the instance runs for, else the tenant's (ADR-0028 D7).
func (ss *session) timeout(x *FlowInstance, step *platform.Step) (time.Time, bool) {
	if step.WorkingDays > 0 {
		cal := platform.Calendar{}
		if d := ss.f.host.Directory(); d != nil {
			party := ""
			if x.OnBehalf != "" {
				party = "member:" + x.OnBehalf
			}
			cal = d.Calendar(party, ss.now.UTC().Format(time.DateOnly))
		}
		return cal.After(ss.now, step.WorkingDays), true
	}
	return ss.now.Add(step.Timeout), step.Timeout > 0
}

// descendants are existing scheduler tokens, including nested forks/calls.
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

// Any is a race for the first successful path. A refused candidate does not
// win or rerun other branches; all candidates failing uses the fork's handler.
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
