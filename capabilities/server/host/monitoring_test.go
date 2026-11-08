package host

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai/runtime"
	"platformserver/services/automate/runtime"
	"platformserver/services/devops/packages"
	"platformserver/services/functions/authoring"
	"platformserver/services/resources/authoring"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	"platformserver/services/work"
)

type monitoringFixture struct {
	ledger      *platform.Ledger
	undoBlocked bool
}

type monitoringEvent struct {
	platform.Record
	Title string `json:"title"`
}

func (a *monitoringFixture) AcceptedLedger() *platform.Ledger { return a.ledger }
func (a *monitoringFixture) Declarations() []*pb.AuthorityDeclaration {
	return a.ledger.Declarations()
}
func (a *monitoringFixture) SnapshotFormat() string { return "platform.ledger.v1" }
func (a *monitoringFixture) Snapshot() (json.RawMessage, error) {
	return a.ledger.Snapshot()
}
func (a *monitoringFixture) Restore(raw json.RawMessage) error { return a.ledger.Restore(raw) }
func (a *monitoringFixture) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if s.GetSchema().GetName() == "probe.undo" && a.undoBlocked {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The probe undo failed")
		}
		return func(r *pb.ChangeRecord) {
			c.Put(r, monitoringEvent{Record: platform.Record{ID: s.GetTarget().GetId()}, Title: s.GetSchema().GetName()})
		}, nil
	})
}
func (a *monitoringFixture) Manifest() platform.Manifest {
	failed := platform.Step{Name: "fail", End: true, NoRetry: true, Output: func(platform.Caller, *platform.Run) (json.RawMessage, *kernel.Error) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The probe Flow failed")
	}}
	target := func(_ platform.Caller, r *platform.Run) string { return "step:" + r.ID }
	var flows []platform.Flow
	for _, name := range []string{"waiting", "done", "compensated", "rollback"} {
		flow := platform.Flow{Name: name, Title: "Probe " + name, Version: 1, Owners: []string{"runner"}, Start: platform.Start{
			On: []string{"probe.start"}, Begin: func(_ platform.Caller, e platform.Event) (string, any, bool) {
				var input struct{ Flow string }
				json.Unmarshal(e.Record.GetSubmission().GetPayload(), &input)
				return e.Record.GetSubmission().GetTarget().GetId(), nil, input.Flow == name
			},
		}}
		switch name {
		case "waiting":
			flow.Steps = []platform.Step{{Name: "wait", Wait: &platform.Wait{Until: func(platform.Caller, *platform.Run) bool { return false }}}}
		case "done":
			flow.Steps = []platform.Step{{Name: "end", End: true}}
		case "compensated":
			flow.Steps = []platform.Step{failed}
		case "rollback":
			flow.Steps = []platform.Step{{Name: "act", Act: &platform.Act{Action: "probe.step", Target: target}, Undo: &platform.Act{Action: "probe.undo", Target: target}, Next: "fail"}, failed}
		}
		flows = append(flows, flow)
	}
	return platform.Manifest{ID: "probe", Title: "Probe", Version: "1", Actions: a.ledger.Catalog, Roles: []string{"runner"},
		Entities: []platform.Entity{{Type: "probe.event", Title: "Probe event", Model: monitoringEvent{}, Display: "title"}}, Flows: flows,
		Agents: []platform.Agent{{Name: "helper", Title: "Probe helper", Instructions: "Answer the goal", Budget: platform.Budget{Steps: 10, Tokens: 1000, Actions: 10}}},
		Jobs:   []platform.Job{{Name: "failed", Title: "Failed probe job", Every: time.Minute}},
		Operations: []platform.Operation{{Name: "echo", Title: "Echo", Roles: []string{"runner"},
			Input: platform.ValueSchema{Type: "string"}, Output: platform.ValueSchema{Type: "string"}, Binding: platform.OperationBinding{Kind: "native"},
			Limits: platform.OperationLimits{TimeoutMillis: 100, MemoryPages: 1, MaxInputBytes: 1024, MaxOutputBytes: 1024}}}}
}
func (a *monitoringFixture) Run(platform.Caller, string, time.Time) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The probe job failed")
}
func (a *monitoringFixture) Compute(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
	return input, nil
}

func newMonitoringTenant(tb testing.TB) *invariantTenant {
	tb.Helper()
	id := "monitoring-invariant"
	services := platform.DefaultRuntimeCapabilities(security.Standard{})
	services.BindOwner = invariantBindOwner
	services.NewAgentEvaluator = authoring.NewAgentEvaluator
	services.NewTransactionExecutor = business.NewRuntime
	console := controlowner.NewConsole(id, func(id string) controlowner.Owners {
		actions := append(platform.AccessActions(), platform.ProjectActions()...)
		actions = append(actions, packages.Actions()...)
		return controlowner.Owners{Policies: security.NewPolicies(), Packages: packages.New(id), Actions: actions}
	}, platform.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin, build.ID: build.Builder, "probe": "runner", platform.FlowApp: automation.Admin, platform.AgentApp: platform.AgentAdmin}}},
		platform.Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, "probe": "runner"}}},
		platform.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: build.Builder, "probe": "runner"}}},
		platform.Seat{Subjects: []string{"scoped"}, Member: platform.Member{ID: "scoped", Roles: map[string]string{build.ID: build.Builder, controlowner.PlatformApp: controlowner.Admin, platform.FlowApp: automation.Admin, platform.AgentApp: platform.AgentAdmin}, Scopes: []string{build.SchemaSourcePulled}}},
		platform.Seat{Subjects: []string{"user"}, Member: platform.Member{ID: "user", Roles: map[string]string{}}})
	var actions []platform.Action
	for _, schema := range []string{"probe.start", "probe.step", "probe.undo"} {
		actions = append(actions, platform.Action{Schema: schema, Target: "probe.event", Title: schema, Description: "Drive the monitoring owner boundary probe", Roles: []string{"runner"}, Payload: []platform.Field{{Name: "flow", Type: "text"}}})
	}
	probe := &monitoringFixture{ledger: platform.NewLedger(id, "probe", platform.NewCatalog(actions...), "probe.event"), undoBlocked: true}
	tenant, err := NewTenantWithCapabilities(id, services, console, build.New(id), work.New(id), automation.New(id), agents.New(id), probe)
	if err != nil {
		tb.Fatal(err)
	}
	live := &invariantTenant{Tenant: tenant}
	tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		live.entries = append(live.entries, entry)
		return entry.Body, nil
	}
	seedInvariantProject(tb, live)
	return live
}

func TestInvariantK4MonitoringOwnerBoundaries(t *testing.T) {
	live := newMonitoringTenant(t)
	now := invariantClock.Add(10 * time.Minute)
	admin, _ := live.Member("admin")
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	live.accept(t, "build.alpha.create", "build.alpha", "A1", map[string]any{"note": "protected input"})
	live.accept(t, "build.dataset.create", build.DatasetType, "DS1", map[string]any{"name": "input", "title": "Input"})
	live.accept(t, "build.dataset.create", build.DatasetType, "DS2", map[string]any{"name": "output", "title": "Output"})
	live.accept(t, build.SchemaDatasetLoad, build.DatasetType, "DS1", map[string]any{"rows": []map[string]any{{"id": "A1", "note": "input"}}})
	live.accept(t, "build.source.create", build.SourceType, "SRC1", map[string]any{"name": "feed", "title": "Feed", "url": "https://example.test/data", "dataset": "DS1", "every": "1m"})
	live.accept(t, "build.source.publish", build.SourceType, "SRC1", map[string]any{})
	sourceView, refusal := live.RecordOf(t.Context(), live.member(t), build.SourceType, "SRC1", now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	source := sourceView.Record.(build.Source)
	live.accept(t, build.SchemaSourcePulled, build.SourceType, "SRC1", map[string]any{"pull": build.SourcePull{At: invariantClock, DefinitionVersion: source.ExecutionVersion(), Error: "source failed"}})
	live.accept(t, "build.pipeline.create", build.PipelineType, "PL1", map[string]any{"name": "transform", "title": "Transform", "input": "DS1", "outputDataset": "DS2", "steps": []platform.TransformStep{{Kind: "select", Columns: []string{"id"}}}})
	live.accept(t, "build.pipeline.publish", build.PipelineType, "PL1", map[string]any{})
	pipelineView, refusal := live.RecordOf(t.Context(), live.member(t), build.PipelineType, "PL1", now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	pipeline := pipelineView.Record.(build.Pipeline)
	if refusal := live.pipelineResult(live.member(t), pipeline, build.PipelineRun{At: invariantClock, DefinitionVersion: pipeline.ExecutionVersion(), Quarantined: 1}, "finished", nil, invariantClock); refusal != nil {
		t.Fatal(refusal)
	}
	live.accept(t, build.SchemaDatasetProjected, build.DatasetType, "DS1", map[string]any{"projection": build.DatasetProjection{Object: "build.alpha", Version: 1, At: invariantClock, Rows: 1, Refused: 1}})
	live.accept(t, "build.code.create", build.CodeType, "CODE1", map[string]any{"name": "algorithm", "title": "Algorithm", "language": "go", "source": "package main\nfunc main() {}", "input": platform.ValueSchema{Type: "string"}, "output": platform.ValueSchema{Type: "string"}, "roles": []string{build.Builder}, "limits": platform.OperationLimits{TimeoutMillis: 100, MemoryPages: 1, MaxInputBytes: 1024, MaxOutputBytes: 1024}})
	live.accept(t, build.SchemaCodeCompile, build.CodeType, "CODE1", map[string]any{})
	codeView, refusal := live.RecordOf(t.Context(), live.member(t), build.CodeType, "CODE1", now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	code := codeView.Record.(build.Code)
	claimed, ok := live.claimOperation(code.Call, invariantClock)
	if !ok {
		t.Fatal("the accepted compilation could not be claimed")
	}
	live.settle(code.Call, platform.Outcome{Effect: code.Call, Generation: claimed.Generation, Result: "rejected", Detail: "compiler failed"}, invariantClock.Add(time.Second))
	if live.quarantined() {
		t.Fatal("retaining the compilation failure quarantined the tenant")
	}
	codeView, refusal = live.RecordOf(t.Context(), live.member(t), build.CodeType, "CODE1", now)
	if refusal != nil || codeView.Record.(build.Code).State != "failed" {
		for _, effect := range live.Effects(now) {
			if effect.ID == code.Call {
				t.Fatalf("compilation failure was not applied: state=%s callback=%s refusal=%v", effect.State, effect.CallbackError, refusal)
			}
		}
		t.Fatal("the compilation effect disappeared")
	}
	call, refusal := live.InvokeOperation(live.member(t), platform.OperationRequest{App: "probe", Name: "echo", Key: "builder-call", Inputs: platform.Raw("hello"), Sources: []string{"build.alpha/A1"}}, invariantClock.Add(2*time.Second))
	if refusal != nil {
		t.Fatal(refusal)
	}
	other, _ := live.Member("other")
	otherCall, refusal := live.InvokeOperation(other, platform.OperationRequest{App: "probe", Name: "echo", Key: "other-call", Inputs: platform.Raw("private")}, invariantClock.Add(3*time.Second))
	if refusal != nil {
		t.Fatal(refusal)
	}
	live.mu.Lock()
	job := live.work.Job("job:probe/failed")
	jobOutcome := live.acceptWork(job, invariantClock.Add(4*time.Second))
	live.mu.Unlock()
	if jobOutcome == "ok" || live.quarantined() {
		t.Fatalf("failed job outcome=%q quarantine=%v", jobOutcome, live.quarantined())
	}
	live.opsMu.Lock()
	live.outbound = append(live.outbound,
		&effect{Effect: platform.Effect{ID: "ordinary-delivery", Endpoint: "webhook", Event: "probe/notice", Target: "build.alpha/A1", At: invariantClock, State: "failed", Body: "hidden payload", Error: "hidden failure"}},
		&effect{Effect: platform.Effect{ID: "writeback-delivery", App: build.ID, Endpoint: build.WritebackEndpoint + "sample", Event: "writeback/sample", Target: "build.alpha/A1", At: invariantClock, Last: invariantClock, State: "rejected", Body: "hidden payload", Error: "hidden failure"}},
		&effect{Effect: platform.Effect{ID: "unknown-delivery", Endpoint: "webhook", Event: "probe/unknown", At: invariantClock, State: "unknown"}},
		&effect{Effect: platform.Effect{ID: "dispatch-delivery", App: build.ID, Endpoint: build.WritebackEndpoint + "dispatch", Event: build.ID + "/dispatch", At: invariantClock, State: "failed"}})
	live.opsMu.Unlock()
	host := NewHost(Tokens(map[string]string{"admin": "admin", "builder": "builder", "other": "other", "scoped": "scoped", "user": "user"}), live.Tenant)
	host.Now = func() time.Time { return now }
	handler := host.Handler()
	read := func(member, query string) MonitoringRunPage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/monitoring/runs"+query, nil)
		req.Header.Set("Authorization", "Bearer "+member)
		req.Header.Set(TenantHeader, live.ID)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s %s: HTTP %d: %s", member, query, recorder.Code, recorder.Body)
		}
		if strings.Contains(recorder.Body.String(), "hidden payload") || strings.Contains(recorder.Body.String(), "hidden failure") {
			t.Fatalf("%s received protected effect details", member)
		}
		var page MonitoringRunPage
		if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	find := func(page MonitoringRunPage, id string) MonitoringRun {
		t.Helper()
		for _, run := range page.Runs {
			if run.ID == id {
				return run
			}
		}
		t.Fatalf("missing %s in %+v", id, page.Runs)
		return MonitoringRun{}
	}
	t.Run("authorized owners and full overview", func(t *testing.T) {
		beforeRecords := live.recordImage(t)
		beforeEntries := len(live.entries)
		workBefore, err := live.work.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		adminPage := read("admin", "?limit=100")
		builderPage := read("builder", "?limit=100")
		userPage := read("user", "?limit=100")
		if userPage.Total != 0 || userPage.Overview != (MonitoringOverview{}) {
			t.Fatalf("unprivileged user received runs or counts: %+v", userPage)
		}
		if adminPage.Overview != (MonitoringOverview{FailedRuns: 7, DelayedSyncs: 1, FailedDeliveries: 3}) {
			t.Fatalf("administrator overview: %+v", adminPage.Overview)
		}
		if builderPage.Overview != (MonitoringOverview{FailedRuns: 5, DelayedSyncs: 1, FailedDeliveries: 2}) {
			t.Fatalf("builder overview: %+v", builderPage.Overview)
		}
		if find(builderPage, "compute:"+call.ID).Retryable {
			t.Fatal("compute call advertises an invented retry")
		}
		for _, page := range []MonitoringRunPage{adminPage, builderPage} {
			for _, run := range page.Runs {
				if run.ID == "compute:"+otherCall.ID || page.Overview == adminPage.Overview && run.ID == "compute:"+call.ID {
					t.Fatalf("another caller's compute appeared in %+v", page)
				}
			}
		}
		for _, run := range builderPage.Runs {
			if run.Source == "work" || run.ID == "effect:ordinary-delivery" || run.ID == "effect:unknown-delivery" {
				t.Fatalf("builder received administrator-only data: %+v", run)
			}
		}
		for _, source := range []string{"flow", "agent"} {
			if !slices.ContainsFunc(adminPage.Coverage, func(c MonitoringCoverage) bool {
				return c.Source == source && c.Readable && c.Basis == "retained" && c.Limitation == "permission-filtered"
			}) {
				t.Fatalf("missing explicit coverage for %s", source)
			}
		}
		if !slices.ContainsFunc(adminPage.Coverage, func(c MonitoringCoverage) bool {
			return c.Source == "delivery-attempts" && !c.Readable && c.Limitation == "not-included"
		}) {
			t.Fatal("delivery attempts coverage changed")
		}
		page1 := read("admin", "?limit=2")
		page2 := read("admin", "?offset=2&limit=2")
		if page1.Total != adminPage.Total || page2.Total != adminPage.Total || page1.Overview != adminPage.Overview || page2.Overview != adminPage.Overview || !reflect.DeepEqual(page1.Runs, adminPage.Runs[:2]) || !reflect.DeepEqual(page2.Runs, adminPage.Runs[2:4]) {
			t.Fatal("pagination changed the ordering, total or overview")
		}
		filtered := read("admin", "?type=delivery&status=failed&limit=1")
		if filtered.Total != 3 || len(filtered.Runs) != 1 || filtered.Runs[0].Type != "delivery" || filtered.Runs[0].Status != "failed" || filtered.Overview != adminPage.Overview {
			t.Fatalf("filtered page: %+v", filtered)
		}
		workAfter, err := live.work.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if len(live.entries) != beforeEntries || !reflect.DeepEqual(workBefore, workAfter) || len(differingTypes(beforeRecords, live.recordImage(t))) != 0 {
			t.Fatal("monitoring changed accepted records or work state")
		}
		if find(adminPage, "work:job:probe/failed").Status != "failed" || job.State != "scheduled" || job.Status != "" {
			t.Fatalf("job view changed the authoritative job or lost its failure: %+v", job)
		}
		value, refusal := live.Read(t.Context(), admin, "work")
		if refusal != nil {
			t.Fatal(refusal)
		}
		for _, task := range value.([]automation.Task) {
			if task.ID == job.ID && (task.Error != "" || task.Status != "failed") {
				t.Fatalf("work did not preserve safe status and withhold the raw error: %+v", task)
			}
		}
	})
	t.Run("original target authorization and retry route", func(t *testing.T) {
		page := read("admin", "?limit=100")
		for id, schema := range map[string]string{"source:SRC1": "build.source.pull", "code:CODE1": build.SchemaCodeCompile, "work:job:probe/failed": controlowner.SchemaWorkRetry, "effect:ordinary-delivery": controlowner.SchemaEffectRetry} {
			run := find(page, id)
			if !run.Retryable || run.RetrySchema != schema || run.RetryTarget == nil || run.RetrySchemaVersion == nil || run.RetrySchemaVersion.Check() != nil {
				t.Fatalf("missing original recovery descriptor for %s: %+v", id, run)
			}
		}
		if find(page, "effect:dispatch-delivery").Retryable || find(page, "effect:unknown-delivery").Retryable || find(page, "projection:DS1:build.alpha").Retryable {
			t.Fatal("an owner-specific or ambiguous outcome advertises generic retry")
		}
		scoped := read("scoped", "?limit=100")
		if find(scoped, "source:SRC1").Retryable || find(scoped, "effect:ordinary-delivery").Retryable {
			t.Fatal("a scoped identity received an unauthorized retry")
		}
		run := find(page, "effect:ordinary-delivery")
		submission := &pb.Submission{TenantId: live.ID, PrincipalId: admin.ID, Authority: controlowner.PlatformApp, IdempotencyKey: "monitoring-retry", Target: &pb.EntityRef{Type: run.RetryTarget.Type, Id: run.RetryTarget.ID}, Schema: &pb.SchemaRef{Name: run.RetrySchema, Version: run.RetrySchemaVersion.Family}, Payload: platform.Raw(map[string]any{})}
		raw, err := protojson.Marshal(submission)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer admin")
		req.Header.Set(TenantHeader, live.ID)
		req.Header.Set(SchemaVersionHeader, run.RetrySchemaVersion.Version)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK || find(read("admin", "?limit=100"), run.ID).Status != "pending" {
			t.Fatalf("original retry failed: HTTP %d: %s", recorder.Code, recorder.Body)
		}
	})
	t.Run("flow and agent owner authorization and recovery", func(t *testing.T) {
		beforeAdmin, beforeBuilder := read("admin", "?limit=100").Overview, read("builder", "?limit=100").Overview
		submit := func(member, app, schema, typ, id string, payload any) {
			t.Helper()
			m, _ := live.Member(member)
			s := &pb.Submission{TenantId: live.ID, PrincipalId: member, Authority: app, IdempotencyKey: schema + ":" + id,
				Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
			if _, refusal := live.Submit(m, s, now); refusal != nil {
				t.Fatalf("%s %s: %v", member, schema, refusal)
			}
		}
		startFlow := func(member, name, key string) string {
			t.Helper()
			submit(member, "probe", "probe.start", "probe.event", key, map[string]string{"flow": name})
			live.mu.Lock()
			defer live.mu.Unlock()
			for _, task := range slices.Clone(live.work.Queued(platform.FlowApp)) {
				if outcome := live.acceptWork(task, now); outcome != "ok" {
					t.Fatalf("Flow delivery: %s", outcome)
				}
			}
			return "probe." + name + ":" + key
		}
		waiting := startFlow("builder", "waiting", "waiting")
		done := startFlow("builder", "done", "done")
		compensated := startFlow("builder", "compensated", "compensated")
		rollback := startFlow("builder", "rollback", "rollback")
		cancelled := startFlow("builder", "waiting", "cancelled")
		submit("admin", platform.FlowApp, automation.SchemaFlowStop, platform.FlowInstanceType, cancelled, map[string]any{})
		privateFlow := startFlow("other", "compensated", "private-flow")
		for _, id := range []string{"running", "waiting", "done", "failed", "cancelled", "private-agent"} {
			member := "builder"
			if id == "private-agent" {
				member = "other"
			}
			submit(member, platform.AgentApp, platform.SchemaRunStart, platform.RunType, id, map[string]string{"agent": "probe.helper", "goal": "Goal " + id})
		}
		for _, step := range []platform.AgentResultStep{
			{Run: "waiting", Tool: "ask", Arguments: platform.Raw(map[string]string{"question": "What next?"})},
			{Run: "done", Content: "Completed the goal"},
			{Run: "failed", Stop: "the model failed 3 times"},
			{Run: "private-agent", Stop: "no model is set for agents"},
		} {
			if !live.applyAgentStep(step, now.Add(time.Second)) {
				t.Fatalf("the Agent result was not accepted: %+v", step)
			}
		}
		submit("builder", platform.AgentApp, platform.SchemaRunCancel, platform.RunType, "cancelled", map[string]any{})
		beforeRecords, beforeEntries := live.recordImage(t), len(live.entries)
		adminPage, builderPage, otherPage := read("admin", "?limit=100"), read("builder", "?limit=100"), read("other", "?limit=100")
		if len(live.entries) != beforeEntries || len(differingTypes(beforeRecords, live.recordImage(t))) != 0 {
			t.Fatal("monitoring changed Flow or Agent accepted records")
		}
		for _, page := range []MonitoringRunPage{adminPage, builderPage} {
			for id, want := range map[string]string{waiting: "pending", done: "ok", compensated: "failed", rollback: "running", cancelled: "cancelled"} {
				run := find(page, "flow:"+id)
				view, refusal := live.RecordOf(t.Context(), admin, platform.FlowInstanceType, id, now)
				if refusal != nil {
					t.Fatal(refusal)
				}
				x := view.Record.(platform.FlowInstance)
				ref := platform.AssetRef{App: "probe", Kind: platform.AssetFlow, Name: x.Flow}
				if run.Type != "automation" || run.Source != "flow" || run.Status != want || run.OwnerStatus != x.State || run.RecordRef != platform.FlowInstanceType+"/"+id || run.ResourceRef == nil || *run.ResourceRef != ref || run.Title != x.Title || !run.When.Equal(x.Changed.At) || run.Retryable {
					t.Fatalf("Flow owner projection: %+v; owner=%+v", run, x)
				}
			}
			for id, want := range map[string]string{"running": "running", "waiting": "pending", "done": "ok", "failed": "failed", "cancelled": "cancelled"} {
				run := find(page, "agent:"+id)
				view, refusal := live.RecordOf(t.Context(), admin, platform.RunType, id, now)
				if refusal != nil {
					t.Fatal(refusal)
				}
				x := view.Record.(platform.AgentRunRecord)
				ref := platform.AssetRef{App: "probe", Kind: platform.AssetAgent, Name: "helper"}
				if run.Type != "function" || run.Source != "agent" || run.Status != want || run.OwnerStatus != x.State || run.RecordRef != platform.RunType+"/"+id || run.ResourceRef == nil || *run.ResourceRef != ref || run.Title != x.Title || !run.When.Equal(x.Changed.At) || run.Retryable || run.RetrySchema != "" || run.RetryTarget != nil {
					t.Fatalf("Agent owner projection: %+v; owner=%+v", run, x)
				}
			}
		}
		find(adminPage, "flow:"+privateFlow)
		find(adminPage, "agent:private-agent")
		for _, run := range builderPage.Runs {
			if run.ID == "flow:"+privateFlow || run.ID == "agent:private-agent" {
				t.Fatalf("another participant's run was disclosed: %+v", run)
			}
		}
		for _, run := range otherPage.Runs {
			if (run.Source == "flow" || run.Source == "agent") && run.ID != "flow:"+privateFlow && run.ID != "agent:private-agent" {
				t.Fatalf("the other participant received builder's run: %+v", run)
			}
		}
		if page := read("user", "?limit=100"); page.Total != 0 || page.Overview != (MonitoringOverview{}) {
			t.Fatalf("an unrelated member received runs or counts: %+v", page)
		}
		for _, check := range []struct {
			before, after MonitoringOverview
			failed        int
		}{{beforeAdmin, adminPage.Overview, 4}, {beforeBuilder, builderPage.Overview, 2}} {
			if check.after.FailedRuns != check.before.FailedRuns+check.failed || check.after.DelayedSyncs != check.before.DelayedSyncs || check.after.FailedDeliveries != check.before.FailedDeliveries {
				t.Fatalf("Flow and Agent overview changed another owner's counts: %+v", check)
			}
		}
		for _, check := range []struct{ typ, id string }{{"automation", "flow:" + waiting}, {"function", "agent:running"}} {
			page := read("builder", "?type="+check.typ+"&limit=100")
			find(page, check.id)
			if page.Overview != builderPage.Overview {
				t.Fatal("type filtering changed the overview")
			}
		}
		for attempt := 1; attempt <= 4; attempt++ {
			live.mu.Lock()
			outcome := live.acceptWork(live.work.Job("job:flow/timers"), now.Add(time.Duration(attempt)*time.Minute))
			live.mu.Unlock()
			if outcome != "ok" {
				t.Fatalf("Flow undo retry %d: %s", attempt, outcome)
			}
		}
		page := read("admin", "?limit=100")
		run := find(page, "flow:"+rollback)
		if run.Status != "failed" || run.OwnerStatus != "stuck" || !run.Retryable || run.RetrySchema != automation.SchemaFlowRetry || run.RetryTarget == nil || *run.RetryTarget != (MonitoringRetryTarget{Type: platform.FlowInstanceType, ID: rollback}) || run.RetrySchemaVersion == nil || run.RetrySchemaVersion.Check() != nil || run.RetryKind != "retry" {
			t.Fatalf("the stuck Flow has no original recovery action: %+v", run)
		}
		if page.Overview.FailedRuns != adminPage.Overview.FailedRuns+1 || find(read("builder", "?limit=100"), run.ID).Retryable || find(read("scoped", "?limit=100"), run.ID).Retryable {
			t.Fatal("the stuck Flow lost its failure or disclosed an unauthorized retry")
		}
		for _, member := range []string{"builder", "admin"} {
			if member == "admin" {
				live.app("probe").(*monitoringFixture).undoBlocked = false
			}
			s := &pb.Submission{TenantId: live.ID, PrincipalId: member, Authority: platform.FlowApp, IdempotencyKey: "monitoring-flow-retry:" + member,
				Target: &pb.EntityRef{Type: run.RetryTarget.Type, Id: run.RetryTarget.ID}, Schema: &pb.SchemaRef{Name: run.RetrySchema, Version: run.RetrySchemaVersion.Family}, Payload: platform.Raw(map[string]any{})}
			raw, err := protojson.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(raw))
			req.Header.Set("Authorization", "Bearer "+member)
			req.Header.Set(TenantHeader, live.ID)
			req.Header.Set(SchemaVersionHeader, run.RetrySchemaVersion.Version)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			want := http.StatusForbidden
			if member == "admin" {
				want = http.StatusOK
			}
			if recorder.Code != want {
				t.Fatalf("%s Flow retry: HTTP %d: %s", member, recorder.Code, recorder.Body)
			}
		}
		after := read("admin", "?limit=100")
		recovered := find(after, run.ID)
		view, refusal := live.RecordOf(t.Context(), admin, platform.FlowInstanceType, rollback, now)
		if refusal != nil {
			t.Fatal(refusal)
		}
		x := view.Record.(platform.FlowInstance)
		if recovered.Status != "failed" || recovered.OwnerStatus != "compensated" || recovered.Retryable || after.Overview != page.Overview || !slices.ContainsFunc(x.Trace, func(line platform.FlowTraceLine) bool { return line.What == "retried" && line.By == "admin" }) || !slices.ContainsFunc(x.Trace, func(line platform.FlowTraceLine) bool { return line.What == "undone" }) {
			t.Fatalf("the Flow owner did not complete its original recovery: %+v; owner=%+v", recovered, x)
		}
	})
	t.Run("operation source permissions are rechecked", func(t *testing.T) {
		submission := &pb.Submission{TenantId: live.ID, PrincipalId: admin.ID, Authority: controlowner.PlatformApp, IdempotencyKey: "monitoring-revoke", Target: &pb.EntityRef{Type: controlowner.MemberType, Id: "builder"}, Schema: &pb.SchemaRef{Name: controlowner.SchemaRevoke, Version: 1}, Payload: platform.Raw(map[string]string{"app": build.ID, "role": build.Builder})}
		if _, refusal := live.Submit(admin, submission, now); refusal != nil {
			t.Fatal(refusal)
		}
		page := read("builder", "?limit=100")
		for _, run := range page.Runs {
			if run.ID == "compute:"+call.ID {
				t.Fatal("the compute result survived revocation of its protected source")
			}
		}
	})
	t.Run("query and authentication boundary", func(t *testing.T) {
		for _, query := range []string{"?type=flow", "?status=broken", "?offset=-1", "?limit=0", "?limit=101", "?offset=x", "?limit="} {
			req := httptest.NewRequest(http.MethodGet, "/v1/monitoring/runs"+query, nil)
			req.Header.Set("Authorization", "Bearer admin")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("%s: HTTP %d: %s", query, recorder.Code, recorder.Body)
			}
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/monitoring/runs", nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous monitoring: HTTP %d", recorder.Code)
		}
		_, refusal := recordRead(live.Tenant, context.Background(), func(t *Tenant) (MonitoringRunPage, *kernel.Error) {
			return t.monitoringRuns(platform.Member{ID: "admin", Tenant: "another-tenant", Roles: admin.Roles}, MonitoringRunFilter{Limit: 50}, now)
		})
		if refusal == nil {
			t.Fatal("another tenant was admitted to monitoring")
		}
	})
}
