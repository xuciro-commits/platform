package host

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/records"
	"platformserver/services/ai/gateway"
	agentowner "platformserver/services/ai/runtime"
	"platformserver/services/resources/authoring"

	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (d *stagedDecision) RequestFunction(c platform.Caller, r *pb.ChangeRecord, request platform.FunctionRequest) (platform.FunctionCall, *kernel.Error) {
	call, effect, refusal := d.tenant.planFunction(c, r, request, d.Records, d.Intents)
	if refusal != nil {
		if d.Failure == nil {
			d.Failure = refusal
		}
		return platform.FunctionCall{}, refusal
	}
	if !d.ProbeOnly {
		d.Intents = append(d.Intents, effect)
	}
	return call, nil
}

func (t *Tenant) planFunction(c platform.Caller, r *pb.ChangeRecord, request platform.FunctionRequest, store *records.Store, pending []platform.Effect) (platform.FunctionCall, platform.Effect, *kernel.Error) {
	ctx := agentowner.FunctionContext{Tenant: t.ID, App: t.App, Member: t.Member, AuthorityOf: t.AuthorityOf,
		Authorize: func(c platform.Caller, owner string, f platform.AIFunction, sources []string, definition string, at time.Time) *kernel.Error {
			return authorizationRefusal(t.authorizeCapability(c, owner, f.Name, f.Roles, f.Check() == nil, authorizationDetails{Store: store, At: at, Sources: sources, Definition: definition}))
		},
		Source: func(member platform.Member, request platform.FunctionRequest, f platform.AIFunction, owner, sourceRef, definition string, version int) (agentowner.FunctionSource, *kernel.Error) {
			source := strings.TrimPrefix(sourceRef, f.Object+"/")
			at := r.GetRecordedTime().AsTime()
			var err error
			var sourceRevision uint32
			var sourceValues map[string]json.RawMessage
			var raw []byte
			var historySources []string
			var conversationErr error
			if refusal := func() *kernel.Error {
				store.Lock()
				defer store.Unlock()
				et := store.Type(f.Object)
				if et == nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The AI function source is not readable")
				}
				row := et.Rows().Get(source)
				if row == nil || records.Header(row.Value).Archived || !t.readableIn(store, member, sourceRef, at) {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The AI function source is not readable")
				}
				input := map[string]any{}
				for _, name := range f.Fields {
					field, found := et.Info().Field(name)
					if !found || field.AIInput != nil && !*field.AIInput {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The AI function source is not readable")
					}
					input[name] = row.Value.FieldByIndex(field.Index).Interface()
				}
				sourceRevision = records.Header(row.Value).Revision
				sourceValues = map[string]json.RawMessage{}
				for field, value := range input {
					sourceValues[field] = platform.Raw(value)
				}
				payload, preparedSources, preparedErr := agentowner.FunctionConversation(request, f, owner, sourceRef, definition, version, input, func(id string) (agentowner.FunctionHistory, error) { return t.functionHistory(store, member, id) })
				historySources, conversationErr = preparedSources, preparedErr
				raw, err = json.Marshal(payload)
				return nil
			}(); refusal != nil {
				return agentowner.FunctionSource{}, refusal
			}
			if conversationErr != nil {
				return agentowner.FunctionSource{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, conversationErr.Error())
			}
			if err != nil {
				return agentowner.FunctionSource{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The AI function input exceeds its byte budget")
			}
			return agentowner.FunctionSource{Revision: sourceRevision, Values: sourceValues, Raw: raw, HistorySources: historySources}, nil
		},
		Readable: func(m platform.Member, ref string, at time.Time) bool {
			return t.mayReadIn(t.Projection, m, at, false)(ref)
		}, Closure: t.functionClosure, BindApplications: func(ref platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
			return t.bindApplications(ref, release, dependencies)
		},
		Model: func(c platform.Caller, r *pb.ChangeRecord, q platform.Request, pending []platform.Effect, options platform.ModelOptions) (agentowner.FunctionDispatch, *kernel.Error) {
			effect := t.planModelRequest(c, r, q, pending)
			var ask aigateway.ModelAsk
			_ = json.Unmarshal([]byte(effect.Body), &ask)
			binding, provider, problem := t.modelResolver().Resolve("generation", ask.Model, options)
			if problem != nil {
				return agentowner.FunctionDispatch{}, problem
			}
			stage := platform.AIStageRuntime
			if t.simulation {
				stage = platform.AIStageDesign
			}
			binding, problem = t.modelResolver().Stage(binding, provider, stage)
			if problem != nil {
				return agentowner.FunctionDispatch{}, problem
			}
			return agentowner.FunctionDispatch{Model: ask.Model, Binding: binding, Effect: effect}, nil
		}, Bind: func(effect platform.Effect, binding agentowner.FunctionBinding, model platform.AIModelBinding) platform.Effect {
			var ask aigateway.ModelAsk
			_ = json.Unmarshal([]byte(effect.Body), &ask)
			ask.Function = &binding
			ask.Binding = &model
			ask.ConfigurationError = ""
			body, _ := json.Marshal(ask)
			effect.Body = string(body)
			return effect
		},
	}
	return agentowner.PlanFunction(ctx, c, r, request, pending)
}

func (t *Tenant) functionAllowed(app string, ask aigateway.ModelAsk, at time.Time) bool {
	ctx := agentowner.FunctionAccess{Tenant: t.ID, Member: t.Member, AuthorityOf: t.AuthorityOf, Suspended: t.suspended,
		Authorize: func(member platform.Member, owner string, f platform.AIFunction, sources []string, definition string, at time.Time) bool {
			c := platform.NewCaller(runtime{t: t, scoped: true}, member, owner, false, false)
			return t.authorizeCapability(c, owner, f.Name, f.Roles, true, authorizationDetails{At: at, Sources: sources, Definition: definition}).Allow
		},
		Source: func(typ, id string) (platform.EntityInfo, bool) {
			t.Projection.Lock()
			defer t.Projection.Unlock()
			et := t.Projection.Type(typ)
			if et == nil {
				return platform.EntityInfo{}, false
			}
			row := et.Rows().Get(id)
			return et.Info(), row != nil && !records.Header(row.Value).Archived
		},
	}
	return agentowner.FunctionAllowed(ctx, app, ask.Function, ask.Model, ask.Record, ask.Call, ask.Prompt, at)
}

func (h hostView) ValidateInstallFunction(f platform.AIFunction) error {
	if err := f.Check(); err != nil {
		return err
	}
	manifest := h.app.Manifest()
	h.t.Projection.Lock()
	defer h.t.Projection.Unlock()
	et := h.t.Projection.Type(f.Object)
	if et == nil || et.Info().App != manifest.ID {
		return fmt.Errorf("function %s needs its owner's source object", f.Name)
	}
	for _, role := range f.Roles {
		if !slices.Contains(manifest.AllRoles(), role) {
			return fmt.Errorf("function %s names an undeclared role", f.Name)
		}
	}
	for _, name := range f.Fields {
		field, ok := et.Info().Field(name)
		if !ok || field.AIInput != nil && !*field.AIInput || !slices.Contains([]string{"text", "longtext", "choice", "integer", "decimal", "boolean"}, field.Type) || slices.Contains(h.t.narrowable(et), name) {
			return fmt.Errorf("function %s needs direct scalar source fields", f.Name)
		}
	}
	return nil
}

func (h hostView) InstallFunction(c platform.Caller, f platform.AIFunction, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if version < 1 {
		return fmt.Errorf("function %s needs a published version", f.Name)
	}
	if err := h.ValidateInstallFunction(f); err != nil {
		return err
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetFunction, Name: f.Name}
	def := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".function-" + strconv.Itoa(version), ContractVersion: 1, Function: &f}
	def, err := h.t.describer().DescribeDefinition(def)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if i >= 0 && h.t.definitions[i].Source != "tenant" {
		return fmt.Errorf("function %s is owned by code", ref)
	}
	for i := range h.t.definitions {
		d := &h.t.definitions[i]
		if d.Ref.App == ref.App && d.Source == "code" && d.Action != nil {
			if action, ok := h.app.Manifest().Actions.Action(d.Ref.Name); ok {
				d.Action = &action
			}
		}
	}
	h.t.storeDefinition(i, def)
	return nil
}

func (h hostView) Function(app, name string, version int) (platform.AIFunction, int, bool) {
	return agentowner.FunctionDefinition(h.t.App(app), name, version)
}

func (t *Tenant) functionClosure(app string, f platform.AIFunction, version int, retained *string) (platform.ReleaseCandidate, string, error) {
	body, err := json.Marshal(f)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	sourceVersion := ""
	if version > 0 {
		sourceVersion = t.App(app).Manifest().Version + ".function-" + strconv.Itoa(version)
	}
	return t.capabilityClosure(platform.AssetRef{App: app, Kind: platform.AssetFunction, Name: f.Name}, body, sourceVersion, retained)
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
