package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
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
}

func (h hostView) FlowExecution(c platform.Caller, app, name string, version int, binding host.FlowBinding) (host.FlowHost, error) {
	if binding.Release == "" {
		return h, nil
	}
	current, _, err := h.flowClosure(app, name, version)
	if err == nil && current.ID == binding.Dependencies {
		return h, nil
	}
	closed, err := h.retainedFlowClosure(app, name, version, binding)
	if err != nil {
		return nil, err
	}
	return h.retainedExecution(c, binding.Release, closed.Assets)
}

func (h hostView) retainedExecution(c platform.Caller, release string, assets []platform.ReleaseAsset) (host.FlowHost, error) {
	if h.decision == nil || !c.Staging() {
		return nil, fmt.Errorf("retained flow execution requires its private decision")
	}
	view := h.installationDraft()
	view.apps = slices.Clone(h.t.apps)
	rt := &flowExecutionRuntime{stagedDecision: h.decision, view: view, release: release, shapes: map[string]platform.EntityInfo{}}
	for i, owner := range view.apps {
		selected := []platform.ReleaseAsset{}
		for _, asset := range assets {
			if asset.Ref.App == owner.Manifest().ID {
				selected = append(selected, asset)
			}
		}
		if len(selected) == 0 {
			continue
		}
		compiler, ok := owner.(host.ReleaseExecutionOwner)
		if !ok {
			current, err := h.t.releaseAssetsLocked(nil, false)
			if err != nil {
				return nil, err
			}
			for _, asset := range selected {
				a := slices.IndexFunc(current, func(x platform.ReleaseAsset) bool { return x.Ref == asset.Ref })
				before, _ := canonicalDigest(asset)
				var after string
				if a >= 0 {
					after, _ = canonicalDigest(current[a])
				}
				if before != after {
					return nil, fmt.Errorf("retained dependency %s has no compatible execution owner", asset.Ref)
				}
			}
			continue
		}
		bound, err := compiler.ForkReleaseExecution(selected)
		if err != nil || bound == nil || bound.AcceptedLedger() != owner.AcceptedLedger() {
			return nil, fmt.Errorf("retained owner %s cannot select its original execution: %v", owner.Manifest().ID, err)
		}
		view.apps[i] = bound
		for _, action := range bound.Manifest().Actions.All() {
			view.owner["action:"+action.Schema] = bound
		}
		for _, entity := range bound.Manifest().Entities {
			if !slices.ContainsFunc(selected, func(a platform.ReleaseAsset) bool {
				return a.Ref.Kind == platform.AssetObject && a.Ref.Name == entity.Type
			}) {
				continue
			}
			info, err := platform.Describe(bound.Manifest().ID, entity, func(reflect.Type) string { return "" })
			if err != nil {
				return nil, err
			}
			rt.shapes[info.Type] = info
			view.owner[info.Type] = bound
		}
		host.Attach(bound, hostView{t: view, app: bound, decision: h.decision})
	}
	view.definitions = slices.Clone(view.definitions)
	for i, definition := range view.definitions {
		for _, asset := range assets {
			if asset.Ref != definition.Ref {
				continue
			}
			switch asset.Ref.Kind {
			case platform.AssetQuery:
				var query platform.NamedQuery
				if err := json.Unmarshal(asset.Body, &query); err != nil {
					return nil, err
				}
				definition.Query = &query
			case platform.AssetFunction:
				var function platform.AIFunction
				if err := json.Unmarshal(asset.Body, &function); err != nil {
					return nil, err
				}
				definition.Function = &function
			}
			view.definitions[i] = definition
		}
	}
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
	return platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
		owner := h.rt.view.owner["action:"+s.GetSchema().GetName()]
		if owner == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The held action has no retained owner")
		}
		return platform.Decide(c, owner, s, at)
	})
}
func (h flowExecution) Action(schema string) (string, platform.Action, bool) {
	owner := h.rt.view.owner["action:"+schema]
	if owner == nil {
		return "", platform.Action{}, false
	}
	a, ok := owner.Manifest().Actions.Action(schema)
	return owner.Manifest().ID, a, ok
}
func (h flowExecution) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	owner := h.rt.view.owner["action:"+s.GetSchema().GetName()]
	if owner == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	if declared, ok := owner.Manifest().Actions.Action(s.GetSchema().GetName()); ok && declared.Approval != nil && !c.Automation {
		return c.RequestApproval(owner.Manifest().ID, s, at)
	}
	return platform.Decide(c, owner, s, at)
}

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
		private := r.stateApp(r.tenant.app(platform.WorkApp))
		if work, ok := private.(host.AttachedWork); ok {
			work.AttachWork(flowExecution{hostView: hostView{t: r.tenant, app: private, decision: r.stagedDecision}, rt: r})
		}
		return r.decideApp(c, private, s, at)
	}
	if bound := r.view.app(app.Manifest().ID); bound != nil {
		if bound != r.tenant.app(app.Manifest().ID) {
			return r.decideApp(c, bound, s, at)
		}
	}
	return r.stagedDecision.Decide(c, app, s, at)
}

// Rows stay current; the selected metadata decodes retained fields. A renamed
// field follows its explicit alias, while a retyped field needs its saved image.
func (r *flowExecutionRuntime) readView() (*Tenant, *kernel.Error) {
	store := r.records.forkRecords()
	for typ, info := range r.shapes {
		current := store.types[typ]
		if current == nil {
			return nil, migrationRefusal(fmt.Errorf("retained object %s has no current storage", typ))
		}
		rows := make(map[string]*row, len(current.rows))
		for id, value := range current.rows {
			raw, err := value.image()
			if err != nil {
				return nil, migrationRefusal(err)
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil {
				return nil, migrationRefusal(fmt.Errorf("retained record cannot be decoded"))
			}
			for _, old := range info.Fields {
				next, found := current.info.Field(old.Name)
				if found && next.AliasOf != "" {
					fields[old.Name] = fields[next.AliasOf]
				} else if !found || next.Type != old.Type || next.Ref != old.Ref || next.ReadOnly && !old.ReadOnly && projectionFor(value.projection, r.release) != nil {
					var prior map[string]json.RawMessage
					source := projectionFor(value.projection, r.release)
					shape, _ := canonicalDigest(info.Fields)
					if source == nil || source.Shape != shape || json.Unmarshal(source.Value, &prior) != nil {
						return nil, migrationRefusal(fmt.Errorf("retained field %s.%s has no saved source image", typ, old.Name))
					}
					fields[old.Name] = prior[old.Name]
				}
			}
			decoded := reflect.New(info.Go)
			raw, _ = json.Marshal(fields)
			if err := json.Unmarshal(raw, decoded.Interface()); err != nil {
				return nil, migrationRefusal(err)
			}
			rows[id] = &row{value: decoded.Elem(), history: value.history}
		}
		store.types[typ] = &entityType{info: info, rows: rows, knowledge: current.knowledge}
		store.byGo[info.Go] = store.types[typ]
	}
	view := (hostView{t: r.view}).installationDraft()
	view.records = store
	return view, nil
}

func (r *flowExecutionRuntime) Get(c platform.Caller, typ reflect.Type, id string) (any, bool) {
	view, err := r.readView()
	if err != nil {
		r.FailDecision(err)
		return nil, false
	}
	return view.records.get(c, typ, id)
}
func (r *flowExecutionRuntime) Check(c platform.Caller, entity any) *kernel.Error {
	view, err := r.readView()
	if err != nil {
		return err
	}
	return view.records.check(c, entity, r.at)
}
func (r *flowExecutionRuntime) Find(c platform.Caller, typ reflect.Type, q platform.Query) ([]any, int, *kernel.Error) {
	view, err := r.readView()
	if err != nil {
		return nil, 0, err
	}
	d := *r.stagedDecision
	d.records = view.records
	return d.Find(c, typ, q)
}
func (r *flowExecutionRuntime) ReadRecord(c platform.Caller, typ, id string, at time.Time) (json.RawMessage, *kernel.Error) {
	if refusal := authorizationRefusal(r.stagedDecision.Authorize(c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id}, at)); refusal != nil {
		return nil, refusal
	}
	view, err := r.readView()
	if err != nil {
		return nil, err
	}
	raw, refusal := view.readRecordFrom(view.records, c, typ, id, at)
	if refusal != nil {
		return nil, refusal
	}
	// Old field names must not reveal a target whose current field grant was
	// revoked. Current membership and policy still govern every retained read.
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if current := r.records.types[typ]; current != nil {
		for name := range fields {
			field, declared := current.info.Field(name)
			if declared && field.AliasOf != "" {
				field, declared = current.info.Field(field.AliasOf)
			}
			if declared && !field.ReadsAny(c.RolesIn(current.info.App)) {
				delete(fields, name)
			}
		}
	}
	raw, marshalErr := json.Marshal(fields)
	return raw, migrationRefusal(marshalErr)
}
func (r *flowExecutionRuntime) ReadRecordPath(c platform.Caller, typ, id string, path []string, at time.Time) (json.RawMessage, []string, *kernel.Error) {
	view, err := r.readView()
	if err != nil {
		return nil, nil, err
	}
	// The shared traversal calls the same scoped ReadRecord implementation.
	return readRecordPath(c, typ, id, path, at, view.entity, func(typ, id string, at time.Time) (json.RawMessage, *kernel.Error) {
		return r.ReadRecord(c, typ, id, at)
	})
}

func (r *flowExecutionRuntime) ReadQuery(c platform.Caller, app, name string, input json.RawMessage, at time.Time) (json.RawMessage, *kernel.Error) {
	view, err := r.readView()
	if err != nil {
		return nil, err
	}
	return view.readQueryFrom(view.records, c, app, name, input, at)
}
func (r *flowExecutionRuntime) ReadQueryVersion(c platform.Caller, app, name string, version int, input json.RawMessage, at time.Time) (json.RawMessage, *kernel.Error) {
	view, err := r.readView()
	if err != nil {
		return nil, err
	}
	return view.readQueryVersionFrom(view.records, c, app, name, version, input, at)
}
func (r *flowExecutionRuntime) ReadRuleQuery(c platform.Caller, set platform.RuleCollection, input json.RawMessage, at time.Time) (json.RawMessage, *kernel.Error) {
	view, err := r.readView()
	if err != nil {
		return nil, err
	}
	return view.readRuleQueryFrom(view.records, c, set, input, at)
}
func (r *flowExecutionRuntime) ReadRecordSet(c platform.Caller, object, id, name string, at time.Time) (platform.RecordSetResult, *kernel.Error) {
	question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: object, Record: id}
	if refusal := authorizationRefusal(r.stagedDecision.Authorize(c, question, at)); refusal != nil {
		return platform.RecordSetResult{}, refusal
	}
	view, err := r.readView()
	if err != nil {
		return platform.RecordSetResult{}, err
	}
	return view.readRecordSetFrom(view.records, c, object, id, name, at)
}
func (r *flowExecutionRuntime) Authorize(c platform.Caller, q platform.AuthzRequest, at time.Time) platform.AuthzDecision {
	view, err := r.readView()
	if err != nil {
		r.FailDecision(err)
		return platform.AuthzDecision{}
	}
	c.Member = r.tenant.authorizationMember(c.Member)
	decision := view.authorizeFrom(view.records, c, q, at, false)
	r.RecordAuthorization(c, q, decision)
	return decision
}

func (r *flowExecutionRuntime) Put(c platform.Caller, receipt *pb.ChangeRecord, entity any) (refusal *kernel.Error) {
	defer func() {
		if refusal != nil {
			r.FailDecision(refusal)
		}
	}()
	view, err := r.readView()
	if err != nil {
		return err
	}
	old := view.records.of(c, reflect.TypeOf(entity))
	if old == nil {
		return r.stagedDecision.Put(c, receipt, entity)
	}
	if _, retained := r.shapes[old.info.Type]; !retained {
		return r.stagedDecision.Put(c, receipt, entity)
	}
	current := r.records.types[old.info.Type]
	id := recordOf(copyOf(old.info.Go, entity)).ID
	var input, before, result map[string]json.RawMessage
	if json.Unmarshal(jsonOf(entity), &input) != nil {
		return migrationRefusal(fmt.Errorf("retained action wrote an invalid record"))
	}
	if prior := old.rows[id]; prior != nil {
		_ = json.Unmarshal(jsonOf(prior.value.Interface()), &before)
	}
	mappedBefore, mappedInput, extractions, mappingErr := r.mapRetainedWrite(c.ID, old.info.Type, before, input)
	if mappingErr != nil {
		return migrationRefusal(mappingErr)
	}
	if err := r.applyRetainedExtractions(c, receipt, extractions); err != nil {
		return err
	}
	if prior := current.rows[id]; prior != nil {
		raw, _ := prior.image()
		_ = json.Unmarshal(raw, &result)
	} else {
		result = maps.Clone(mappedInput)
	}
	fields := []string{}
	for _, target := range current.info.Fields {
		if string(mappedInput[target.Name]) == string(mappedBefore[target.Name]) {
			continue
		}
		name := target.Name
		if target.AliasOf != "" {
			target, _ = current.info.Field(target.AliasOf)
		}
		lifecycle := current.info.Lifecycle != nil && current.info.Lifecycle.Field == target.Name
		if target.ReadOnly && !lifecycle {
			return migrationRefusal(fmt.Errorf("retained action writes a retired field %s", target.Name))
		}
		result[target.Name] = mappedInput[name]
		fields = append(fields, target.Name)
	}
	question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "write", Type: current.info.Type, Record: id, Fields: fields}
	if current.rows[id] == nil {
		question.Kind = "create"
	}
	grant := r.tenant.authorizeFrom(r.records, c, question, r.at, false)
	r.RecordAuthorization(c, question, grant)
	if refusal := authorizationRefusal(grant); refusal != nil {
		return refusal
	}
	result["id"], result["archived"] = input["id"], input["archived"]
	decoded := reflect.New(current.info.Go)
	raw, _ := json.Marshal(result)
	if json.Unmarshal(raw, decoded.Interface()) != nil {
		return migrationRefusal(fmt.Errorf("retained action result does not match current storage"))
	}
	if refusal := r.stagedDecision.Check(c, decoded.Elem().Interface()); refusal != nil {
		return refusal
	}
	if refusal := r.stagedDecision.Put(c, receipt, decoded.Elem().Interface()); refusal != nil {
		return refusal
	}
	// Keep the newly accepted source values for another old-version step.
	if source := projectionFor(current.rows[id].projection, r.release); source != nil {
		source.Value = jsonOf(input)
		if refusal := refreshRecordProjection(current.rows[id].projection, current.rows[id].value.Interface()); refusal != nil {
			return refusal
		}
	}
	return nil
}
