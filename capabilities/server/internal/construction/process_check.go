package construction

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

// ProcessLookup resolves declarations used by authoring checks. It has no
// submission, installation, tenant state, network or scheduling authority.
type ProcessLookup struct {
	Entity        func(string) (platform.EntityInfo, bool)
	Action        func(string) (string, platform.Action, bool)
	Function      func(string, string, int) (platform.AIFunction, int, bool)
	Operation     func(string, string, int) (platform.Operation, int, bool)
	Query         func(string, int) (platform.NamedQuery, int, bool)
	ValidateQuery func(platform.NamedQuery) error
}

func CheckProcess(p Process, entity platform.Entity, lookup ProcessLookup, objects ...platform.Entity) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	actionModels := map[string]platform.Entity{}
	for _, model := range append(objects, entity) {
		if model.Type != "" {
			actionModels[model.Type] = model
		}
	}
	if !Named(p.Name) {
		return refuse("The process name {name} must be lower-case letters and digits", p.Name)
	}
	if old, ok := ReadPublication[Process](p.Published); ok && (old.Name != p.Name || old.Object != p.Object) {
		return refuse("A published process keeps its name and object")
	}
	if p.Kind != "" && p.Kind != "flow" && p.Kind != "automation" {
		return refuse("A process is authored as a flow or an automation")
	}
	if p.Kind == "automation" {
		if err := CheckAutomation(p); err != nil {
			return err
		}
	}
	if p.Every != "" {
		every, parseErr := time.ParseDuration(p.Every)
		if parseErr != nil || every < time.Minute || every > 366*24*time.Hour {
			return refuse("A schedule repeats every period between 1m and a year, such as 15m, 1h or 24h")
		}
		if p.Manual || p.Object != "" || p.When != "" {
			return refuse("A scheduled process has no source record and is not started by hand")
		}
	}
	continuousStart := p.Continuous != nil && p.Continuous.Intake != nil
	if continuousStart && (p.Manual || p.Every != "" || p.Object != "" || p.When != "") {
		return refuse("A controlled continuous Process starts from its published source, not by hand, on a schedule or from a record state")
	}
	if !p.Manual && p.Every == "" && !continuousStart {
		var states []platform.State
		if entity.Type == p.Object && entity.Lifecycle != nil {
			states = entity.Lifecycle.States
		} else if info, ok := lookup.Entity(p.Object); ok && info.Lifecycle != nil {
			for _, state := range info.Lifecycle.States {
				states = append(states, platform.State{Name: state.Name})
			}
		}
		if !slices.ContainsFunc(states, func(s platform.State) bool { return s.Name == p.When }) {
			return refuse("The object has no process start state {state}", p.When)
		}
	}
	if p.InputSchema != nil {
		if err := p.InputSchema.Check(); err != nil {
			return refuse("Workflow input: " + err.Error())
		}
	}
	if len(p.Input) > 0 {
		if _, err := platform.DecodeValue(p.Input, 64<<10); err != nil {
			return refuse("Workflow input: " + err.Error())
		}
		if p.InputSchema != nil {
			if err := p.InputSchema.Validate(p.Input, 64<<10); err != nil {
				return refuse("Workflow input: " + err.Error())
			}
		}
	}
	if len(p.Lanes) > 32 {
		return refuse("A process is drawn across at most 32 lanes")
	}
	lanes := map[string]bool{}
	for _, lane := range p.Lanes {
		if !Named(lane.Name) || lanes[lane.Name] {
			return refuse("The lane {lane} needs a unique lower-case name", lane.Name)
		}
		lanes[lane.Name] = true
	}
	if len(p.Steps) < 1 || len(p.Steps) > 128 {
		return refuse("A process needs 1–128 typed steps")
	}
	nodes := map[string]ProcessStep{}
	for _, step := range p.Steps {
		if !Named(step.Name) || nodes[step.Name].Name != "" {
			return refuse("The process step {step} needs a unique lower-case name", step.Name)
		}
		nodes[step.Name] = step
	}
	parents := map[string]string{}
	for _, step := range p.Steps {
		problem := func(message string) *kernel.Error { return refuse("Node " + step.Name + ": " + message) }
		if !slices.Contains([]string{"payload", "query", "action", "transform", "branch", "switch", "foreach", "while", "fork", "join", "ask", "wait", "subflow", "ai", "compute", "end", "fail", "break", "continue"}, step.Kind) {
			return problem("choose one typed block kind")
		}
		if step.Lane != "" && !lanes[step.Lane] {
			return refuse("Node "+step.Name+": names the lane {lane}, which this process does not declare", step.Lane)
		}
		if len(step.Inputs) > 64 {
			return problem("a block accepts at most 64 input bindings")
		}
		if step.Ask != "" && step.Kind != "ask" || step.Act != "" && step.Kind != "action" || step.Function != nil && step.Kind != "ai" || step.Operation != nil && step.Kind != "compute" || (step.Query != "" || step.QueryVersion != 0) && step.Kind != "query" || len(step.Answers) > 0 && step.Kind != "ask" || len(step.Cases) > 0 && step.Kind != "ask" && step.Kind != "branch" && step.Kind != "switch" || len(step.Branches) > 0 && step.Kind != "fork" || step.Body != "" && step.Kind != "foreach" && step.Kind != "while" {
			return problem("configuration must match this block kind")
		}
		if step.TimeoutSeconds < 0 || step.TimeoutSeconds > 30*86400 || step.UntilSeconds < 0 || step.UntilSeconds > 30*86400 {
			return problem("wait/timeout exceeds its bound")
		}
		if step.RetryAttempts < 0 || step.RetryAttempts > 20 || step.RetryBackoffMs < 0 || step.RetryBackoffMs > int((5*time.Minute)/time.Millisecond) {
			return problem("retry attempts/backoff exceed their bounds")
		}
		if step.TimeoutSeconds > 0 && step.Error == "" {
			return problem("a timeout needs an error path")
		}
		bindings := maps.Clone(step.Inputs)
		if bindings == nil {
			bindings = map[string]platform.Binding{}
		}
		for name, binding := range map[string]*platform.Binding{"value": step.Value, "target": step.Target, "collection": step.Collection} {
			if binding != nil {
				bindings[name] = *binding
			}
		}
		for name, binding := range bindings {
			if err := binding.Check(); err != nil {
				return problem(name + ": " + err.Error())
			}
			if binding.Source == "step" && nodes[binding.Step].Name == "" {
				return problem(name + ": upstream node is missing")
			}
		}
		if step.Condition != nil {
			if err := step.Condition.Check(); err != nil {
				return problem(err.Error())
			}
		}
		switch step.Kind {
		case "ask":
			if step.Ask == "" {
				return problem("choose a recipient role")
			}
			scope := entity.Scope
			if entity.Type != p.Object {
				if info, ok := lookup.Entity(p.Object); ok {
					scope = info.Scope
				}
			}
			readable := step.Ask == Builder || p.Object == "" || len(scope.Levels) == 0 && step.Ask == User || scope.Levels[step.Ask] == platform.ScopeTenant
			if !readable {
				return problem("recipient role must read all records of its source")
			}
			seen := map[string]bool{}
			for _, answer := range step.Answers {
				if strings.TrimSpace(answer) == "" || seen[answer] {
					return problem("answers must be unique and nonempty")
				}
				seen[answer] = true
			}
			for answer := range step.Cases {
				if !seen[answer] {
					return problem("answer path does not name an allowed answer")
				}
			}
		case "action":
			schema := step.Act
			if !strings.Contains(schema, ".") {
				schema = p.Object + "." + schema
			}
			owner, action, ok := lookup.Action(schema)
			if model, selected := actionModels[schema[:strings.LastIndex(schema, ".")]]; selected {
				ok = false
				for _, candidate := range platform.EntityActions(model) {
					if candidate.Schema == schema {
						action = candidate
						owner = ID
						ok = true
						break
					}
				}
			}
			if step.Protocol == "" {
				if !ok || action.Automation {
					return problem("action " + schema + " is not declared as an exposed native action")
				}
				_ = owner
				for _, field := range action.Payload {
					if field.Required {
						if _, ok := step.Inputs[field.Name]; !ok {
							return problem("required action input " + field.Name + " is missing")
						}
					}
				}
			}
			if step.Act == "" {
				return problem("choose an action")
			}
			if p.Object == "" && step.Target == nil {
				return problem("choose an explicit target")
			}
		case "ai":
			if step.Function == nil {
				return problem("choose a retained AI function")
			}
			f, _, ok := lookup.Function(cmp.Or(step.Function.App, ID), step.Function.Name, step.Function.Version)
			if !ok || cmp.Or(step.Function.App, ID) == ID && step.Function.Version < 1 || cmp.Or(step.Function.App, ID) != ID && step.Function.Version != 0 || f.Object != p.Object && step.Target == nil {
				return problem("AI function source/version is not installed")
			}
		case "compute":
			if step.Operation == nil || step.Operation.Name == "" || step.Operation.Version < 0 {
				return problem("choose a retained computation")
			}
			op, _, ok := lookup.Operation(cmp.Or(step.Operation.App, ID), step.Operation.Name, step.Operation.Version)
			if !ok {
				return problem("computation owner/version is not installed")
			}
			if step.Value != nil {
				if len(step.Inputs) > 0 {
					return problem("choose a whole input value or named field bindings")
				}
				if step.Value.Source == "literal" {
					if err := op.Input.Validate(step.Value.Value, 64<<10); err != nil {
						return problem("input: " + err.Error())
					}
				}
				break
			}
			for _, field := range op.Input.Required {
				if _, ok := step.Inputs[field]; !ok {
					return problem("required compute input " + field + " is missing")
				}
			}
			for name, binding := range step.Inputs {
				schema, ok := op.Input.Properties[name]
				if !ok {
					return problem("unknown compute input " + name)
				}
				if binding.Source == "literal" {
					if err := schema.Validate(binding.Value, 64<<10); err != nil {
						return problem("input " + name + ": " + err.Error())
					}
				}
			}
		case "query":
			if step.Query == "" || step.App == "" {
				return problem("choose an owner query")
			}
			if step.App == ID {
				q, ordinal, ok := lookup.Query(step.Query, step.QueryVersion)
				if !ok || step.QueryVersion < 1 || step.QueryVersion > 64 || ordinal != step.QueryVersion {
					return problem("choose an exact retained tenant query version")
				}
				if err := lookup.ValidateQuery(q); err != nil {
					return problem(err.Error())
				}
				for name, binding := range step.Inputs {
					if name != "for" || q.By == "" {
						return problem("unknown query input " + name)
					}
					if binding.Source == "literal" {
						var value string
						if json.Unmarshal(binding.Value, &value) != nil || value == "" {
							return problem("query for input needs a record ID")
						}
					}
				}
				if q.By != "" {
					if _, ok := step.Inputs["for"]; !ok {
						return problem("query requires its for input")
					}
				}
			} else if step.QueryVersion != 0 {
				return problem("code queries use their owner version")
			}
		case "branch":
			if step.Condition == nil || step.Cases["true"] == "" || step.Cases["false"] == "" {
				return problem("choose a predicate and true/false paths")
			}
		case "switch":
			if step.Value == nil || len(step.Cases) == 0 || step.Next == "" {
				return problem("switch needs a value, cases and default path")
			}
		case "foreach", "while":
			if step.Body == "" || step.MaxIterations < 1 || step.MaxIterations > 10000 || step.Concurrency < 0 || step.Concurrency > 32 {
				return problem("loop needs a body, bounded iterations/concurrency")
			}
			if step.Kind == "foreach" && step.Collection == nil || step.Kind == "while" && (step.Condition == nil || step.Concurrency > 1) {
				return problem("choose loop collection/condition")
			}
			if parents[step.Body] != "" {
				return problem("a body belongs to one loop scope")
			}
			parents[step.Body] = step.Name
		case "fork":
			if len(step.Branches) < 2 || len(step.Branches) > 32 || step.Mode != "" && step.Mode != "all" && step.Mode != "any" {
				return problem("fork needs 2–32 branches and all/any mode")
			}
		case "subflow":
			if step.Flow == "" || step.FlowVersion < 1 {
				return problem("choose a retained subflow")
			}
		case "wait":
			if step.UntilSeconds == 0 && step.Condition == nil {
				return problem("choose a duration or wait predicate")
			}
		case "end", "join", "break", "continue":
			if step.Next != "" || len(step.Cases) > 0 {
				return problem("scope end has no next control edge")
			}
		}
		for _, to := range processPaths(step) {
			if to != "" && nodes[to].Name == "" {
				return problem("control path " + to + " is missing")
			}
		}
	}
	// Cycles are rejected even if they would wait: iteration is an explicit
	// loop scope with its cursor, not a Choose callback jumping backwards.
	seen, active := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if name == "" {
			return nil
		}
		if active[name] {
			return fmt.Errorf("Node %s: arbitrary control cycle; use a loop scope", name)
		}
		if seen[name] {
			return nil
		}
		active[name] = true
		for _, next := range processPaths(nodes[name]) {
			if err := visit(next); err != nil {
				return err
			}
		}
		active[name] = false
		seen[name] = true
		return nil
	}
	if err := visit(p.Steps[0].Name); err != nil {
		return refuse(err.Error())
	}
	for _, step := range p.Steps {
		if !seen[step.Name] {
			return refuse("Node " + step.Name + ": unreachable from the entry")
		}
	}
	if err := CheckProcessScopes(p); err != nil {
		return refuse(err.Error())
	}
	return nil
}
func processPaths(s ProcessStep) []string {
	paths := []string{s.Next, s.Error, s.Body}
	paths = append(paths, s.Branches...)
	paths = append(paths, slices.Collect(maps.Values(s.Cases))...)
	return paths
}

// processImage checks the already saved version family, independently of
// draft rules. Old versions are flat images, not copies of their own history.
func ReadProcessImage(image []byte) (Process, error) {
	var record Process
	if err := json.Unmarshal(image, &record); err != nil {
		return Process{}, err
	}
	if record.State != "published" || record.Version < 1 || record.Version > 64 || len(record.Versions) != record.Version || record.Published != record.Versions[record.Version-1] {
		return Process{}, fmt.Errorf("accepted process has an invalid version family")
	}
	var latest Process
	for index, raw := range record.Versions {
		version, ok := ReadPublication[Process](raw)
		if !ok || version.ID != record.ID || version.Version != index+1 || version.Published != "" || len(version.Versions) != 0 || index > 0 && (version.Name != latest.Name || version.Object != latest.Object) {
			return Process{}, fmt.Errorf("accepted process has a malformed version %d", index+1)
		}
		latest = version
	}
	return latest, nil
}

// checkAutomation holds an automation to its sentence: it starts
// from a record reaching a state, never by hand; at most one condition, first,
// whose false path ends the run; then effects - actions, AI and compute steps -
// one after another, each leading to the next, the last to nothing. Anything
// richer is a flow and is edited on the map.
func CheckAutomation(p Process) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if p.Manual || p.Every == "" && (p.Object == "" || p.When == "") {
		return refuse("An automation starts when a record of an object reaches a state, or on a schedule")
	}
	if len(p.Steps) == 0 {
		return refuse("An automation needs at least one effect")
	}
	ends := map[string]bool{}
	for _, s := range p.Steps {
		if s.Kind == "end" {
			ends[s.Name] = true
		}
	}
	chain := slices.DeleteFunc(slices.Clone(p.Steps), func(s ProcessStep) bool { return s.Kind == "end" })
	for i, s := range chain {
		last := i == len(chain)-1
		next := ""
		if !last {
			next = chain[i+1].Name
		}
		switch s.Kind {
		case "branch":
			if i != 0 {
				return refuse("An automation's condition comes first, before its effects")
			}
			if s.Cases["true"] != next || (s.Cases["false"] != "" && !ends[s.Cases["false"]]) {
				return refuse("An automation's condition leads to its first effect when true and ends the run when false")
			}
		case "action", "ai", "compute":
			if s.Next != next || len(s.Cases) > 0 || len(s.Branches) > 0 || s.Body != "" {
				return refuse("An automation's effects run one after another; step {name} leaves that line", s.Name)
			}
		default:
			return refuse("An automation's effects are actions, AI functions and compute operations; {name} is a {kind} step, which belongs on a flow map", s.Name, s.Kind)
		}
	}
	if len(chain) == 0 || chain[len(chain)-1].Kind == "branch" {
		return refuse("An automation needs at least one effect after its condition")
	}
	return nil
}
