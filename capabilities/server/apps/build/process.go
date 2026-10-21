package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (b *Build) processEntity() platform.Entity {
	return platform.Entity{Type: construction.ProcessType, Title: "Process", Plural: "Processes", Model: construction.Process{}, Display: "title", Description: "Typed capability blocks compiled to the platform's native flow.",
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{construction.Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{construction.Builder}, Capability: "processes"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{construction.Builder}, Capability: "processes", Payload: []platform.Field{}, Do: b.publishProcess},
				{Name: "run", Title: "Run", From: []string{"published"}, To: []string{"published"}, Roles: []string{construction.Builder}, Capability: "processes", Payload: []platform.Field{{Name: "key", Type: "string", Description: "Stable run key"}, {Name: "input", Type: "string", Description: "Typed JSON input"}}, Do: b.runProcess}}}}
}

func (b *Build) publishProcess(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	p, ok := record.(*construction.Process)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	procs := b.host.BuildProcesses()
	if procs == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This tenant runs no processes")
	}
	if !c.Replaying {
		if err := b.checkFlow(*p, b.installed[p.Object]); err != nil {
			return err
		}
		if p.Continuous != nil && p.Continuous.Intake != nil {
			source, ok := platform.Get[construction.Source](c, p.Continuous.Intake.SourceRecord)
			if !ok || !source.Stream || source.State != "published" || source.Name != p.Continuous.Source {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The intake names a readable published continuous source")
			}
			if err := b.checkSource(c, source); err != nil {
				return err
			}
		}
	}
	if p.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A process may retain at most 64 published versions")
	}
	p.Version++
	if p.Every != "" || p.Continuous != nil && p.Continuous.Intake != nil {
		p.Scheduler = c.ID
	} else {
		p.Scheduler = ""
	}
	fl := b.flowOf(*p)
	if c.Staging() {
		if err := procs.Validate(b, fl); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	} else if err := procs.Install(b, fl); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	p.Published = published(*p)
	p.Versions = append(p.Versions, p.Published)
	return nil
}

func (b *Build) runProcess(c platform.Caller, record any, raw json.RawMessage, now time.Time) *kernel.Error {
	p, ok := record.(*construction.Process)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	installed, ok := construction.ReadPublication[construction.Process](p.Published)
	if !ok || !installed.Manual {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Publish a manual workflow before running it")
	}
	var request struct {
		Key   string `json:"key"`
		Input string `json:"input"`
	}
	if json.Unmarshal(raw, &request) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a stable key and JSON input")
	}
	if request.Key == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A manual run needs a stable key")
	}
	input := installed.Input
	if request.Input != "" {
		input = json.RawMessage(request.Input)
	}
	if len(input) == 0 {
		input = json.RawMessage("{}")
	}
	if installed.InputSchema != nil {
		if err := installed.InputSchema.Validate(input, 64<<10); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	start, ok := b.host.BuildProcesses().(interface {
		StartManual(platform.Caller, string, string, int, string, json.RawMessage, time.Time) *kernel.Error
	})
	if !ok {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This tenant runs no manual processes")
	}
	return start.StartManual(c, construction.ID, installed.Name, installed.Version, request.Key, input, now)
}

func (b *Build) checkFlow(p construction.Process, entity platform.Entity, objects ...platform.Entity) *kernel.Error {
	plans, err := b.processInventory()
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The process inventory cannot be checked")
	}
	for _, other := range plans {
		installed, _ := construction.ReadPublication[construction.Process](other.Published)
		if other.ID != p.ID && (other.Name == p.Name || installed.Name == p.Name) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The process name {name} is already used", p.Name)
		}
	}
	return b.checkFlowOn(p, entity, objects...)
}

// CheckProcess exposes the exact publication compiler to edit/test feedback.
// It returns node-qualified errors instead of trusting frontend edge rules.
func (b *Build) CheckProcess(c platform.Caller, p construction.Process) *kernel.Error {
	objects, err := readDefinitionInventory[construction.Object](c)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The object inventory cannot be checked")
	}
	source := b.installed[p.Object]
	models := []platform.Entity{}
	for _, object := range objects {
		if object.Archived {
			continue
		}
		model := Entity(object)
		models = append(models, model)
		if model.Type == p.Object {
			source = model
		}
	}
	return b.checkFlow(p, source, models...)
}

func (b *Build) checkFlowOn(p construction.Process, entity platform.Entity, objects ...platform.Entity) *kernel.Error {
	lookup := construction.ProcessLookup{Entity: b.lookupEntity, Query: b.QueryDefinition}
	if b.host != nil {
		lookup.Action, lookup.Function = b.host.Action, b.host.Function
		lookup.Operation, lookup.ValidateQuery = b.host.Operation, b.host.ValidateInstallQuery
	}
	return construction.CheckProcess(p, entity, lookup, objects...)
}

// installProcesses installs every published version of every process, oldest
// first, after the objects they run on (a restore).
func (b *Build) installProcesses() error {
	procs := b.host.BuildProcesses()
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
		if _, err := construction.ReadProcessImage(image); err != nil {
			return err
		}
		for _, raw := range p.Versions {
			was, ok := construction.ReadPublication[construction.Process](raw)
			if !ok {
				return fmt.Errorf("process %s: unreadable version", p.Name)
			}
			if err := procs.Install(b, b.flowOf(was)); err != nil {
				return fmt.Errorf("process %s: %v", p.Name, err)
			}
		}
	}
	return nil
}

func (b *Build) processInventory() ([]construction.Process, error) {
	return readDefinitionInventory[construction.Process](b.host.Automation(platform.Caller{}, construction.ID))
}
