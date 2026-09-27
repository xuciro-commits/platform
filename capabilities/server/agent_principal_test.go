package platformserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
)

// A run started for a person cannot become an app-automation run when that
// person disappears or loses the app grant between model turns.
func TestAgentPrincipalRevocation(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tn := stockTenant(t, NewAgents("t-1"))
	tn.agents.defs["stock.guide"] = &agentDef{app: "stock"}
	for _, x := range []struct{ id, who string }{{"R1", "departed"}, {"R2", "ana"}} {
		if err := tn.automation(AgentApp, false).PutAt(now, "test.run", AgentRunRecord{Record: platform.Record{ID: x.id}, Agent: "stock.guide", OnBehalf: x.who, State: "running"}); err != nil {
			t.Fatal(err)
		}
	}
	missing := tn.agents.reader(AgentRunRecord{OnBehalf: "departed"})
	if missing == nil || missing.Tenant == tn.ID {
		t.Fatalf("removed person's reader became automation or remained authorized: %+v", missing)
	}
	turns := tn.agents.due(now)
	if len(turns) != 2 {
		t.Fatalf("turns: %+v", turns)
	}
	for _, turn := range turns {
		if turn.run.ID == "R1" && !strings.Contains(turn.stop, "no longer has access") {
			t.Errorf("removed person's run: %+v", turn)
		}
	}
	var entries []Entry
	tn.Record = func(entry Entry) { entries = append(entries, entry) }
	tn.agentStep(stepBody{Run: "R1", Tool: "knowledge", Observation: []byte(`[{"text":"private passage"}]`)}, now)
	stopped, ok := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "R1")
	if !ok || stopped.State != "stopped" || !strings.Contains(stopped.Stopped, "no longer has access") || len(stopped.Citations) != 0 {
		t.Errorf("revoked in-flight tool was applied: %+v", stopped)
	}
	var recorded stepBody
	if len(entries) != 1 || json.Unmarshal(entries[0].Body, &recorded) != nil || recorded.Stop == "" || recorded.Tool != "" || len(recorded.Observation) != 0 {
		t.Errorf("revocation not fixed in journaled step: %+v, %+v", entries, recorded)
	}
	revoked := platform.As(AgentApp, platform.Member{ID: "ana", Tenant: tn.ID, Roles: map[string]string{}})
	runs, err := tn.agents.Read(revoked, "runs")
	if err != nil || len(runs.([]AgentRunRecord)) != 0 {
		t.Errorf("revoked member saw old run trace: %+v, %v", runs, err)
	}
	agents, err := tn.agents.Read(revoked, "agents")
	if err != nil || len(agents.([]AgentInfo)) != 0 {
		t.Errorf("revoked member saw agent instructions: %+v, %v", agents, err)
	}
}
