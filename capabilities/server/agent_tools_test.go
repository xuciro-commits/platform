package platformserver

import (
	"encoding/json"
	"platformserver/internal/agents"
	"slices"
	"testing"
)

func TestAgentToolInputsDoNotCollideWithTraceMetadata(t *testing.T) {
	kit := newSystemTenant(t)
	definition := kit.tenant.app(AgentApp).(*Agents).def("system.architect")
	if definition == nil {
		t.Fatal("system architect missing")
	}
	for _, tool := range definition.tools {
		if len(tool.tool.Required) != len(slices.Compact(slices.Sorted(slices.Values(tool.tool.Required)))) {
			t.Fatalf("duplicate required keys in %s", tool.tool.Name)
		}
	}
	tool := definition.tools[agentToolName("system.change.propose")]
	schema := tool.tool.Properties["payload"].(map[string]any)
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
