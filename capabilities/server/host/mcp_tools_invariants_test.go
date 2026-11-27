package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	appauthoring "platformserver/services/apps/authoring"
	"platformserver/services/devops/packages"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	"platformserver/services/work"
)

func TestInvariantK53MCPTypeGeneratedToolSurface(t *testing.T) {
	live := newInvariantTenantWithApps(t, "invariant-k53-mcp",
		[]platform.Service{work.New("invariant-k53-mcp", actions.NewAuthority)},
		runtimeprotocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin"}}},
		runtimeprotocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{"invariantproject": "viewer", build.ID: ontology.UserRole}}},
		runtimeprotocol.Seat{Subjects: []string{"auditor"}, Member: platform.Member{ID: "auditor", Roles: map[string]string{}}},
	)
	_, handler := observationHost(live.Tenant)

	mcpRPC := func(role, session string, body any) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "http://localhost:8495/mcp", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+observationToken(live.ID, role))
		req.Header.Set("Platform-Tenant", live.ID)
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var parsed map[string]any
		if rec.Body.Len() > 0 {
			if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
				t.Fatalf("decode mcp response (%d): %v %s", rec.Code, err, rec.Body.String())
			}
		}
		return rec, parsed
	}

	initRes, initBody := mcpRPC("builder", "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"clientInfo":      map[string]any{"name": "claude-code", "version": "2.1.311"},
		},
	})
	if initRes.Code != http.StatusOK {
		t.Fatalf("initialize status=%d body=%s", initRes.Code, initRes.Body.String())
	}
	session := initRes.Header().Get("Mcp-Session-Id")
	if session == "" {
		t.Fatalf("expected Mcp-Session-Id header on initialize response: %+v", initBody)
	}
	notifRes, _ := mcpRPC("builder", session, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if notifRes.Code != http.StatusAccepted {
		t.Fatalf("expected 202 on notifications/initialized, got %d", notifRes.Code)
	}

	listTools := func(role, sess string) map[string]map[string]any {
		t.Helper()
		res, body := mcpRPC(role, sess, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
		if res.Code != http.StatusOK {
			t.Fatalf("tools/list (%s): %d %s", role, res.Code, res.Body.String())
		}
		result, _ := body["result"].(map[string]any)
		items, _ := result["tools"].([]any)
		out := map[string]map[string]any{}
		for _, item := range items {
			m, _ := item.(map[string]any)
			name, _ := m["name"].(string)
			out[name] = m
		}
		return out
	}

	callTool := func(role, sess, name string, args map[string]any) (map[string]any, bool) {
		t.Helper()
		res, body := mcpRPC(role, sess, map[string]any{
			"jsonrpc": "2.0", "id": 3, "method": "tools/call",
			"params": map[string]any{"name": name, "arguments": args},
		})
		if res.Code != http.StatusOK {
			t.Fatalf("tools/call %s (%s): %d %s", name, role, res.Code, res.Body.String())
		}
		result, _ := body["result"].(map[string]any)
		isErr, _ := result["isError"].(bool)
		content, _ := result["content"].([]any)
		if len(content) == 0 {
			t.Fatalf("tools/call %s empty content: %+v", name, body)
		}
		first, _ := content[0].(map[string]any)
		text, _ := first["text"].(string)
		var payload map[string]any
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			t.Fatalf("tools/call %s invalid JSON text %q: %v", name, text, err)
		}
		return payload, isErr
	}

	initialTools := listTools("builder", session)
	for name := range initialTools {
		if strings.HasPrefix(name, "read_") || strings.HasPrefix(name, "load_") || strings.HasPrefix(name, "aggregate_") {
			t.Fatalf("unexpected legacy handwritten/split tool %q in tools/list", name)
		}
	}
	if _, ok := initialTools["query_invariantproject_ticket"]; ok {
		t.Fatal("unpublished type appeared in tools/list")
	}

	const typ = "invariantproject.ticket"
	denyAI := false
	live.accept(t, "build.object.create", build.ObjectType, "TICKET", map[string]any{
		"name": "ticket", "title": "Ticket", "plural": "Tickets", "display": "title",
		"description": "Customer request tracked by the team.",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true, "search": true},
			{"name": "stage", "title": "Stage", "type": "choice", "default": "new", "choices": "new:New,triaged:Triaged"},
			{"name": "note", "title": "Note", "type": "text"},
			{"name": "secret", "title": "Secret", "type": "text", "aiInput": denyAI},
		},
		"states": []map[string]any{{"name": "open", "title": "Open"}},
		"access": []map[string]any{
			{"role": "viewer", "read": "all"},
			{"role": "manager", "read": "all", "create": true, "edit": true, "archive": true},
		},
		"ask": map[string]any{"archive": "Archiving hides the ticket from active lists."},
		"actionTypes": []map[string]any{
			{"ref": map[string]any{"app": "build", "kind": "action", "name": typ + ".triage"}},
			{"ref": map[string]any{"app": "build", "kind": "action", "name": typ + ".dispatch"}},
		},
	})
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "TICKET-TRIAGE", map[string]any{
		"name": typ + ".triage", "object": typ,
		"definition": map[string]any{
			"name": "triage", "title": "Triage ticket", "description": "Mark the ticket triaged with a note.",
			"from": []string{"open"}, "roles": []string{"manager"},
			"inputs": []map[string]any{{"name": "note", "title": "Note", "type": "text", "required": true}},
			"transaction": map[string]any{
				"subject": typ,
				"steps": []map[string]any{{
					"name": "u", "kind": "update", "target": map[string]any{"type": typ, "id": "$subject"},
					"changes": []map[string]any{
						{"field": "stage", "expr": map[string]any{"kind": "choice", "lit": map[string]any{"value": "triaged"}}},
						{"field": "note", "expr": map[string]any{"kind": "text", "ref": "input.note"}},
					},
				}},
			},
		},
	})
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "TICKET-DISPATCH", map[string]any{
		"name": typ + ".dispatch", "object": typ,
		"definition": map[string]any{
			"name": "dispatch", "title": "Dispatch outbound message", "description": "Send external update.",
			"from": []string{"open"}, "roles": []string{"manager"},
			"ask":    "Sends an external notification immediately.",
			"inputs": []map[string]any{{"name": "message", "title": "Message", "type": "text", "required": true}},
			"transaction": map[string]any{
				"subject": typ,
				"steps": []map[string]any{{
					"name": "u", "kind": "update", "target": map[string]any{"type": typ, "id": "$subject"},
					"changes": []map[string]any{{"field": "note", "expr": map[string]any{"kind": "text", "ref": "input.message"}}},
				}},
			},
		},
	})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "TICKET-TRIAGE", nil)
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "TICKET-DISPATCH", nil)
	live.accept(t, "build.object.publish", build.ObjectType, "TICKET", nil)
	tmpl := appauthoring.ListDetailPage("ticket", "Tickets", "", platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: typ}, []string{"title", "stage"}, []string{"title", "stage", "note"}, nil)
	live.accept(t, "build.page.create", runtimeprotocol.PageType, "PAGE-TICKET", map[string]any{
		"format": tmpl.Format, "name": "ticket", "title": "Tickets", "object": typ,
		"dataBindings": tmpl.DataBindings, "actionTriggers": tmpl.ActionTriggers, "scopeVisibility": tmpl.ScopeVisibility, "widgetSpec": tmpl.WidgetSpec,
	})
	live.accept(t, "build.app.create", runtimeprotocol.AppType, "APP", map[string]any{
		"name": "invariantproject", "title": "Tickets", "pages": []string{"ticket"},
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: typ}},
	})
	live.accept(t, "build.page.publish", runtimeprotocol.PageType, "PAGE-TICKET", nil)
	live.accept(t, "build.app.publish", runtimeprotocol.AppType, "APP", nil)

	builderTools := listTools("builder", session)
	queryTool, ok := builderTools["query_invariantproject_ticket"]
	if !ok {
		t.Fatalf("missing query tool for published type: keys=%v", slices.Sorted(func(yield func(string) bool) {
			for k := range builderTools {
				if !yield(k) {
					return
				}
			}
		}))
	}
	queryDesc, _ := queryTool["description"].(string)
	if !strings.Contains(queryDesc, "Customer request tracked by the team.") || strings.Contains(queryDesc, "secret") {
		t.Fatalf("query tool description should include help and hide aiInput:false field: %q", queryDesc)
	}
	createTool, ok := builderTools["invariantproject_ticket_create"]
	if !ok {
		t.Fatal("missing create tool")
	}
	rawCreateSchema, _ := json.Marshal(createTool["inputSchema"])
	if strings.Contains(string(rawCreateSchema), `"secret"`) {
		t.Fatalf("create tool leaked aiInput:false field secret: %s", rawCreateSchema)
	}
	dispatchTool, ok := builderTools["invariantproject_ticket_dispatch"]
	if !ok {
		t.Fatal("missing dispatch tool")
	}
	dispatchDesc, _ := dispatchTool["description"].(string)
	if dispatchTool["x-platform-risk"] != "ask" || !strings.Contains(dispatchDesc, "Sends an external notification immediately.") {
		t.Fatalf("dispatch tool missing ask risk/reason: %+v", dispatchTool)
	}
	triageTool, ok := builderTools["invariantproject_ticket_triage"]
	if !ok || triageTool["x-platform-risk"] != "direct" {
		t.Fatalf("triage tool risk mismatch: %+v", triageTool)
	}
	if _, ok := builderTools["work_question_answer"]; ok {
		t.Fatal("work.question.answer must not be exposed on /mcp")
	}

	viewerTools := listTools("other", "")
	if _, ok := viewerTools["query_invariantproject_ticket"]; !ok {
		t.Fatal("viewer should see query_invariantproject_ticket")
	}
	if _, ok := viewerTools["invariantproject_ticket_triage"]; ok {
		t.Fatal("viewer should not see manager action tool")
	}
	outsiderTools := listTools("auditor", "")
	if _, ok := outsiderTools["query_invariantproject_ticket"]; ok {
		t.Fatal("outsider should not see query_invariantproject_ticket")
	}
	if _, isErr := callTool("auditor", "", "query_invariantproject_ticket", map[string]any{}); !isErr {
		t.Fatal("expected outsider query to be refused")
	}

	live.accept(t, typ+".create", typ, "T-HUMAN", map[string]any{"title": "Human seeded", "secret": "TOP-SECRET-TOKEN"})

	created, isErr := callTool("builder", session, "invariantproject_ticket_create", map[string]any{
		"target":  "T-MCP",
		"payload": map[string]any{"title": "Created via MCP"},
	})
	if isErr || created["status"] != "accepted" {
		t.Fatalf("expected direct create acceptance, got err=%v body=%+v", isErr, created)
	}
	if link, _ := created["link"].(string); !strings.HasSuffix(link, "/invariantproject/ticket/T-MCP") {
		t.Fatalf("expected workspace record link ending in /invariantproject/ticket/T-MCP, got %q", link)
	}

	if _, isErr := callTool("builder", session, "invariantproject_ticket_edit", map[string]any{
		"target":  "T-HUMAN",
		"payload": map[string]any{"secret": "OVERWRITE"},
	}); !isErr {
		t.Fatal("expected AI write to aiInput:false field secret to be refused")
	}

	queried, isErr := callTool("builder", session, "query_invariantproject_ticket", map[string]any{
		"search": "Human",
	})
	if isErr {
		t.Fatalf("query failed: %+v", queried)
	}
	rawQueried, _ := json.Marshal(queried)
	if strings.Contains(string(rawQueried), "TOP-SECRET-TOKEN") || strings.Contains(string(rawQueried), `"secret"`) {
		t.Fatalf("query leaked aiInput:false value: %s", rawQueried)
	}
	if link, _ := queried["link"].(string); !strings.HasSuffix(link, "/invariantproject/ticket") {
		t.Fatalf("expected workspace list link ending in /invariantproject/ticket, got %q", link)
	}

	counted, isErr := callTool("builder", session, "query_invariantproject_ticket", map[string]any{
		"measures": []string{"count"},
	})
	if isErr {
		t.Fatalf("aggregate query failed: %+v", counted)
	}
	if link, _ := counted["link"].(string); !strings.HasSuffix(link, "/invariantproject/ticket") {
		t.Fatalf("expected workspace list link on aggregate, got %q", link)
	}

	triaged, isErr := callTool("builder", session, "invariantproject_ticket_triage", map[string]any{
		"target":  "T-MCP",
		"payload": map[string]any{"note": "Checked by MCP"},
	})
	if isErr || triaged["status"] != "accepted" {
		t.Fatalf("triage failed: err=%v body=%+v", isErr, triaged)
	}
	recMap, _ := triaged["record"].(map[string]any)
	if recMap["stage"] != "triaged" || recMap["note"] != "Checked by MCP" {
		t.Fatalf("unexpected triaged record: %+v", triaged)
	}

	held, isErr := callTool("builder", session, "invariantproject_ticket_dispatch", map[string]any{
		"target":  "T-MCP",
		"payload": map[string]any{"message": "Hello customer"},
	})
	if isErr || held["status"] != "pending_approval" {
		t.Fatalf("expected ask action to return pending_approval, got err=%v body=%+v", isErr, held)
	}
	qid, _ := held["questionId"].(string)
	if qid == "" || !strings.Contains(held["link"].(string), "/home/inbox/approvals?question="+url.QueryEscape(qid)) {
		t.Fatalf("expected inbox approval link with questionId, got %+v", held)
	}
	afterHold, _ := callTool("builder", session, "query_invariantproject_ticket", map[string]any{"id": "T-MCP"})
	rows, _ := afterHold["records"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["note"] != "Checked by MCP" {
		t.Fatalf("ask action mutated record before approval: %+v", afterHold)
	}

	approveSub := live.submission("work.question.answer", runtimeprotocol.WorkQuestionType, qid, map[string]any{"choice": "approve", "note": "Approved"})
	approveSub.Authority = runtimeprotocol.WorkApp
	if _, refusal := live.Submit(live.member(t), approveSub, invariantClock); refusal != nil {
		t.Fatalf("human inbox approval failed: %+v", refusal)
	}
	afterApprove, _ := callTool("builder", session, "query_invariantproject_ticket", map[string]any{"id": "T-MCP"})
	rows, _ = afterApprove["records"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["note"] != "Hello customer" {
		t.Fatalf("approved ask action did not execute once: %+v", afterApprove)
	}

	rawPkg, err := os.ReadFile(fixturePath(t, "production-provider"))
	if err != nil {
		t.Fatal(err)
	}
	var offer platform.PackageDescriptor
	if err := json.Unmarshal(rawPkg, &offer); err != nil {
		t.Fatal(err)
	}
	for i, asset := range offer.Blueprint.Assets {
		if asset.Ref.Name == "build.stockreceipt" {
			var object map[string]json.RawMessage
			_ = json.Unmarshal(asset.Body, &object)
			object["access"] = platform.Raw([]ontology.Access{{Role: ontology.UserRole, Read: "none"}})
			offer.Blueprint.Assets[i].Body = platform.Raw(object)
		}
	}
	preview, err := live.InspectPackage(t.Context(), runtimeprotocol.PackageInspectRequest{Content: platform.Raw(offer)})
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := live.Member("admin")
	pkgSubmit := func(schema string, body any) {
		t.Helper()
		s := live.submission(schema, runtimeprotocol.PackageType, preview.Descriptor.ID, body)
		s.Authority, s.PrincipalId = "platform", admin.ID
		if _, refusal := live.Submit(admin, s, invariantClock); refusal != nil {
			t.Fatalf("%s: %s %s", schema, refusal.Code, refusal.Message)
		}
	}
	pkgSubmit(packages.SchemaPackageImport, map[string]any{"id": preview.Descriptor.ID, "digest": preview.Digest})
	pkgSubmit(packages.SchemaPackageInstall, map[string]any{"id": preview.Descriptor.ID, "version": preview.Descriptor.Version})

	installedTools := listTools("builder", session)
	if _, ok := installedTools["query_build_batchproviderorder"]; !ok {
		t.Fatal("installed package object query tool missing")
	}
	if _, ok := installedTools["build_batchproviderorder_start"]; !ok {
		t.Fatal("installed package action tool missing")
	}
	pkgSubmit(packages.SchemaPackageDrain, map[string]any{"id": preview.Descriptor.ID})
	pkgSubmit(packages.SchemaPackageRetire, map[string]any{"id": preview.Descriptor.ID})

	retiredTools := listTools("builder", session)
	if _, ok := retiredTools["query_build_batchproviderorder"]; ok {
		t.Fatal("retired package query tool remained in tools/list")
	}
	if _, ok := retiredTools["build_batchproviderorder_start"]; ok {
		t.Fatal("retired package action tool remained in tools/list")
	}
}
