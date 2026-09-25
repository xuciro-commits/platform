package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The flow app runs the flows apps declare (ADR-0020). An instance is a record
// of it; every step it takes is one of its decisions, made inside the owned
// work that caused it (a delivery of an event, or its timer job), so replay
// takes it again. Acts are submitted as the declaring app's automation
// principal; people are asked through that app's tasks (ADR-0017).
const (
	FlowApp         = "flow"
	InstanceType    = "flow.instance"
	FlowAdmin       = "admin"
	SchemaFlowStart = "flow.instance.start"
	SchemaFlowStep  = "flow.instance.step"
	SchemaFlowRetry = "flow.instance.retry"
	SchemaFlowSkip  = "flow.instance.skip"
	SchemaFlowStop  = "flow.instance.cancel"
	SchemaFlowMove  = "flow.instance.move"
	stepAttempts    = 5
	// Compensate as a next step undoes the completed acts, newest first.
	Compensate = platform.Compensate
)

// FlowInstance is one run of a flow.
type FlowInstance struct {
	platform.Record
	Flow     string      `json:"flow" field:"readonly,search"` // "<app>.<name>"
	Title    string      `json:"title" field:"readonly,search"`
	Version  int         `json:"version" field:"readonly"`
	Key      string      `json:"key" field:"readonly,search"`
	State    string      `json:"state" field:"readonly" choices:"running,waiting,done,compensating,compensated,canceled,stuck"`
	OnBehalf string      `json:"onBehalf,omitempty" field:"readonly" title:"On behalf of"`
	Data     string      `json:"data,omitempty" field:"readonly" type:"longtext"`
	Answer   string      `json:"answer,omitempty" field:"readonly"`
	Parent   string      `json:"parent,omitempty" field:"readonly"` // the instance that called it
	Tokens   []Token     `json:"tokens" field:"readonly" title:"Where it stands"`
	Undo     []UndoEntry `json:"undo" field:"readonly" title:"To undo"`
	Trace    []TraceLine `json:"trace" field:"readonly"`
	Seq      int         `json:"seq" field:"readonly"` // tokens and tasks made, for their IDs
}

// Token is where a path of the instance stands (BPMN's token): a step it is at
// or waits in. Parallel branches have one each.
type Token struct {
	ID       int       `json:"id"`
	Step     string    `json:"step"`
	Branch   string    `json:"branch,omitempty"` // the All or Any step it runs in
	Waits    string    `json:"waits,omitempty"`  // ready, retry, wait, ask, call, join, undo, stuck
	Attempts int       `json:"attempts,omitempty"`
	Due      time.Time `json:"due,omitzero"` // a retry, a timeout, or a wait's time
	Task     string    `json:"task,omitempty"`
	Child    string    `json:"child,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// UndoEntry is a completed act's compensation, built when it completed.
type UndoEntry struct {
	Step     string          `json:"step"`
	App      string          `json:"app"`
	Protocol string          `json:"protocol,omitempty"`
	Action   string          `json:"action"`
	Target   string          `json:"target"`
	Payload  json.RawMessage `json:"payload"`
}

// TraceLine is why the instance moved (ADR-0020 D8).
type TraceLine struct {
	At     time.Time `json:"at"`
	Step   string    `json:"step,omitempty"`
	What   string    `json:"what"`
	Detail string    `json:"detail,omitempty"`
	By     string    `json:"by,omitempty"`
}

type flowDef struct {
	app string
	platform.Flow
	steps map[string]*platform.Step
}

// Flows is a tenant's flow app.
type Flows struct {
	mu     sync.Mutex
	t      *Tenant
	ledger *platform.Ledger
	defs   map[string][]*flowDef // "<app>.<name>" → versions, ascending
	// chosen are the versions new instances took during the input being
	// handled, journaled with it; pins are those of the entry being replayed.
	chosen, pins map[string]int
}

// version is the one a new instance of flow takes: the highest declared, or
// in a replay the one the journal says it took.
func (f *Flows) version(c platform.Caller, flow string) (*flowDef, *kernel.Error) {
	if c.Replaying {
		if v, ok := f.pins[flow]; ok {
			if d := f.def(flow, v); d != nil {
				return d, nil
			}
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
	}
	d, ok := f.latest(flow)
	if !ok {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if f.chosen == nil {
		f.chosen = map[string]int{}
	}
	f.chosen[flow] = d.Version
	return d, nil
}

func NewFlows(tenant string) *Flows {
	admin := []string{FlowAdmin}
	var actions []platform.Action
	for _, e := range flowEntities() {
		actions = append(actions, platform.EntityActions(e)...)
	}
	host := "Made by the host as the flow runs."
	actions = append(actions,
		platform.Action{Schema: SchemaFlowStart, Target: InstanceType, Capability: "flows", Title: "Start flow", Description: host, Payload: []platform.Field{}, Roles: admin},
		platform.Action{Schema: SchemaFlowStep, Target: InstanceType, Capability: "flows", Title: "Take step", Description: host, Payload: []platform.Field{}, Roles: admin},
		platform.Action{Schema: SchemaFlowRetry, Target: InstanceType, Capability: "flows", Title: "Retry", Payload: []platform.Field{}, Roles: admin,
			Description: "Try a stuck or waiting instance's steps again, with fresh attempts."},
		platform.Action{Schema: SchemaFlowSkip, Target: InstanceType, Capability: "flows", Title: "Skip step", Roles: admin,
			Description: "Leave the step a stuck path is at, as if it had ended, and go on.", Payload: []platform.Field{{Name: "token", Type: "integer", Required: true, Description: "The path"}}},
		platform.Action{Schema: SchemaFlowStop, Target: InstanceType, Capability: "flows", Title: "Cancel", Payload: []platform.Field{}, Roles: admin,
			Description: "Stop a running instance; its open tasks close. Nothing is undone."},
		platform.Action{Schema: SchemaFlowMove, Target: InstanceType, Capability: "flows", Title: "Move to the next version", Payload: []platform.Field{}, Roles: admin,
			Description: "Move a running instance to its flow's next version, as that version's mapping says."})
	return &Flows{ledger: platform.NewLedger(tenant, FlowApp, platform.NewCatalog(actions...), InstanceType), defs: map[string][]*flowDef{}}
}

func flowEntities() []platform.Entity {
	return []platform.Entity{{Type: InstanceType, Title: "Flow instance", Model: FlowInstance{}, Display: "title"}}
}

func (f *Flows) Manifest() platform.Manifest {
	return platform.Manifest{ID: FlowApp, Title: "Flows", Version: "1", Actions: f.ledger.Catalog, Entities: flowEntities(), Reads: []string{"flows"},
		Jobs: []platform.Job{{Name: "timers", Title: "Take flows' due steps: retries, timeouts, times and conditions", Every: time.Second}}}
}

func (f *Flows) Declarations() []*pb.AuthorityDeclaration { return f.ledger.Declarations() }
func (f *Flows) Snapshot() (json.RawMessage, error)       { return f.ledger.Snapshot() }
func (f *Flows) Restore(raw json.RawMessage) error        { return f.ledger.Restore(raw) }
func (f *Flows) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// declare registers the flows of the tenant's apps, checking each (NewTenant).
func (f *Flows) declare(a platform.App) error {
	m := a.Manifest()
	for _, fl := range m.Flows {
		id := m.ID + "." + fl.Name
		d := &flowDef{app: m.ID, Flow: fl, steps: map[string]*platform.Step{}}
		if fl.Name == "" || fl.Title == "" || fl.Version < 1 || len(fl.Steps) == 0 || len(fl.Start.On) == 0 || fl.Start.Begin == nil {
			return fmt.Errorf("flow %s: name, title, version, a start and steps are required", id)
		}
		versions := f.defs[id]
		if len(versions) > 0 && versions[len(versions)-1].Version >= fl.Version {
			return fmt.Errorf("flow %s: versions must be declared in ascending order", id)
		}
		for _, on := range fl.Start.On {
			if protocol, _, ok := strings.Cut(on, "#"); ok {
				if !slices.ContainsFunc(m.Consumes, func(c platform.Consumption) bool { return c.Protocol == protocol }) {
					return fmt.Errorf("flow %s starts on %s of a protocol %s does not consume", id, on, m.ID)
				}
			} else if _, own := m.Actions.Action(on); !own {
				return fmt.Errorf("flow %s starts on %s, neither %s's action nor a protocol event", id, on, m.ID)
			}
		}
		for i := range fl.Steps {
			s := &fl.Steps[i]
			if s.Name == "" || d.steps[s.Name] != nil || s.Name[0] == '@' {
				return fmt.Errorf("flow %s: step %d needs a unique name", id, i+1)
			}
			d.steps[s.Name] = s
			kinds := 0
			for _, set := range []bool{s.Act != nil, s.Wait != nil, s.Ask != nil, s.Call != nil, len(s.All) > 0, len(s.Any) > 0, s.Agent != nil} {
				if set {
					kinds++
				}
			}
			if kinds != 1 {
				return fmt.Errorf("flow %s: step %s must be exactly one kind", id, s.Name)
			}
		}
		for _, s := range fl.Steps {
			refs := append(append([]string{s.Next, s.OnTimeout, s.Fault}, s.All...), s.Any...)
			if s.Call != nil {
				refs = nil
				if _, ok := f.latest(m.ID + "." + s.Call.Flow); !ok && s.Call.Flow != fl.Name {
					return fmt.Errorf("flow %s: step %s calls %s, not declared before it", id, s.Name, s.Call.Flow)
				}
				refs = append(refs, s.Next, s.OnTimeout)
			}
			for _, r := range refs {
				if r != "" && r != Compensate && d.steps[r] == nil {
					return fmt.Errorf("flow %s: step %s goes to %s, not a step", id, s.Name, r)
				}
			}
			if s.Timeout > 0 && s.OnTimeout == "" {
				return fmt.Errorf("flow %s: step %s times out to nowhere", id, s.Name)
			}
			if act := s.Act; act != nil && (act.Action == "" || act.Target == nil) {
				return fmt.Errorf("flow %s: step %s acts without an action and a target", id, s.Name)
			}
			if w := s.Wait; w != nil && (w.On == "") != (w.Match == nil) {
				return fmt.Errorf("flow %s: step %s waits for an event without matching it", id, s.Name)
			}
			if a := s.Ask; a != nil && (a.Title == nil || a.To == nil) {
				return fmt.Errorf("flow %s: step %s asks without a title or people", id, s.Name)
			}
		}
		for from, to := range fl.From {
			if d.steps[to] == nil || len(versions) == 0 || versions[len(versions)-1].steps[from] == nil {
				return fmt.Errorf("flow %s: version %d moves %s to %s, not steps of both versions", id, fl.Version, from, to)
			}
		}
		f.defs[id] = append(versions, d)
	}
	return nil
}

func (f *Flows) latest(id string) (*flowDef, bool) {
	v := f.defs[id]
	if len(v) == 0 {
		return nil, false
	}
	return v[len(v)-1], true
}

func (f *Flows) def(id string, version int) *flowDef {
	for _, d := range f.defs[id] {
		if d.Version == version {
			return d
		}
	}
	return nil
}

// Check refuses a tenant whose running instances need a flow version its code
// no longer declares (ADR-0020 D6): the host starts only when every running
// instance can go on.
func (f *Flows) Check() error {
	c := f.t.automation(FlowApp, true)
	for _, x := range f.running(c) {
		if f.def(x.Flow, x.Version) == nil {
			return fmt.Errorf("flow instance %s runs %s version %d, which the code no longer declares", x.ID, x.Flow, x.Version)
		}
	}
	return nil
}

func (f *Flows) running(c platform.Caller) []FlowInstance {
	live, _ := json.Marshal([]any{"|", []any{"state", "=", "running"}, "|", []any{"state", "=", "waiting"}, "|", []any{"state", "=", "compensating"}, []any{"state", "=", "stuck"}})
	out, _, _ := platform.Find[FlowInstance](c, platform.Query{Domain: live, Sort: []string{"id"}})
	return out
}

// interested reports whether an event starts a flow or may end a wait.
func (f *Flows) interested(names []string, e platform.Event) bool {
	for _, versions := range f.defs {
		if d := versions[len(versions)-1]; slices.ContainsFunc(d.Start.On, func(on string) bool { return slices.Contains(names, on) }) {
			return true
		}
		for _, d := range versions {
			for _, s := range d.Steps {
				if s.Wait != nil && slices.Contains(names, s.Wait.On) || s.Ask != nil && s.Ask.On != "" && slices.Contains(names, s.Ask.On) {
					return true
				}
			}
		}
	}
	s := e.Record.GetSubmission()
	return s.GetSchema().GetName() == "work.task.complete" && e.App == WorkApp
}

// handle takes an event delivered to the flow app: it starts flows and ends
// waits. It runs as owned work, so a failure is retried and replay repeats it.
func (f *Flows) handle(c platform.Caller, e platform.Event, names []string, now time.Time) *kernel.Error {
	s := e.Record.GetSubmission()
	for _, id := range slices.Sorted(maps.Keys(f.defs)) {
		latest, _ := f.latest(id)
		if !slices.ContainsFunc(latest.Start.On, func(on string) bool { return slices.Contains(names, on) }) {
			continue
		}
		key, data, ok := latest.Start.Begin(f.t.automation(latest.app, c.Replaying), e)
		if !ok {
			continue
		}
		d, err := f.version(c, id)
		if err != nil {
			return err
		}
		if err := f.start(c, d, key, data, s.GetPrincipalId(), &e, "", now); err != nil {
			return err
		}
	}
	for _, x := range f.running(c) {
		d := f.def(x.Flow, x.Version)
		if d == nil {
			continue
		}
		run := f.run(&x)
		for i := range x.Tokens {
			tok := &x.Tokens[i]
			step := d.steps[tok.Step]
			app := f.t.automation(d.app, c.Replaying)
			switch {
			case tok.Waits == "wait" && step != nil && step.Wait != nil && step.Wait.On != "" && slices.Contains(names, step.Wait.On) && step.Wait.Match(app, run, e):
				if err := f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					ss.event = &e
					ss.trace(in, tok.Step, "event", s.GetSchema().GetName()+" "+target(s), s.GetPrincipalId())
					ss.next(in, tok.ID, "")
				}); err != nil {
					return err
				}
			case tok.Waits == "ask" && s.GetSchema().GetName() == "work.task.complete" && s.GetTarget().GetId() == tok.Task:
				task, _ := platform.Get[WorkTask](f.t.automation(WorkApp, c.Replaying), tok.Task)
				answer := cmpOr(task.Answer, "done")
				if err := f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					in.Answer = answer
					ss.trace(in, tok.Step, "answered", answer, task.Assignee)
					ss.next(in, tok.ID, "")
				}); err != nil {
					return err
				}
			case tok.Waits == "ask" && step != nil && step.Ask != nil && step.Ask.On != "" && slices.Contains(names, step.Ask.On) && step.Ask.Match(app, run, e):
				if err := f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					ss.close = append(ss.close, tok.Task)
					ss.event, in.Answer = &e, "event"
					ss.trace(in, tok.Step, "event", s.GetSchema().GetName()+" "+target(s)+" closed the task", s.GetPrincipalId())
					ss.next(in, tok.ID, "")
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Run takes what is due: retries, timeouts, times and conditions.
func (f *Flows) Run(c platform.Caller, _ string, now time.Time) *kernel.Error {
	for _, x := range f.running(c) {
		d := f.def(x.Flow, x.Version)
		if d == nil {
			continue
		}
		run := f.run(&x)
		for _, tok := range x.Tokens {
			step := d.steps[tok.Step]
			due := !tok.Due.IsZero() && !tok.Due.After(now)
			holds := tok.Waits == "wait" && step != nil && step.Wait != nil && step.Wait.Until != nil && step.Wait.Until(f.t.automation(d.app, c.Replaying), run)
			if !due && !holds {
				continue
			}
			var err *kernel.Error
			switch {
			case holds:
				err = f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					ss.trace(in, tok.Step, "condition", "it holds", "")
					ss.next(in, tok.ID, "")
				})
			case tok.Waits == "retry" || tok.Waits == "undo":
				err = f.step(c, x.ID, now, func(ss *session, in *FlowInstance) { ss.token(in, tok.ID).Waits = "ready" })
			case tok.Waits == "wait" && step != nil && step.Wait != nil && step.Wait.At != nil && tok.Due.Equal(step.Wait.At(f.t.automation(d.app, c.Replaying), run)):
				err = f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					ss.trace(in, tok.Step, "time", "reached", "")
					ss.next(in, tok.ID, "")
				})
			case tok.Waits == "wait" || tok.Waits == "ask" || tok.Waits == "call":
				err = f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					if tok.Task != "" {
						ss.close = append(ss.close, tok.Task)
					}
					ss.trace(in, tok.Step, "timeout", step.Timeout.String(), "")
					ss.next(in, tok.ID, step.OnTimeout)
				})
			}
			if err != nil {
				return err
			}
			break // one step per instance per run: the next run sees its new state
		}
	}
	return nil
}

func (f *Flows) run(x *FlowInstance) *platform.Run {
	return &platform.Run{ID: x.ID, Flow: x.Flow, Version: x.Version, Key: x.Key, OnBehalf: x.OnBehalf, Data: json.RawMessage(cmpOr(x.Data, "null")), Answer: x.Answer}
}

// Read "flows": the declared flows, for the workspace to draw.
func (f *Flows) Read(c platform.Caller, _ string) (any, *kernel.Error) {
	type stepView struct {
		Name, Title, Kind string
		Next              []string `json:"next"`
	}
	type flowView struct {
		ID      string     `json:"id"`
		App     string     `json:"app"`
		Title   string     `json:"title"`
		Version int        `json:"version"`
		Start   []string   `json:"start"`
		Steps   []stepView `json:"steps"`
	}
	out := []flowView{}
	for _, id := range slices.Sorted(maps.Keys(f.defs)) {
		for _, d := range f.defs[id] {
			v := flowView{ID: id, App: d.app, Title: d.Title, Version: d.Version, Start: d.Start.On}
			for _, s := range d.Steps {
				sv := stepView{Name: s.Name, Title: cmpOr(s.Title, s.Name), Kind: kindOf(s), Next: []string{}}
				for _, n := range append(append([]string{s.Next, s.OnTimeout, s.Fault}, s.All...), s.Any...) {
					if n != "" && !slices.Contains(sv.Next, n) {
						sv.Next = append(sv.Next, n)
					}
				}
				v.Steps = append(v.Steps, sv)
			}
			out = append(out, v)
		}
	}
	return out, nil
}

func kindOf(s platform.Step) string {
	switch {
	case s.Act != nil:
		return "act"
	case s.Wait != nil:
		return "wait"
	case s.Ask != nil:
		return "ask"
	case s.Call != nil:
		return "call"
	case len(s.All) > 0:
		return "all"
	case len(s.Any) > 0:
		return "any"
	}
	return "agent"
}
