package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	acceptedformat "platformserver/kernel/acceptedformat"
	runtimeprotocol "platformserver/kernel/protocol"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/actions"
	build "platformserver/services/resources/authoring"
	"platformserver/services/work"
)

func TestInvariantK54AIProvenanceSourceSlot(t *testing.T) {
	seats := []runtimeprotocol.Seat{
		{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin"}}},
	}
	apps := func(id string) []platform.Service {
		return []platform.Service{work.New(id, actions.NewAuthority)}
	}
	live := newInvariantTenantWithApps(t, "invariant-k54-prov", apps("invariant-k54-prov"), seats...)
	_, handler := observationHost(live.Tenant)

	if persistence.FormatEpoch != 1 {
		t.Fatalf("storage format epoch changed: got %d want 1", persistence.FormatEpoch)
	}

	const typ = "invariantproject.ticket"
	live.accept(t, "build.object.create", build.ObjectType, "TICKET", map[string]any{
		"name": "ticket", "title": "Ticket", "plural": "Tickets", "display": "title",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true},
			{"name": "note", "title": "Note", "type": "text"},
		},
		"states": []map[string]any{
			{"name": "open", "title": "Open"},
			{"name": "closed", "title": "Closed"},
		},
		"access": []map[string]any{
			{"role": "manager", "read": "all", "create": true, "edit": true, "archive": true},
		},
		"actionTypes": []map[string]any{
			{"ref": map[string]any{"app": "build", "kind": "action", "name": typ + ".close"}},
			{"ref": map[string]any{"app": "build", "kind": "action", "name": typ + ".send"}},
		},
	})
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "TICKET-CLOSE", map[string]any{
		"name": typ + ".close", "object": typ,
		"definition": map[string]any{
			"name": "close", "title": "Close ticket",
			"from": []string{"open"}, "to": "closed", "roles": []string{"manager"},
			"transaction": map[string]any{
				"subject": typ,
				"steps": []map[string]any{{
					"name": "u", "kind": "update", "target": map[string]any{"type": typ, "id": "$subject"},
					"changes": []map[string]any{{"field": "state", "expr": map[string]any{"kind": "choice", "lit": map[string]any{"value": "closed"}}}},
				}},
			},
		},
	})
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "TICKET-SEND", map[string]any{
		"name": typ + ".send", "object": typ,
		"definition": map[string]any{
			"name": "send", "title": "Send ticket update",
			"from": []string{"open", "closed"}, "roles": []string{"manager"},
			"ask":    "Sends outbound customer notice.",
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
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "TICKET-CLOSE", nil)
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "TICKET-SEND", nil)
	live.accept(t, "build.object.publish", build.ObjectType, "TICKET", nil)
	live.accept(t, typ+".create", typ, "T-1", map[string]any{"title": "Scanner Order"})

	mcpRPC := func(role, session string, extraHeaders map[string]string, body any) *httptest.ResponseRecorder {
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
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	initRes := mcpRPC("builder", "", nil, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"clientInfo":      map[string]any{"name": "claude-code", "version": "2.1.311"},
		},
	})
	session := initRes.Header().Get("Mcp-Session-Id")
	if session == "" {
		t.Fatalf("missing Mcp-Session-Id: %s", initRes.Body.String())
	}

	editRes := mcpRPC("builder", session, nil, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name":      "invariantproject_ticket_edit",
			"arguments": map[string]any{"target": "T-1", "payload": map[string]any{"note": "Drafted via MCP"}},
			"_meta": map[string]any{
				"model": "claude-sonnet-4-6",
				"event": "evt-scan-1",
				"run":   "turn-7",
				"usage": map[string]any{"input": 1200, "output": 180, "cached": 400, "cost": 0.0063},
			},
		},
	})
	if editRes.Code != http.StatusOK {
		t.Fatalf("mcp edit status=%d body=%s", editRes.Code, editRes.Body.String())
	}
	rawEdit := live.entries[len(live.entries)-1].Body
	recAccepted, _, err := acceptedformat.DecodeRecord(rawEdit)
	if err != nil || recAccepted.Version != acceptedformat.RecordClaimVersion || len(recAccepted.Claims) != 1 {
		t.Fatalf("expected acceptedformat.Record v6 with 1 source claim, err=%v version=%d claims=%d", err, recAccepted.Version, len(recAccepted.Claims))
	}
	if recAccepted.Claims[0].Attribute != "$ai" || recAccepted.Claims[0].Connector != "ai:mcp" {
		t.Fatalf("unexpected source claim header: %+v", recAccepted.Claims[0])
	}
	if state := live.Claims.Identity.State(); len(state.Entities) != 0 || len(state.Redirects) != 0 {
		t.Fatalf("AI provenance must not pollute connector identity graph: %+v", state)
	}

	closeRes := mcpRPC("builder", session, nil, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{
			"name":      "invariantproject_ticket_close",
			"arguments": map[string]any{"target": "T-1"},
			"_meta":     map[string]any{"model": "claude-sonnet-4-6", "event": "evt-scan-2", "run": "turn-8"},
		},
	})
	if closeRes.Code != http.StatusOK {
		t.Fatalf("mcp pure-transition close status=%d body=%s", closeRes.Code, closeRes.Body.String())
	}
	rawClose := live.entries[len(live.entries)-1].Body
	closeAccepted, _, err := acceptedformat.DecodeRecord(rawClose)
	if err != nil || closeAccepted.Kind != "pure-transition" || closeAccepted.Version != acceptedformat.RecordClaimVersion || len(closeAccepted.Claims) != 1 {
		t.Fatalf("pure-transition AI write did not carry source claim: err=%v kind=%s ver=%d claims=%d", err, closeAccepted.Kind, closeAccepted.Version, len(closeAccepted.Claims))
	}

	delegationCaller := map[string]any{
		"kind": "agent", "name": "overdue-handler", "version": "3",
		"model": "claude-sonnet-4-6", "event": "due:T-1", "run": "run-12",
		"input": 950, "output": 140, "cached": 200, "cost": 0.0048,
	}
	rawCaller, _ := json.Marshal(delegationCaller)
	sendRes := mcpRPC("builder", "", map[string]string{"Platform-AI-Caller": string(rawCaller)}, map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{
			"name":      "invariantproject_ticket_send",
			"arguments": map[string]any{"target": "T-1", "payload": map[string]any{"message": "Shipped today"}},
		},
	})
	var sendRPC struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(sendRes.Body.Bytes(), &sendRPC); err != nil || len(sendRPC.Result.Content) == 0 {
		t.Fatalf("decode ask response: %v %s", err, sendRes.Body.String())
	}
	var held struct {
		Status     string `json:"status"`
		QuestionID string `json:"questionId"`
	}
	_ = json.Unmarshal([]byte(sendRPC.Result.Content[0].Text), &held)
	if held.Status != "pending_approval" || held.QuestionID == "" {
		t.Fatalf("expected pending_approval question, got %+v", held)
	}

	approveSub := live.submission("work.question.answer", runtimeprotocol.WorkQuestionType, held.QuestionID, map[string]any{"choice": "approve", "note": "Go ahead"})
	approveSub.Authority = runtimeprotocol.WorkApp
	if _, refusal := live.Submit(live.member(t), approveSub, invariantClock); refusal != nil {
		t.Fatalf("approve failed: %+v", refusal)
	}
	rawApprove := live.entries[len(live.entries)-1].Body
	batchAccepted, _, err := acceptedformat.DecodeBatch(rawApprove)
	if err != nil || batchAccepted.Version != 11 || len(batchAccepted.Claims) == 0 {
		t.Fatalf("approved ask batch did not store AI source claim in Batch v11: err=%v ver=%d claims=%d", err, batchAccepted.Version, len(batchAccepted.Claims))
	}

	type aiProv struct {
		Kind       string  `json:"kind"`
		Name       string  `json:"name"`
		Version    string  `json:"version"`
		Tool       string  `json:"tool"`
		Model      string  `json:"model"`
		Event      string  `json:"event"`
		Run        string  `json:"run"`
		ApprovedBy string  `json:"approvedBy"`
		Input      int     `json:"input"`
		Output     int     `json:"output"`
		Cached     int     `json:"cached"`
		Cost       float64 `json:"cost"`
	}
	readSection := func(target *Tenant, rtype, rid string) ([]string, map[string]aiProv) {
		t.Helper()
		sec, refusal := target.objectOwner(target.Projection).RecordSection(live.member(t), rtype, rid, "history", invariantClock)
		if refusal != nil {
			t.Fatalf("RecordSection(%s/%s) refused: %+v", rtype, rid, refusal)
		}
		raw, _ := json.Marshal(sec)
		var decoded struct {
			History []struct {
				Change string `json:"change"`
			} `json:"history"`
			AI map[string]aiProv `json:"ai"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		changes := make([]string, len(decoded.History))
		for i, h := range decoded.History {
			changes[i] = h.Change
		}
		return changes, decoded.AI
	}

	checkHistory := func(target *Tenant, label string) {
		t.Helper()
		changes, aiMap := readSection(target, typ, "T-1")
		if len(changes) != 4 {
			t.Fatalf("%s expected 4 history entries, got %d", label, len(changes))
		}
		createChg, editChg, closeChg, sendChg := changes[3], changes[2], changes[1], changes[0]

		if _, hasAI := aiMap[createChg]; hasAI {
			t.Fatalf("%s human create should not carry AI provenance: %+v", label, aiMap[createChg])
		}
		editAI, ok := aiMap[editChg]
		if !ok || editAI.Kind != "mcp" || editAI.Name != "claude-code" || editAI.Version != "2.1.311" ||
			editAI.Tool != "invariantproject_ticket_edit" || editAI.Model != "claude-sonnet-4-6" ||
			editAI.Event != "evt-scan-1" || editAI.Run != "turn-7" ||
			editAI.Input != 1200 || editAI.Output != 180 || editAI.Cached != 400 || editAI.Cost != 0.0063 {
			t.Fatalf("%s unexpected edit AI provenance: %+v", label, editAI)
		}
		closeAI, ok := aiMap[closeChg]
		if !ok || closeAI.Kind != "mcp" || closeAI.Tool != "invariantproject_ticket_close" || closeAI.Run != "turn-8" {
			t.Fatalf("%s unexpected pure-transition AI provenance: %+v", label, closeAI)
		}
		sendAI, ok := aiMap[sendChg]
		if !ok || sendAI.Kind != "agent" || sendAI.Name != "overdue-handler" || sendAI.Version != "3" ||
			sendAI.Tool != "invariantproject_ticket_send" || sendAI.Model != "claude-sonnet-4-6" ||
			sendAI.Event != "due:T-1" || sendAI.Run != "run-12" || sendAI.ApprovedBy != "builder" ||
			sendAI.Input != 950 || sendAI.Output != 140 || sendAI.Cached != 200 || sendAI.Cost != 0.0048 {
			t.Fatalf("%s unexpected ask-approved delegation AI provenance: %+v", label, sendAI)
		}
		qChanges, qAI := readSection(target, runtimeprotocol.WorkQuestionType, held.QuestionID)
		for _, ch := range qChanges {
			if _, hasAI := qAI[ch]; hasAI {
				t.Fatalf("%s work.question human/system change should not carry AI badge: %+v", label, qAI[ch])
			}
		}
	}

	checkHistory(live.Tenant, "live")

	replayed := newInvariantTenantSetup(t, live.ID, apps(live.ID), seats...)
	replayed.project = invariantProject
	if err := replayed.recoverEntries(live.entries); err != nil {
		t.Fatalf("journal replay failed: %v", err)
	}
	checkHistory(replayed.Tenant, "replayed")

	snapRaw, seq, err := live.Snapshot(func() int64 { return int64(len(live.entries)) })
	if err != nil {
		t.Fatalf("snapshot capture failed: %v", err)
	}
	snapTenant := newInvariantTenantSetup(t, live.ID, apps(live.ID), seats...)
	snapTenant.project = invariantProject
	if err := snapTenant.recoverSnapshot(snapRaw, seq); err != nil {
		t.Fatalf("snapshot restore failed: %v", err)
	}
	checkHistory(snapTenant.Tenant, "snapshot")
}
