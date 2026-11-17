package host

import (
	"encoding/json"
	"testing"
	"time"

	"platformserver/kernel/records"
	"platformserver/platform"
	functionauthoring "platformserver/services/functions/authoring"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK1FunctionAuthoringPublishesEvaluatesReviewsAndReplays(t *testing.T) {
	live := newInvariantTenant(t, "invariant-function-authoring")
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})

	declaration := func(instructions string) map[string]any {
		return map[string]any{
			"name": "answer", "title": "Order answer", "description": "Return a typed order answer", "object": "build.alpha",
			"fields": []string{"note"}, "instructions": instructions,
			"output": []platform.Field{{Name: "answer", Type: "string", Description: "Answer"}}, "roles": []string{build.Builder},
			"maxInputBytes": 1024, "maxOutputBytes": 1024, "maxTokens": 100,
		}
	}
	live.accept(t, "build.function.create", functionauthoring.FunctionType, "FUNCTION", declaration("Return the original answer."))
	live.accept(t, "build.function.publish", functionauthoring.FunctionType, "FUNCTION", map[string]any{})

	owner := live.App(build.ID).(*build.Build)
	ledger, err := owner.Authority().ForkRead()
	if err != nil {
		t.Fatal(err)
	}
	fork, err := owner.Fork(ledger)
	if err != nil {
		t.Fatal(err)
	}
	live.accept(t, "build.function.edit", functionauthoring.FunctionType, "FUNCTION", map[string]any{"instructions": "Return the revised answer."})

	preview, err := owner.DraftReleaseAssetsMulti([]build.JointDraftRef{{Kind: platform.AssetFunction, ID: "FUNCTION"}}, live.member(t), false)
	if err != nil {
		t.Fatal(err)
	}
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetFunction, Name: "answer"}
	var candidate platform.ReleaseAsset
	for _, asset := range preview.After {
		if asset.Ref == ref {
			candidate = asset
		}
	}
	var next platform.AIFunction
	if candidate.SourceVersion != "1.function-2" || json.Unmarshal(candidate.Body, &next) != nil || next.Instructions != "Return the revised answer." {
		t.Fatalf("function draft lost the edited version: %+v", candidate)
	}
	live.accept(t, "build.function.publish", functionauthoring.FunctionType, "FUNCTION", map[string]any{})

	functionVersion := func(app platform.App, version, expected int, instructions string) {
		t.Helper()
		definition, ordinal, ok := app.(platform.FunctionDefinitions).FunctionDefinition("answer", version)
		if !ok || ordinal != expected || definition.Instructions != instructions {
			t.Fatalf("function version %d: ordinal=%d found=%t definition=%+v", version, ordinal, ok, definition)
		}
	}
	functionVersion(owner, 1, 1, "Return the original answer.")
	functionVersion(owner, 0, 2, "Return the revised answer.")
	functionVersion(fork, 0, 1, "Return the original answer.")
	retained, err := owner.FunctionReleaseAsset("answer", "1.function-1")
	if err != nil {
		t.Fatalf("retained function asset: %v", err)
	}
	var retainedDefinition platform.AIFunction
	if retained.Ref != ref || retained.SourceVersion != "1.function-1" || json.Unmarshal(retained.Body, &retainedDefinition) != nil || retainedDefinition.Instructions != "Return the original answer." {
		t.Fatalf("retained function asset: %+v", retained)
	}

	live.accept(t, "build.testplan.create", build.TestPlanType, "PLAN", map[string]any{
		"variant": "evaluation-set", "name": "answerquality", "title": "Answer quality", "function": "FUNCTION", "functionVersion": 1, "at": "2000-01-01T00:00:00Z",
		"steps":      []platform.TestStep{{Type: functionauthoring.FunctionCallType, ID: "evaluation", Action: functionauthoring.SchemaFunctionCall, Payload: "{}", Expect: "accepted"}},
		"evaluation": []platform.EvaluationPolicy{{MinQuality: 1, MaxCostUSD: 1, MaxLatencyMillis: 1000, Cases: []platform.EvaluationCase{{Name: "case", Input: json.RawMessage(`{"note":"ready"}`), Expected: json.RawMessage(`{"answer":"ok"}`)}}}},
	})
	live.accept(t, "build.testplan.publish", build.TestPlanType, "PLAN", map[string]any{})
	replayed := live.restore(t, live.entries)
	restored := replayed.App(build.ID).(*build.Build)
	for _, requested := range []struct {
		version, expected int
		instructions      string
	}{
		{version: 1, expected: 1, instructions: "Return the original answer."},
		{version: 0, expected: 2, instructions: "Return the revised answer."},
	} {
		definition, ordinal, ok := restored.FunctionDefinition("answer", requested.version)
		if !ok || ordinal != requested.expected {
			t.Fatalf("replayed function version %d: ordinal=%d found=%t", requested.version, ordinal, ok)
		}
		if definition.Instructions != requested.instructions {
			t.Fatalf("replayed function version %d changed: %+v", requested.version, definition)
		}
	}

	member := replayed.member(t)
	store := replayed.Projection
	entity := store.Type(functionauthoring.FunctionCallType)
	if entity == nil {
		t.Fatal("function call entity is unavailable")
	}
	run := functionauthoring.FunctionRun{
		Record: platform.Record{ID: "PROPOSAL"},
		App:    build.ID, Function: "answer", Version: 2, Member: member.ID, State: "ready", Decision: "pending",
		ExpiresAt: invariantClock.Add(time.Hour), Output: `{"answer":"ok"}`,
		Contract: platform.AIFunction{Proposal: &platform.ProposalPolicy{ExpiresAfterSeconds: 3600}},
	}
	value := records.Copy(entity.Info().Go, run)
	records.Header(value).ID = run.ID
	store.Lock()
	entity.SetRow(run.ID, &records.Row{Value: value})
	store.Unlock()
	replayed.invalidateReadImage()
	seeded, refusal := replayed.RecordOf(t.Context(), member, functionauthoring.FunctionCallType, run.ID, invariantClock)
	if refusal != nil {
		t.Fatalf("seed proposal: %v", refusal)
	}
	seededRun, ok := seeded.Record.(functionauthoring.FunctionRun)
	if !ok || seededRun.Contract.Proposal == nil || seededRun.State != "ready" || seededRun.Decision != "pending" || seededRun.Member != member.ID {
		t.Fatalf("seeded proposal differs: %+v", seededRun)
	}
	replayed.accept(t, functionauthoring.SchemaProposalReview, functionauthoring.FunctionCallType, run.ID, map[string]any{"decision": "rejected"})
	view, refusal := replayed.RecordOf(t.Context(), member, functionauthoring.FunctionCallType, run.ID, invariantClock)
	if refusal != nil {
		t.Fatal(refusal)
	}
	reviewed, ok := view.Record.(functionauthoring.FunctionRun)
	if !ok || reviewed.Decision != "rejected" || reviewed.DecidedBy != member.ID {
		t.Fatalf("proposal review was not retained: %+v", reviewed)
	}
}
