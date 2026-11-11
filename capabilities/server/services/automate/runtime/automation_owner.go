package automation

import (
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	ID                        = platform.FlowApp
	InstanceType              = platform.FlowInstanceType
	Admin                     = "admin"
	SchemaFlowStart           = "flow.instance.start"
	SchemaFlowStep            = "flow.instance.step"
	SchemaFlowBatch           = "flow.instance.batch"
	SchemaFlowRetry           = "flow.instance.retry"
	SchemaFlowSkip            = "flow.instance.skip"
	SchemaFlowStop            = "flow.instance.cancel"
	SchemaFlowResume          = "flow.instance.resume"
	SchemaFlowRetire          = "flow.instance.retire"
	SchemaFlowReplay          = "flow.dead-letter.replay"
	SchemaFlowMove            = "flow.instance.move"
	SchemaFlowArtifactCleanup = "flow.instance.artifact-cleanup"
	stepAttempts              = 5
	Compensate                = platform.Compensate
)

type FlowInstance = platform.FlowInstance

type Token = platform.FlowToken

type LoopFrame = platform.FlowLoopFrame

type UndoEntry = platform.FlowUndoEntry

type TraceLine = platform.FlowTraceLine

type flowDef struct {
	app string
	platform.Flow
	steps map[string]*platform.Step
}

type Flows struct {
	mu           sync.Mutex
	host         platform.FlowHost
	ledger       *platform.Ledger
	defs         map[string][]*flowDef
	chosen, pins map[string]int
}

var _ platform.ContinuousFlowOwner = (*Flows)(nil)

var _ platform.AttachedFlow = (*Flows)(nil)

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

func (f *Flows) AttachFlow(h platform.FlowHost) { f.host = h }

func New(tenant string) *Flows {
	admin := []string{Admin}
	var actions []platform.Action
	for _, e := range flowEntities() {
		if e.Type == InstanceType {
			actions = append(actions, platform.EntityActions(e)...)
		}
	}
	byHost := "Made by the host as the flow runs."
	actions = append(actions,
		platform.Action{Schema: SchemaFlowStart, Target: InstanceType, Capability: "flows", Title: "Start flow", Description: byHost, Payload: []platform.Field{}, Roles: admin},
		platform.Action{Schema: SchemaFlowStep, Target: InstanceType, Capability: "flows", Title: "Take step", Description: byHost, Payload: []platform.Field{}, Roles: admin},
		platform.Action{Schema: SchemaFlowBatch, Target: InstanceType, Capability: "flows", Title: "Accept source batch", Description: byHost, Automation: true,
			Payload: []platform.Field{{Name: "batch", Type: "text", Required: true}, {Name: "digest", Type: "text", Required: true}}},
		platform.Action{Schema: SchemaFlowRetry, Target: InstanceType, Capability: "flows", Title: "Retry", Payload: []platform.Field{}, Roles: admin,
			Description: "Try a stuck or waiting instance's steps again, with fresh attempts."},
		platform.Action{Schema: SchemaFlowSkip, Target: InstanceType, Capability: "flows", Title: "Skip step", Roles: admin,
			Description: "Leave the step a stuck path is at, as if it had ended, and go on.", Payload: []platform.Field{{Name: "token", Type: "integer", Required: true, Description: "The path"}}},
		platform.Action{Schema: SchemaFlowStop, Target: InstanceType, Capability: "flows", Title: "Cancel", Payload: []platform.Field{}, Roles: admin,
			Description: "Stop a running instance; its open tasks close. Nothing is undone."},
		platform.Action{Schema: SchemaFlowResume, Target: InstanceType, Capability: "flows", Title: "Resume continuous flow", Payload: []platform.Field{}, Roles: admin,
			Description: "Continue a stopped stream from its accepted source checkpoint and retained version."},
		platform.Action{Schema: SchemaFlowRetire, Target: InstanceType, Capability: "flows", Title: "Retire continuous state", Payload: []platform.Field{}, Roles: admin,
			Description: "Release a terminal stream's frame and checkpoint; this permanently ends same-version resumption."},
		platform.Action{Schema: SchemaFlowReplay, Target: "flow.dead-letter", Capability: "flows", Title: "Replay dead letter", Payload: []platform.Field{}, Roles: admin,
			Description: "Retry the original rejected action under current authorization and its original logical key."},
		platform.Action{Schema: SchemaFlowMove, Target: InstanceType, Capability: "flows", Title: "Move to the next version", Payload: []platform.Field{}, Roles: admin,
			Description: "Move a running instance to its flow's next version, as that version's mapping says."},
		platform.Action{Schema: SchemaFlowArtifactCleanup, Target: InstanceType, Capability: "flows", Title: "Retire superseded Flow artifacts", Payload: []platform.Field{{Name: "digest", Type: "text", Required: true}}, Automation: true,
			Description: "Acknowledge host deletion of already-superseded frame and checkpoint artifacts."})
	return &Flows{ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(actions...), InstanceType, "flow.output", "flow.dead-letter"), defs: map[string][]*flowDef{}}
}

func flowEntities() []platform.Entity {
	return []platform.Entity{{Type: InstanceType, Title: "Flow instance", Model: FlowInstance{}, Display: "title",
		Scope:   platform.Scope{Participants: func(record any) []string { return []string{record.(FlowInstance).OnBehalf} }},
		Derived: []platform.Derivation{{From: "sources", Fields: []string{"data", "outputs", "tokens", "trace", "answer", "batch"}}}, Withheld: "withheld"},
		{Type: "flow.output", Title: "Flow output", Model: platform.FlowOutput{}, Display: "name",
			Scope:   platform.Scope{Participants: func(record any) []string { return []string{record.(platform.FlowOutput).OnBehalf} }},
			Derived: []platform.Derivation{{From: "sources", Fields: []string{"value"}}}, Withheld: "withheld"},
		{Type: "flow.dead-letter", Title: "Flow dead letter", Model: platform.FlowDeadLetterAsset{}, Display: "instance",
			Scope:   platform.Scope{Participants: func(record any) []string { return []string{record.(platform.FlowDeadLetterAsset).OnBehalf} }},
			Derived: []platform.Derivation{{From: "sources", Fields: []string{"letter", "payload", "target"}}}, Withheld: "withheld"}}
}

func (f *Flows) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: platform.BuiltinManifestTitle(ID), Version: "1", Actions: f.ledger.Catalog, Entities: flowEntities(), Reads: []string{"flows"},
		ReadSources: map[string]string{"flows": "record"},
		Jobs:        []platform.Job{{Name: "timers", Title: "Take flows' due steps: retries, timeouts, times and conditions", Every: time.Second}}}
}

func (f *Flows) Declarations() []*pb.AuthorityDeclaration { return f.ledger.Declarations() }

func (f *Flows) AcceptedLedger() *platform.Ledger { return f.ledger }

func (f *Flows) SnapshotFormat() string { return "platform.ledger.v1" }

func (f *Flows) Snapshot() (json.RawMessage, error) { return f.ledger.Snapshot() }

func (f *Flows) Restore(raw json.RawMessage) error { return f.ledger.Restore(raw) }

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

func (f *Flows) Install(a platform.App, fl platform.Flow) error {
	d, err := f.check(a.Manifest(), fl)
	if err != nil {
		return err
	}
	id := a.Manifest().ID + "." + fl.Name
	f.defs[id] = append(f.defs[id], d)
	return nil
}

func (f *Flows) HasPublishedFlow(id string) bool { _, ok := f.latest(id); return ok }

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

func (f *Flows) Versions() map[string]int {
	v := f.chosen
	f.chosen = nil
	return v
}

func (f *Flows) Pin(versions map[string]int) { f.pins = versions }

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

func (f *Flows) HasRunningDependency(typ string) (bool, error) {
	return f.hasRunningDependency(typ, false)
}

func (f *Flows) HasUnboundRunningDependency(typ string) (bool, error) {
	return f.hasRunningDependency(typ, true)
}

func (f *Flows) hasRunningDependency(typ string, unboundOnly bool) (bool, error) {
	c := f.host.Automation(platform.Caller{Replaying: true}, ID)
	found := false
	err := f.eachRunning(c, func(row FlowInstance) error {
		binding, err := f.binding(row)
		depends := strings.HasPrefix(row.Subject, typ+"/") || slices.ContainsFunc(binding.Assets, func(ref platform.AssetRef) bool {
			return ref.Kind == platform.AssetObject && ref.Name == typ
		})
		found = found || depends && (!unboundOnly || binding.Release == "")
		return err
	})
	return found, err
}

type FlowDefinition = platform.FlowDefinition

type FlowStep = platform.FlowStep

func (f *Flows) Read(c platform.Caller, _ string) (any, *kernel.Error) {
	out := []FlowDefinition{}
	for _, id := range slices.Sorted(maps.Keys(f.defs)) {
		for _, d := range f.defs[id] {
			v := flowDefinitionOf(d)
			out = append(out, v)
		}
	}
	return out, nil
}

func (f *Flows) StartManual(c platform.Caller, app, name string, version int, key, subject string, input json.RawMessage, now time.Time) *kernel.Error {
	d := f.def(app+"."+name, version)
	if d == nil || (!d.Start.Manual && d.Start.OnBehalf == "" || d.Start.Continuous) || key == "" || len(key) > 256 || len(input) > 64<<10 || !json.Valid(input) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an installed manual flow, bounded key and JSON input")
	}
	if !d.Start.Manual {
		key = "manual:" + key
	}
	return f.start(f.host.Automation(c, ID), d, key, input, c.ID, nil, "", subject, now, true)
}

func (f *Flows) HasDefinition(app, name string, version int) bool {
	return f.def(app+"."+name, version) != nil
}

func (f *Flows) Fork(ledger *platform.Ledger) (platform.App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	defs := maps.Clone(f.defs)
	for id, versions := range defs {
		defs[id] = slices.Clone(versions)
	}
	return &Flows{host: f.host, ledger: ledger, defs: defs,
		chosen: maps.Clone(f.chosen), pins: maps.Clone(f.pins)}, nil
}

func (a *Flows) NewSimulationApp(tenant string) platform.App { return New(tenant) }
