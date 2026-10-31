package automation

import (
	"cmp"
	"fmt"
	"maps"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func (f *Flows) Validate(a platform.App, fl platform.Flow) error {
	_, err := f.check(a.Manifest(), fl)
	return err
}

func (f *Flows) InstallationDraft() platform.Processes {
	draft := &Flows{defs: maps.Clone(f.defs)}
	for id, versions := range draft.defs {
		draft.defs[id] = slices.Clone(versions)
	}
	return draft
}

func (f *Flows) check(m platform.Manifest, fl platform.Flow) (*flowDef, error) {
	id := m.ID + "." + fl.Name
	d := &flowDef{app: m.ID, Flow: fl, steps: map[string]*platform.Step{}}
	byEvent, byState, byClock, byContinuous := len(fl.Start.On) > 0 && fl.Start.Begin != nil, fl.Start.Type != "" && fl.Start.When != nil, fl.Start.Every > 0, fl.Start.Continuous
	if fl.Name == "" || fl.Title == "" || fl.Version < 1 || len(fl.Steps) == 0 || boolCount(byEvent, byState, fl.Start.Manual, byClock, byContinuous) != 1 {
		return nil, fmt.Errorf("flow %s: name, title, version, steps and one start — on events, on a record state, by hand, on a schedule or from a continuous source — are required", id)
	}
	if byContinuous && (fl.Continuous == nil || fl.Continuous.Intake == nil || fl.Start.OnBehalf == "" || fl.Start.Enabled == nil) {
		return nil, fmt.Errorf("flow %s: an automatic continuous start needs a controlled intake, publisher and active-owner check", id)
	}
	if !byContinuous && fl.Start.Enabled != nil {
		return nil, fmt.Errorf("flow %s: an active-owner check is only valid for a continuous start", id)
	}
	if byClock && fl.Start.Every < time.Minute {
		return nil, fmt.Errorf("flow %s: a schedule repeats at least every minute", id)
	}
	if byState && !slices.ContainsFunc(m.Entities, func(e platform.Entity) bool { return e.Type == fl.Start.Type }) && (m.ID != "build" || f.host == nil || !f.host.Declares(fl.Start.Type)) {
		return nil, fmt.Errorf("flow %s starts on the state of %s, not an entity type of %s", id, fl.Start.Type, m.ID)
	}
	if fl.Continuous != nil {
		if fl.Continuous.ErrorPolicy != "" && fl.Continuous.ErrorPolicy != "stop" && fl.Continuous.ErrorPolicy != "deadLetter" {
			return nil, fmt.Errorf("flow %s: a continuous error policy is stop or deadLetter", id)
		}
		if err := checkOperators(*fl.Continuous); err != nil {
			return nil, fmt.Errorf("flow %s: %w", id, err)
		}
		if fl.Continuous.Source == "" || fl.Continuous.Batch < 0 || fl.Continuous.State < 0 || fl.Continuous.FrameBytes < 0 || fl.Continuous.CheckpointEvery < 0 || fl.Continuous.CheckpointEvery > 1_000_000 || fl.Continuous.DeadLetterMaxRecords < 0 || fl.Continuous.DeadLetterMaxRecords > 1_000_000 || fl.Continuous.DeadLetterTTLMs < 0 || fl.Continuous.DeadLetterTTLMs > int((366*24*time.Hour)/time.Millisecond) || !fl.Continuous.DeadLetter {
			return nil, fmt.Errorf("flow %s: a continuous flow names its source, bounded budgets and dead-letter retention", id)
		}
		if fl.Continuous.Window != nil {
			if err := checkWindow(*fl.Continuous.Window); err != nil {
				return nil, fmt.Errorf("flow %s: %w", id, err)
			}
		}
		if intake := fl.Continuous.Intake; intake != nil {
			if intake.SourceRecord == "" || intake.Key == "" || len(intake.Partition) < 1 || len(intake.Partition) > 8 || intake.EventTime == "" || intake.Value == "" || fl.Continuous.Window == nil || fl.Continuous.Batch < 1 || intake.Offset != "" && intake.Offset != "latest" && intake.Offset != "earliest" {
				return nil, fmt.Errorf("flow %s: an intake needs a source record, event columns, window and positive batch budget", id)
			}
			if len(intake.Fields) > 32 {
				return nil, fmt.Errorf("flow %s: intake metadata has at most 32 declared fields", id)
			}
			columns := append(append([]string{intake.Key, intake.EventTime, intake.Value}, intake.Partition...), intake.Fields...)
			for _, column := range columns {
				if len(column) == 0 || len(column) > 128 || strings.IndexFunc(column, func(r rune) bool {
					return r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
				}) >= 0 || column[0] >= '0' && column[0] <= '9' {
					return nil, fmt.Errorf("flow %s: intake columns must be plain identifiers", id)
				}
			}
			partitions := slices.Clone(intake.Partition)
			slices.Sort(partitions)
			if len(slices.Compact(partitions)) != len(intake.Partition) {
				return nil, fmt.Errorf("flow %s: intake partition columns must be unique", id)
			}
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
		if s.Retry != nil && (s.Retry.Attempts < 0 || s.Retry.Attempts > 20 || s.Retry.Backoff < 0 || s.Retry.Backoff > 5*time.Minute) {
			return nil, fmt.Errorf("flow %s: step %s has a retry schedule outside its bounds", id, s.Name)
		}
	}
	if fl.Continuous != nil && fl.Continuous.Entry != "" {
		step := d.steps[fl.Continuous.Entry]
		if (!fl.Start.Manual && !fl.Start.Continuous) || fl.Continuous.Window == nil || fl.Steps[0].Name != fl.Continuous.Entry || step == nil || step.Operation == nil && (fl.Continuous.Aggregate == nil || step.Evaluate == nil && step.Loop == nil) {
			return nil, fmt.Errorf("flow %s: a window Compute entry is the first step of a manually or source-started event-time flow", id)
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
		if act := s.Act; act != nil && (act.Action == "" || (act.Target == nil && act.TargetValue == nil)) {
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

func (f *Flows) Check() error {
	c := f.host.Automation(platform.Caller{Replaying: true}, ID)
	return f.eachRunning(c, f.checkBinding)
}

func (f *Flows) checkBinding(x FlowInstance) error {
	_, err := f.binding(x)
	return err
}

func (f *Flows) binding(x FlowInstance) (platform.FlowBinding, error) {
	d := f.def(x.Flow, x.Version)
	if d == nil {
		return platform.FlowBinding{}, fmt.Errorf("flow instance %s runs %s version %d, which the code no longer declares", x.ID, x.Flow, x.Version)
	}
	bound, err := f.host.BindFlow(d.app, d.Name, d.Version, platform.FlowBinding{Dependencies: x.Dependencies, Release: x.Release, Applications: x.Applications, Existing: true})
	if err != nil {
		return bound, fmt.Errorf("flow instance %s: %w", x.ID, err)
	}
	if bound.Dependencies != x.Dependencies || bound.Release != x.Release {
		return bound, fmt.Errorf("flow instance %s has no exact starting release binding", x.ID)
	}
	return bound, nil
}

func flowDefinitionOf(d *flowDef) FlowDefinition {
	id := d.app + "." + d.Name
	v := FlowDefinition{ID: id, App: d.app, Title: d.Title, Version: d.Version, Start: append([]string{}, d.Start.On...), Steps: []FlowStep{}}
	if d.Start.Type != "" {
		v.Start = append(v.Start, "state of "+d.Start.Type)
	}
	if d.Start.Continuous {
		v.Start = append(v.Start, "continuous source")
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
	return v
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
