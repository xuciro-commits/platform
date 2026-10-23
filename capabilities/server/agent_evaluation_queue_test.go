package platformserver

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/internal/construction"
	"platformserver/platform"
)

func TestQueuedCandidateAgentEvaluationsKeepIndependentCheckpoints(t *testing.T) {
	const tenant = "queued-agent-evaluations"
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, tenant, []Seat{seatOf("builder", "build:builder", "platform:admin")}, build.New(tenant), ai.New(tenant), work.New(tenant), NewAgents(tenant))
	}
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "case", "title": "Case", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	configureDeclaredModel(t, tn, at)
	var selection []construction.JointDraftRef
	for _, name := range []string{"first", "second"} {
		decide(t, tn, "builder", "build", "build.agent.create", build.AgentType, name, map[string]any{"name": name, "title": name, "description": "Read a case", "instructions": "Report Ready", "tools": []string{"build.case.edit"}, "cases": []build.AgentCase{{Name: "ready", Goal: "Give a result", Expect: "Ready"}}}, at)
		selection = append(selection, construction.JointDraftRef{Kind: platform.AssetAgent, ID: name})
	}
	member := memberOf(t, tn, "builder")
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("candidate: %+v %v", preview, err)
	}
	candidate, err := tn.SaveReleaseCandidates(member, selection, preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	second := ""
	calls := 0
	tn.AIClient = func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			var err error
			second, err = tn.EvaluateRelease(member, ReleaseEvaluationRequest{CandidateID: candidate, Agent: "build.second", Key: "second"}, at)
			if err != nil {
				t.Fatal(err)
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"Ready"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"cost":0.001}}`))}, nil
	}
	first, err := tn.EvaluateRelease(member, ReleaseEvaluationRequest{CandidateID: candidate, Agent: "build.first", Key: "first"}, at)
	if err != nil {
		t.Fatal(err)
	}
	tn.Evaluate(at)
	for _, id := range []string{first, second} {
		report, ok := platform.Get[Evaluation](tn.automation(AgentApp, false), id)
		if !ok || report.State != "done" || report.Score != 1 || len(report.Cases) != 1 || report.Cases[0].Runs != 3 {
			t.Fatalf("queued report did not complete: %s, fault=%+v", platform.Raw(report), tn.fault.Load())
		}
	}
	if calls != 6 {
		t.Fatalf("queued reports repeated or missed model calls: %d", calls)
	}
	CheckReplay(t, tn, entries, compose)
}
