package host

import (
	"fmt"
	"platformserver/kernel/records"
	"platformserver/services/ai/gateway"
	agentowner "platformserver/services/ai/runtime"
	functionauthoring "platformserver/services/functions/authoring"
	"platformserver/services/resources/authoring"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (d *stagedDecision) RequestFunction(c platform.Caller, r *pb.ChangeRecord, request platform.FunctionRequest) (platform.FunctionCall, *kernel.Error) {
	call, effect, refusal := d.tenant.planFunction(c, r, request, d.Records, d.Intents)
	return intend(d, call, effect, refusal)
}

func (t *Tenant) planFunction(c platform.Caller, r *pb.ChangeRecord, request platform.FunctionRequest, store *records.Store, pending []platform.Effect) (platform.FunctionCall, platform.Effect, *kernel.Error) {
	ctx := agentowner.FunctionContext{Tenant: t.ID, App: t.App, Member: t.Member, AuthorityOf: t.AuthorityOf,
		Authorize: func(c platform.Caller, owner string, f platform.AIFunction, sources []string, definition string, at time.Time) *kernel.Error {
			return authorizationRefusal(t.authorizeCapability(c, owner, f.Name, f.Roles, f.Check() == nil, authorizationDetails{Store: store, At: at, Sources: sources, Definition: definition}))
		},
		Source: func(member platform.Member, request platform.FunctionRequest, f platform.AIFunction, owner, sourceRef, definition string, version int) (agentowner.FunctionSource, *kernel.Error) {
			return agentowner.ReadFunctionSource(store, request, f, owner, sourceRef, definition, version,
				func() bool { return t.readableIn(store, member, sourceRef, r.GetRecordedTime().AsTime()) },
				func(id string) (agentowner.FunctionHistory, error) { return t.functionHistory(store, member, id) })
		},
		Readable: func(m platform.Member, ref string, at time.Time) bool {
			return t.mayReadIn(t.Projection, m, at, false)(ref)
		}, Closure: t.closures().Function, BindApplications: func(ref platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
			return t.bindApplications(ref, release, dependencies)
		},
		Model: func(c platform.Caller, r *pb.ChangeRecord, q platform.Request, pending []platform.Effect, options platform.ModelOptions) (agentowner.FunctionDispatch, *kernel.Error) {
			stage := platform.AIStageRuntime
			if t.simulation {
				stage = platform.AIStageDesign
			}
			return t.modelResolver().FunctionDispatch(t.planModelRequest(c, r, q, pending), options, stage)
		},
		Bind: aigateway.BindFunction,
	}
	return agentowner.PlanFunction(ctx, c, r, request, pending)
}

func (t *Tenant) functionAllowed(app string, ask aigateway.ModelAsk, at time.Time) bool {
	ctx := agentowner.FunctionAccess{Tenant: t.ID, Member: t.Member, AuthorityOf: t.AuthorityOf, Suspended: t.suspended,
		Authorize: func(member platform.Member, owner string, f platform.AIFunction, sources []string, definition string, at time.Time) bool {
			c := platform.NewCaller(runtime{t: t, scoped: true}, member, owner, false, false)
			return t.authorizeCapability(c, owner, f.Name, f.Roles, true, authorizationDetails{At: at, Sources: sources, Definition: definition}).Allow
		},
		Source: func(typ, id string) (platform.EntityInfo, bool) { return records.LockedActive(t.Projection, typ, id) },
	}
	return agentowner.FunctionAllowed(ctx, app, ask.Function, ask.Model, ask.Record, ask.Call, ask.Prompt, at)
}

func (h hostView) functionInstall() functionauthoring.FunctionInstall {
	return functionauthoring.FunctionInstall{Manifest: h.app.Manifest(),
		Source: func(object string) (platform.EntityInfo, bool) {
			et := h.t.Projection.Type(object)
			if et == nil {
				return platform.EntityInfo{}, false
			}
			return et.Info(), true
		},
		Narrowable: func(object string) []string { return h.t.narrowable(h.t.Projection.Type(object)) },
		Check:      h.ValidateInstallFunction,
		Describe: func(def platform.Definition) (platform.Definition, error) {
			return h.t.describer().DescribeDefinition(def)
		},
		Definitions: func() []platform.Definition { return h.t.definitions },
		Store:       h.t.storeDefinition,
	}
}

func (h hostView) ValidateInstallFunction(f platform.AIFunction) error {
	h.t.Projection.Lock()
	defer h.t.Projection.Unlock()
	return h.functionInstall().Validate(f)
}

func (h hostView) InstallFunction(c platform.Caller, f platform.AIFunction, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	return h.functionInstall().Install(f, version)
}

func (h hostView) Function(app, name string, version int) (platform.AIFunction, int, bool) {
	return agentowner.FunctionDefinition(h.t.App(app), name, version)
}

func (t *Tenant) functionHistory(store *records.Store, member platform.Member, id string) (agentowner.FunctionHistory, error) {
	et := store.Type(build.FunctionCallType)
	if et == nil {
		return agentowner.FunctionHistory{}, fmt.Errorf("conversation history is unavailable")
	}
	reader := platform.NewCaller(runtime{t: t, scoped: true}, member, et.Info().App, false, false)
	decision := t.authorizeFrom(store, reader, platform.AuthzRequest{Principal: member.ID, Tenant: member.Tenant, Kind: "derive", Type: et.Info().Type, Record: id, Fields: []string{"question", "output"}}, time.Now(), true)
	if !decision.Allow || len(decision.Withheld) > 0 {
		return agentowner.FunctionHistory{}, fmt.Errorf("conversation history is not readable")
	}
	row := et.Rows().Get(id)
	if row == nil {
		return agentowner.FunctionHistory{}, fmt.Errorf("conversation history is unavailable")
	}
	run, ok := row.Value.Interface().(build.FunctionRun)
	if !ok || run.Member != member.ID {
		return agentowner.FunctionHistory{}, fmt.Errorf("conversation history belongs to another context")
	}

	return agentowner.FunctionHistory{Archived: run.Archived, State: run.State, App: run.App, Function: run.Function, Version: run.Version, Definition: run.Definition, Source: run.Source, Question: run.Question, Output: run.Output}, nil
}
