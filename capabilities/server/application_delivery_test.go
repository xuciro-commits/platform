package platformserver

import (
	"encoding/json"
	"strings"
	"testing"

	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/internal/construction"
	"platformserver/platform"
)

func TestApplicationDeliveryBeforeExternalConfiguration(t *testing.T) {
	const id = "independent-delivery"
	tn := builderTenant(t, id, ai.New(id), NewAgents(id))
	at := txnAt
	submit := func(schema, typ, record string, payload any) {
		t.Helper()
		decide(t, tn, "builder", construction.ID, schema, typ, record, payload, at)
	}
	submit(construction.ObjectType+".create", construction.ObjectType, "OBJECT", map[string]any{
		"name": "entry", "title": "Entry", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}},
	})
	submit(construction.ConnectionType+".create", construction.ConnectionType, "CONNECTION", map[string]any{"name": "remote", "title": "Remote", "kind": "http"})
	submit(construction.SourceType+".create", construction.SourceType, "SOURCE", map[string]any{"name": "rows", "title": "Rows", "connection": "CONNECTION", "profile": "json", "url": "rows", "object": "build.entry", "key": "id", "mapping": []construction.SourceField{{From: "note", To: "note"}}})
	function := platform.RecordAdviceFunction("build.entry", []string{"note"}, []string{"builder"})
	function.Name = "advice"
	submit(construction.FunctionType+".create", construction.FunctionType, "FUNCTION", map[string]any{"name": function.Name, "title": function.Title, "description": function.Description, "object": function.Object, "fields": function.Fields, "instructions": function.Instructions, "output": function.Output, "roles": function.Roles, "maxInputBytes": function.MaxInputBytes, "maxOutputBytes": function.MaxOutputBytes, "maxTokens": function.MaxTokens})
	submit(construction.TestPlanType+".create", construction.TestPlanType, "PLAN", map[string]any{"name": "quality", "title": "Quality", "function": "FUNCTION", "at": at, "steps": []construction.TestStep{{Type: "build.entry", ID: "SAMPLE", Action: "build.entry.create", Payload: `{"note":"Example"}`, Expect: "accepted"}}, "evaluation": []construction.EvaluationPolicy{{MinQuality: 1, MaxCostUSD: 1, MaxLatencyMillis: 1000, Cases: []construction.EvaluationCase{{Name: "one", Input: json.RawMessage(`{"note":"Example"}`), Expected: json.RawMessage(`{"summary":"Example","category":"routine","review":false}`)}}}}})
	submit(construction.QueryType+".create", construction.QueryType, "QUERY", map[string]any{"name": "entries", "title": "Entries", "description": "Read entries", "object": "build.entry", "limit": 20})
	submit(build.AgentType+".create", build.AgentType, "AGENT", map[string]any{"name": "assistant", "title": "Assistant", "description": "Read entries", "instructions": "Read the supplied facts", "tools": []string{"query:entries"}})
	submit(construction.AppType+".create", construction.AppType, "APP", map[string]any{"name": "desk", "title": "Desk", "resources": []platform.AssetRef{
		{App: "build", Kind: platform.AssetObject, Name: "build.entry"}, {App: "build", Kind: platform.AssetSource, Name: "rows"},
		{App: "build", Kind: platform.AssetFunction, Name: function.Name}, {App: "build", Kind: platform.AssetEvaluation, Name: "quality"}, {App: "build", Kind: platform.AssetAgent, Name: "assistant"},
	}})
	member := memberOf(t, tn, "builder")
	closure, err := tn.ReferencedDrafts(member, platform.AssetApp, "APP")
	if err != nil {
		t.Fatal(err)
	}
	selection := append([]construction.JointDraftRef{{Kind: platform.AssetApp, ID: "APP"}}, closure.Drafts...)
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview requires an external environment: %+v %v", preview, err)
	}
	saved, err := tn.SaveReleaseCandidates(member, selection, preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	review, err := tn.ReviewSavedRelease(member, saved)
	if err != nil || !review.CanActivate {
		t.Fatalf("review requires configuration or evaluation: %+v %v", review, err)
	}
	if _, err := tn.ActivateRelease(member, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	source, _ := platform.Get[construction.Source](tn.automation("build", false), "SOURCE")
	connection, _ := platform.Get[construction.Connection](tn.automation("build", false), "CONNECTION")
	if source.Requested || source.Puller != "" || connection.Address != "" || connection.Secret != "" || connection.State == "ready" || len(tn.Effects(at)) != 0 {
		t.Fatal("delivery executed or configured an external operation")
	}
	if reason := refuse(t, tn, "builder", "build", construction.SourceType+".pull", construction.SourceType, "SOURCE", map[string]any{}, at); !strings.Contains(strings.ToLower(reason), "connection") {
		t.Fatalf("unconfigured source did not explain its refusal: %s", reason)
	}
	if _, err := tn.EvaluateRelease(member, ReleaseEvaluationRequest{CandidateID: saved, PlanID: "PLAN", Key: "evaluate"}, at); err == nil {
		t.Fatal("evaluation called an unconfigured model")
	}
	submit("build.entry.create", "build.entry", "ENTRY", map[string]any{"note": "Local work remains available"})
	if reason := refuse(t, tn, "builder", "build", construction.SchemaFunctionCall, construction.FunctionCallType, "CALL", map[string]any{"name": function.Name, "source": "ENTRY"}, at); reason == "ok" {
		t.Fatal("AI call accepted without an available model")
	}
	if len(tn.Effects(at)) != 0 {
		t.Fatal("refused operation queued an external request")
	}
}
