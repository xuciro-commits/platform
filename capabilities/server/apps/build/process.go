package build

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
	"platformserver/platform"
)

const (
	ProcessType   = "build.process"
	SchemaProcess = ProcessType + ".publish"
)

// Process is a long-running process this organisation defines over one of its
// objects (#132): it starts when a record enters a state, and each step asks
// the people of a role or takes one of the object's actions. It compiles to
// the platform's own flow runtime; each publication is the flow's next
// version, and running instances keep the version they started on.
type Process struct {
	platform.Record
	Name   string        `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"review"`
	Title  string        `json:"title" field:"required,search" title:"What people call it" example:"Visit review"`
	Object string        `json:"object" field:"required" title:"Object it runs on" help:"A published object of this builder" example:"build.visit"`
	When   string        `json:"when" field:"required" title:"Starts when a record enters" help:"A state of the object" example:"review"`
	Steps  []ProcessStep `json:"steps" field:"aside" title:"Steps"`
	State  string        `json:"state" field:"readonly" choices:"draft,published"`
	// Version is the flow version the last publication installed; Versions are
	// every published definition, oldest first, so a restore keeps them all.
	Version   int      `json:"version,omitempty" field:"readonly"`
	Published string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions  []string `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

// ProcessStep asks a role (Ask, with its Answers), takes an action (Act),
// or calls a retained function version through a native action and wait.
// Next is the default path; Branches maps an allowed answer to a named step.
type ProcessStep struct {
	Name     string                `json:"name" help:"Lower-case letters and digits" example:"check"`
	Title    string                `json:"title,omitempty" title:"What people read" example:"Check the visit"`
	Ask      string                `json:"ask,omitempty" title:"Asks the role" help:"A role of the builder app" example:"user"`
	Answers  []string              `json:"answers,omitempty" help:"What the person may answer"`
	Act      string                `json:"act,omitempty" title:"Takes the action" help:"An action of the object, by its name" example:"approve"`
	Function *platform.FunctionRef `json:"function,omitempty" title:"Published AI function"`
	Next     string                `json:"next,omitempty" help:"The step after it; empty: the process ends"`
	Branches map[string]string     `json:"branches,omitempty" title:"Answer branches"`
}

func (b *Build) processEntity() platform.Entity {
	return platform.Entity{Type: ProcessType, Title: "Process", Plural: "Processes", Model: Process{}, Display: "title",
		Description: "A process this organisation runs over one of its objects: it starts when a record enters a state, then asks people and takes actions.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Roles: []string{Builder}, Capability: "processes"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being defined; nothing runs yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "Running: records entering its state start it."}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"},
				Roles: []string{Builder}, Capability: "processes", Payload: []platform.Field{},
				Description: "Install the process as its next version. Instances already running keep the version they started on.",
				Do:          b.publishProcess}}}}
}

// publishProcess checks the process, then installs it as the flow's next
// version; staged, it only validates, and the accepted result installs it.
func (b *Build) publishProcess(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	p, ok := record.(*Process)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	procs := b.host.Processes()
	if procs == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This tenant runs no processes")
	}
	if !c.Replaying {
		if err := b.checkFlow(*p); err != nil {
			return err
		}
	}
	if p.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A process may retain at most 64 published versions")
	}
	p.Version++
	fl := flowOf(*p)
	if c.Staging() {
		if err := procs.Validate(b, fl); err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
	} else if err := procs.Install(b, fl); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	p.Published = published(*p)
	p.Versions = append(p.Versions, p.Published)
	return nil
}

// checkFlow refuses a process that could not run: a name that is not a name,
// an object this builder has not published, a start state it has not, a step
// that is neither one ask nor one action, or a path to no step.
func (b *Build) checkFlow(p Process) *kernel.Error {
	plans, err := b.processInventory()
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The process inventory cannot be checked")
	}
	for _, other := range plans {
		installed, _ := wasPublished[Process](other.Published)
		if other.ID != p.ID && (other.Name == p.Name || installed.Name == p.Name) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The process name {name} is already used", p.Name)
		}
	}
	return b.checkFlowOn(p, b.installed[p.Object])
}

func (b *Build) checkFlowOn(p Process, entity platform.Entity) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if !named(p.Name) {
		return refuse("The process name {name} must be lower-case letters and digits", p.Name)
	}
	if old, ok := wasPublished[Process](p.Published); ok && (old.Name != p.Name || old.Object != p.Object) {
		return refuse("A published process keeps its name and object")
	}
	if entity.Type != p.Object || entity.Lifecycle == nil {
		return refuse("The process needs a published builder object with states")
	}
	if !slices.ContainsFunc(entity.Lifecycle.States, func(s platform.State) bool { return s.Name == p.When }) {
		return refuse("The object has no process start state {state}", p.When)
	}
	if len(p.Steps) < 1 || len(p.Steps) > 64 {
		return refuse("A process needs 1–64 typed steps")
	}
	steps := map[string]bool{}
	for _, step := range p.Steps {
		if !named(step.Name) || steps[step.Name] {
			return refuse("The process step {step} needs a unique lower-case name", step.Name)
		}
		steps[step.Name] = true
	}
	readable := map[string]bool{Builder: true}
	if len(entity.Scope.Levels) == 0 {
		readable[User] = true
	}
	for role, level := range entity.Scope.Levels {
		if level == platform.ScopeTenant {
			readable[role] = true
		}
	}
	functionVersions := map[string]int{}
	for _, step := range p.Steps {
		kinds := 0
		if step.Ask != "" {
			kinds++
		}
		if step.Act != "" {
			kinds++
		}
		if step.Function != nil {
			if version := functionVersions[step.Function.Name]; version != 0 && version != step.Function.Version {
				return refuse("A process must use one retained version of each function")
			}
			functionVersions[step.Function.Name] = step.Function.Version
			kinds++
		}
		if kinds != 1 {
			return refuse("The process step {step} must ask a role, take an action or call a function", step.Name)
		}
		if step.Function != nil {
			f, _, ok := b.FunctionDefinition(step.Function.Name, step.Function.Version)
			if !ok || step.Function.Version < 1 || f.Object != p.Object {
				return refuse("The process function must name a retained version on its source object")
			}
			if len(step.Answers) != 0 || len(step.Branches) != 0 {
				return refuse("Only an ask step may declare answers and branches")
			}
		}
		if step.Ask != "" && !readable[step.Ask] {
			return refuse("The process role {role} must read all records of its object", step.Ask)
		}
		if step.Act != "" {
			action := slices.IndexFunc(entity.Lifecycle.Transitions, func(t platform.Transition) bool { return t.Name == step.Act })
			if action < 0 {
				return refuse("The process action {action} is not declared on its object", step.Act)
			}
			transition := entity.Lifecycle.Transitions[action]
			if transition.Approval != nil || slices.ContainsFunc(transition.Payload, func(f platform.Field) bool { return f.Required }) {
				return refuse("This process action needs unsupported approval or required inputs")
			}
			if len(step.Answers) != 0 || len(step.Branches) != 0 {
				return refuse("Only an ask step may declare answers and branches")
			}
		}
		answers := map[string]bool{}
		for _, answer := range step.Answers {
			if strings.TrimSpace(answer) == "" || answers[answer] {
				return refuse("Process answers must be nonempty and unique")
			}
			answers[answer] = true
		}
		if step.Next != "" && !steps[step.Next] {
			return refuse("The process path {path} does not name a step", step.Next)
		}
		for _, answer := range slices.Sorted(maps.Keys(step.Branches)) {
			to := step.Branches[answer]
			if !answers[answer] || !steps[to] {
				return refuse("The process branch {answer} must name an allowed answer and step", answer)
			}
		}
	}
	return nil
}

// processImage checks the already saved version family, independently of
// draft rules. Old versions are flat images, not copies of their own history.
func processImage(image []byte) (Process, error) {
	var record Process
	if err := json.Unmarshal(image, &record); err != nil {
		return Process{}, err
	}
	if record.State != "published" || record.Version < 1 || record.Version > 64 || len(record.Versions) != record.Version || record.Published != record.Versions[record.Version-1] {
		return Process{}, fmt.Errorf("accepted process has an invalid version family")
	}
	var latest Process
	for index, raw := range record.Versions {
		version, ok := wasPublished[Process](raw)
		if !ok || version.ID != record.ID || version.Version != index+1 || version.Published != "" || len(version.Versions) != 0 || index > 0 && (version.Name != latest.Name || version.Object != latest.Object) {
			return Process{}, fmt.Errorf("accepted process has a malformed version %d", index+1)
		}
		latest = version
	}
	return latest, nil
}

// flowOf is the process as the platform's flow: it starts when a record of
// its object enters When, keyed by the record; an ask is a task for the role,
// an act the object's own action on that record, as the builder app.
func flowOf(p Process) platform.Flow {
	fl := platform.Flow{Name: p.Name, Title: p.Title, Version: p.Version, Subject: p.Object, Owners: []string{Builder},
		Start: platform.Start{Type: p.Object, When: func(_ platform.Caller, record any) bool {
			raw, _ := json.Marshal(record)
			var r struct{ State string }
			_ = json.Unmarshal(raw, &r)
			return r.State == p.When
		}}}
	for _, s := range p.Steps {
		step := platform.Step{Name: s.Name, Title: s.Title, Next: s.Next}
		ref := func(_ platform.Caller, r *platform.Run) string { return p.Object + "/" + r.Key }
		if s.Ask != "" {
			title, role := cmp.Or(s.Title, s.Name), s.Ask
			step.Ask = &platform.Ask{Answers: s.Answers, Ref: ref,
				Title: func(_ platform.Caller, r *platform.Run) string { return title + ": " + r.Key },
				To: func(platform.Caller, *platform.Run) []platform.Recipient {
					return []platform.Recipient{{AppRole: role}}
				}}
			if pick := s.Branches; len(pick) > 0 {
				next := s.Next
				step.Choose = func(_ platform.Caller, r *platform.Run) (string, string) {
					if to, ok := pick[r.Answer]; ok {
						return to, r.Answer
					}
					return next, r.Answer
				}
			}
		} else if s.Function != nil {
			// A function step is two ordinary native steps: accept the request,
			// then wait for its durable reply. The call key includes the native
			// sequence so loops make new calls while retries keep the same one.
			function := *s.Function
			wait := "_function_" + s.Name
			step.Next = wait
			step.Act = &platform.Act{Action: SchemaFunctionCall,
				Target: func(_ platform.Caller, r *platform.Run) string {
					return fmt.Sprintf("%s:%s:%d", r.ID, s.Name, r.Sequence)
				},
				Payload: func(_ platform.Caller, r *platform.Run) any {
					return platform.FunctionRequest{Name: function.Name, Version: function.Version, Source: r.Key, OnBehalf: r.OnBehalf, Release: &r.Release}
				},
				Done: func(_ platform.Caller, r *platform.Run, target *pb.EntityRef) {
					calls := platform.DataOf[map[string]string](r)
					if calls == nil {
						calls = map[string]string{}
					}
					calls[s.Name] = target.GetId()
					r.Set(calls)
				}}
			fl.Steps = append(fl.Steps, step)
			step = platform.Step{Name: wait, Title: cmp.Or(s.Title, s.Name), Next: s.Next, Wait: &platform.Wait{Until: func(c platform.Caller, r *platform.Run) bool {
				call, ok := platform.Get[FunctionRun](c, platform.DataOf[map[string]string](r)[s.Name])
				return ok && (call.State == "ready" || call.State == "rejected")
			}}}
		} else {
			step.Act = &platform.Act{Action: p.Object + "." + s.Act,
				Target: func(_ platform.Caller, r *platform.Run) string { return r.Key }}
		}
		fl.Steps = append(fl.Steps, step)
	}
	return fl
}

// installProcesses installs every published version of every process, oldest
// first, after the objects they run on (a restore, ADR-0034 D4).
func (b *Build) installProcesses() error {
	procs := b.host.Processes()
	list, err := b.processInventory()
	if err != nil {
		return err
	}
	for _, p := range list {
		if len(p.Versions) == 0 {
			continue
		}
		if procs == nil {
			return fmt.Errorf("published process %s needs the flow app", p.ID)
		}
		image, _ := json.Marshal(p)
		if _, err := processImage(image); err != nil {
			return err
		}
		for _, raw := range p.Versions {
			was, ok := wasPublished[Process](raw)
			if !ok {
				return fmt.Errorf("process %s: unreadable version", p.Name)
			}
			if err := procs.Install(b, flowOf(was)); err != nil {
				return fmt.Errorf("process %s: %v", p.Name, err)
			}
		}
	}
	return nil
}

func (b *Build) processInventory() ([]Process, error) {
	return readDefinitionInventory[Process](b.host.Automation(platform.Caller{}, ID))
}
