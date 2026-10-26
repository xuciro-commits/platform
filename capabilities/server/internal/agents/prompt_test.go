package agents

import (
	"encoding/json"
	"reflect"
	"testing"

	"platformserver/platform"
)

func TestPromptPairsRetainedAndCompleteCalls(t *testing.T) {
	calls := []platform.ToolCall{{ID: "provided", Name: "read", Arguments: json.RawMessage(`{"n":9007199254740993}`)}}
	req := (Prompt{Goal: "review", OnBehalf: "ana", Ref: "ticket/T1", HasContext: true,
		History: []HistoryStep{
			{Tool: "read", Rationale: "original rationale", Outcome: "original answer"},
			{Calls: calls, Content: "provider text", Tool: "read", CallID: "provided", Outcome: "accepted answer"},
			{Tool: "finish", CallID: "continued", Outcome: "finished"},
		},
		Tools: map[string]platform.Tool{"z": {Name: "last"}, "a": {Name: "first"}},
	}).Request("model")
	want := []platform.Message{
		{Role: "user", Content: "Goal: review\nOn behalf of: ana\nAbout ticket/T1:\n"},
		{Role: "assistant", Content: "original rationale", ToolCalls: []platform.ToolCall{{ID: "s1", Name: "read", Arguments: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolCallID: "s1", Content: "original answer"},
		{Role: "assistant", Content: "provider text", ToolCalls: calls},
		{Role: "tool", ToolCallID: "provided", Content: "accepted answer"},
		{Role: "tool", ToolCallID: "continued", Content: "finished"},
	}
	if !reflect.DeepEqual(req.Messages[1:], want) || req.Model != "model" || !req.PromptCache {
		t.Fatalf("changed retained conversation: %+v", req)
	}
	if len(req.Tools) != 2 || req.Tools[0].Name != "first" || req.Tools[1].Name != "last" {
		t.Fatalf("changed tool order: %+v", req.Tools)
	}
}

func TestActionPayloadRetainsNumberEncodingByCallFormat(t *testing.T) {
	raw := json.RawMessage(`{"target":"T1","rationale":"read first","amount":9007199254740993}`)
	if got := string(ActionPayload(raw, false)); got != `{"amount":9007199254740992}` {
		t.Fatalf("legacy payload changed: %s", got)
	}
	if got := string(ActionPayload(raw, true)); got != `{"amount":9007199254740993}` {
		t.Fatalf("complete call lost number: %s", got)
	}
}
