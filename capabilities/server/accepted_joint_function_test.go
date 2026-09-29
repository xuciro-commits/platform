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
	defer journal.pool.Exec(ctx, `delete from journal where tenant=$1`, id)
	if _, err := journal.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	compose := func() *Tenant {
		t.Helper()
		tn, err := NewTenant(id, NewConsole(id,
			Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{
				build.ID: build.Builder, PlatformApp: Admin, ai.ID: ai.Admin, flow.ID: flow.Admin}}},
			Seat{Subjects: []string{"operator"}, Member: platform.Member{ID: "operator", Roles: map[string]string{build.ID: build.User}}}),
			ai.New(id), work.New(id), flow.New(id), build.New(id))
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
	live := compose()
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
		"steps": []build.ProcessStep{{Name: "infer", Function: &platform.FunctionRef{Name: "advice", Version: 1}, Next: "review"},
			{Name: "review", Ask: build.User, Answers: []string{"approve"}, Next: "close"}, {Name: "close", Act: "close"}},
	})
	submit(build.ID, build.SchemaProcess, build.ProcessType, "P", struct{}{})
	submit(ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "fixture", map[string]string{"kind": "local", "baseUrl": provider.URL})
	submit(ai.ID, ai.SchemaModelEnable, ai.ModelType, "fixture/probe", map[string]string{"access": "users"})
	submit(build.ID, build.TestPlanType+".create", build.TestPlanType, "EVAL", map[string]any{
		"title": "Synthetic quality", "function": "F", "model": "fixture/probe", "at": at,
		"steps": []build.TestStep{{Type: "build.intake", ID: "SAMPLE", Action: "build.intake.create",
			Payload: `{"note":"Synthetic"}`, Expect: "accepted"}},
		"evaluation": []build.EvaluationPolicy{{MinQuality: 1, MaxCostUSD: 0.05, MaxLatencyMillis: 300000,
			Cases: []build.EvaluationCase{{Name: "synthetic", Input: json.RawMessage(`{"note":"Synthetic"}`),
				Expected: json.RawMessage(`{"summary":"Check source","category":"review","review":true}`)}}}},
	})
	preview, err := live.PreviewRelease(builder, platform.AssetObject, "O")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("joint candidate: %+v %v", preview, err)
	}
	seen := map[platform.AssetKind]bool{}
	for _, asset := range preview.Included {
		if asset.App == build.ID && (asset.Kind == platform.AssetFunction || asset.Kind == platform.AssetPage || asset.Kind == platform.AssetFlow) {
			seen[asset.Kind] = true
		}
	}
	if !seen[platform.AssetFunction] || !seen[platform.AssetPage] || !seen[platform.AssetFlow] {
		t.Fatalf("joint candidate omitted a shared asset: %+v", preview.Included)
	}
	if _, err := live.SaveReleaseCandidate(builder, platform.AssetObject, "O", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	if _, err := live.ActivateRelease(builder, preview.CandidateID, "before-evaluation", at); err == nil {
		t.Fatal("joint candidate activated without an evaluation")
	}
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
			recovered := compose()
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
	if attempts != build.EvaluationRepeats || !ok || report.State != "passed" {
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
	recovered := compose()
	recovered.AIClient = func(*http.Request) (*http.Response, error) {
		t.Error("replay called the provider")
		return nil, errors.New("no provider I/O during replay")
	}
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if recovered.ActiveRelease() != preview.CandidateID ||
		!bytes.Equal(recovered.releaseCandidates[preview.CandidateID], live.releaseCandidates[preview.CandidateID]) {
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
}
