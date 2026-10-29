package platformserver

import agentowner "platformserver/engine/agents"

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	controlowner "platformserver/kernel/control"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/apps/work"
	automation "platformserver/engine/automation"
	acceptedformat "platformserver/kernel/acceptedformat"

	"platformserver/platform"
)

func TestPostgresUsageResultsKeepAIStateAcrossCheckpointAndFullRecovery(t *testing.T) {
	approveTestInference(t, "http://localhost:1234", "local")
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("sql-usage-state-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	at := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		tn := composeTenant(t, id, []platform.Seat{seatOf("ana", "platform:admin", "ai:admin", "agent:admin", "desk:clerk")}, ai.New(id), work.New(id), automation.New(id), agentowner.New(id), newDesk(id))
		tn.AIClient = func(*http.Request) (*http.Response, error) {
			t.Fatal("saved usage recovery called a model")
			return nil, nil
		}
		tn.outbound = append(tn.outbound, &effect{Effect: platform.Effect{ID: "usage-model-effect", App: "desk", Endpoint: acceptedformat.ModelEndpoint, State: "pending", At: at, Due: at}})
		return tn
	}
	live := compose()
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	decide(t, live, "ana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": "http://localhost:1234/v1"}, at)
	decide(t, live, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/model", map[string]any{"access": "users"}, at)
	decide(t, live, "ana", controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, "agent/model", map[string]any{"value": "local/model"}, at)
	decide(t, live, "ana", platform.AgentApp, platform.SchemaRunStart, platform.RunType, "R1", map[string]any{"agent": "desk.triage", "goal": "answer", "act": true}, at)
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	check := func(tenant *Tenant, want int) {
		t.Helper()
		if tenant.quarantined() {
			t.Fatalf("metered tenant stopped: %+v", tenant.fault.Load())
		}
		owner := tenant.ai.(*ai.AI)
		actual, err := owner.AcceptedState()
		if err != nil {
			t.Fatal(err)
		}
		stored, err := j.StateValue(ctx, id, "app-state", ai.ID, j.Position(id))
		if err != nil {
			t.Fatal(err)
		}
		a, err := platform.CanonicalDigest(actual)
		if err != nil {
			t.Fatal(err)
		}
		b, err := platform.CanonicalDigest(stored)
		if err != nil {
			t.Fatal(err)
		}
		if a != b || len(tenant.ai.Usage()) != want {
			t.Fatalf("SQL AI state differs from owner: usage=%d want=%d SQL=%s owner=%s", len(tenant.ai.Usage()), want, stored, actual)
		}
	}
	check(live, 0)
	model, _, problem := live.ai.Model("local/model")
	if problem != nil {
		t.Fatal(problem)
	}
	reserve := func(m platform.Member, call string) {
		t.Helper()
		if why := live.reserveCall(m, model, platform.ChatRequest{Call: call}, 30, 20, at); why != nil {
			t.Fatal(why)
		}
	}
	reserve(memberOf(t, live, "ana"), "direct-call")
	check(live, 0)
	direct := platform.AIUsage{Call: "direct-call", At: at.Add(time.Second), Member: "ana", Model: "local/model", Input: 7, Output: 2, TokensReported: true}
	if refusal := live.meter(memberOf(t, live, "ana"), direct); refusal != nil {
		t.Fatal(refusal)
	}
	check(live, 1)
	reserve(platform.Member{ID: "app:desk", Tenant: id}, "effect-call")
	reserve(live.agents.AgentPrincipal("desk.triage"), "agent-call")
	reserve(memberOf(t, live, "ana"), "unanswered-call")
	check(live, 1)
	// Save an owner-only checkpoint before effect and agent usage. Both later
	// readings must apply once from their original accepted-result suffix.
	if _, err = live.checkpointRuntime(ctx, j, "usage-checkpoint"); err != nil {
		t.Fatal(err)
	}
	effectUsage := platform.AIUsage{Call: "effect-call", At: at.Add(2 * time.Second), Member: "app:desk", Model: "local/model", Input: 11, Output: 3, TokensReported: true}
	live.settleWithUsage("usage-model-effect", platform.Outcome{Effect: "usage-model-effect", Result: "delivered"}, &effectUsage, effectUsage.At)
	check(live, 2)
	agentUsage := platform.AIUsage{Call: "agent-call", At: at.Add(3 * time.Second), Member: live.agents.AgentPrincipal("desk.triage").ID, Agent: true, Model: "local/model", Input: 13, Output: 5, TokensReported: true}
	live.applyAgentStep(platform.AgentResultStep{Run: "R1", Content: "saved answer", Usage: agentUsage}, agentUsage.At, false)
	check(live, 3)
	entries, err := j.Entries(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, entry := range entries {
		var envelope struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(entry.Body, &envelope) == nil {
			kinds[envelope.Kind] = true
		}
	}
	for _, kind := range []string{"usage-result", "effect-result", "agent-result"} {
		if !kinds[kind] {
			t.Fatal("test did not exercise the original accepted result", kind)
		}
	}
	if r, ok := live.ai.Reservation("unanswered-call"); !ok || r.State != "pending" {
		t.Fatal("unfinished call lost its liability", r)
	}
	if q := live.ai.Quota(memberOf(t, live, "ana"), live.limits(), at); q.UnsettledCalls != 1 || q.ReservedTokens != 50 {
		t.Fatal("settlement cleared another call", q)
	}
	expected, err := live.ai.(*ai.AI).AcceptedState()
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	deployment := Deployment{SnapshotEvery: 1, Rebuild: func(name string) (*Tenant, error) {
		if name != id {
			return nil, fmt.Errorf("unexpected tenant %s", name)
		}
		return compose(), nil
	}}
	if err = deployment.restoreTenants(ctx, j, "usage-new-build", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	check(restored, 3)
	after, err := restored.ai.(*ai.AI).AcceptedState()
	if err != nil {
		t.Fatal(err)
	}
	if !sameStoredJSON(expected, after) {
		t.Fatal("checkpoint plus usage suffix changed metering")
	}
	// Administrative recovery deliberately rebuilds from the complete original
	// log, compares SQL state, then replaces only this tenant's stopped runtime.
	restored.quarantine(fmt.Errorf("test requests administrative full-log recovery"))
	registry := newTenantRegistry([]*Tenant{restored})
	position := j.Position(id)
	if err = deployment.retryTenant(ctx, j, registry, CodeOf(restored), id); err != nil {
		t.Fatal(err)
	}
	repaired := registry.current(id)
	check(repaired, 3)
	final, err := repaired.ai.(*ai.AI).AcceptedState()
	if err != nil {
		t.Fatal(err)
	}
	if !sameStoredJSON(expected, final) || j.Position(id) != position {
		t.Fatal("full-log recovery repeated usage or appended another result")
	}
}
