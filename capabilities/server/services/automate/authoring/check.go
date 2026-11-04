package authoring

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

func CheckProcess(p Process, entity platform.Entity, lookup ProcessLookup, objects []platform.Entity, engines ...platform.RuleEngine) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	actionModels := map[string]platform.Entity{}
	for _, model := range append(objects, entity) {
		if model.Type != "" {
			actionModels[model.Type] = model
		}
	}
	if !ontology.Named(p.Name) {
		return refuse("The process name {name} must be lower-case letters and digits", p.Name)
	}
	if old, ok := readPublished[Process](p.Published); ok && (old.Name != p.Name || old.Object != p.Object) {
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
		if p.Trigger == nil && !slices.ContainsFunc(states, func(s platform.State) bool { return s.Name == p.When }) {
			return refuse("The object has no process start state {state}", p.When)
		}
	}
	if p.Trigger != nil {
		if p.Kind != "automation" || p.Object == "" || p.Every != "" || p.Manual {
			return refuse("A record condition belongs to an object automation")
		}
		if err := platform.CheckCondition(*p.Trigger, engines...); err != nil {
			return refuse(err.Error())
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
		if !ontology.Named(lane.Name) || lanes[lane.Name] {
			return refuse("The lane {lane} needs a unique lower-case name", lane.Name)
		}
		lanes[lane.Name] = true
	}
	if len(p.Steps) < 1 || len(p.Steps) > 128 {
		return refuse("A process needs 1–128 typed steps")
	}
	nodes := map[string]ProcessStep{}
	for _, step := range p.Steps {
		if !ontology.Named(step.Name) || nodes[step.Name].Name != "" {
			return refuse("The process step {step} needs a unique lower-case name", step.Name)
		}
		nodes[step.Name] = step
	}
	parents := map[string]string{}
	for _, step := range p.Steps {
		problem := func(message string) *kernel.Error { return refuse("Node " + step.Name + ": " + message) }
		if !slices.Contains([]string{"payload", "query", "notify", "action", "transform", "branch", "switch", "foreach", "while", "fork", "join", "ask", "wait", "subflow", "ai", "compute", "end", "fail", "break", "continue"}, step.Kind) {
			return problem("choose one typed block kind")
		}
		if step.Lane != "" && !lanes[step.Lane] {
			return refuse("Node "+step.Name+": names the lane {lane}, which this process does not declare", step.Lane)
		}
		if len(step.Inputs) > 64 {
			return problem("a block accepts at most 64 input bindings")
		}
		if step.Notify != nil && step.Kind != "notify" || step.Ask != "" && step.Kind != "ask" || step.Act != "" && step.Kind != "action" || step.Function != nil && step.Kind != "ai" || step.Operation != nil && step.Kind != "compute" || (step.Query != "" || step.QueryVersion != 0) && step.Kind != "query" || len(step.Answers) > 0 && step.Kind != "ask" || len(step.Cases) > 0 && step.Kind != "ask" && step.Kind != "branch" && step.Kind != "switch" || len(step.Branches) > 0 && step.Kind != "fork" || step.Body != "" && step.Kind != "foreach" && step.Kind != "while" {
			return problem("configuration must match this block kind")
		}
		if step.TimeoutSeconds < 0 || step.TimeoutSeconds > 30*86400 || step.UntilSeconds < 0 || step.UntilSeconds > 30*86400 {
			return problem("wait/timeout exceeds its bound")
		}
		if step.WaitUntil != nil && step.Kind != "wait" {
			return problem("a deadline belongs to a wait block")
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
		for name, binding := range map[string]*platform.Binding{"value": step.Value, "target": step.Target, "collection": step.Collection, "waitUntil": step.WaitUntil} {
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
			if err := platform.CheckCondition(*step.Condition, engines...); err != nil {
				return problem(err.Error())
			}
		}
		switch step.Kind {
		case "notify":
			if step.Notify == nil {
				return problem("a notification needs its message and recipient")
			}
			if err := step.Notify.Check(); err != nil {
				return problem(err.Error())
			}
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
						owner = lookup.App
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
			f, _, ok := lookup.Function(cmp.Or(step.Function.App, lookup.App), step.Function.Name, step.Function.Version)
			if !ok || cmp.Or(step.Function.App, lookup.App) == lookup.App && step.Function.Version < 1 || cmp.Or(step.Function.App, lookup.App) != lookup.App && step.Function.Version != 0 || f.Object != p.Object && step.Target == nil {
				return problem("AI function source/version is not installed")
			}
		case "compute":
			if step.Operation == nil || step.Operation.Name == "" || step.Operation.Version < 0 {
				return problem("choose a retained computation")
			}
			op, _, ok := lookup.Operation(cmp.Or(step.Operation.App, lookup.App), step.Operation.Name, step.Operation.Version)
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
			if step.App == lookup.App {
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
			if step.WaitUntil != nil {
				if step.UntilSeconds != 0 || step.Condition != nil {
					return problem("choose one deadline, duration or wait predicate")
				}
				if step.WaitUntil.Source == "literal" {
					var value string
					if json.Unmarshal(step.WaitUntil.Value, &value) != nil {
						return problem("a deadline must be a timestamp")
					}
					if at, err := time.Parse(time.RFC3339Nano, value); err != nil || at.Year() < 1 {
						return problem("a deadline must be a timestamp")
					}
				}
				if step.WaitUntil.Source == "subject" {
					field, err := platform.RecordPathField(p.Object, step.WaitUntil.Path, func(typ string) (platform.EntityInfo, bool) {
						if model, ok := actionModels[typ]; ok {
							info, err := platform.Describe(lookup.App, model, func(reflect.Type) string { return "" })
							return info, err == nil
						}
						return lookup.Entity(typ)
					})
					if err != nil || field.Type != "datetime" {
						return problem("a deadline must read a declared timestamp")
					}
				}
			} else if step.UntilSeconds == 0 && step.Condition == nil {
				return problem("choose a deadline, duration or wait predicate")
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

func CheckAutomation(p Process) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if p.Manual || p.Every == "" && (p.Object == "" || p.When == "" && p.Trigger == nil) {
		return refuse("An automation starts from an object condition or a schedule")
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
		case "action", "notify", "ai", "compute":
			if s.Next != next || len(s.Cases) > 0 || len(s.Branches) > 0 || s.Body != "" {
				return refuse("An automation's effects run one after another; step {name} leaves that line", s.Name)
			}
		default:
			return refuse("An automation's effects are actions, notifications, AI functions and compute operations; {name} is a {kind} step, which belongs on a flow map", s.Name, s.Kind)
		}
	}
	if len(chain) == 0 || chain[len(chain)-1].Kind == "branch" {
		return refuse("An automation needs at least one effect after its condition")
	}
	return nil
}

func CheckProcessScopes(p Process) error {
	nodes := map[string]ProcessStep{}
	for _, node := range p.Steps {
		nodes[node.Name] = node
	}
	scope := map[string]string{}
	visited := map[string]bool{}
	var assign func(string, string) error
	assign = func(name, current string) error {
		if name == "" {
			return nil
		}
		node := nodes[name]
		if prior, ok := scope[name]; ok && prior != current {
			if (node.Kind == "end" || node.Kind == "join") && node.Value == nil && len(node.Inputs) == 0 {
				return nil
			}
			return fmt.Errorf("Node %s: paths cross distinct iteration/parallel scopes; finish that scope before joining", name)
		}
		scope[name] = current
		key := current + ":" + name
		if visited[key] {
			return nil
		}
		visited[key] = true
		if node.Kind == "foreach" || node.Kind == "while" {
			if err := assign(node.Body, current+"/loop:"+name); err != nil {
				return err
			}
		}
		if node.Kind == "fork" {
			for _, branch := range node.Branches {
				if err := assign(branch, current+"/fork:"+name+":"+branch); err != nil {
					return err
				}
			}
		}
		for _, next := range append([]string{node.Next, node.Error}, slices.Collect(maps.Values(node.Cases))...) {
			if err := assign(next, current); err != nil {
				return err
			}
		}
		return nil
	}
	if err := assign(p.Steps[0].Name, ""); err != nil {
		return err
	}
	predecessors := map[string][]string{}
	for _, node := range p.Steps {
		for _, to := range processPaths(node) {
			if to != "" {
				predecessors[to] = append(predecessors[to], node.Name)
			}
		}
	}
	dominators := map[string]map[string]bool{}
	for _, node := range p.Steps {
		set := map[string]bool{}
		if node.Name == p.Steps[0].Name {
			set[node.Name] = true
		} else {
			for name := range nodes {
				set[name] = true
			}
		}
		dominators[node.Name] = set
	}
	for changed := true; changed; {
		changed = false
		for _, node := range p.Steps[1:] {
			var set map[string]bool
			for _, from := range predecessors[node.Name] {
				if set == nil {
					set = maps.Clone(dominators[from])
				} else {
					for name := range set {
						if !dominators[from][name] {
							delete(set, name)
						}
					}
				}
			}
			if set == nil {
				set = map[string]bool{}
			}
			set[node.Name] = true
			if !maps.Equal(set, dominators[node.Name]) {
				dominators[node.Name] = set
				changed = true
			}
		}
	}
	for _, node := range p.Steps {
		if (node.Kind == "break" || node.Kind == "continue") && !strings.Contains(scope[node.Name], "/loop:") {
			return fmt.Errorf("Node %s: loop control requires an enclosing iteration scope", node.Name)
		}
		bindings := slices.Collect(maps.Values(node.Inputs))
		for _, binding := range []*platform.Binding{node.Value, node.Target, node.Collection, node.WaitUntil} {
			if binding != nil {
				bindings = append(bindings, *binding)
			}
		}
		if node.Condition != nil {
			refs, err := platform.ConditionBindings(*node.Condition)
			if err != nil {
				return fmt.Errorf("Node %s: %v", node.Name, err)
			}
			bindings = append(bindings, refs...)
		}
		for _, binding := range bindings {
			if binding.Source == "item" || binding.Source == "index" {
				if !strings.Contains(scope[node.Name], "/loop:") && node.Kind != "while" {
					return fmt.Errorf("Node %s: %s is available only inside a loop scope", node.Name, binding.Source)
				}
			}
			if binding.Source == "step" {
				if binding.Step == node.Name || !dominators[node.Name][binding.Step] {
					return fmt.Errorf("Node %s: output %s is not guaranteed by its upstream control path", node.Name, binding.Step)
				}
				if !strings.HasPrefix(scope[node.Name], scope[binding.Step]) {
					return fmt.Errorf("Node %s: output %s belongs to another scope; bind its loop/fork result", node.Name, binding.Step)
				}
				if strings.Contains(scope[node.Name], "/loop:"+binding.Step) {
					return fmt.Errorf("Node %s: loop %s has not completed inside its own body", node.Name, binding.Step)
				}
			}
		}
	}
	return nil
}
