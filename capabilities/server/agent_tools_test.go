package platformserver

import agentowner "platformserver/engine/agents"

import (
	"encoding/json"
	"platformserver/internal/agents"
	"platformserver/platform"
	"slices"
	"testing"
)

func TestAgentToolInputsDoNotCollideWithTraceMetadata(t *testing.T) {
	kit := newSystemTenant(t)
	owner := kit.tenant.app(platform.AgentApp).(*agentowner.Agents)
	// This tests generic tool envelope compilation. The live system assistant
	// no longer receives a write tool, and its owner refuses direct writes.
	if err := owner.DeclareAgentApp(toolInputContractApp{kit.tenant.app("system")}); err != nil {
		t.Fatal(err)
	}
	definition := owner.AgentEvaluationRuntime().Definition("tool-input-contract.architect")
	if definition == nil {
		t.Fatal("system architect missing")
	}
	for _, tool := range definition.Toolset {
		if len(tool.Tool.Required) != len(slices.Compact(slices.Sorted(slices.Values(tool.Tool.Required)))) {
			t.Fatalf("duplicate required keys in %s", tool.Tool.Name)
		}
	}
	tool := definition.Toolset["system_change_propose"]
	schema := tool.Tool.Properties["payload"].(map[string]any)
	if !slices.Contains(schema["required"].([]string), "rationale") {
		t.Fatal("business rationale disappeared from the owner's input")
	}
	raw := json.RawMessage(`{"target":"proposal-1","rationale":"tool trace","payload":{"rationale":"business reason","target":"business target","quantity":9007199254740993}}`)
	body := agents.ActionPayload(raw, true)
	var input map[string]json.RawMessage
	if err := json.Unmarshal(body, &input); err != nil {
		t.Fatal(err)
	}
	if string(input["rationale"]) != `"business reason"` || string(input["target"]) != `"business target"` || string(input["quantity"]) != "9007199254740993" {
		t.Fatalf("owner payload changed: %s", body)
	}
}

type toolInputContractApp struct{ platform.App }

func (app toolInputContractApp) Manifest() platform.Manifest {
	m := app.App.Manifest()
	m.ID = "tool-input-contract"
	m.Agents[0].Tools = []string{"system.change.propose"}
	return m
}
