package platformserver

import agentowner "platformserver/engine/agents"

import (
	"strings"
	"testing"
	"time"

	"platformserver/platform"
)

// A run started for a person cannot become an app-automation run when that
// person disappears or loses the app grant between model turns.
func TestAgentPrincipalRevocation(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tn := stockTenant(t, agentowner.New("t-1"))
	if err := tn.agents.DeclareAgentApp(agentFixtureApp{App: tn.app("stock"), definition: platform.Agent{Name: "guide", Title: "Stock guide", Instructions: "Read stock"}}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ id, who string }{{"R1", "departed"}, {"R2", "ana"}} {
		if err := tn.automation(platform.AgentApp, true).PutAt(now, "test.run", platform.AgentRunRecord{Record: platform.Record{ID: x.id}, Agent: "stock.guide", OnBehalf: x.who, State: "running"}); err != nil {
			t.Fatal(err)
		}
	}
	missing := tn.agents.AgentEvaluationRuntime().Reader(platform.AgentRunRecord{OnBehalf: "departed"})
	if missing == nil || missing.Tenant == tn.ID {
		t.Fatalf("removed person's reader became automation or remained authorized: %+v", missing)
	}
	turns := tn.agents.(*agentowner.Agents).DueAgentTurns(now)
	if len(turns) != 2 {
		t.Fatalf("turns: %+v", turns)
	}
	for _, turn := range turns {
		if turn.Run.ID == "R1" && !strings.Contains(turn.Stop, "no longer has access") {
			t.Errorf("removed person's run: %+v", turn)
		}
	}
	var entries []Entry
	tn.Record = func(entry Entry) { entries = append(entries, entry) }
	tn.agentStep(platform.AgentResultStep{Run: "R1", Tool: "knowledge", Observation: []byte(`[{"text":"private passage"}]`)}, now)
	stopped, ok := platform.Get[platform.AgentRunRecord](tn.automation(platform.AgentApp, false), "R1")
	if !ok || stopped.State != "stopped" || !strings.Contains(stopped.Stopped, "no longer has access") || len(stopped.Citations) != 0 {
		t.Errorf("revoked in-flight tool was applied: %+v", stopped)
	}
	// Nobody eligible is left to take it over: the run says so and creates no task.
	if stopped.Handoff != "No eligible recipient is configured" || stopped.Task != "" {
		t.Errorf("a stopped run without a recipient claimed a handoff: %+v", stopped)
	}
	if len(entries) != 1 || entries[0].Kind != "accepted-result" {
		t.Fatalf("revocation did not accept exactly one result: %+v", entries)
	}
	recorded, decodeErr := decodeAcceptedAgent(entries[0].Body)
	if decodeErr != nil || recorded.Step.Stop == "" || recorded.Step.Tool != "" || len(recorded.Step.Observation) != 0 {
		t.Errorf("revocation not fixed in accepted step: %+v, %v", recorded.Step, decodeErr)
	}
	revoked := platform.As(platform.AgentApp, platform.Member{ID: "ana", Tenant: tn.ID, Roles: map[string]string{}})
	runs, err := tn.agents.(*agentowner.Agents).Read(revoked, "runs")
	if err != nil || len(runs.([]platform.AgentRunRecord)) != 0 {
		t.Errorf("revoked member saw old run trace: %+v, %v", runs, err)
	}
	agents, err := tn.agents.(*agentowner.Agents).Read(revoked, "agents")
	if err != nil || len(agents.([]platform.AgentInfo)) != 0 {
		t.Errorf("revoked member saw agent instructions: %+v, %v", agents, err)
	}
}
