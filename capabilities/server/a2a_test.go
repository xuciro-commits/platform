package platformserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// ADR-0022 D6, D7: a published agent answers A2A 1.0 tasks for the members who
// call it, acting within their grants; an agent asks an external agent through
// an effect and goes on with its answer, which the journal keeps.
func TestA2A(t *testing.T) {
	model := scriptedModel(t)
	var heard []string
	partner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		heard = append(heard, r.Header.Get("A2A-Version")+" "+r.Header.Get("Authorization")+" "+string(body))
		var req struct {
			Params struct{ Message struct{ MessageID string } }
		}
		json.Unmarshal(body, &req)
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"task":{"id":"t-%s","contextId":"c","status":{"state":"TASK_STATE_COMPLETED"},
			"artifacts":[{"artifactId":"a","name":"answer","parts":[{"data":{"product":"P-200","leadTimeDays":12}}]}]}}}`, req.Params.Message.MessageID)
	}))
	defer partner.Close()
	var journal []Entry
	build := func() *Tenant {
		seat := func(id string, roles map[string]string) Seat {
			return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: roles}}
		}
		tn, err := NewTenant("t-1", NewConsole("t-1", seat("ana", map[string]string{"desk": "clerk", PlatformApp: Admin, AIApp: AIAdmin, AgentApp: AgentAdmin}),
			seat("bo", map[string]string{"desk": "viewer"})), NewAI("t-1"), work.New("t-1"), flow.New("t-1"), NewAgents("t-1"), newDesk("t-1"))
		if err != nil {
			t.Fatal(err)
		}
		tn.AIClient = func(*http.Request) (*http.Response, error) { t.Fatal("a replay called a model"); return nil, nil }
		return tn
	}
	tn := build()
	tn.AIClient = nil
	tn.Record = func(e Entry) { journal = append(journal, e) }
	tn.Secrets = func(string) ([]byte, bool) { return []byte("partner-token"), true }
	member := func(id string) platform.Member { m, _ := tn.app(PlatformApp).(*Console).Member(id); return m }
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	h := NewHost(Tokens(map[string]string{"ana-token": "ana", "bo-token": "bo"}), tn)
	h.Now = func() time.Time { return now }
	keys := 0
	do := func(who, authority, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	think := func(n int) {
		for range n {
			now = now.Add(time.Second)
			tn.Think(now)
			tn.Work(now)
			tn.Dispatch(now)
		}
	}
	expect := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s:\n got %s\nwant %s", what, got, want)
		}
	}
	card := func(agent string) string {
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/a2a/t-1/"+agent+"/.well-known/agent-card.json", nil))
		return fmt.Sprint(rec.Code, " ", rec.Body.String())
	}
	rpc := func(token, agent, method string, params any) map[string]any {
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 7, "method": method, "params": params})
		req := httptest.NewRequest("POST", "/a2a/t-1/"+agent, strings.NewReader(string(raw)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("A2A-Version", "1.0")
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	send := func(token, text, id, task string) map[string]any {
		msg := map[string]any{"messageId": id, "role": "ROLE_USER", "parts": []map[string]any{{"text": text}}}
		if task != "" {
			msg["taskId"] = task
		}
		return rpc(token, "desk.triage", "SendMessage", map[string]any{"message": msg, "configuration": map[string]any{"returnImmediately": true}})
	}
	state := func(out map[string]any) string {
		if e, ok := out["error"].(map[string]any); ok {
			return fmt.Sprint("error ", e["code"])
		}
		task, _ := out["result"].(map[string]any)
		if inner, ok := task["task"].(map[string]any); ok {
			task = inner
		}
		status, _ := task["status"].(map[string]any)
		return fmt.Sprint(task["id"], " ", status["state"])
	}
	for _, id := range []string{"T1", "T3"} {
		do("ana", "desk", "desk.ticket.open", "desk.ticket", id, map[string]string{"subject": "Wifi down in " + id})
	}
	do("ana", AIApp, SchemaProviderAdd, ProviderType, "lm", map[string]any{"kind": "local", "baseUrl": model.URL + "/v1"})
	do("ana", AIApp, SchemaModelEnable, ModelType, "lm/scripted", map[string]string{"access": "users"})
	do("ana", PlatformApp, SchemaSettingSet, SettingType, "agent/model", map[string]string{"value": "lm/scripted"})

	// Unpublished, there is no card; published, the card names the JSON-RPC binding.
	expect("unpublished", card("desk.triage")[:3], "404")
	expect("unpublished call", state(send("ana-token", "x", "m-0", "")), "error -32001")
	do("ana", PlatformApp, SchemaSettingSet, SettingType, "agent/published", map[string]string{"value": "desk.triage"})
	var c struct {
		Name       string
		Interfaces []struct{ URL, ProtocolBinding, ProtocolVersion string } `json:"supportedInterfaces"`
	}
	json.Unmarshal([]byte(card("desk.triage")[4:]), &c)
	expect("card", fmt.Sprint(c.Name, " ", c.Interfaces), "Triage [{http://example.com/a2a/t-1/desk.triage JSONRPC 1.0}]")
	expect("not a member", fmt.Sprint(rpc("nobody", "desk.triage", "GetTask", map[string]string{"id": "x"})), "map[]")

	// A task for ana: the agent acts within her grants, no drafts.
	first := send("ana-token", "Answer ticket T1: wifi", "m-1", "")
	id := strings.Fields(state(first))[0]
	expect("submitted", state(first), id+" TASK_STATE_SUBMITTED")
	expect("resent", state(send("ana-token", "Answer ticket T1: wifi", "m-1", "")), id+" TASK_STATE_SUBMITTED")
	think(4)
	done := rpc("ana-token", "desk.triage", "GetTask", map[string]string{"id": id})
	expect("completed", state(done), id+" TASK_STATE_COMPLETED")
	x, _ := platform.Get[Ticket](tn.automation("desk", false), "T1")
	expect("acted as the agent", x.Status+" "+x.Changed.By, "answered agent:desk.triage")
	expect("only its caller", state(rpc("bo-token", "desk.triage", "GetTask", map[string]string{"id": id})), "error -32001")

	// A question is input required; the caller's next message answers it.
	asked := send("ana-token", "ask about T3", "m-2", "")
	think(2)
	id = strings.Fields(state(asked))[0]
	waiting := rpc("ana-token", "desk.triage", "GetTask", map[string]string{"id": id})
	expect("input required", state(waiting), id+" TASK_STATE_INPUT_REQUIRED")
	send("ana-token", "yes", "m-3", id)
	think(3)
	expect("answered", state(rpc("ana-token", "desk.triage", "GetTask", map[string]string{"id": id})), id+" TASK_STATE_COMPLETED")

	// Canceled while it works.
	loop := send("ana-token", "loop", "m-4", "")
	id = strings.Fields(state(loop))[0]
	think(1)
	expect("canceled", state(rpc("ana-token", "desk.triage", "CancelTask", map[string]string{"id": id})), id+" TASK_STATE_CANCELED")

	// Calling out: the scout's question goes to the partner's agent as an
	// effect; its answer, journaled, resumes the run.
	expect("a2a endpoint", do("ana", PlatformApp, SchemaEndpointAdd, EndpointType, "partner",
		map[string]any{"kind": "a2a", "url": partner.URL, "secret": "partner", "effects": []string{"desk/lookup"}, "allowPrivate": true}), "ok")
	do("ana", AgentApp, SchemaRunStart, RunType, "S1", map[string]any{"agent": "desk.scout", "goal": "scout the lead time"})
	think(4)
	run, _ := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "S1")
	expect("scouted", run.State+" "+run.Result, `done after 1: sent desk/lookup to 1 receivers; waiting for the answer
answered: {"product":"P-200","leadTimeDays":12}`)
	expect("heard", fmt.Sprint(len(heard), strings.HasPrefix(heard[0], "1.0 Bearer partner-token "), strings.Contains(heard[0], `"messageId":"t-1:desk:lookup:S1:1:partner"`)), "1 true true")

	// An endpoint whose tasks cannot be recalled holds what an agent sends;
	// discarded, the run goes on with that.
	do("ana", PlatformApp, SchemaEndpointRemove, EndpointType, "partner", map[string]any{})
	do("ana", PlatformApp, SchemaEndpointAdd, EndpointType, "partner-2",
		map[string]any{"kind": "a2a", "url": partner.URL, "effects": []string{"desk/lookup"}, "allowPrivate": true, "irreversible": true})
	do("ana", AgentApp, SchemaRunStart, RunType, "S2", map[string]any{"agent": "desk.scout", "goal": "scout again"})
	think(2)
	effects := tn.Effects(now)
	expect("held", effects[0].State+" "+effects[0].Run, "held S2")
	do("ana", PlatformApp, SchemaEffectDiscard, EffectType, effects[0].ID, map[string]any{})
	think(2)
	run, _ = platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "S2")
	expect("discarded", fmt.Sprint(run.State, " ", strings.HasSuffix(run.Result, "discarded by ana"), " ", run.Signals[0].Kind, " ", len(heard)), "done true discarded 1")
	CheckReplay(t, tn, journal, build)
}
