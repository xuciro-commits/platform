package platformserver

import (
	"encoding/json"
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

func TestCandidateAgentQueriesUseSavedDefinitionsAndCurrentScope(t *testing.T) {
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, "query-candidate", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("reader", "build:reader"), seatOf("other", "build:reader")}, build.New("query-candidate"), ai.New("query-candidate"), work.New("query-candidate"), NewAgents("query-candidate"))
	}
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "queryitem", "title": "Query item", "fields": []construction.Field{{Name: "title", Title: "Title", Type: "text"}, {Name: "active", Title: "Active", Type: "boolean"}}, "access": []construction.Access{{Role: "reader", Read: "own", Create: true, Edit: true}}}, at)
	for _, row := range []struct {
		member, id, title string
		active            bool
	}{{"reader", "own", "Visible source", true}, {"reader", "closed", "Closed source", false}, {"other", "foreign", "Foreign source", true}} {
		decide(t, tn, row.member, "build", "build.queryitem.create", "build.queryitem", row.id, map[string]any{"title": row.title, "active": row.active}, at)
	}
	configureDeclaredModel(t, tn, at)
	decide(t, tn, "builder", "build", "build.query.create", construction.QueryType, "QUERY", map[string]any{"name": "openitems", "title": "Open items", "description": "Only active records in the current reader's scope", "object": "build.queryitem", "domain": []any{[]any{"active", "=", true}}, "sort": []string{"id"}, "limit": 20}, at)
	decide(t, tn, "builder", "build", "build.agent.create", build.AgentType, "AGENT", map[string]any{"name": "queryassistant", "title": "Query assistant", "description": "Read open items", "instructions": "Read the query, then report what it returned.", "tools": []string{"query:openitems"}, "cases": []build.AgentCase{{Name: "query", Goal: "Read open items", Expect: "Visible source"}}}, at)
	selection := []construction.JointDraftRef{{Kind: platform.AssetQuery, ID: "QUERY"}, {Kind: platform.AssetAgent, ID: "AGENT"}}
	member := memberOf(t, tn, "builder")
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("query candidate: %+v %v", preview, err)
	}
	candidate, err := tn.SaveReleaseCandidates(member, selection, preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	// The live query does not exist, and the mutable draft now says the opposite.
	decide(t, tn, "builder", "build", "build.query.edit", construction.QueryType, "QUERY", map[string]any{"domain": []any{[]any{"active", "=", false}}}, at)
	modelCalls := 0
	tn.AIClient = func(request *http.Request) (*http.Response, error) {
		modelCalls++
		var body struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			return nil, err
		}
		message := map[string]any{"tool_calls": []any{map[string]any{"id": "query", "type": "function", "function": map[string]any{"name": "query_openitems", "arguments": `{"rationale":"Read original scoped records"}`}}}}
		for _, turn := range body.Messages {
			if turn.Role == "tool" {
				message = map[string]any{"content": turn.Content}
				break
			}
		}
		raw := platform.Raw(map[string]any{"choices": []any{map[string]any{"message": message}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "cost": 0.001}})
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	}
	id, err := tn.EvaluateRelease(member, ReleaseEvaluationRequest{CandidateID: candidate, Agent: "build.queryassistant", Key: "measure"}, at)
	if err != nil {
		t.Fatal(err)
	}
	tn.Evaluate(at)
	report, ok := platform.Get[Evaluation](tn.automation(AgentApp, false), id)
	if !ok || report.State != "done" || report.Score != 1 || modelCalls != 6 {
		t.Fatalf("the candidate did not execute its saved query: %s (%d calls)", platform.Raw(report), modelCalls)
	}
	if _, err := tn.ActivateRelease(member, candidate, "activate", at); err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "reader", AgentApp, SchemaRunStart, RunType, "RUN", map[string]any{"agent": "build.queryassistant", "goal": "Read my open items"}, at)
	tn.Think(at)
	run, _ := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "RUN")
	if len(run.Steps) != 1 || !strings.Contains(run.Steps[0].Outcome, "Visible source") || strings.Contains(run.Steps[0].Outcome, "Foreign source") || strings.Contains(run.Steps[0].Outcome, "Closed source") {
		t.Fatalf("agent query bypassed the reader's current scope: %s", platform.Raw(run))
	}
	// Publishing the changed predicate changes this agent's effective tools.
	// An in-flight run must hand over, not silently begin using a new query.
	decide(t, tn, "builder", "build", "build.query.publish", construction.QueryType, "QUERY", map[string]any{}, at)
	before := modelCalls
	tn.Think(at.Add(time.Second))
	run, _ = platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "RUN")
	if run.State != "stopped" || modelCalls != before {
		t.Fatalf("query publication did not stop the old run: %s", platform.Raw(run))
	}
	decide(t, tn, "reader", AgentApp, SchemaRunStart, RunType, "REVOKED", map[string]any{"agent": "build.queryassistant", "goal": "Read my current items"}, at)
	decide(t, tn, "builder", PlatformApp, SchemaRevoke, MemberType, "reader", map[string]any{"app": "build"}, at)
	tn.Think(at.Add(2 * time.Second))
	revoked, _ := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "REVOKED")
	if revoked.State != "stopped" || modelCalls != before {
		t.Fatalf("a revoked reader called the model: %s", platform.Raw(revoked))
	}
	CheckReplay(t, tn, entries, compose)
}
