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
	"time"
)

type session struct {
	f                  *Flows
	c                  platform.Caller
	now                time.Time
	changed            map[string]*FlowInstance
	order              []string
	assigns            []flowTask
	runs               []platform.RunStart
	signals            []runSignal
	close              []string
	event              *platform.Event
	moves              int
	operations         []operationStart
	preparedOperations map[int]platform.OperationRequest
	cancelOperations   []string
	withdrawals        []approvalWithdrawal
	assets             []any
	executions         map[string]platform.FlowHost
}

type approvalWithdrawal struct{ app, id, member string }

type operationStart struct {
	instance string
	token    int
	request  platform.OperationRequest
}

type runSignal struct {
	run string
	platform.RunSignal
}

func (ss *session) review(x *FlowInstance, step *platform.Step, kind, by, detail string) {
	runs := ss.f.host.Runs()
	if step == nil || step.Ask == nil || step.Ask.Reviews == "" || runs == nil {
		return
	}
	if done := runs.Finished(ss.c, x.ID, step.Ask.Reviews); len(done) > 0 {
		ss.signals = append(ss.signals, runSignal{run: done[len(done)-1], RunSignal: platform.RunSignal{At: ss.now, Kind: kind, By: by, Detail: detail}})
	}
}

type flowTask struct {
	app string
	platform.Assignment
}

const movesPerDecision = 1000

func (f *Flows) session(c platform.Caller, now time.Time) *session {
	return &session{f: f, c: c, now: now, changed: map[string]*FlowInstance{}, preparedOperations: map[int]platform.OperationRequest{}}
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
		if _, err := ss.act(nil, withdrawal.app, "", "work.approval.withdraw", withdrawal.id, json.RawMessage("{}"), "flow-withdraw:"+withdrawal.id, withdrawal.member); err != nil {
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
	for _, asset := range ss.assets {
		ss.c.Put(r, asset)
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

func (f *Flows) RunEnded(c platform.Caller, run platform.RunEnd, now time.Time) {
	c = f.host.Automation(c, ID)
	x, ok := platform.Get[FlowInstance](c, run.Flow)
	if !ok || ended(x.State) {
		return
	}
	i := slices.IndexFunc(x.Tokens, func(t Token) bool { return t.Child == run.ID && t.Waits == "agent" })
	if i < 0 {
		return
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
	if x.Batch != nil && len(x.Trace) > 128 {
		x.Trace = slices.Clone(x.Trace[len(x.Trace)-128:])
	}
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
	return ss.execution(x).Automation(ss.c, ss.def(x).app)
}

func (ss *session) execution(x *FlowInstance) platform.FlowHost {
	if x.Release == "" {
		return ss.f.host
	}
	if h := ss.executions[x.ID]; h != nil {
		return h
	}
	d := ss.def(x)
	h, err := ss.f.host.FlowExecution(ss.c, d.app, d.Name, d.Version, platform.FlowBinding{Dependencies: x.Dependencies, Release: x.Release})
	if err != nil {
		ss.c.FailDecision(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error()))
		return ss.f.host
	}
	if ss.executions == nil {
		ss.executions = map[string]platform.FlowHost{}
	}
	ss.executions[x.ID] = h
	return h
}

func (ss *session) run(x *FlowInstance, token ...int) *platform.Run {
	r := ss.f.run(x)
	r.Event = ss.event
	r.Now = ss.now
	if len(token) > 0 {
		tok := ss.token(x, token[0])
		if len(tok.Input) > 0 {
			r.Data = slices.Clone(tok.Input)
		}
		r.Outputs = maps.Clone(tok.Outputs)
		r.Frames = slices.Clone(tok.Frames)
	}
	return r
}

func (f *Flows) decide(c platform.Caller, schema, id, key string, now time.Time, build func(ss *session) *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
	s := &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: InstanceType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte("{}")}
	return f.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		ss := f.session(c, now)
		if err := build(ss); err != nil {
			return nil, err
		}
		if err := ss.retainDeadLetters(); err != nil {
			return nil, err
		}
		return ss.apply, nil
	})
}

func (f *Flows) step(c platform.Caller, id string, now time.Time, change func(*session, *FlowInstance)) *kernel.Error {
	return f.update(c, id, now, func(ss *session, in *FlowInstance) *kernel.Error {
		change(ss, in)
		return nil
	})
}

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

func (f *Flows) start(c platform.Caller, d *flowDef, key string, data any, onBehalf string, e *platform.Event, parent string, now time.Time, deferAdvance ...bool) *kernel.Error {
	if d.Start.Paused != nil && d.Start.Paused(f.host.Automation(c, d.app)) {
		if d.Start.Manual {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The process is paused")
		}
		return nil
	}
	flow := d.app + "." + d.Name
	id := flow + ":" + key
	for n := 2; ; n++ {
		x, known := platform.Get[FlowInstance](c, id)
		if !known {
			break
		}
		if !ended(x.State) {
			return nil
		}
		id = fmt.Sprintf("%s:%s#%d", flow, key, n)
	}
	bound, bindingErr := f.host.BindFlow(d.app, d.Name, d.Version, platform.FlowBinding{})
	if bindingErr != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: bindingErr.Error()}
	}
	_, err := f.decide(c, SchemaFlowStart, id, "start:"+id, now, func(ss *session) *kernel.Error {
		ss.event = e
		x := ss.create(d, id, key, data, onBehalf, parent)
		x.Dependencies, x.Release, x.Applications = bound.Dependencies, bound.Release, bound.Applications
		if len(deferAdvance) > 0 && deferAdvance[0] {
			if d.Continuous != nil {
				x.Tokens[0].Waits = "stream"
			} else {
				x.Tokens[0].Waits = "yield"
				x.Tokens[0].Due = now
			}
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
			if step.Choose != nil {
				var reason string
				r := ss.run(x, token)
				to, reason = step.Choose(ss.app(x), r)
				x.Data = string(r.Data)
				ss.trace(x, tok.Step, "chose", cmp.Or(to, "the end")+": "+reason, "")
			}
		}
	}
	*tok = Token{ID: tok.ID, Step: to, Branch: tok.Branch, Parent: tok.Parent, Frames: tok.Frames, Input: tok.Input, Outputs: tok.Outputs, Waits: "ready"}
	if x.State == "waiting" {
		x.State = "running"
	}
}

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

func (f *Flows) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := s.GetTarget().GetId()
	schema := s.GetSchema().GetName()
	if schema == SchemaFlowStart || schema == SchemaFlowStep || schema == SchemaFlowBatch || schema == SchemaFlowArtifactCleanup {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	var p struct{ Token int }
	json.Unmarshal(s.GetPayload(), &p)
	if schema == SchemaFlowReplay {
		return f.replayDeadLetter(c, s, now)
	}
	return f.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		ss := f.session(f.host.Automation(c, ID), now)
		x := ss.load(id)
		if x == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if ended(x.State) && schema != SchemaFlowResume && schema != SchemaFlowRetire {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		if schema != SchemaFlowStop && schema != SchemaFlowRetire {
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
					if d := ss.def(x); d.Continuous != nil && d.Continuous.Entry != "" && t.Step == d.Continuous.Entry && (t.Waits == "stuck" || t.Waits == "retry") {
						t.Waits, t.Attempts, t.Due, t.Task = "retry", 0, now, ""
						continue
					}
					t.Waits, t.Attempts, t.Due, t.Task = "ready", 0, time.Time{}, ""
				}
			}
			if x.State == "stuck" {
				d := ss.def(x)
				if d.Continuous != nil && d.Continuous.Entry != "" && slices.ContainsFunc(x.Tokens, func(t Token) bool { return t.Step == d.Continuous.Entry && t.Waits == "retry" }) {
					x.State = "waiting"
				} else {
					x.State = map[bool]string{true: "compensating", false: "running"}[slices.ContainsFunc(x.Tokens, func(t Token) bool { return t.Step == "@undo" })]
				}
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
			x.Resumable = ss.def(x) != nil && ss.def(x).Continuous != nil && len(x.Tokens) == 1 && x.Tokens[0].Waits == "stream"
			ss.stopPaths(x)
			x.State, x.Tokens = "canceled", nil
			ss.trace(x, "", "canceled", "", c.ID)
			return ss.apply, nil
		case SchemaFlowResume:
			d := ss.def(x)
			if x.State != "canceled" || !x.Resumable || d == nil || d.Continuous == nil || d.Start.Enabled != nil && !d.Start.Enabled(ss.c) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only a stopped continuous Flow without canceled in-flight work can resume its retained version")
			}
			x.Resumable, x.State = false, "waiting"
			x.Tokens = []Token{{ID: ss.tokenID(x), Step: d.Steps[0].Name, Waits: "stream"}}
			ss.trace(x, "", "resumed", "accepted source checkpoint retained", c.ID)
			return ss.apply, nil
		case SchemaFlowRetire:
			if !ended(x.State) || x.CleanupPending || x.Batch == nil || x.Batch.Sealed == nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only a terminal continuous frame without pending cleanup can be retired")
			}
			refs := []platform.FlowStateArtifact{*x.Batch.Sealed}
			if x.Batch.CheckpointArtifact != nil {
				refs = append(refs, *x.Batch.CheckpointArtifact)
			}
			x.Batch, x.Outputs, x.Resumable = nil, nil, false
			x.ArtifactCleanup, x.CleanupPending = refs, true
			if err := validateArtifactCleanup(*x); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
			ss.trace(x, "", "state retired", "terminal frame and checkpoint released", c.ID)
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
			binding, err := f.host.BindFlow(to.app, to.Name, to.Version, platform.FlowBinding{})
			if err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The target flow version cannot be bound")
			}
			x.Version = to.Version
			x.Dependencies, x.Release, x.Applications = binding.Dependencies, binding.Release, binding.Applications
		default:
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
		}
		ss.advance(x)
		return ss.apply, nil
	})
}
