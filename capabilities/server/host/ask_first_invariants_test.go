package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	build "platformserver/services/resources/authoring"
	"platformserver/services/work"
)

func TestInvariantK52AskFirstHoldsAIActionsUntilInboxApproval(t *testing.T) {
	viewerSeat := runtimeprotocol.Seat{
		Subjects: []string{"other"},
		Member:   platform.Member{ID: "other", Roles: map[string]string{"invariantproject": "viewer"}},
	}
	live := newInvariantTenantWithApps(t, "invariant-k52-ask", []platform.Service{work.New("invariant-k52-ask", actions.NewAuthority)}, viewerSeat)

	live.accept(t, "build.object.create", build.ObjectType, "BAD-LEDGER", map[string]any{
		"name": "badledger", "title": "Bad ledger", "ledger": true,
		"fields":   []map[string]any{{"name": "bucket", "title": "Bucket", "type": "text", "required": true}, {"name": "qty", "title": "Qty", "type": "integer", "required": true}},
		"balances": []map[string]any{{"name": "bal", "title": "Bal", "keys": []string{"bucket"}, "measures": []string{"qty"}}},
		"ask":      map[string]any{"archive": true},
	})
	if _, refusal := live.Submit(live.member(t), live.submission("build.object.publish", build.ObjectType, "BAD-LEDGER", nil), invariantClock); refusal == nil || !strings.Contains(refusal.Message, "archive") {
		t.Fatalf("expected ledger archive ask refusal, got %+v", refusal)
	}

	live.accept(t, "build.object.create", build.ObjectType, "BAD-ACT", map[string]any{
		"name": "badact", "title": "Bad act",
		"fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}},
		"states": []map[string]any{{"name": "open", "title": "Open"}},
	})
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "BAD-PING", map[string]any{
		"name": "invariantproject.badact.ping", "object": "invariantproject.badact",
		"definition": map[string]any{
			"name": "ping", "title": "Ping", "from": []string{"open"}, "ask": "   ",
			"transaction": map[string]any{"subject": "invariantproject.badact", "steps": []map[string]any{{"name": "s", "kind": "update", "target": map[string]any{"type": "invariantproject.badact", "id": "$subject"}, "changes": []map[string]any{{"field": "note", "expr": map[string]any{"kind": "text", "lit": map[string]any{"value": "ok"}}}}}}},
		},
	})
	if _, refusal := live.Submit(live.member(t), live.submission("build.actiontype.publish", build.ActionTypeType, "BAD-PING", nil), invariantClock); refusal == nil || !strings.Contains(refusal.Message, "ask") {
		t.Fatalf("expected empty ask reason refusal, got %+v", refusal)
	}

	const typ = "invariantproject.notice"
	live.accept(t, "build.object.create", build.ObjectType, "NOTICE", map[string]any{
		"name": "notice", "title": "Notice", "display": "title",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true, "search": true},
			{"name": "note", "title": "Note", "type": "text"},
		},
		"states": []map[string]any{{"name": "open", "title": "Open"}},
		"access": []map[string]any{
			{"role": "viewer", "read": "all"},
			{"role": "manager", "read": "all", "create": true, "edit": true, "archive": true},
		},
		"ask":         map[string]any{"archive": "Removes this record from active lists."},
		"actionTypes": []map[string]any{{"ref": map[string]any{"app": "build", "kind": "action", "name": typ + ".send"}}},
	})
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "NOTICE-SEND", map[string]any{
		"name": typ + ".send", "object": typ,
		"definition": map[string]any{
			"name": "send", "title": "Send notice", "from": []string{"open"}, "roles": []string{"manager"},
			"ask":    "Sends an outbound message to the customer.",
			"inputs": []map[string]any{{"name": "message", "title": "Message", "type": "text", "required": true}},
			"transaction": map[string]any{
				"subject": typ,
				"steps": []map[string]any{{
					"name": "write", "kind": "update", "target": map[string]any{"type": typ, "id": "$subject"},
					"changes": []map[string]any{{"field": "note", "expr": map[string]any{"kind": "text", "ref": "input.message"}}},
				}},
			},
		},
	})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "NOTICE-SEND", nil)
	live.accept(t, "build.object.publish", build.ObjectType, "NOTICE", nil)

	_, handler := observationHost(live.Tenant)
	entResp := observationRequest(t, handler, live.ID, observationToken(live.ID, "builder"), "/v1/entities", nil)
	var entities []map[string]any
	if entResp.Code != http.StatusOK || json.Unmarshal(entResp.Body.Bytes(), &entities) != nil {
		t.Fatalf("entities HTTP %d: %s", entResp.Code, entResp.Body)
	}
	entIdx := slices.IndexFunc(entities, func(e map[string]any) bool { return e["type"] == typ })
	if entIdx < 0 {
		t.Fatalf("missing entity %s", typ)
	}
	askList, _ := entities[entIdx]["ask"].([]any)
	if len(askList) != 2 {
		t.Fatalf("expected 2 ask entries on entity description, got %#v", entities[entIdx]["ask"])
	}

	actResp := observationRequest(t, handler, live.ID, observationToken(live.ID, "builder"), "/v1/actions", nil)
	var catalog []map[string]any
	if actResp.Code != http.StatusOK || json.Unmarshal(actResp.Body.Bytes(), &catalog) != nil {
		t.Fatalf("actions HTTP %d: %s", actResp.Code, actResp.Body)
	}
	sendIdx := slices.IndexFunc(catalog, func(a map[string]any) bool { return a["schema"] == typ+".send" })
	archIdx := slices.IndexFunc(catalog, func(a map[string]any) bool { return a["schema"] == typ+".archive" })
	if sendIdx < 0 || catalog[sendIdx]["ask"] != true || catalog[sendIdx]["askReason"] != "Sends an outbound message to the customer." {
		t.Fatalf("send action catalog ask metadata: %#v", catalog[sendIdx])
	}
	if archIdx < 0 || catalog[archIdx]["ask"] != true || catalog[archIdx]["askReason"] != "Removes this record from active lists." {
		t.Fatalf("archive action catalog ask metadata: %#v", catalog[archIdx])
	}

	live.accept(t, typ+".create", typ, "N-1", map[string]any{"title": "Order #1"})
	live.accept(t, typ+".send", typ, "N-1", map[string]any{"message": "Direct human send"})

	readNote := func(id string) (string, bool, uint32) {
		t.Helper()
		view, refusal := live.RecordOf(t.Context(), live.member(t), typ, id, invariantClock)
		if refusal != nil {
			if refusal.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND {
				return "", true, 0
			}
			t.Fatalf("read %s: %+v", id, refusal)
		}
		var row struct {
			Revision uint32 `json:"revision"`
			Archived bool   `json:"archived"`
			Note     string `json:"note"`
		}
		_ = json.Unmarshal(platform.Raw(view.Record), &row)
		return row.Note, row.Archived, row.Revision
	}
	if note, _, _ := readNote("N-1"); note != "Direct human send" {
		t.Fatalf("human call should execute directly, got note=%q", note)
	}

	schemaVersionOf := func(schema string) string {
		idx := slices.IndexFunc(catalog, func(a map[string]any) bool { return a["schema"] == schema })
		if idx < 0 {
			return ""
		}
		sv, _ := catalog[idx]["schemaVersion"].(map[string]any)
		v, _ := sv["version"].(string)
		return v
	}
	sendHTTP := func(principal, aiHeader string, sub *pb.Submission) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := protojson.Marshal(sub)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+observationToken(live.ID, principal))
		req.Header.Set("Platform-Tenant", live.ID)
		req.Header.Set("Content-Type", "application/json")
		if ver := schemaVersionOf(sub.GetSchema().GetName()); ver != "" {
			req.Header.Set(SchemaVersionHeader, ver)
		}
		if aiHeader != "" {
			req.Header.Set("Platform-AI-Caller", aiHeader)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	unauthSub := &pb.Submission{
		TenantId: live.ID, PrincipalId: "other", Authority: build.ID, IdempotencyKey: "k52-viewer-ai",
		Target: &pb.EntityRef{Type: typ, Id: "N-1"}, Schema: &pb.SchemaRef{Name: typ + ".send", Version: 1},
		Payload: platform.Raw(map[string]any{"message": "Blocked"}),
	}
	if res := sendHTTP("other", `{"kind":"mcp","name":"claude-code"}`, unauthSub); res.Code != http.StatusForbidden {
		t.Fatalf("expected unauthorized AI call to fail with 403, got %d: %s", res.Code, res.Body)
	}

	aiSend := &pb.Submission{
		TenantId: live.ID, PrincipalId: "builder", Authority: build.ID, IdempotencyKey: "k52-ai-send-1",
		Target: &pb.EntityRef{Type: typ, Id: "N-1"}, Schema: &pb.SchemaRef{Name: typ + ".send", Version: 1},
		Payload: platform.Raw(map[string]any{"message": "Confirm delivery window"}),
	}
	if res := sendHTTP("builder", `{"kind":"mcp","name":"claude-code","tool":"invariantproject_notice_send"}`, aiSend); res.Code != http.StatusOK {
		t.Fatalf("expected AI ask call to be accepted as question, got %d: %s", res.Code, res.Body)
	}
	noteBefore, _, revBefore := readNote("N-1")
	if noteBefore != "Direct human send" {
		t.Fatalf("AI ask call must not write business record before approval; got note=%q", noteBefore)
	}

	inboxResp := observationRequest(t, handler, live.ID, observationToken(live.ID, "builder"), "/v1/inbox", nil)
	var tasks []runtimeprotocol.WorkTask
	if inboxResp.Code != http.StatusOK || json.Unmarshal(inboxResp.Body.Bytes(), &tasks) != nil || len(tasks) != 1 {
		t.Fatalf("expected 1 open inbox question task, got HTTP %d: %s", inboxResp.Code, inboxResp.Body)
	}
	qID := strings.TrimPrefix(tasks[0].Ref, "work.question/")
	if qID == tasks[0].Ref || qID == "" {
		t.Fatalf("expected work.question/ ref on inbox task, got %+v", tasks[0])
	}

	aiSelfApprove := &pb.Submission{
		TenantId: live.ID, PrincipalId: "builder", Authority: work.ID, IdempotencyKey: "k52-ai-self-approve",
		Target: &pb.EntityRef{Type: "work.question", Id: qID}, Schema: &pb.SchemaRef{Name: "work.question.answer", Version: 1},
		Payload: platform.Raw(map[string]any{"choice": "approve"}),
	}
	if res := sendHTTP("builder", `{"kind":"mcp","name":"claude-code"}`, aiSelfApprove); res.Code != http.StatusForbidden {
		t.Fatalf("AI caller must not answer its own question: got %d: %s", res.Code, res.Body)
	}

	approveSub := &pb.Submission{
		TenantId: live.ID, PrincipalId: "builder", Authority: work.ID, IdempotencyKey: "k52-human-approve-1",
		Target: &pb.EntityRef{Type: "work.question", Id: qID}, Schema: &pb.SchemaRef{Name: "work.question.answer", Version: 1},
		Payload: platform.Raw(map[string]any{"choice": "approve"}),
	}
	if res := sendHTTP("builder", "", approveSub); res.Code != http.StatusOK {
		t.Fatalf("human approval failed: %d: %s", res.Code, res.Body)
	}
	noteAfter, _, revAfter := readNote("N-1")
	if noteAfter != "Confirm delivery window" || revAfter != revBefore+1 {
		t.Fatalf("expected approved ask action to run once: note=%q rev=%d (was %d)", noteAfter, revAfter, revBefore)
	}

	dupApprove := &pb.Submission{
		TenantId: live.ID, PrincipalId: "builder", Authority: work.ID, IdempotencyKey: "k52-human-approve-2",
		Target: &pb.EntityRef{Type: "work.question", Id: qID}, Schema: &pb.SchemaRef{Name: "work.question.answer", Version: 1},
		Payload: platform.Raw(map[string]any{"choice": "approve"}),
	}
	if res := sendHTTP("builder", "", dupApprove); res.Code != http.StatusConflict {
		t.Fatalf("duplicate approval must be rejected with 409, got %d: %s", res.Code, res.Body)
	}
	if _, _, revCheck := readNote("N-1"); revCheck != revAfter {
		t.Fatalf("duplicate approval mutated record revision: %d != %d", revCheck, revAfter)
	}

	aiArchive := &pb.Submission{
		TenantId: live.ID, PrincipalId: "builder", Authority: build.ID, IdempotencyKey: "k52-ai-archive-1",
		Target: &pb.EntityRef{Type: typ, Id: "N-1"}, Schema: &pb.SchemaRef{Name: typ + ".archive", Version: 1},
		Payload: []byte("{}"),
	}
	if res := sendHTTP("builder", `{"kind":"mcp","name":"claude-code"}`, aiArchive); res.Code != http.StatusOK {
		t.Fatalf("expected AI archive ask to create question, got %d: %s", res.Code, res.Body)
	}
	if _, archived, _ := readNote("N-1"); archived {
		t.Fatal("record was archived before approval")
	}
	inboxResp2 := observationRequest(t, handler, live.ID, observationToken(live.ID, "builder"), "/v1/inbox", nil)
	_ = json.Unmarshal(inboxResp2.Body.Bytes(), &tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 open archive question task, got %+v", tasks)
	}
	qID2 := strings.TrimPrefix(tasks[0].Ref, "work.question/")
	rejectSub := &pb.Submission{
		TenantId: live.ID, PrincipalId: "builder", Authority: work.ID, IdempotencyKey: "k52-human-reject-1",
		Target: &pb.EntityRef{Type: "work.question", Id: qID2}, Schema: &pb.SchemaRef{Name: "work.question.answer", Version: 1},
		Payload: platform.Raw(map[string]any{"choice": "reject"}),
	}
	if res := sendHTTP("builder", "", rejectSub); res.Code != http.StatusOK {
		t.Fatalf("human reject failed: %d: %s", res.Code, res.Body)
	}
	if _, archived, _ := readNote("N-1"); archived {
		t.Fatal("rejected archive question must not archive record")
	}

	aiExpiring := &pb.Submission{
		TenantId: live.ID, PrincipalId: "builder", Authority: build.ID, IdempotencyKey: "k52-ai-expiring-1",
		Target: &pb.EntityRef{Type: typ, Id: "N-1"}, Schema: &pb.SchemaRef{Name: typ + ".send", Version: 1},
		Payload: platform.Raw(map[string]any{"message": "Should expire"}),
	}
	if _, refusal := live.Submit(runtimeprotocol.WithAICaller(live.member(t), &runtimeprotocol.AICaller{Kind: "delegation", Name: "daily", Due: invariantClock.Add(10 * time.Minute)}), aiExpiring, invariantClock); refusal != nil {
		t.Fatalf("submit expiring ask: %+v", refusal)
	}
	inboxResp3 := observationRequest(t, handler, live.ID, observationToken(live.ID, "builder"), "/v1/inbox", nil)
	_ = json.Unmarshal(inboxResp3.Body.Bytes(), &tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 open expiring question task, got %+v", tasks)
	}
	qID3 := strings.TrimPrefix(tasks[0].Ref, "work.question/")
	lateApprove := &pb.Submission{
		TenantId: live.ID, PrincipalId: "builder", Authority: work.ID, IdempotencyKey: "k52-late-approve",
		Target: &pb.EntityRef{Type: "work.question", Id: qID3}, Schema: &pb.SchemaRef{Name: "work.question.answer", Version: 1},
		Payload: platform.Raw(map[string]any{"choice": "approve"}),
	}
	if _, refusal := live.Submit(live.member(t), lateApprove, invariantClock.Add(30*time.Minute)); refusal != nil {
		t.Fatalf("late answer should transition question to expired cleanly, got %+v", refusal)
	}
	if noteNow, _, _ := readNote("N-1"); noteNow != "Confirm delivery window" {
		t.Fatalf("expired question must not execute held action: note=%q", noteNow)
	}

	before := live.recordImage(t)
	fresh := newInvariantTenantSetup(t, live.ID, []platform.Service{work.New(live.ID, actions.NewAuthority)}, viewerSeat)
	fresh.project = invariantProject
	if err := fresh.recoverEntries(live.entries); err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	if diff := differingTypes(before, fresh.recordImage(t)); len(diff) > 0 {
		t.Fatalf("replay differed on types: %v", diff)
	}
}
