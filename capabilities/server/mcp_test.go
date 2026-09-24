package platformserver

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMCP drives the host as an MCP client would: initialize, list the member's
// tools, call one, and fail to call one outside the catalog.
func TestMCP(t *testing.T) {
	tn, a, _ := setup(t, nil)
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
	tn.app(PlatformApp).(*Console).members["bo"].Roles["a"] = "writer"
	if _, body := rpc(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"a_note","arguments":{"target":"x","idempotencyKey":"m1"}}}`); !strings.Contains(body, `"isError":false`) || !strings.Contains(body, `changeId`) {
		t.Fatalf("call: %s", body)
	}
	if a.texts["x"] == "" {
		t.Fatal("the tool call did not reach app a")
	}
}
