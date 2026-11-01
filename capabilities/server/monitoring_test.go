package platformserver

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
	"platformserver/engine/authorization"
	"platformserver/engine/automation"
	"platformserver/engine/business"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"platformserver/studio/authoring"
	"platformserver/studio/build"
	"platformserver/studio/packages"
)

type monitoringFixture struct {
	ledger *platform.Ledger
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
func (a *monitoringFixture) Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}
func (a *monitoringFixture) Manifest() platform.Manifest {
	return platform.Manifest{ID: "probe", Title: "Probe", Version: "1", Actions: a.ledger.Catalog, Roles: []string{"runner"},
		Jobs: []platform.Job{{Name: "failed", Title: "Failed probe job", Every: time.Minute}},
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
	services := platform.DefaultRuntimeCapabilities()
	services.BindOwner = invariantBindOwner
	services.NewAgentEvaluator = authoring.NewAgentEvaluator
	services.NewTransactionExecutor = business.NewRuntime
	console := controlowner.NewConsole(id, func(id string) controlowner.Owners {
		actions := append(platform.AccessActions(), platform.ProjectActions()...)
		actions = append(actions, packages.Actions()...)
		return controlowner.Owners{Policies: authorization.NewPolicies(), Packages: packages.New(id), Actions: actions}
	}, platform.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin, build.ID: build.Builder, "probe": "runner"}}},
		platform.Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, "probe": "runner"}}},
		platform.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: build.Builder, "probe": "runner"}}},
		platform.Seat{Subjects: []string{"scoped"}, Member: platform.Member{ID: "scoped", Roles: map[string]string{build.ID: build.Builder, controlowner.PlatformApp: controlowner.Admin}, Scopes: []string{build.SchemaSourcePulled}}},
		platform.Seat{Subjects: []string{"user"}, Member: platform.Member{ID: "user", Roles: map[string]string{}}})
	probe := &monitoringFixture{ledger: platform.NewLedger(id, "probe", platform.NewCatalog())}
	tenant, err := NewTenantWithCapabilities(id, services, console, build.New(id), probe)
	if err != nil {
		tb.Fatal(err)
	}
	live := &invariantTenant{Tenant: tenant}
	tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		live.entries = append(live.entries, entry)
		return entry.Body, nil
	}
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
	sourceView, refusal := live.RecordOf(live.member(t), build.SourceType, "SRC1", now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	source := sourceView.Record.(build.Source)
	live.accept(t, build.SchemaSourcePulled, build.SourceType, "SRC1", map[string]any{"pull": build.SourcePull{At: invariantClock, DefinitionVersion: source.ExecutionVersion(), Error: "source failed"}})
	live.accept(t, "build.pipeline.create", build.PipelineType, "PL1", map[string]any{"name": "transform", "title": "Transform", "input": "DS1", "outputDataset": "DS2", "steps": []platform.TransformStep{{Kind: "select", Columns: []string{"id"}}}})
	live.accept(t, "build.pipeline.publish", build.PipelineType, "PL1", map[string]any{})
	pipelineView, refusal := live.RecordOf(live.member(t), build.PipelineType, "PL1", now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	pipeline := pipelineView.Record.(build.Pipeline)
	live.accept(t, build.SchemaPipelineRan, build.PipelineType, "PL1", map[string]any{"run": build.PipelineRun{At: invariantClock, DefinitionVersion: pipeline.ExecutionVersion(), Quarantined: 1}})
	live.accept(t, build.SchemaDatasetProjected, build.DatasetType, "DS1", map[string]any{"projection": build.DatasetProjection{Object: "build.alpha", Version: 1, At: invariantClock, Rows: 1, Refused: 1}})
	live.accept(t, "build.code.create", build.CodeType, "CODE1", map[string]any{"name": "algorithm", "title": "Algorithm", "language": "go", "source": "package main\nfunc main() {}", "input": platform.ValueSchema{Type: "string"}, "output": platform.ValueSchema{Type: "string"}, "roles": []string{build.Builder}, "limits": platform.OperationLimits{TimeoutMillis: 100, MemoryPages: 1, MaxInputBytes: 1024, MaxOutputBytes: 1024}})
	live.accept(t, build.SchemaCodeCompile, build.CodeType, "CODE1", map[string]any{})
	codeView, refusal := live.RecordOf(live.member(t), build.CodeType, "CODE1", now)
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
	codeView, refusal = live.RecordOf(live.member(t), build.CodeType, "CODE1", now)
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
		for _, source := range []string{"delivery-attempts", "flow", "agent"} {
			if !slices.ContainsFunc(adminPage.Coverage, func(c MonitoringCoverage) bool {
				return c.Source == source && !c.Readable && c.Limitation == "not-included"
			}) {
				t.Fatalf("missing explicit coverage for %s", source)
			}
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
		value, refusal := live.Read(admin, "work")
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
		_, refusal := recordRead(live.Tenant, context.Background(), func() (MonitoringRunPage, *kernel.Error) {
			return live.monitoringRuns(platform.Member{ID: "admin", Tenant: "another-tenant", Roles: admin.Roles}, MonitoringRunFilter{Limit: 50}, now)
		})
		if refusal == nil {
			t.Fatal("another tenant was admitted to monitoring")
		}
	})
}
