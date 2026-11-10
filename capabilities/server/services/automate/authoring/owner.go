package authoring

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	automateowner "platformserver/services/automate"
)

type Environment interface {
	Application() platform.App
	Host() platform.BuildHost
	Ledger() *platform.Ledger
	AppID() string
	Version() string
	Roles() []string
	Objects(platform.Caller) ([]platform.Entity, error)
	InstalledObject(string) (platform.Entity, bool)
	Entity(string) (platform.EntityInfo, bool)
	Query(string, int) (platform.NamedQuery, int, bool)
	ValidateQuery(platform.NamedQuery) error
	CheckIntake(platform.Caller, Process) *kernel.Error
	FunctionCallAction() string
	FunctionResult(platform.Caller, string) (FunctionOutcome, bool)
	JointMode() bool
}

type Owner struct {
	env Environment
}

func New(env Environment) *Owner {
	return &Owner{env: env}
}

func (o *Owner) Fork(env Environment) *Owner {
	return &Owner{env: env}
}

func (o *Owner) ProcessEntity() platform.Entity {
	return platform.Entity{Type: ProcessType, Title: "Process", Plural: "Processes", Model: Process{}, Display: "title", Description: "Typed capability blocks compiled to the platform's native flow.",
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "processes"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "processes", Payload: []platform.Field{}, Do: o.publishProcess},
				{Name: "run", Title: "Run", From: []string{"published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "processes", Payload: []platform.Field{{Name: "subject", Type: "string", Description: "Record to run the automation on"}, {Name: "key", Type: "string", Description: "Stable run key"}, {Name: "input", Type: "string", Description: "Typed JSON input"}}, Do: o.runProcess},
				{Name: "pause", Title: "Pause", From: []string{"published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "processes", Payload: []platform.Field{{Name: "reason", Type: "string"}}, Do: func(_ platform.Caller, record any, raw json.RawMessage, _ time.Time) *kernel.Error {
					var input struct {
						Reason string `json:"reason"`
					}
					_ = json.Unmarshal(raw, &input)
					record.(*Process).PauseReason = input.Reason
					record.(*Process).Paused = true
					return nil
				}},
				{Name: "resume", Title: "Resume", From: []string{"published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "processes", Payload: []platform.Field{}, Do: func(c platform.Caller, record any, _ json.RawMessage, now time.Time) *kernel.Error {
					current := record.(*Process)
					installed, _ := readPublished[Process](current.Published)
					if reason := o.automationOwnerRefusal(c, current.ID, installed, now); reason != nil {
						return reason
					}
					record.(*Process).PauseReason = ""
					record.(*Process).Paused = false
					return nil
				}}}}}
}

func (o *Owner) ProcessActions() []platform.Action {
	return platform.EntityActions(o.ProcessEntity())
}

func (o *Owner) Entities() []platform.Entity {
	return []platform.Entity{o.ProcessEntity()}
}

func (o *Owner) QualifiedName(name string) string {
	return o.env.AppID() + "." + name
}

func (o *Owner) Inventory() ([]Process, error) {
	c := o.env.Host().Automation(platform.Caller{}, o.env.AppID())
	return readInventory[Process](c, "process")
}

func (o *Owner) Check(c platform.Caller, p Process) *kernel.Error {
	models, err := o.env.Objects(c)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The object inventory cannot be checked")
	}
	source := platform.Entity{}
	if installed, ok := o.env.InstalledObject(p.Object); ok {
		source = installed
	}
	for _, model := range models {
		if model.Type == p.Object {
			source = model
		}
	}
	return o.checkFlow(p, source, models, c.Rules())
}

func (o *Owner) CheckDraft(c platform.Caller, record Process, source platform.Entity, models []platform.Entity, member platform.Member, validate bool) (Process, platform.AssetRef, platform.AssetRef, bool, error) {
	prior, hadPrior := readPublished[Process](record.Published)
	priorRef := platform.AssetRef{}
	if hadPrior {
		priorRef = platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetFlow, Name: o.QualifiedName(prior.Name)}
	}
	nextRef := platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetFlow, Name: o.QualifiedName(record.Name)}
	if err := o.checkFlow(record, source, models); err != nil {
		return Process{}, priorRef, nextRef, hadPrior, fmt.Errorf("%s", err.Message)
	}
	if record.Version >= 64 {
		return Process{}, priorRef, nextRef, hadPrior, fmt.Errorf("a process may retain at most 64 published versions")
	}
	record.Version++
	if record.Manual {
		record.Scheduler = ""
	} else {
		record.Scheduler = member.ID
	}
	if validate {
		if o.env.Host().BuildProcesses() == nil {
			return Process{}, priorRef, nextRef, hadPrior, fmt.Errorf("this tenant runs no processes")
		}
		if err := o.validate(record); err != nil {
			return Process{}, priorRef, nextRef, hadPrior, err
		}
	}
	record.State = "published"
	record.Published = processImage(record)
	record.Versions = append(slices.Clone(record.Versions), record.Published)
	return record, priorRef, nextRef, hadPrior, nil
}

func (o *Owner) checkFlow(p Process, entity platform.Entity, objects []platform.Entity, engines ...platform.RuleEngine) *kernel.Error {
	plans, err := o.Inventory()
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The process inventory cannot be checked")
	}
	for _, other := range plans {
		installed, _ := readPublished[Process](other.Published)
		if other.ID != p.ID && (other.Name == p.Name || installed.Name == p.Name) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The process name {name} is already used", p.Name)
		}
	}
	return o.checkFlowOn(p, entity, objects, engines...)
}

func (o *Owner) checkFlowOn(p Process, entity platform.Entity, objects []platform.Entity, engines ...platform.RuleEngine) *kernel.Error {
	lookup := ProcessLookup{
		App:           o.env.AppID(),
		Entity:        o.env.Entity,
		Action:        o.env.Host().Action,
		Function:      o.env.Host().Function,
		Operation:     o.env.Host().Operation,
		Query:         o.env.Query,
		ValidateQuery: o.env.ValidateQuery,
	}
	return CheckProcess(p, entity, lookup, objects, engines...)
}

func (o *Owner) publishProcess(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	p, ok := record.(*Process)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	procs := o.env.Host().BuildProcesses()
	if procs == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This tenant runs no processes")
	}
	if !c.Replaying {
		source, _ := o.env.InstalledObject(p.Object)
		if err := o.checkFlow(*p, source, nil, c.Rules()); err != nil {
			return err
		}
		if err := o.env.CheckIntake(c, *p); err != nil {
			return err
		}
	}
	if p.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A process may retain at most 64 published versions")
	}
	p.Version++
	if p.Manual {
		p.Scheduler = ""
	} else {
		p.Scheduler = c.ID
	}
	fl := o.Flow(*p)
	if c.Staging() {
		if err := procs.Validate(o.env.Application(), fl); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	} else if err := procs.Install(o.env.Application(), fl); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	p.Published = processImage(*p)
	p.Versions = append(p.Versions, p.Published)
	return nil
}

func (o *Owner) runProcess(c platform.Caller, record any, raw json.RawMessage, now time.Time) *kernel.Error {
	p, ok := record.(*Process)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if p.Paused {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The process is paused")
	}
	installed, ok := readPublished[Process](p.Published)
	if !ok || !installed.Manual && installed.Kind != "automation" || installed.Continuous != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Publish an automation or manual workflow before running it")
	}
	var request struct {
		Subject string `json:"subject"`
		Key     string `json:"key"`
		Input   string `json:"input"`
	}
	if json.Unmarshal(raw, &request) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a stable key and JSON input")
	}
	if request.Key == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A manual run needs a stable key")
	}
	if installed.Kind == "automation" {
		if refusal := o.automationOwnerRefusal(c, p.ID, installed, now); refusal != nil {
			return refusal
		}
		member, _ := o.env.Host().Member(installed.Scheduler)
		c = o.env.Host().Caller(c, member, o.env.AppID())
	}
	if installed.Object != "" {
		if request.Subject == "" {
			if installed.Kind == "automation" {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a subject record to run this automation")
			}
			request.Subject = request.Key
		}
		if _, refusal := c.ReadRecord(installed.Object, request.Subject, now); refusal != nil {
			return refusal
		}
	}
	input := installed.Input
	if installed.Every != "" && request.Input == "" {
		input = platform.Raw(map[string]string{"period": "manual:" + request.Key})
	}
	if request.Input != "" {
		input = json.RawMessage(request.Input)
	}
	if len(input) == 0 {
		input = json.RawMessage("{}")
	}
	if installed.InputSchema != nil && request.Input != "" {
		if err := installed.InputSchema.Validate(input, 64<<10); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	start, ok := o.env.Host().BuildProcesses().(interface {
		StartManual(platform.Caller, string, string, int, string, string, json.RawMessage, time.Time) *kernel.Error
	})
	if !ok {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This tenant runs no manual processes")
	}
	return start.StartManual(c, o.env.AppID(), installed.Name, installed.Version, request.Key, request.Subject, input, now)
}

func (o *Owner) Flow(p Process) platform.Flow {
	execution := ProcessExecution{
		App:            o.env.AppID(),
		FunctionAction: o.env.FunctionCallAction(),
		FunctionResult: o.env.FunctionResult,
		StateField:     "state",
		Refused: func(c platform.Caller, refusal *kernel.Error, now time.Time) {
			if p.Kind == "automation" || !p.Manual {
				o.pauseAutomation(c, p.ID, "The automation owner is no longer authorized", now)
			}
		},
		Notify: func(c platform.Caller, r *platform.Run, step string, n automateowner.NotificationEffect, subject json.RawMessage) (json.RawMessage, *kernel.Error) {
			member, ok := o.env.Host().Member(r.OnBehalf)
			if !ok {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
			}
			info, _ := o.env.Entity(p.Object)
			return automateowner.Notify(o.env.Host().Caller(c, member, o.env.AppID()), r, step, n, p.Object, info.Display, subject, r.Now), nil
		},
		Paused: func(c platform.Caller) bool {
			current, ok := platform.Get[Process](o.env.Host().Automation(c, o.env.AppID()), p.ID)
			return ok && current.Paused
		},
		ContinuousEnabled: func(c platform.Caller) bool {
			if p.ID == "" || p.Version < 1 {
				return false
			}
			current, ok := platform.Get[Process](o.env.Host().Automation(c, o.env.AppID()), p.ID)
			return ok && current.State == "published" && current.Version == p.Version && current.Name == p.Name
		},
		Subject: func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
			member, ok := o.env.Host().Member(r.OnBehalf)
			if !ok {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
			}
			raw, err := o.env.Host().Caller(c, member, o.env.AppID()).ReadRecord(p.Object, processSubject(p.Object, r), r.Now)
			if err == nil {
				var fields map[string]json.RawMessage
				info, known := o.env.Entity(p.Object)
				if known && json.Unmarshal(raw, &fields) == nil {
					for name := range fields {
						if _, declared := info.Field(name); declared {
							r.Sources = append(r.Sources, p.Object+"/"+processSubject(p.Object, r)+"#"+name)
						}
					}
				}
			}
			return raw, err
		},
		SubjectPath: func(c platform.Caller, r *platform.Run, path []string) (json.RawMessage, []string, *kernel.Error) {
			member, known := o.env.Host().Member(r.OnBehalf)
			if !known {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
			}
			return o.env.Host().Caller(c, member, o.env.AppID()).ReadRecordPath(p.Object, processSubject(p.Object, r), path, r.Now)
		},
		Query: func(c platform.Caller, r *platform.Run, app, name string, version int, input json.RawMessage) (json.RawMessage, *kernel.Error) {
			member, ok := o.env.Host().Member(r.OnBehalf)
			if !ok {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
			}
			return o.env.Host().Caller(c, member, app).ReadQueryVersion(app, name, version, input, r.Now)
		},
	}
	if p.Every == "" && !(p.Continuous != nil && p.Continuous.Intake != nil) && !p.Manual {
		if info, ok := o.env.Entity(p.Object); ok && info.Lifecycle != nil {
			execution.StateField = info.Lifecycle.Field
		}
	}
	return CompileProcess(p, execution)
}

func (o *Owner) validate(p Process) error {
	procs := o.env.Host().BuildProcesses()
	if procs == nil {
		return fmt.Errorf("this tenant runs no processes")
	}
	return procs.Validate(o.env.Application(), o.Flow(p))
}

func (o *Owner) Install(c platform.Caller, p Process) error {
	procs := o.env.Host().BuildProcesses()
	if procs == nil {
		return fmt.Errorf("this tenant runs no processes")
	}
	if err := procs.Install(o.env.Application(), o.Flow(p)); err != nil {
		return err
	}
	return nil
}

func (o *Owner) Restore() error {
	procs := o.env.Host().BuildProcesses()
	list, err := o.Inventory()
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
		if _, err := ReadProcessImage(image); err != nil {
			return err
		}
		for _, raw := range p.Versions {
			was, ok := readPublished[Process](raw)
			if !ok {
				return fmt.Errorf("process %s: unreadable version", p.Name)
			}
			if err := procs.Install(o.env.Application(), o.Flow(was)); err != nil {
				return fmt.Errorf("process %s: %v", p.Name, err)
			}
		}
	}
	return nil
}

func (o *Owner) CheckObjectDefinition(c platform.Caller, entity, previous platform.Entity, changed, compatible bool) error {
	procs := o.env.Host().BuildProcesses()
	if procs != nil && changed && previous.Type != "" {
		running, err := procs.HasUnboundRunningDependency(previous.Type)
		if err != nil {
			return err
		}
		if running && !compatible {
			return fmt.Errorf("finish the running processes before changing their object definition")
		}
	}
	if o.env.JointMode() {
		return nil
	}
	processes, err := o.Inventory()
	if err != nil {
		return err
	}
	for _, process := range processes {
		if process.Published == "" {
			continue
		}
		raw, _ := json.Marshal(process)
		installed, err := ReadProcessImage(raw)
		if err != nil {
			return err
		}
		if installed.Object != entity.Type && installed.Object != previous.Type {
			continue
		}
		if problem := o.checkFlowOn(installed, entity, nil); problem != nil {
			return fmt.Errorf("%s", problem.Message)
		}
	}
	return nil
}

func processImage(p Process) string {
	p.Paused = false
	p.PauseReason = ""
	p.Published = ""
	p.Versions = nil
	return string(platform.Raw(p))
}

func readPublished[T any](raw string) (T, bool) {
	var out T
	if raw == "" || json.Unmarshal([]byte(raw), &out) != nil {
		return out, false
	}
	return out, true
}

func readInventory[T any](c platform.Caller, label string) ([]T, error) {
	var all []T
	for {
		rows, count, err := platform.Find[T](c, platform.Query{Limit: 500, Offset: len(all), Sort: []string{"id"}})
		if err != nil {
			return nil, err
		}
		if count > 1000 {
			return nil, fmt.Errorf("builder release inventory exceeds 1000 definitions of one kind")
		}
		all = append(all, rows...)
		if len(all) >= count {
			return all, nil
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("builder definition inventory is incomplete")
		}
	}
}
