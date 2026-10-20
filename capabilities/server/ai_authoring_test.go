package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestAIAuthoringUsesSavedModelsOnlyForExplicitJointDelivery(t *testing.T) {
	const id = "ai-authoring"
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	tn := composeTenant(t, id, []Seat{seatOf("builder", "build:builder", "platform:admin", "ai:admin"), seatOf("user", "build:user")}, build.New(id), ai.New(id), work.New(id), NewAgents(id))
	m := memberOf(t, tn, "builder")
	const object = "build.aiauthoring"
	decide(t, tn, "builder", construction.ID, "build.object.create", construction.ObjectType, "OBJECT", map[string]any{
		"name": "aiauthoring", "title": "AI authoring", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}},
		"states": []construction.State{{Name: "open", Title: "Open"}}, "actions": []construction.Action{{Name: "review", Title: "Review", From: []string{"open"}}},
		"access": []map[string]any{{"role": "lead", "read": "all"}},
	}, at)
	decide(t, tn, "builder", construction.ID, "build.query.create", construction.QueryType, "QUERY", map[string]any{"name": "ainotes", "title": "AI notes", "description": "Saved notes", "object": object, "limit": 20}, at)
	decide(t, tn, "builder", construction.ID, "build.function.create", construction.FunctionType, "FUNCTION", map[string]any{"name": "aiauthoring", "title": "AI authoring", "description": "Summarize the note", "object": object, "fields": []string{"note"}, "instructions": "Return a summary", "output": []platform.Field{{Name: "summary", Type: "string", Required: true, Description: "Summary"}}, "maxInputBytes": 4096, "maxOutputBytes": 1024, "maxTokens": 256, "roles": []string{construction.Builder}}, at)
	tools := []string{object + ".review", "query:ainotes"}
	decide(t, tn, "builder", construction.ID, "build.agent.create", build.AgentType, "AGENT", map[string]any{"name": "aiauthoring", "title": "AI authoring", "description": "Review notes", "instructions": "Review the note", "tools": tools, "handoffRole": "lead"}, at)
	decide(t, tn, "builder", construction.ID, "build.agent.edit", build.AgentType, "AGENT", map[string]any{"instructions": "Review the saved note"}, at)
	before := platform.Raw(tn.Definitions(m))
	for _, typ := range []string{construction.FunctionType, build.AgentType} {
		record := map[string]string{construction.FunctionType: "FUNCTION", build.AgentType: "AGENT"}[typ]
		if got := refuse(t, tn, "builder", construction.ID, typ+".publish", typ, record, map[string]any{}, at); got == "ok" {
			t.Fatalf("direct publication of %s resolved an unselected model", typ)
		}
	}
	selection := []construction.JointDraftRef{{Kind: platform.AssetAgent, ID: "AGENT"}, {Kind: platform.AssetFunction, ID: "FUNCTION"}, {Kind: platform.AssetQuery, ID: "QUERY"}, {Kind: platform.AssetObject, ID: "OBJECT"}}
	decide(t, tn, "builder", construction.ID, "build.query.edit", construction.QueryType, "QUERY", map[string]any{"sort": []string{"missing"}}, at)
	if invalid, err := tn.PreviewReleaseDrafts(m, selection); err == nil && invalid.Diagnostic == "" {
		t.Fatal("joint query skipped the original selected-model field checks")
	}
	decide(t, tn, "builder", construction.ID, "build.query.edit", construction.QueryType, "QUERY", map[string]any{"sort": []string{"note"}}, at)
	preview, err := tn.PreviewReleaseDrafts(m, selection)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("explicit model/function/agent selection: %+v %v", preview, err)
	}
	candidate, err := tn.SaveReleaseCandidates(m, selection, preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, platform.Raw(tn.Definitions(m))) {
		t.Fatal("saving and reviewing AI declarations changed installed definitions")
	}
	if _, err := tn.ActivateRelease(m, candidate, "activate", at); err == nil {
		t.Fatal("AI declarations activated without a passing evaluation")
	}
	if why := refuse(t, tn, "user", construction.ID, "build.agent.edit", build.AgentType, "AGENT", map[string]any{"instructions": "Unauthorized"}, at); !strings.Contains(why, "DENIED") {
		t.Fatalf("ordinary member changed the builder declaration: %s", why)
	}
}
