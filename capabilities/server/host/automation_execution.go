package host

import (
	"encoding/json"
	"fmt"
	automation "platformserver/services/automate/runtime"

	"reflect"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	runtimekernel "platformserver/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type flowExecution struct {
	hostView
	rt *flowExecutionRuntime
}

type flowExecutionRuntime struct {
	*stagedDecision
	view    *Tenant
	release string
	shapes  map[string]platform.EntityInfo
	assets  []platform.ReleaseAsset
	reads   records.ForkCache
}

func (h hostView) FlowExecution(c platform.Caller, app, name string, version int, binding platform.FlowBinding) (platform.FlowHost, error) {
	if binding.Release == "" {
		return h, nil
	}
	current, _, err := h.flowBindings().Closure(app, name, version)
	if err == nil && current.ID == binding.Dependencies {
		return h, nil
	}
	closed, err := h.flowBindings().Retained(app, name, version, binding)
	if err != nil {
		return nil, err
	}
	return h.retainedExecution(c, binding.Release, closed.Assets)
}

func (h hostView) retainedExecution(c platform.Caller, release string, assets []platform.ReleaseAsset) (platform.FlowHost, error) {
	if h.decision == nil || !c.Staging() {
		return nil, fmt.Errorf("retained flow execution requires its private decision")
	}
	view := h.installationDraft()
	apps, shapes, err := automation.RetainedOwners{Apps: h.t.apps, Assets: assets,
		Current: func() ([]platform.ReleaseAsset, error) { return h.t.releaseAssetsLocked(nil, false) },
		Native:  h.t.nativeFlowAsset,
		Claim:   func(i int, key string) { view.owner[key] = view.ids[i] },
		Bind:    func(bound platform.App) { (hostView{t: view, app: bound, decision: h.decision}).bindOwner(bound) },
	}.Select()
	if err != nil {
		return nil, err
	}
	rt := &flowExecutionRuntime{stagedDecision: h.decision, view: view, release: release, shapes: shapes, assets: slices.Clone(assets)}
	view.ownerSet = view.with(apps)
	definitions, err := automation.RetainedDefinitions(view.definitions, assets)
	if err != nil {
		return nil, err
	}
	view.definitions = definitions
	return flowExecution{hostView: h, rt: rt}, nil
}

func (h flowExecution) Automation(parent platform.Caller, app string) platform.Caller {
	c := h.hostView.Automation(parent, app)
	return platform.NewCaller(h.rt, c.Member, app, c.Replaying, c.Automation)
}
func (h flowExecution) Caller(parent platform.Caller, m platform.Member, app string) platform.Caller {
	return platform.NewCaller(h.rt, m, app, parent.Replaying, false)
}

func (h flowExecution) ActiveRelease() string { return h.rt.release }

func (h flowExecution) Attempt(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return runtimekernel.AttemptTo(c, func() platform.App { return h.rt.actionOwner(s.GetSchema().GetName()) }, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The held action has no retained owner"), s, at)
}
func (h flowExecution) Action(schema string) (string, platform.Action, bool) {
	owner := h.rt.actionOwner(schema)
	if owner == nil {
		return "", platform.Action{}, false
	}
	a, ok := owner.Manifest().Actions.Action(schema)
	return owner.Manifest().ID, a, ok
}
func (h flowExecution) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return runtimekernel.SubmitTo(c, h.rt.actionOwner(s.GetSchema().GetName()), &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}, s, at)
}

func (r *flowExecutionRuntime) actionOwner(schema string) platform.App {
	return r.view.App(r.view.owner["action:"+schema])
}

func (r *flowExecutionRuntime) StagedDecision() platform.StagedDecision { return r }
func (r *flowExecutionRuntime) DecisionCatalog(ledger *platform.Ledger) *platform.Catalog {
	for _, owner := range r.view.apps {
		if owner.AcceptedLedger() == ledger {
			return owner.Manifest().Actions
		}
	}
	return nil
}
func (r *flowExecutionRuntime) Decide(c platform.Caller, app platform.App, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if app.Manifest().ID == platform.WorkApp {
		private := r.stateApp(r.tenant.App(platform.WorkApp))
		context := hostView{t: r.tenant, app: private, decision: r.stagedDecision}
		bindings := context.ownerBindings()
		bindings.Work = flowExecution{hostView: context, rt: r}
		r.tenant.services.BindOwner(private, bindings)
		return r.decideAppFrom(c, private, s, at, r.view, r.release, r.assets)
	}
	if bound := r.view.App(app.Manifest().ID); bound != nil {
		if bound != r.tenant.App(app.Manifest().ID) {
			return r.decideAppFrom(c, bound, s, at, r.view, r.release, r.assets)
		}
	}
	return r.stagedDecision.decideAppFrom(c, r.stateApp(app), s, at, r.view, r.release, r.assets)
}

func (r *flowExecutionRuntime) readView() (*Tenant, *kernel.Error) {
	store, err := automation.RetainedView(&r.reads, r.Records, r.release, r.shapes)
	if err != nil {
		return nil, runtimekernel.ConflictRefusal(err)
	}
	return (hostView{t: r.view}).installationView(store), nil
}

func (r *flowExecutionRuntime) Get(c platform.Caller, typ reflect.Type, id string) (any, bool) {
	value, ok, err := automation.RetainedGet(r.Records, r.shapes, r.release, c, typ, id)
	if err != nil {
		r.FailDecision(runtimekernel.ConflictRefusal(err))
		return nil, false
	}
	return value, ok
}
func retainedRead[T any](r *flowExecutionRuntime, read func(view *Tenant) (T, *kernel.Error)) (output T, refusal *kernel.Error) {
	defer automation.RecoverRetained(r.FailDecision, &refusal)
	view, err := r.readView()
	if err != nil {
		return output, err
	}
	return read(view)
}

func (r *flowExecutionRuntime) Check(c platform.Caller, entity any) *kernel.Error {
	_, refusal := retainedRead(r, func(view *Tenant) (struct{}, *kernel.Error) {
		return struct{}{}, view.Projection.Check(c, entity, r.At)
	})
	return refusal
}
func (r *flowExecutionRuntime) Find(c platform.Caller, typ reflect.Type, q platform.Query) (out []any, total int, refusal *kernel.Error) {
	defer automation.RecoverRetained(r.FailDecision, &refusal)
	view, err := r.readView()
	if err != nil {
		return nil, 0, err
	}
	d := *r.stagedDecision
	d.Records = view.Projection
	return d.Find(c, typ, q)
}
func (r *flowExecutionRuntime) mayRead(c platform.Caller, typ, id string, at time.Time) *kernel.Error {
	return authorizationRefusal(r.stagedDecision.Authorize(c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id}, at))
}

func (r *flowExecutionRuntime) ReadRecord(c platform.Caller, typ, id string, at time.Time) (output json.RawMessage, refusal *kernel.Error) {
	defer automation.RecoverRetained(r.FailDecision, &refusal)
	if refusal := r.mayRead(c, typ, id, at); refusal != nil {
		return nil, refusal
	}
	view, err := r.readView()
	if err != nil {
		return nil, err
	}
	raw, refusal := view.objectOwner(view.Projection).ReadRecordFrom(view.Projection, c, typ, id, at)
	if refusal != nil {
		return nil, refusal
	}
	return automation.ReadableRecord(raw, r.Records.Type(typ), c)
}
func (r *flowExecutionRuntime) ReadRecordPath(c platform.Caller, typ, id string, path []string, at time.Time) (output json.RawMessage, refs []string, refusal *kernel.Error) {
	defer automation.RecoverRetained(r.FailDecision, &refusal)
	view, err := r.readView()
	if err != nil {
		return nil, nil, err
	}
	return automation.ReadRecordPath(c, typ, id, path, at, view.Entity, func(typ, id string, at time.Time) (json.RawMessage, *kernel.Error) {
		return r.ReadRecord(c, typ, id, at)
	})
}

func (r *flowExecutionRuntime) ReadQuery(c platform.Caller, app, name string, input json.RawMessage, at time.Time) (json.RawMessage, *kernel.Error) {
	return retainedRead(r, func(view *Tenant) (json.RawMessage, *kernel.Error) {
		return view.queryReads(view.Projection).Query(c, app, name, input, at)
	})
}
func (r *flowExecutionRuntime) ReadQueryVersion(c platform.Caller, app, name string, version int, input json.RawMessage, at time.Time) (json.RawMessage, *kernel.Error) {
	return retainedRead(r, func(view *Tenant) (json.RawMessage, *kernel.Error) {
		return view.queryReads(view.Projection).QueryVersion(c, app, name, version, input, at)
	})
}
func (r *flowExecutionRuntime) ReadRuleQuery(c platform.Caller, set platform.RuleCollection, input json.RawMessage, at time.Time) (json.RawMessage, *kernel.Error) {
	return retainedRead(r, func(view *Tenant) (json.RawMessage, *kernel.Error) {
		return view.queryReads(view.Projection).RuleQuery(c, set, input, at)
	})
}
func (r *flowExecutionRuntime) ReadRecordSet(c platform.Caller, object, id, name string, at time.Time) (output platform.RecordSetResult, refusal *kernel.Error) {
	defer automation.RecoverRetained(r.FailDecision, &refusal)
	if refusal := r.mayRead(c, object, id, at); refusal != nil {
		return platform.RecordSetResult{}, refusal
	}
	view, err := r.readView()
	if err != nil {
		return platform.RecordSetResult{}, err
	}
	return view.readRecordSetFrom(view.Projection, c, object, id, name, at)
}
func (r *flowExecutionRuntime) Authorize(c platform.Caller, q platform.AuthzRequest, at time.Time) (decision platform.AuthzDecision) {
	defer automation.RecoverRetained(r.FailDecision, nil)
	view, err := r.readView()
	if err != nil {
		r.FailDecision(err)
		return platform.AuthzDecision{}
	}
	c.Member = r.tenant.authorizationMember(c.Member)
	decision = view.authorizeFrom(view.Projection, c, q, at, false)
	r.RecordAuthorization(c, q, decision)
	return decision
}

func (r *flowExecutionRuntime) Put(c platform.Caller, receipt *pb.ChangeRecord, entity any) (refusal *kernel.Error) {
	defer automation.RecoverRetained(r.FailDecision, &refusal)
	defer func() {
		if refusal != nil {
			r.FailDecision(refusal)
		}
	}()
	view, err := r.readView()
	if err != nil {
		return err
	}
	return automation.RetainedPut{Shapes: r.shapes, Release: r.release, Current: r.Records,
		Authorize: func(question platform.AuthzRequest) *kernel.Error {
			grant := r.tenant.authorizeFrom(r.Records, c, question, r.At, false)
			r.RecordAuthorization(c, question, grant)
			return authorizationRefusal(grant)
		},
		Check: func(decoded any) *kernel.Error { return r.stagedDecision.Check(c, decoded) },
		Put:   func(decoded any) *kernel.Error { return r.stagedDecision.Put(c, receipt, decoded) },
	}.Run(c, view.Projection.Of(c, reflect.TypeOf(entity)), entity)
}
