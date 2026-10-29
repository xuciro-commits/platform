package platformserver

import agentowner "platformserver/engine/agents"
import platform "platformserver/platform"

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"platformserver/aigateway"
	"platformserver/apps/work"

	"strings"
	"testing"
	"time"
)

// TestMCP drives the host as an MCP client would: initialize, list the member's
// tools, call one, and fail to call one outside the catalog.
func TestMCP(t *testing.T) {
	tn, a := mcpProposalTenant(t)
	h := NewHost(Tokens(map[string]string{"bo-token": "bo"}), tn)
	rpc := func(body string) (int, string) {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer bo-token")
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	if _, body := rpc(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`); !strings.Contains(body, `"tools":{"listChanged":false}`) {
		t.Fatalf("initialize: %s", body)
	}
	if status, _ := rpc(`{"jsonrpc":"2.0","method":"notifications/initialized"}`); status != 202 {
		t.Fatalf("notification: %d", status)
	}
	// bo holds a role in b only: b's action and read are offered, a's are not.
	if _, body := rpc(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`); strings.Contains(body, `"name":"a_note"`) || !strings.Contains(body, `"name":"b_note"`) || !strings.Contains(body, `"name":"read_b-notes"`) {
		t.Fatalf("tools: %s", body)
	}
	if _, body := rpc(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"a_note","arguments":{"target":"x","text":"hi"}}}`); !strings.Contains(body, `"isError":true`) {
		t.Fatalf("a call outside the catalog: %s", body)
	}
	rewriteMember(t, testConsoleOf(tn), "bo", func(member *platform.Member) { member.Roles["a"] = "writer" })
	if _, body := rpc(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"a_note","arguments":{"target":"x","idempotencyKey":"m1"}}}`); !strings.Contains(body, `"isError":false`) || !strings.Contains(body, `awaiting-confirmation`) {
		t.Fatalf("call: %s", body)
	}
	if a.texts["x"] != "" {
		t.Fatal("the MCP tool wrote business data before confirmation")
	}
	id := "mcp-" + base64.RawURLEncoding.EncodeToString([]byte("bo:m1"))
	decide(t, tn, "bo", platform.AgentApp, platform.SchemaRunConfirm, platform.RunType, id, map[string]any{}, time.Now().UTC())
	if a.texts["x"] == "" {
		t.Fatal("confirmed proposal did not reach the original action")
	}
	if _, body := rpc(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"agent_run_confirm","arguments":{"target":"x"}}}`); !strings.Contains(body, `"isError":true`) {
		t.Fatal("MCP could confirm its own proposal", body)
	}
}

func TestMCPNegotiatesSupportedVersionsAndPreservesNumbers(t *testing.T) {
	tn, a := mcpProposalTenant(t)
	h := NewHost(Tokens(map[string]string{"token": "ana"}), tn)
	rpc := func(header, body string) (int, string) {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token")
		if header != "" {
			req.Header.Set("MCP-Protocol-Version", header)
		}
		recorder := httptest.NewRecorder()
		h.Handler().ServeHTTP(recorder, req)
		return recorder.Code, recorder.Body.String()
	}
	for _, version := range []string{"2025-03-26", aigateway.MCPProtocolVersion, "2099-01-01"} {
		status, body := rpc("", fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q}}`, version))
		var reply struct {
			Result struct{ ProtocolVersion string }
		}
		if err := json.Unmarshal([]byte(body), &reply); err != nil {
			t.Fatal(err)
		}
		want := version
		if version == "2099-01-01" {
			want = aigateway.MCPProtocolVersion
		}
		if status != 200 || reply.Result.ProtocolVersion != want {
			t.Fatalf("negotiated an unsupported version: %d %s", status, body)
		}
	}
	for _, version := range []string{"", "2025-03-26", aigateway.MCPProtocolVersion, "2099-01-01"} {
		status, body := rpc(version, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		want := 200
		if version == "2099-01-01" {
			want = 400
		}
		if status != want {
			t.Fatalf("version %q: %d %s", version, status, body)
		}
	}
	status, body := rpc(aigateway.MCPProtocolVersion, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"a_note","arguments":{"target":"precise","idempotencyKey":"mcp-precise","text":"中文","number":9007199254740993}}}`)
	if status != 200 || !strings.Contains(body, `"isError":false`) || a.texts["precise"] != "" {
		t.Fatalf("MCP bypassed a proposal: %s", body)
	}
	id := "mcp-" + base64.RawURLEncoding.EncodeToString([]byte("ana:mcp-precise"))
	decide(t, tn, "ana", platform.AgentApp, platform.SchemaRunConfirm, platform.RunType, id, map[string]any{}, time.Now().UTC())
	if a.texts["precise"] != `{"number":9007199254740993,"text":"中文"}` {
		t.Fatal("original proposal lost exact JSON numbers", a.texts["precise"])
	}
	status, body = rpc(aigateway.MCPProtocolVersion, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"a_note","arguments":{"target":"corrected","idempotencyKey":"mcp-corrected","number":9007199254740993}}}`)
	if status != 200 || !strings.Contains(body, `"isError":false`) {
		t.Fatal("proposal for correction was refused", body)
	}
	id = "mcp-" + base64.RawURLEncoding.EncodeToString([]byte("ana:mcp-corrected"))
	decide(t, tn, "ana", platform.AgentApp, platform.SchemaRunConfirm, platform.RunType, id, map[string]any{"payload": json.RawMessage(`{"number":9007199254740992}`)}, time.Now().UTC())
	if a.texts["corrected"] != `{"number":9007199254740992}` {
		t.Fatal("confirming a large integer correction reused the original proposal", a.texts["corrected"])
	}
}

func mcpProposalTenant(t *testing.T) (*Tenant, *notes) {
	const id = "mcp-proposals"
	a, b := newNotes(id, "a"), newNotes(id, "b")
	tenant := composeTenant(t, id, []platform.Seat{seatOf("ana", "platform:admin", "a:writer", "b:writer"), seatOf("bo", "b:writer")}, a, b, agentowner.New(id), work.New(id))
	return tenant, a
}
