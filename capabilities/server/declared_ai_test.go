package platformserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
)

// The original model protocol chooses a real declared tool. The normal agent
// owner still probes the caller, creates its checkpoint and accepts the result.
func declaredAgentDraft(t *testing.T, tn *Tenant, member, runID, agent, schema, target string, payload map[string]any, at time.Time) AgentRunRecord {
	t.Helper()
	previous := tn.AIClient
	defer func() { tn.AIClient = previous }()
	arguments := map[string]any{"target": target, "rationale": "Propose the confirmed business task for human review"}
	for key, value := range payload {
		arguments[key] = value
	}
	tn.AIClient = func(*http.Request) (*http.Response, error) {
		tool := map[string]any{"id": "business-tool", "type": "function", "function": map[string]any{"name": agentToolName(schema), "arguments": string(platform.Raw(arguments))}}
		raw := platform.Raw(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"tool_calls": []any{tool}}}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "cost": 0.001},
		})
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	}
	decide(t, tn, member, AgentApp, SchemaRunStart, RunType, runID, map[string]any{"agent": agent, "goal": "Propose the confirmed business task", "act": true}, at)
	turns := tn.agents.DueAgentTurns(at)
	if len(turns) != 1 || turns[0].Run.ID != runID {
		t.Fatalf("unexpected original agent turns: %+v", turns)
	}
	tn.take(turns[0], at)
	run, ok := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), runID)
	if !ok || run.State != "waiting" || len(run.Draft) != 1 || run.Draft[0].Action != schema || run.ActionsUsed != 0 {
		t.Fatalf("the declared business tool bypassed or missed its checkpoint: %s", platform.Raw(run))
	}
	return run
}

func TestDeclaredMaterialPlannerWaitsForHumanAndReadsOriginalSupplierAnswer(t *testing.T) {
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	seats := []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("planner", "build:supervisor"), seatOf("other", "build:supervisor")}
	var entries []Entry
	calls := 0
	supplier := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			WriteJSON(w, http.StatusOK, map[string]any{})
			return
		}
		calls++
		var rpc struct {
			ID string `json:"id"`
		}
		if json.NewDecoder(r.Body).Decode(&rpc) != nil || rpc.ID == "" {
			t.Error("missing original inquiry identity")
			http.Error(w, "invalid inquiry", 400)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": map[string]any{"task": map[string]any{"id": rpc.ID, "status": map[string]any{"state": "TASK_STATE_COMPLETED"}, "artifacts": []any{map[string]any{"parts": []any{map[string]any{"text": "Supplier states three working days; confirm the order separately."}}}}}}})
	})
	tn := declaredIndustryTenant(t, "material-planner", "manufacturing", seats, at, declaredFixtureOptions{Entries: &entries, Supplier: supplier})
	declaredAgentDraft(t, tn, "planner", "INQUIRY", "build.materialplanner", "build.mesleadtime.create", "leadtime", map[string]any{"product": "Assembly P1", "question": "What is the current lead time for the required components?"}, at)
	if page, err := tn.Records(memberOf(t, tn, "planner"), "build.mesleadtime", platform.Query{}, at); err != nil || page.Total != 0 || calls != 0 {
		t.Fatal("the draft sent an inquiry before human confirmation")
	}
	if got := refuse(t, tn, "other", AgentApp, SchemaRunConfirm, RunType, "INQUIRY", map[string]any{}, at); got == "ok" {
		t.Fatal("another member confirmed the person's inquiry")
	}
	request := submission(tn, memberOf(t, tn, "planner"), AgentApp, SchemaRunConfirm, RunType, "INQUIRY", map[string]any{})
	first, failure := tn.Submit(memberOf(t, tn, "planner"), request, at)
	if failure != nil {
		t.Fatal(failure)
	}
	if again, failure := tn.Submit(memberOf(t, tn, "planner"), request, at); failure != nil || first.GetChangeId() != again.GetChangeId() {
		t.Fatal("inquiry confirmation lost its original receipt")
	}
	tn.Dispatch(at)
	row := declaredRecord(t, tn, "planner", "build.mesleadtime", "leadtime", at)
	if calls != 1 || row["answer"] != "Supplier states three working days; confirm the order separately." {
		t.Fatalf("the original A2A answer was not readable: calls=%d row=%s", calls, platform.Raw(row))
	}
	if got := refuse(t, tn, "planner", "build", "build.mesleadtime.edit", "build.mesleadtime", "leadtime", map[string]any{"answer": "Invented delivery promise"}, at); got == "ok" {
		t.Fatal("a planner forged the supplier's original answer")
	}
	CheckReplay(t, tn, entries, func() *Tenant { return composeDeclaredTenant(t, tn.ID, seats) })
	if calls != 1 {
		t.Fatal("recovery repeated a supplier inquiry")
	}
}
