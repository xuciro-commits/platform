package platformserver

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/ai"
	"platformserver/apps/flow"
	"platformserver/apps/knowledge"
	"platformserver/apps/work"
	"platformserver/platform"
)

// ADR-0022 3a: documents and apps' knowledge fields are found by words and by
// meaning, only by those who may read them; an agent's search is journaled
// with its step and cited, replay neither searches nor embeds, and every model
// call's transcript is kept outside the journal.
func TestKnowledge(t *testing.T) {
	embeds := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/embeddings") { // hashed words: texts sharing words point alike
			var req struct{ Input []string }
			json.NewDecoder(r.Body).Decode(&req)
			embeds++
			var data []map[string]any
			for i, text := range req.Input {
				v := make([]float32, 16)
				for _, w := range words(text) {
					h := fnv.New32a()
					h.Write([]byte(w))
					v[h.Sum32()%16]++
				}
				data = append(data, map[string]any{"index": i, "embedding": v})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]int{"prompt_tokens": 7 * len(req.Input)}})
			return
		}
		var req struct{ Messages []map[string]any }
		json.NewDecoder(r.Body).Decode(&req)
		last := req.Messages[len(req.Messages)-1]
		name, args := "knowledge", map[string]any{"query": "wifi password", "rationale": "the house rules may say"}
		if last["role"] == "tool" {
			name, args = "finish", map[string]any{"result": "found: " + fmt.Sprint(last["content"])[:40], "rationale": "cited"}
		}
		raw, _ := json.Marshal(args)
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c","type":"function","function":{"name":%q,"arguments":%q}}]}}],"usage":{"prompt_tokens":40,"completion_tokens":8}}`, name, string(raw))
	}))
	defer model.Close()
	var journal []Entry
	build := func() *Tenant {
		seat := func(id string, roles map[string]string) Seat {
			return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: roles}}
		}
		tn, err := NewTenant("t-1", NewConsole("t-1", seat("ana", map[string]string{"desk": "clerk", knowledge.ID: knowledge.Editor, PlatformApp: Admin, ai.ID: ai.Admin, AgentApp: AgentAdmin}),
			seat("cy", map[string]string{"other": "x"})), ai.New("t-1"), work.New("t-1"), flow.New("t-1"), NewAgents("t-1"), knowledge.New("t-1"), newDesk("t-1"))
		if err != nil {
			t.Fatal(err)
		}
		tn.AIClient = func(*http.Request) (*http.Response, error) { t.Fatal("a replay called a model"); return nil, nil }
		return tn
	}
	tn := build()
	tn.AIClient = nil
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.app(PlatformApp).(*Console).Member(id); return m }
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
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
	expect := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s:\n got %s\nwant %s", what, got, want)
		}
	}
	found := func(who, q string) string {
		m := member(who)
		var out []string
		for _, p := range tn.Knowledge(&m, "", q, 5, now) {
			out = append(out, fmt.Sprintf("%s#%d", p.Document, p.Chunk))
		}
		return strings.Join(out, " ")
	}
	expect("rules", do("ana", knowledge.ID, knowledge.DocumentType+".create", knowledge.DocumentType, "RULES", map[string]any{"title": "House rules",
		"text": "# Arrival\n\nCheck-in from 15:00.\n\n# Wifi\n\nThe wifi password is on the key card; the front desk resets it."}), "ok")
	expect("playbook", do("ana", knowledge.ID, knowledge.DocumentType+".create", knowledge.DocumentType, "PLAYBOOK", map[string]any{"title": "Desk playbook",
		"text": "Answer wifi complaints within the hour.", "apps": []string{"desk"}}), "ok")
	expect("a reader of desk", do("cy", knowledge.ID, knowledge.DocumentType+".create", knowledge.DocumentType, "X", map[string]any{"title": "x", "text": "x"}), "ERROR_CODE_POLICY_DENIED")
	do("ana", "desk", "desk.ticket.open", "desk.ticket", "T1", map[string]string{"subject": "Wifi"})
	do("ana", "desk", "desk.ticket.answer", "desk.ticket", "T1", map[string]string{"reply": "Reset the wifi router in room 12."})

	// By words, within what each may read: the playbook and the ticket's
	// reply (a knowledge field) are the desk's.
	expect("ana", found("ana", "wifi reset"), "desk.ticket/T1#reply#0 knowledge.document/RULES#1 knowledge.document/PLAYBOOK#0")
	expect("cy", found("cy", "wifi reset"), "knowledge.document/RULES#1")

	// With an embedding model, passages are embedded as owned work, once.
	do("ana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "lm", map[string]any{"kind": "local", "baseUrl": model.URL + "/v1"})
	do("ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "lm/embed", map[string]string{"access": "users"})
	do("ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "lm/chat", map[string]string{"access": "users"})
	do("ana", PlatformApp, SchemaSettingSet, SettingType, "knowledge/embedding-model", map[string]string{"value": "lm/embed"})
	tn.Embed(now)
	tn.Embed(now)
	expect("embedded once", fmt.Sprint(embeds), "1")
	expect("by meaning too", found("cy", "password key card")+fmt.Sprint(" ", embeds, " ", tn.ai.Spent("app:knowledge", now) > 0), "knowledge.document/RULES#1 2 true") // the query was embedded, and metered

	// An agent's search: journaled with its step, cited on the run.
	do("ana", PlatformApp, SchemaSettingSet, SettingType, "agent/model", map[string]string{"value": "lm/chat"})
	do("ana", AgentApp, SchemaRunStart, RunType, "R1", map[string]string{"agent": "desk.triage", "goal": "What is the wifi password?"})
	for range 3 {
		now = now.Add(time.Second)
		tn.Think(now)
		tn.Work(now)
	}
	run, _ := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "R1")
	expect("run", run.State+" "+run.Citations[0].Document+" "+run.Citations[0].Title, "done knowledge.document/RULES House rules")
	expect("transcripts", fmt.Sprint(len(tn.Transcripts("R1", 10)), " ", strings.Contains(string(tn.Transcripts("R1", 10)[1].Request), "What is the wifi password?")), "2 true")
	CheckReplay(t, tn, journal, build)
}
