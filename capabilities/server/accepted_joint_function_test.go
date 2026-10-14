package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// A committed activation must recover with its measured gate and shared
// page/flow closure even if the process dies before applying the pointer.
func TestJournalAcceptedJointFunctionActivationCrash(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	journal, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("joint-function-crash-%d", time.Now().UnixNano())
	defer journal.Pool().Exec(ctx, `delete from journal where tenant=$1`, id)
	if _, err := journal.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	targetID := id + "-target"
	defer journal.Pool().Exec(ctx, `delete from journal where tenant=$1`, targetID)
	compose := func(tenant string) *Tenant {
		t.Helper()
		tn, err := NewTenant(tenant, NewConsole(tenant,
			Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{
				build.ID: build.Builder, PlatformApp: Admin, ai.ID: ai.Admin, flow.ID: flow.Admin, AgentApp: AgentAdmin}}},
			Seat{Subjects: []string{"operator"}, Member: platform.Member{ID: "operator", Roles: map[string]string{build.ID: build.User}}}),
			ai.New(tenant), work.New(tenant), flow.New(tenant), build.New(tenant), NewAgents(tenant))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": `{"summary":"Check source","category":"review","review":true}`}}},
			"usage":   map[string]any{"prompt_tokens": 4, "completion_tokens": 8, "cost": 0.01},
		})
	}))
	defer provider.Close()
	live := compose(id)
	live.Record = func(e Entry) {
		if err := journal.Append(ctx, id, e); err != nil {
			t.Fatal(err)
		}
	}
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return journal.AppendAccepted(ctx, id, e, key, hash)
	}
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	builder, _ := live.Member("builder")
	key := 0
	submit := func(app, schema, typ, target string, payload any) {
		t.Helper()
		key++
		_, refusal := live.Submit(builder, &pb.Submission{TenantId: id, PrincipalId: builder.ID, Authority: app,
			IdempotencyKey: fmt.Sprint(key), Schema: &pb.SchemaRef{Name: schema, Version: 1},
			Target: &pb.EntityRef{Type: typ, Id: target}, Payload: platform.Raw(payload)}, at)
		if refusal != nil {
			t.Fatalf("%s: %v", schema, refusal)
		}
	}
	submit(build.ID, build.ObjectType+".create", build.ObjectType, "O", map[string]any{
		"name": "intake", "title": "Intake", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}},
		"states":  []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}},
		"actions": []build.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done"}},
	})
	submit(build.ID, build.SchemaPublish, build.ObjectType, "O", struct{}{})
	function := platform.RecordAdviceFunction("build.intake", []string{"note"}, []string{build.User, build.Builder})
	function.Name, function.Model = "advice", "fixture/probe"
	submit(build.ID, build.FunctionType+".create", build.FunctionType, "F", function)
	submit(build.ID, build.SchemaFunction, build.FunctionType, "F", struct{}{})
	submit(build.ID, build.PageType+".create", build.PageType, "PAGE", map[string]any{
		"name": "intakeadvice", "title": "Intake advice", "object": "build.intake",
		"sections": []build.Section{{Widget: "table", Fields: []string{"note"}},
			{Widget: "function", Function: &platform.FunctionRef{Name: "advice", Version: 1}}},
	})
	submit(build.ID, build.SchemaRelease, build.PageType, "PAGE", struct{}{})
	submit(build.ID, build.ProcessType+".create", build.ProcessType, "P", map[string]any{
		"name": "review", "title": "Review intake", "object": "build.intake", "when": "open",
		"steps": []build.ProcessStep{{Name: "infer", Kind: "ai", Function: &platform.FunctionRef{Name: "advice", Version: 1}, Next: "review"},
			{Name: "review", Kind: "ask", Ask: build.User, Answers: []string{"approve"}, Next: "close"}, {Name: "close", Kind: "action", Act: "close"}},
	})
	submit(build.ID, build.SchemaProcess, build.ProcessType, "P", struct{}{})
	submit(ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "fixture", map[string]string{"kind": "local", "baseUrl": provider.URL})
	submit(ai.ID, ai.SchemaModelEnable, ai.ModelType, "fixture/probe", map[string]string{"access": "users"})
	submit(build.ID, build.TestPlanType+".create", build.TestPlanType, "EVAL", map[string]any{
		"name": "quality", "title": "Synthetic quality", "function": "F", "model": "fixture/probe", "at": at,
		"steps": []build.TestStep{{Type: "build.intake", ID: "SAMPLE", Action: "build.intake.create",
			Payload: `{"note":"Synthetic"}`, Expect: "accepted"}},
		"evaluation": []build.EvaluationPolicy{{MinQuality: 1, MaxCostUSD: 0.05, MaxLatencyMillis: 300000,
			Cases: []build.EvaluationCase{{Name: "synthetic", Input: json.RawMessage(`{"note":"Synthetic"}`),
				Expected: json.RawMessage(`{"summary":"Check source","category":"review","review":true}`)}}}},
	})
	submit(build.ID, build.SchemaEvaluationSetPublish, build.TestPlanType, "EVAL", struct{}{})
	submit(build.ID, build.AgentType+".create", build.AgentType, "AGENT", map[string]any{"name": "assistant", "title": "Intake assistant", "description": "Prepare intake updates", "instructions": "Prepare a note update", "tools": []string{"build.intake.edit"}, "steps": 4, "tokens": 2000, "actions": 1, "cases": []build.AgentCase{{Name: "note", Goal: "Prepare a note", Takes: []string{"build.intake.edit"}}}})
	submit(build.ID, build.SchemaAgentPublish, build.AgentType, "AGENT", struct{}{})
	submit(build.ID, build.ConnectionType+".create", build.ConnectionType, "CONNECTION", map[string]any{"name": "feed", "title": "Feed", "kind": "http", "address": provider.URL + "/", "allowPrivate": true})
	submit(build.ID, build.ConnectionType+".check", build.ConnectionType, "CONNECTION", struct{}{})
	live.CheckConnections(at)
	submit(build.ID, build.SourceType+".create", build.SourceType, "SOURCE", map[string]any{"name": "feedrows", "title": "Feed rows", "connection": "CONNECTION", "profile": "json", "url": "rows", "object": "build.intake", "key": "id", "mapping": []build.SourceField{{From: "note", To: "note"}}})
	submit(build.ID, build.SourceType+".publish", build.SourceType, "SOURCE", struct{}{})
	submit(build.ID, build.AppType+".create", build.AppType, "APP", map[string]any{"name": "intakeapp", "title": "Intake application", "pages": []string{"intakeadvice"}, "resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetFlow, Name: "build.review"}, {App: build.ID, Kind: platform.AssetEvaluation, Name: "quality"}, {App: build.ID, Kind: platform.AssetAgent, Name: "assistant"}, {App: build.ID, Kind: platform.AssetSource, Name: "feedrows"}}, "profile": platform.ApplicationProfile{ExpectedRecords: map[string]int64{"build.intake": 2}, ExpectedRuns: 1, Retention: map[string]string{"runs": "24h"}, RPO: "1h", RTO: "30m"}})
	preview, err := live.PreviewRelease(builder, platform.AssetApp, "APP")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("joint candidate: %+v %v", preview, err)
	}
	seen := map[platform.AssetKind]bool{}
	for _, asset := range preview.Included {
		if asset.App == build.ID {
			seen[asset.Kind] = true
		}
	}
	if !seen[platform.AssetFunction] || !seen[platform.AssetPage] || !seen[platform.AssetFlow] || !seen[platform.AssetEvaluation] || !seen[platform.AssetAgent] || !seen[platform.AssetSource] || !seen[platform.AssetConnection] {
		t.Fatalf("joint candidate omitted a shared asset: %+v", preview.Included)
	}
	if _, err := live.SaveReleaseCandidate(builder, platform.AssetApp, "APP", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	if _, err := live.ActivateRelease(builder, preview.CandidateID, "before-evaluation", at); err == nil {
		t.Fatal("joint candidate activated without an evaluation")
	}
	submit(build.ID, build.TestPlanType+".edit", build.TestPlanType, "EVAL", map[string]any{"model": "not-enabled/new-draft", "evaluation": []build.EvaluationPolicy{{MinQuality: 1, MaxCostUSD: 0.05, MaxLatencyMillis: 300000, Cases: []build.EvaluationCase{{Name: "newdraft", Input: json.RawMessage(`{"note":"New draft"}`), Expected: json.RawMessage(`{"summary":"Different","category":"review","review":false}`)}}}}})
	reportID, err := live.EvaluateRelease(builder, ReleaseEvaluationRequest{CandidateID: preview.CandidateID, PlanID: "EVAL", Key: "evaluate"}, at)
	if err != nil {
		t.Fatal(err)
	}
	activeJournal := journal
	attempts := 0
	for _, effect := range live.Effects(at) {
		var ask modelAsk
		if json.Unmarshal([]byte(effect.Body), &ask) != nil || !ask.Evaluation || !strings.HasPrefix(ask.Call, reportID+":") {
			continue
		}
		outcome, usage := live.sendModel(effect, at)
		if outcome.Result != "delivered" || usage == nil || !usage.CostReported {
			t.Fatalf("measured call: %+v %+v", outcome, usage)
		}
		if attempts == 0 {
			// Losing the append must not expose the report, meter or effect.
			position := activeJournal.Position(id)
			live.AcceptResult = func(Entry, string, string) ([]byte, error) {
				return nil, errors.New("injected evaluation answer append failure")
			}
			live.settleWithUsage(effect.ID, outcome, usage, at)
			unchanged, ok := platform.Get[build.Evaluation](live.automation(build.ID, false), reportID)
			if !ok || unchanged.Attempts[0].Outcome != "" || len(live.ai.Usage()) != 0 || activeJournal.Position(id) != position {
				t.Fatalf("failed append exposed evaluation state: %+v", unchanged)
			}
			live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
				return activeJournal.AppendAccepted(ctx, id, e, key, hash)
			}
		}
		if attempts == 1 {
			// The next answer commits, then the process disappears before apply.
			live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
				if _, err := activeJournal.AppendAccepted(ctx, id, e, key, hash); err != nil {
					t.Fatal(err)
				}
				panic("crash after evaluation answer append")
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("evaluation answer crash was not injected")
					}
				}()
				live.settleWithUsage(effect.ID, outcome, usage, at)
			}()
			unapplied, ok := platform.Get[build.Evaluation](live.automation(build.ID, false), reportID)
			if !ok || unapplied.Attempts[1].Outcome != "" || len(live.ai.Usage()) != 1 {
				t.Fatalf("unapplied evaluation answer became visible: %+v", unapplied)
			}
			reopened, err := OpenJournal(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			entries, err := reopened.Entries(ctx, id, 0)
			if err != nil {
				t.Fatal(err)
			}
			recovered := compose(id)
			recovered.AIClient = func(*http.Request) (*http.Response, error) {
				t.Error("evaluation replay called the provider")
				return nil, errors.New("no provider I/O during replay")
			}
			if err := recovered.Replay(entries); err != nil {
				t.Fatal(err)
			}
			replayed, ok := platform.Get[build.Evaluation](recovered.automation(build.ID, false), reportID)
			if !ok || replayed.Attempts[1].Outcome != "accepted" || len(recovered.ai.Usage()) != 2 {
				t.Fatalf("evaluation answer and meter did not recover together: %+v", replayed)
			}
			recovered.AIClient = nil
			activeJournal = reopened
			recovered.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
				return activeJournal.AppendAccepted(ctx, id, e, key, hash)
			}
			live = recovered
		} else {
			live.settleWithUsage(effect.ID, outcome, usage, at)
		}
		attempts++
	}
	report, ok := platform.Get[build.Evaluation](live.automation(build.ID, false), reportID)
	if attempts != build.EvaluationRepeats || !ok || report.State != "passed" || report.Set == nil || report.Model != "fixture/probe" || report.Policy[0].Cases[0].Name != "synthetic" {
		t.Fatalf("joint evaluation: %+v (%d attempts)", report, attempts)
	}
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		if _, err := activeJournal.AppendAccepted(ctx, id, e, key, hash); err != nil {
			t.Fatal(err)
		}
		panic("crash after activation append")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("activation crash was not injected")
			}
		}()
		_, _ = live.ActivateRelease(builder, preview.CandidateID, "activate", at)
	}()
	if live.ActiveRelease() != "" {
		t.Fatal("unapplied activation became visible")
	}
	reopened, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	recovered := compose(id)
	recovered.AIClient = func(*http.Request) (*http.Response, error) {
		t.Error("replay called the provider")
		return nil, errors.New("no provider I/O during replay")
	}
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if recovered.ActiveRelease() != preview.CandidateID ||
		!bytes.Equal(recovered.releases.candidates[preview.CandidateID], live.releases.candidates[preview.CandidateID]) {
		t.Fatal("recovery lost the committed joint release")
	}
	replayedReport, ok := platform.Get[build.Evaluation](recovered.automation(build.ID, false), reportID)
	if !ok || replayedReport.State != "passed" || len(replayedReport.Attempts) != build.EvaluationRepeats {
		t.Fatalf("recovery lost the measured gate: %+v", replayedReport)
	}
	recovered.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return reopened.AppendAccepted(ctx, id, e, key, hash)
	}
	recoveredBuilder, _ := recovered.Member("builder")
	if activated, err := recovered.ActivateRelease(recoveredBuilder, preview.CandidateID, "activate", at.Add(time.Second)); err != nil ||
		activated != preview.CandidateID || reopened.Position(id) != int64(len(entries)) {
		t.Fatalf("retry duplicated or lost the activation: %s %v", activated, err)
	}
	root := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: "intakeapp"}
	sourceRuntime, refusal := recovered.ApplicationRuntimeOf(recoveredBuilder, root, at)
	if refusal != nil {
		assets, assetErr := recovered.releaseAssetsLocked(nil, false)
		closure, closureErr := recovered.candidateWithBindings([]platform.AssetRef{root}, assets, []platform.AssetRef{root})
		_, artifactErr := platform.ArtifactOfApplication(root, closure.Assets)
		t.Fatalf("application runtime: %s; owner assets: %v; closure: %v; artifact: %v", refusal.Message, assetErr, closureErr, artifactErr)
	}
	if refusal != nil || len(sourceRuntime.Artifact.Model) == 0 || len(sourceRuntime.Artifact.Rules) == 0 || len(sourceRuntime.Artifact.AI) != 3 || len(sourceRuntime.Artifact.Integrations) != 2 || len(sourceRuntime.Monitoring.AgentBudgets) != 1 || len(sourceRuntime.Maintenance.Retention) != 1 {
		t.Fatalf("full application artifact: %+v %v", sourceRuntime, refusal)
	}
	if bytes.Contains(recovered.releases.raw(preview.CandidateID), []byte(provider.URL)) {
		t.Fatal("application artifact retained an environment address")
	}
	target := compose(targetID)
	if _, err := reopened.Entries(ctx, targetID, 0); err != nil {
		t.Fatal(err)
	}
	target.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		raw, err := reopened.AppendAccepted(ctx, targetID, e, key, hash)
		if err != nil {
			t.Fatalf("target accepted append: %v", err)
		}
		return raw, err
	}
	targetMember := memberOf(t, target, "builder")
	if _, err := PromoteCandidate(recovered, target, preview.CandidateID, "missing-model", true, targetMember, at); err == nil || target.ActiveRelease() != "" {
		t.Fatal("target accepted missing model bindings or installed a partial application")
	}
	decide(t, target, "builder", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "fixture", map[string]string{"kind": "local", "baseUrl": provider.URL}, at)
	decide(t, target, "builder", ai.ID, ai.SchemaModelEnable, ai.ModelType, "fixture/probe", map[string]string{"access": "users"}, at)
	if _, err := PromoteCandidate(recovered, target, preview.CandidateID, "missing-connection", true, targetMember, at); err == nil || target.ActiveRelease() != "" {
		t.Fatal("target accepted a missing connection or installed a partial application")
	}
	decide(t, target, "builder", build.ID, build.ConnectionType+".create", build.ConnectionType, "TARGET-CONNECTION", map[string]any{"name": "feed", "title": "Target feed", "kind": "http", "address": provider.URL + "/", "allowPrivate": true}, at)
	decide(t, target, "builder", build.ID, build.ConnectionType+".check", build.ConnectionType, "TARGET-CONNECTION", map[string]any{}, at)
	target.CheckConnections(at)
	if _, err := PromoteCandidate(recovered, target, preview.CandidateID, "deliver", true, targetMember, at); err != nil {
		t.Fatal(err)
	}
	targetRuntime, refusal := target.ApplicationRuntimeOf(targetMember, root, at)
	if refusal != nil || targetRuntime.Artifact.Version != sourceRuntime.Artifact.Version || target.ActiveRelease() != preview.CandidateID {
		t.Fatalf("second environment changed the artifact: %+v %v", targetRuntime, refusal)
	}
	for i := 0; i < 3; i++ {
		decide(t, target, "operator", build.ID, "build.intake.create", "build.intake", fmt.Sprintf("ROW-%d", i), map[string]any{"note": fmt.Sprintf("Original %d", i)}, at.Add(time.Second))
	}
	// The complete artifact moves again through the same migration owner; a
	// target with rows must not silently install the changed write semantics.
	decide(t, recovered, "builder", build.ID, build.ObjectType+".edit", build.ObjectType, "O", map[string]any{"fields": []map[string]any{{"name": "note", "title": "Original note", "type": "text", "readOnly": true, "aliasOf": "memo", "sourceType": "user-input"}, {"name": "memo", "title": "Memo", "type": "text", "sourceType": "user-input"}}}, at.Add(2*time.Second))
	upgradeDrafts := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetApp, ID: "APP"}}
	next, err := recovered.PreviewReleaseDrafts(recoveredBuilder, upgradeDrafts)
	if err != nil || next.Diagnostic != "" {
		t.Fatalf("complete upgrade preview: %+v %v", next, err)
	}
	if _, err := recovered.SaveReleaseCandidates(recoveredBuilder, upgradeDrafts, next.CandidateID, "save-upgrade", at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteCandidate(recovered, target, next.CandidateID, "unsafe-upgrade", true, targetMember, at.Add(2*time.Second)); err == nil || target.ActiveRelease() != preview.CandidateID {
		t.Fatal("target installed a storage change without its migration")
	}
	if _, err := PromoteCandidate(recovered, target, next.CandidateID, "stage-upgrade", false, targetMember, at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	plan := platform.MigrationPlan{From: preview.CandidateID, To: next.CandidateID, Scope: platform.MigrationScope{Type: "build.intake"}, Steps: []platform.MigrateStep{{Kind: "rename", Types: []string{"build.intake"}, Batch: 1, Fields: []platform.MigrateField{{From: "note", To: "memo"}}}}}
	migration, err := target.ReviewMigrationPlan(targetMember, plan, at.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	decide(t, target, "builder", build.ID, "build.migration.start", platform.MigrationRunType, "MIGRATE", map[string]any{"plan": plan, "review": migration.ID, "reviewedAt": migration.At}, at.Add(2*time.Second))
	for i := 0; i < 3; i++ {
		decide(t, target, "builder", build.ID, "build.migration.advance", platform.MigrationRunType, "MIGRATE", map[string]any{}, at.Add(3*time.Second))
	}
	plans, _, refusal := platform.Find[build.TestPlan](target.automation(build.ID, false), platform.Query{})
	if refusal != nil {
		t.Fatal(refusal)
	}
	planID := ""
	for _, plan := range plans {
		if plan.Name == "quality" {
			planID = plan.ID
		}
	}
	measured, err := target.EvaluateRelease(targetMember, ReleaseEvaluationRequest{CandidateID: next.CandidateID, PlanID: planID, Key: "target-upgrade"}, at.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	for _, effect := range target.Effects(at.Add(4 * time.Second)) {
		var ask modelAsk
		if json.Unmarshal([]byte(effect.Body), &ask) != nil || !ask.Evaluation || !strings.HasPrefix(ask.Call, measured+":") {
			continue
		}
		outcome, usage := target.sendModel(effect, at.Add(4*time.Second))
		if outcome.Result != "delivered" || usage == nil {
			t.Fatal("target measured evaluation did not complete")
		}
		target.settleWithUsage(effect.ID, outcome, usage, at.Add(4*time.Second))
	}
	if _, err := target.ActivateRelease(targetMember, next.CandidateID, "activate-upgrade", at.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	upgraded, refusal := target.ApplicationRuntimeOf(targetMember, root, at.Add(5*time.Second))
	if refusal != nil || upgraded.Previous != sourceRuntime.Artifact.Version || len(upgraded.Maintenance.Migrations) != 1 || upgraded.Maintenance.Migrations[0].State != "complete" || len(upgraded.Warnings) == 0 {
		t.Fatalf("complete artifact lost its migration or operating evidence: %+v %v", upgraded, refusal)
	}
	for i := 0; i < 3; i++ {
		view, refusal := target.RecordOf(targetMember, "build.intake", fmt.Sprintf("ROW-%d", i), at.Add(5*time.Second))
		if refusal != nil || !bytes.Contains(platform.Raw(view.Record), []byte(fmt.Sprintf(`"memo":"Original %d"`, i))) {
			t.Fatalf("target migration lost a record: %v", refusal)
		}
	}
	targetLog, err := reopened.Entries(ctx, targetID, 0)
	if err != nil {
		t.Fatal(err)
	}
	CheckReplay(t, target, targetLog, func() *Tenant { return compose(targetID) })
}
