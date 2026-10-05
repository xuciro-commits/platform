package flow

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/work"
	"platformserver/internal/host"
	"platformserver/platform"
)

// Package flow is the platform's flow app, on the app API and internal/host
// (ADR-0025 D4). It runs the flows apps declare (ADR-0020). An instance is a record
// of it; every step it takes is one of its decisions, made inside the owned
// work that caused it (a delivery of an event, or its timer job), so replay
// takes it again. Acts are submitted as the declaring app's automation
// principal; people are asked through that app's tasks (ADR-0017).
const (
	ID              = "flow"
	InstanceType    = "flow.instance"
	Admin           = "admin"
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
	Flow         string                     `json:"flow" field:"readonly,search"` // "<app>.<name>"
	Title        string                     `json:"title" field:"readonly,search"`
	Version      int                        `json:"version" field:"readonly"`
	Dependencies string                     `json:"dependencies,omitempty" field:"readonly" title:"Dependency release"`
	Release      string                     `json:"release,omitempty" field:"readonly" title:"Active release"`
	Key          string                     `json:"key" field:"readonly,search"`
	Subject      string                     `json:"subject,omitempty" field:"readonly"` // "<type>/<key>" when the flow declares its subject
	State        string                     `json:"state" field:"readonly" choices:"running,waiting,done,compensating,compensated,canceled,stuck"`
	OnBehalf     string                     `json:"onBehalf,omitempty" field:"readonly" title:"On behalf of"`
	Data         string                     `json:"data,omitempty" field:"readonly" type:"longtext"`
	Outputs      map[string]json.RawMessage `json:"outputs,omitempty" field:"readonly" type:"json"`
	Sources      []string                   `json:"sources,omitempty" field:"readonly"`
	Withheld     bool                       `json:"withheld,omitempty" field:"readonly"`
	Answer       string                     `json:"answer,omitempty" field:"readonly"`
	Parent       string                     `json:"parent,omitempty" field:"readonly"` // the instance that called it
	Tokens       []Token                    `json:"tokens" field:"readonly" title:"Where it stands"`
	// Batch is the continuous instance's frame (ADR-0047 §13): the cursor it
	// consumed through, its watermark, node state and dead letters.
	Batch *BatchFrame `json:"batch,omitempty" field:"readonly" type:"json"`
	Undo  []UndoEntry `json:"undo" field:"readonly" title:"To undo"`
	Trace []TraceLine `json:"trace" field:"readonly"`
	Seq   int         `json:"seq" field:"readonly"` // tokens and tasks made, for their IDs
}

// Token is where a path of the instance stands (BPMN's token): a step it is at
// or waits in. Parallel branches have one each.
type Token struct {
	ID        int                        `json:"id"`
	Step      string                     `json:"step"`
	Branch    string                     `json:"branch,omitempty"` // the All or Any step it runs in
	Waits     string                     `json:"waits,omitempty"`  // ready, retry, wait, ask, call, join, undo, stuck
	Attempts  int                        `json:"attempts,omitempty"`
	Due       time.Time                  `json:"due,omitzero"` // a retry, a timeout, or a wait's time
	Task      string                     `json:"task,omitempty"`
	Child     string                     `json:"child,omitempty"`
	Error     string                     `json:"error,omitempty"`
	Parent    int                        `json:"parent,omitempty"`
	Frames    []platform.Frame           `json:"frames,omitempty"`
	Outputs   map[string]json.RawMessage `json:"outputs,omitempty"`
	Operation string                     `json:"operation,omitempty"`
	Loop      *LoopFrame                 `json:"loop,omitempty"`
	Results   map[string]json.RawMessage `json:"results,omitempty"`
}

// LoopFrame belongs to the existing durable token, not a separate graph run.
// Frozen items, assigned cursor and indexed results survive suspended bodies.
type LoopFrame struct {
	Items   []json.RawMessage       `json:"items,omitempty"`
	Outer   []string                `json:"outer,omitempty"`
	Next    int                     `json:"next"`
	Results map[int]json.RawMessage `json:"results,omitempty"`
	State   json.RawMessage         `json:"state,omitempty"`
	While   bool                    `json:"while,omitempty"`
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
	host   host.Host // the tenant running it, once composed
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

// Attach is called by the host when a tenant is composed.
func (f *Flows) Attach(h host.Host) { f.host = h }

// New is a tenant's flow app.
func New(tenant string) *Flows {
	admin := []string{Admin}
	var actions []platform.Action
	for _, e := range flowEntities() {
		actions = append(actions, platform.EntityActions(e)...)
	}
	byHost := "Made by the host as the flow runs."
	actions = append(actions,
		platform.Action{Schema: SchemaFlowStart, Target: InstanceType, Capability: "flows", Title: "Start flow", Description: byHost, Payload: []platform.Field{}, Roles: admin},
		platform.Action{Schema: SchemaFlowStep, Target: InstanceType, Capability: "flows", Title: "Take step", Description: byHost, Payload: []platform.Field{}, Roles: admin},
		platform.Action{Schema: SchemaFlowRetry, Target: InstanceType, Capability: "flows", Title: "Retry", Payload: []platform.Field{}, Roles: admin,
			Description: "Try a stuck or waiting instance's steps again, with fresh attempts."},
		platform.Action{Schema: SchemaFlowSkip, Target: InstanceType, Capability: "flows", Title: "Skip step", Roles: admin,
			Description: "Leave the step a stuck path is at, as if it had ended, and go on.", Payload: []platform.Field{{Name: "token", Type: "integer", Required: true, Description: "The path"}}},
		platform.Action{Schema: SchemaFlowStop, Target: InstanceType, Capability: "flows", Title: "Cancel", Payload: []platform.Field{}, Roles: admin,
			Description: "Stop a running instance; its open tasks close. Nothing is undone."},
		platform.Action{Schema: SchemaFlowMove, Target: InstanceType, Capability: "flows", Title: "Move to the next version", Payload: []platform.Field{}, Roles: admin,
			Description: "Move a running instance to its flow's next version, as that version's mapping says."})
	return &Flows{ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(actions...), InstanceType), defs: map[string][]*flowDef{}}
}

func flowEntities() []platform.Entity {
	return []platform.Entity{{Type: InstanceType, Title: "Flow instance", Model: FlowInstance{}, Display: "title",
		Scope:   platform.Scope{Participants: func(record any) []string { return []string{record.(FlowInstance).OnBehalf} }},
		Derived: []platform.Derivation{{From: "sources", Fields: []string{"data", "outputs", "tokens", "trace", "answer"}}}, Withheld: "withheld"}}
}

func (f *Flows) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "Flows", Version: "1", Actions: f.ledger.Catalog, Entities: flowEntities(), Reads: []string{"flows"},
		Jobs: []platform.Job{{Name: "timers", Title: "Take flows' due steps: retries, timeouts, times and conditions", Every: time.Second}}}
}

func (f *Flows) Declarations() []*pb.AuthorityDeclaration { return f.ledger.Declarations() }
func (f *Flows) AcceptedLedger() *platform.Ledger         { return f.ledger }
func (*Flows) AcceptedWork()                              {}
func (f *Flows) AcceptedActionSchemas() []string {
	var schemas []string
	for _, action := range f.ledger.Catalog.All() {
		schemas = append(schemas, action.Schema)
	}
	return schemas
}
func (f *Flows) Snapshot() (json.RawMessage, error) { return f.ledger.Snapshot() }
func (f *Flows) Restore(raw json.RawMessage) error  { return f.ledger.Restore(raw) }
func (f *Flows) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// Declare registers the flows of the tenant's apps, checking each (NewTenant).
func (f *Flows) Declare(a platform.App) error {
	m := a.Manifest()
	for _, fl := range m.Flows {
		d, err := f.check(m, fl)
		if err != nil {
			return err
		}
		f.defs[m.ID+"."+fl.Name] = append(f.defs[m.ID+"."+fl.Name], d)
	}
	return nil
}

// Install registers a flow an app composed at runtime — a process a tenant
// defined (ADR-0034, #132) — as its next version: running instances keep the
// version they started on (ADR-0020 D6).
func (f *Flows) Install(a platform.App, fl platform.Flow) error {
	d, err := f.check(a.Manifest(), fl)
	if err != nil {
		return err
	}
	id := a.Manifest().ID + "." + fl.Name
	f.defs[id] = append(f.defs[id], d)
	return nil
}

// HasPublishedFlow checks application membership through the original registry.
func (f *Flows) HasPublishedFlow(id string) bool { _, ok := f.latest(id); return ok }

// Validate checks a flow as Install would, registering nothing.
func (f *Flows) Validate(a platform.App, fl platform.Flow) error {
	_, err := f.check(a.Manifest(), fl)
	return err
}

func (f *Flows) check(m platform.Manifest, fl platform.Flow) (*flowDef, error) {
	id := m.ID + "." + fl.Name
	d := &flowDef{app: m.ID, Flow: fl, steps: map[string]*platform.Step{}}
	byEvent, byState := len(fl.Start.On) > 0 && fl.Start.Begin != nil, fl.Start.Type != "" && fl.Start.When != nil
	if fl.Name == "" || fl.Title == "" || fl.Version < 1 || len(fl.Steps) == 0 || boolCount(byEvent, byState, fl.Start.Manual) != 1 {
		return nil, fmt.Errorf("flow %s: name, title, version, steps and one start — on events, or on a record's state — are required", id)
	}
	if byState && !slices.ContainsFunc(m.Entities, func(e platform.Entity) bool { return e.Type == fl.Start.Type }) && (m.ID != "build" || f.host == nil || !f.host.Declares(fl.Start.Type)) {
		return nil, fmt.Errorf("flow %s starts on the state of %s, not an entity type of %s", id, fl.Start.Type, m.ID)
	}
	if fl.Continuous != nil {
		if fl.Continuous.Source == "" || fl.Continuous.Batch < 0 || !fl.Continuous.DeadLetter {
			return nil, fmt.Errorf("flow %s: a continuous flow names its source, a batch budget of zero or more, and keeps dead letters", id)
		}
	}
	versions := f.defs[id]
	if len(versions) > 0 && versions[len(versions)-1].Version >= fl.Version {
		return nil, fmt.Errorf("flow %s: versions must be declared in ascending order", id)
	}
	for _, on := range fl.Start.On {
		if protocol, _, ok := strings.Cut(on, "#"); ok {
			if !slices.ContainsFunc(m.Consumes, func(c platform.Consumption) bool { return c.Protocol == protocol }) {
				return nil, fmt.Errorf("flow %s starts on %s of a protocol %s does not consume", id, on, m.ID)
			}
		} else if _, own := m.Actions.Action(on); !own {
			return nil, fmt.Errorf("flow %s starts on %s, neither %s's action nor a protocol event", id, on, m.ID)
		}
	}
	for i := range fl.Steps {
		s := &fl.Steps[i]
		if s.Name == "" || d.steps[s.Name] != nil || s.Name[0] == '@' {
			return nil, fmt.Errorf("flow %s: step %d needs a unique name", id, i+1)
		}
		d.steps[s.Name] = s
		kinds := 0
		for _, set := range []bool{s.Act != nil, s.Wait != nil, s.Ask != nil, s.Call != nil, len(s.All) > 0, len(s.Any) > 0, s.Agent != nil, s.Evaluate != nil, s.Branch != nil, s.Loop != nil, s.Operation != nil, s.Invoke != nil, s.End, s.LoopControl != ""} {
			if set {
				kinds++
			}
		}
		if kinds != 1 {
			return nil, fmt.Errorf("flow %s: step %s must be exactly one kind", id, s.Name)
		}
	}
	for _, s := range fl.Steps {
		refs := append(append([]string{s.Next, s.OnTimeout, s.Fault}, s.All...), s.Any...)
		if s.Branch != nil {
			refs = append(refs, s.Branch.Paths...)
		}
		if s.Loop != nil {
			refs = append(refs, s.Loop.Body)
		}
		if s.Call != nil {
			refs = nil
			if _, ok := f.latest(m.ID + "." + s.Call.Flow); !ok && s.Call.Flow != fl.Name {
				return nil, fmt.Errorf("flow %s: step %s calls %s, not declared before it", id, s.Name, s.Call.Flow)
			}
			refs = append(refs, s.Next, s.OnTimeout)
		}
		for _, r := range refs {
			if r != "" && r != Compensate && d.steps[r] == nil {
				return nil, fmt.Errorf("flow %s: step %s goes to %s, not a step", id, s.Name, r)
			}
		}
		if (s.Timeout > 0 || s.WorkingDays > 0) && s.OnTimeout == "" {
			return nil, fmt.Errorf("flow %s: step %s times out to nowhere", id, s.Name)
		}
		if s.Evaluate != nil && s.Evaluate.Run == nil || s.Branch != nil && s.Branch.Choose == nil || s.Operation != nil && s.Operation.Request == nil {
			return nil, fmt.Errorf("flow %s: step %s lacks its native implementation", id, s.Name)
		}
		if loop := s.Loop; loop != nil && (loop.Body == "" || boolCount(loop.Items != nil, loop.While != nil) != 1 || loop.MaxIterations < 1 || loop.MaxIterations > 10000 || loop.Concurrency < 1 || loop.Concurrency > 32 || loop.While != nil && loop.Concurrency != 1) {
			return nil, fmt.Errorf("flow %s: loop %s needs a bounded child scope", id, s.Name)
		}
		if invoke := s.Invoke; invoke != nil && (invoke.Act.Action == "" || invoke.Act.Target == nil || invoke.Result == nil) {
			return nil, fmt.Errorf("flow %s: invocation %s needs an existing action and durable reply", id, s.Name)
		}
		if act := s.Act; act != nil && (act.Action == "" || act.Target == nil) {
			return nil, fmt.Errorf("flow %s: step %s acts without an action and a target", id, s.Name)
		}
		if w := s.Wait; w != nil && (w.On == "") != (w.Match == nil) {
			return nil, fmt.Errorf("flow %s: step %s waits for an event without matching it", id, s.Name)
		}
		if a := s.Ask; a != nil && (a.Title == nil || a.To == nil) {
			return nil, fmt.Errorf("flow %s: step %s asks without a title or people", id, s.Name)
		}
		if ag := s.Agent; ag != nil && (ag.Goal == nil || ag.To == nil || !slices.ContainsFunc(m.Agents, func(x platform.Agent) bool { return x.Name == ag.Agent })) {
			return nil, fmt.Errorf("flow %s: step %s gives a goal to %q, not an agent of %s, or without a goal and people", id, s.Name, ag.Agent, m.ID)
		}
	}
	for from, to := range fl.From {
		if d.steps[to] == nil || len(versions) == 0 || versions[len(versions)-1].steps[from] == nil {
			return nil, fmt.Errorf("flow %s: version %d moves %s to %s, not steps of both versions", id, fl.Version, from, to)
		}
	}
	return d, nil
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
// Versions are those new instances took during the input being handled, then forgotten.
func (f *Flows) Versions() map[string]int {
	v := f.chosen
	f.chosen = nil
	return v
}

// Pin gives a replay the versions the entry replayed took.
func (f *Flows) Pin(versions map[string]int) { f.pins = versions }

func (f *Flows) Check() error {
	c := f.host.Automation(platform.Caller{Replaying: true}, ID)
	return f.eachRunning(c, f.checkBinding)
}

func (f *Flows) checkBinding(x FlowInstance) error {
	_, err := f.binding(x)
	return err
}

func (f *Flows) binding(x FlowInstance) (host.FlowBinding, error) {
	d := f.def(x.Flow, x.Version)
	if d == nil {
		return host.FlowBinding{}, fmt.Errorf("flow instance %s runs %s version %d, which the code no longer declares", x.ID, x.Flow, x.Version)
	}
	bound, err := f.host.BindFlow(d.app, d.Name, d.Version, host.FlowBinding{Dependencies: x.Dependencies, Release: x.Release})
	if err != nil {
		return bound, fmt.Errorf("flow instance %s: %w", x.ID, err)
	}
	if bound.Dependencies != x.Dependencies || bound.Release != x.Release {
		return bound, fmt.Errorf("flow instance %s has no exact starting release binding", x.ID)
	}
	return bound, nil
}

func (f *Flows) eachRunning(c platform.Caller, visit func(FlowInstance) error) error {
	live, _ := json.Marshal([]any{"|", []any{"state", "=", "running"}, "|", []any{"state", "=", "waiting"}, "|", []any{"state", "=", "compensating"}, []any{"state", "=", "stuck"}})
	for offset := 0; ; {
		rows, total, err := platform.Find[FlowInstance](c, platform.Query{Domain: live, Sort: []string{"id"}, Limit: 500, Offset: offset})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := visit(row); err != nil {
				return err
			}
		}
		offset += len(rows)
		if offset >= total {
			return nil
		}
		if len(rows) == 0 {
			return fmt.Errorf("cannot read all running flow instances")
		}
	}
}

func (f *Flows) running(c platform.Caller) ([]FlowInstance, *kernel.Error) {
	var out []FlowInstance
	if err := f.eachRunning(c, func(x FlowInstance) error { out = append(out, x); return nil }); err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: err.Error()}
	}
	return out, nil
}

// HasRunningDependency checks every live instance, beyond a single read page.
// Terminal instances retain their history but no longer block source edits.
func (f *Flows) HasRunningDependency(typ string) (bool, error) {
	c := f.host.Automation(platform.Caller{Replaying: true}, ID)
	found := false
	err := f.eachRunning(c, func(row FlowInstance) error {
		binding, err := f.binding(row)
		found = found || strings.HasPrefix(row.Subject, typ+"/") || slices.ContainsFunc(binding.Assets, func(ref platform.AssetRef) bool {
			return ref.Kind == platform.AssetObject && ref.Name == typ
		})
		return err
	})
	return found, err
}

// Interested reports whether an event starts a flow or may end a wait.
func (f *Flows) Interested(names []string, e platform.Event) bool {
	for _, versions := range f.defs {
		if d := versions[len(versions)-1]; slices.ContainsFunc(d.Start.On, func(on string) bool { return slices.Contains(names, on) }) {
			return true
		}
		if d := versions[len(versions)-1]; d.Start.Type != "" && slices.ContainsFunc(e.Changed, func(ref string) bool { return strings.HasPrefix(ref, d.Start.Type+"/") }) {
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
	return s.GetSchema().GetName() == "work.task.complete" && e.App == work.ID
}

// Listen takes an event delivered to the flow app: it starts flows and ends
// waits. It runs as owned work, so a failure is retried and replay repeats it.
func (f *Flows) Listen(c platform.Caller, e platform.Event, names []string, now time.Time) *kernel.Error {
	s := e.Record.GetSubmission()
	for _, id := range slices.Sorted(maps.Keys(f.defs)) {
		latest, _ := f.latest(id)
		if latest.Start.Type != "" { // a record's state (ADR-0028 D8): once per record, when a decision first brings it there
			for _, ref := range e.Changed {
				key, ok := strings.CutPrefix(ref, latest.Start.Type+"/")
				if !ok {
					continue
				}
				if _, started := platform.Get[FlowInstance](c, id+":"+key); started {
					continue
				}
				record, held := f.host.Record(ref)
				if !held || !latest.Start.When(f.host.Automation(c, latest.app), record) {
					continue
				}
				d, err := f.version(c, id)
				if err != nil {
					return err
				}
				if err := f.start(c, d, key, nil, s.GetPrincipalId(), &e, "", now); err != nil {
					return err
				}
			}
			continue
		}
		if latest.Start.Manual {
			continue
		}
		if !slices.ContainsFunc(latest.Start.On, func(on string) bool { return slices.Contains(names, on) }) {
			continue
		}
		key, data, ok := latest.Start.Begin(f.host.Automation(c, latest.app), e)
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
	running, readErr := f.running(c)
	if readErr != nil {
		return readErr
	}
	for _, x := range running {
		d := f.def(x.Flow, x.Version)
		if d == nil {
			continue
		}
		run := f.run(&x)
		run.Now = now
		for i := range x.Tokens {
			tok := &x.Tokens[i]
			run.Outputs = maps.Clone(tok.Outputs)
			run.Frames = slices.Clone(tok.Frames)
			step := d.steps[tok.Step]
			app := f.host.Automation(c, d.app)
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
				task, _ := platform.Get[work.WorkTask](f.host.Automation(c, work.ID), tok.Task)
				answer := cmp.Or(task.Answer, "done")
				if err := f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					in.Answer = answer
					ss.trace(in, tok.Step, "answered", answer, task.Assignee)
					if step != nil && step.Ask != nil && len(step.Ask.Answers) > 0 {
						ss.review(in, step, map[bool]string{true: "accepted", false: "corrected"}[answer == step.Ask.Answers[0]], task.Assignee, answer)
					}
					ss.next(in, tok.ID, "")
				}); err != nil {
					return err
				}
			case tok.Waits == "ask" && step != nil && step.Ask != nil && step.Ask.On != "" && slices.Contains(names, step.Ask.On) && step.Ask.Match(app, run, e):
				if err := f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					ss.close = append(ss.close, tok.Task)
					ss.event, in.Answer = &e, "event"
					ss.review(in, step, "bypassed", s.GetPrincipalId(), s.GetSchema().GetName()+" "+target(s))
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

// Run takes what is due: retries, timeouts, times and conditions.
func (f *Flows) Run(c platform.Caller, _ string, now time.Time) *kernel.Error {
	running, readErr := f.running(c)
	if readErr != nil {
		return readErr
	}
	for _, x := range running {
		d := f.def(x.Flow, x.Version)
		if d == nil {
			continue
		}
		run := f.run(&x)
		run.Now = now
		for _, tok := range x.Tokens {
			run.Outputs = maps.Clone(tok.Outputs)
			run.Frames = slices.Clone(tok.Frames)
			step := d.steps[tok.Step]
			due := !tok.Due.IsZero() && !tok.Due.After(now)
			holds := tok.Waits == "wait" && step != nil && step.Wait != nil && step.Wait.Until != nil && step.Wait.Until(f.host.Automation(c, d.app), run)
			if tok.Waits == "operation" || tok.Waits == "invocation" || tok.Waits == "approval" {
				if err := f.resumeInvocation(c, x.ID, tok.ID, now); err != nil {
					return err
				}
				continue
			}
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
			case tok.Waits == "retry" || tok.Waits == "undo" || tok.Waits == "yield":
				err = f.step(c, x.ID, now, func(ss *session, in *FlowInstance) { ss.token(in, tok.ID).Waits = "ready" })
			case tok.Waits == "wait" && step != nil && step.Wait != nil && step.Wait.At != nil && due:
				err = f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					ss.trace(in, tok.Step, "time", "reached", "")
					ss.next(in, tok.ID, "")
				})
			case tok.Waits == "wait" || tok.Waits == "ask" || tok.Waits == "call" || tok.Waits == "agent":
				err = f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					if tok.Task != "" {
						ss.close = append(ss.close, tok.Task)
					}
					ss.trace(in, tok.Step, "timeout", map[bool]string{true: fmt.Sprintf("%d working days", step.WorkingDays), false: step.Timeout.String()}[step.WorkingDays > 0], "")
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
	return &platform.Run{ID: x.ID, Flow: x.Flow, Version: x.Version, Key: x.Key, OnBehalf: x.OnBehalf, Release: x.Release, Sequence: x.Seq, Sources: slices.Clone(x.Sources), Outputs: maps.Clone(x.Outputs), Data: json.RawMessage(cmp.Or(x.Data, "null")), Answer: x.Answer}
}

// Read "flows": the declared flows, for the workspace to draw.
// FlowDefinition is a declared flow as people see it: its steps and where each may go.
type FlowDefinition struct {
	ID      string     `json:"id"`
	App     string     `json:"app"`
	Title   string     `json:"title"`
	Version int        `json:"version"`
	Start   []string   `json:"start"`
	Steps   []FlowStep `json:"steps"`
}

type FlowStep struct {
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Kind  string   `json:"kind" enum:"action,wait,ask,subflow,fork,agent,transform,branch,foreach,compute,ai,end,break,continue"`
	Next  []string `json:"next"`
	// Chooses: the step's code picks the next one as the instance runs.
	Chooses bool `json:"chooses,omitempty"`
}

func (f *Flows) Read(c platform.Caller, _ string) (any, *kernel.Error) {
	out := []FlowDefinition{}
	for _, id := range slices.Sorted(maps.Keys(f.defs)) {
		for _, d := range f.defs[id] {
			v := FlowDefinition{ID: id, App: d.app, Title: d.Title, Version: d.Version, Start: append([]string{}, d.Start.On...), Steps: []FlowStep{}}
			if d.Start.Type != "" { // started by a record's state, not an event (ADR-0028 D8)
				v.Start = append(v.Start, "state of "+d.Start.Type)
			}
			for _, s := range d.Steps {
				sv := FlowStep{Name: s.Name, Title: cmp.Or(s.Title, s.Name), Kind: kindOf(s), Next: []string{}, Chooses: s.Choose != nil}
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
		return "action"
	case s.Evaluate != nil:
		return "transform"
	case s.Branch != nil:
		return "branch"
	case s.Loop != nil:
		return "foreach"
	case s.Operation != nil:
		return "compute"
	case s.Invoke != nil:
		return "ai"
	case s.LoopControl != "":
		return s.LoopControl
	case s.End:
		return "end"
	case s.Wait != nil:
		return "wait"
	case s.Ask != nil:
		return "ask"
	case s.Call != nil:
		return "subflow"
	case len(s.All) > 0:
		return "fork"
	case len(s.Any) > 0:
		return "fork"
	}
	return "agent"
}

func boolCount(values ...bool) int {
	n := 0
	for _, v := range values {
		if v {
			n++
		}
	}
	return n
}

// OperationEnded continues the same flow token in the host's accepted effect
// settlement. The host provides the staged, already validated result here.
func (f *Flows) OperationEnded(c platform.Caller, call string, now time.Time) *kernel.Error {
	c = f.host.Automation(c, ID)
	running, err := f.running(c)
	if err != nil {
		return err
	}
	for _, x := range running {
		for _, tok := range x.Tokens {
			if tok.Waits == "operation" && tok.Operation == call {
				if err := f.resumeInvocation(c, x.ID, tok.ID, now); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (f *Flows) resumeInvocation(c platform.Caller, id string, token int, now time.Time) *kernel.Error {
	c = f.host.Automation(c, ID)
	x, ok := platform.Get[FlowInstance](c, id)
	if !ok || ended(x.State) {
		return nil
	}
	tok := f.session(c, now).token(&x, token)
	if tok.ID < 0 {
		return nil
	}
	step := f.def(x.Flow, x.Version).steps[tok.Step]
	var output json.RawMessage
	var failure string
	var sources []string
	if tok.Waits == "operation" {
		result, err := c.OperationResult(tok.Operation)
		if err != nil {
			failure = cmp.Or(err.Message, "Operation result is unavailable")
		} else if result.State == "pending" || result.State == "running" {
			return nil
		} else if result.State != "completed" {
			failure = cmp.Or(result.Error, "Operation "+result.State)
		} else {
			output = result.Output
		}
	} else if tok.Waits == "approval" {
		approval, ok := platform.Get[work.ApprovalRequest](f.host.Automation(c, work.ID), tok.Child)
		if !ok || approval.State == "pending" {
			return nil
		}
		if approval.State != "approved" {
			failure = cmp.Or(approval.Outcome, "Approval "+approval.State)
		} else {
			output = platform.Raw(map[string]string{"state": approval.State, "target": approval.Target, "receipt": approval.ID})
		}
	} else if tok.Waits == "invocation" && step != nil && step.Invoke != nil {
		run := f.run(&x)
		run.Now = now
		run.Outputs = maps.Clone(tok.Outputs)
		run.Frames = slices.Clone(tok.Frames)
		var done bool
		var err *kernel.Error
		output, done, err = step.Invoke.Result(f.host.Automation(c, f.def(x.Flow, x.Version).app), run, tok.Child)
		sources = run.Sources
		if !done && err == nil {
			return nil
		}
		if err != nil {
			failure = err.Error()
		}
	} else {
		return nil
	}
	return f.step(c, id, now, func(ss *session, in *FlowInstance) {
		if failure != "" {
			// An accepted refusal is terminal for this call; retrying the block with
			// the same identity must not masquerade as a new model/compute request.
			ss.token(in, token).Attempts = stepAttempts
			ss.failed(in, token, step, failure)
			return
		}
		if !ss.output(in, token, output) {
			ss.outputRefused(in, token)
			return
		}
		ss.sources(in, sources)
		ss.trace(in, tok.Step, "answered", "saved typed output", "")
		ss.next(in, token, "")
	})
}

// StartManual is the one existing flow start decision, called from an
// authorised owner action. It cannot create an instance of a non-manual flow.
func (f *Flows) StartManual(c platform.Caller, app, name string, version int, key string, input json.RawMessage, now time.Time) *kernel.Error {
	d := f.def(app+"."+name, version)
	if d == nil || !d.Start.Manual || key == "" || len(key) > 256 || len(input) > 64<<10 || !json.Valid(input) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an installed manual flow, bounded key and JSON input")
	}
	return f.start(f.host.Automation(c, ID), d, key, input, c.ID, nil, "", now, true)
}

// HasDefinition is a registry lookup for snapshot reconstruction. A sandbox
// already seeded with the same immutable version must not install it twice.
func (f *Flows) HasDefinition(app, name string, version int) bool {
	return f.def(app+"."+name, version) != nil
}
