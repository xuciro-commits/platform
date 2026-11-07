package aigateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
)

type proposalHost struct {
	action   platform.Action
	proposed platform.ModelToolProposal
}

func (h *proposalHost) ModelToolSurface(platform.Member) platform.AgentSurface {
	return platform.AgentSurface{
		Entities: []platform.EntityInfo{{Type: h.action.Target, App: "build", Title: "Widget", Plural: "Widgets"}},
		Actions:  []platform.AgentAction{{Owner: "build", Action: h.action}},
	}
}

func (*proposalHost) AgentObjects(platform.Member, string, json.RawMessage, time.Time) (platform.AgentObservation, *kernel.Error) {
	return platform.AgentObservation{}, nil
}

func (h *proposalHost) Catalog(platform.Member) []platform.Action {
	return []platform.Action{h.action}
}

func (*proposalHost) ModelToolReads(platform.Member) []string { return nil }

func (*proposalHost) Read(context.Context, platform.Member, string, ...json.RawMessage) (any, *kernel.Error) {
	return nil, nil
}

func (h *proposalHost) ProposeModelTool(_ platform.Member, proposal platform.ModelToolProposal, _ time.Time) *kernel.Error {
	h.proposed = proposal
	return nil
}

func TestMCPProposalConfirmationUsesRunRoute(t *testing.T) {
	action := platform.Action{Schema: "build.alpha.approve", Target: "build.alpha", Title: "Approve", Description: "Approve a widget", Roles: []string{"builder"}, Capability: "objects"}
	host := &proposalHost{action: action}
	result := (Tools{Host: host, Now: func() time.Time { return time.Unix(0, 0) }}).Call(
		t.Context(),
		platform.Member{ID: "member"},
		toolName(action.Schema),
		map[string]any{"target": "OBJ", "payload": map[string]any{}, "idempotencyKey": "test"},
	)
	if result["isError"] == true {
		t.Fatalf("proposal failed: %+v", result)
	}
	content := result["content"].([]map[string]any)
	var answer map[string]any
	if err := json.Unmarshal([]byte(content[0]["text"].(string)), &answer); err != nil {
		t.Fatal(err)
	}
	if answer["confirmation"] != "#/run?id="+host.proposed.ID {
		t.Fatalf("confirmation route = %v; proposal = %q", answer["confirmation"], host.proposed.ID)
	}
	if strings.Contains(answer["confirmation"].(string), "agent-run") {
		t.Fatalf("confirmation still points at a non-existent agent-run route: %v", answer["confirmation"])
	}
}
