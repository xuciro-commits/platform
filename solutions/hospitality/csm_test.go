package hospitality

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"csm"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// ADR-0021 D10 (2): the helpdesk's triage agent grounds itself in what the
// CRM knows of the customer, triages and replies; its reply's mail waits for a
// person. A ticket the agent cannot take goes to the desk, and to its leads
// when it is late. A reply promising money is refused by the agent's guard.
func TestCSMTriage(t *testing.T) {
	// The model searches the customer's account, reads its context, triages,
	// replies naming what it found, and finishes; "fail" tickets get errors.
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []map[string]any }
		json.NewDecoder(r.Body).Decode(&req)
		goal, _ := req.Messages[1]["content"].(string)
		var results []string
		for _, m := range req.Messages {
			if m["role"] == "tool" {
				results = append(results, fmt.Sprint(m["content"]))
			}
		}
		if strings.Contains(goal, "fail") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ticket := regexp.MustCompile(`ticket (T-\d+)`).FindStringSubmatch(goal)[1]
		account := regexp.MustCompile(`account (\w+)`).FindStringSubmatch(goal)[1]
		name, args := "finish", map[string]any{"result": "triaged and answered"}
		switch len(results) {
		case 0:
			name, args = "search", map[string]any{"query": account}
		case 1:
			name, args = "context", map[string]any{"type": "crm.account", "id": account}
		case 2:
			name, args = "knowledge", map[string]any{"query": "wifi"}
		case 3:
			name, args = "csm_ticket_triage", map[string]any{"target": ticket, "category": "booking", "priority": "high"}
		case 4:
			found := regexp.MustCompile(`"title":"([^"]+)"`).FindAllStringSubmatch(results[1], -1) // the account's opportunities
			rule := regexp.MustCompile(`"title":"([^"]+)"`).FindStringSubmatch(results[2])         // the passage found
			reply := "About your " + found[len(found)-1][1] + ": the front desk resets the wifi password (" + rule[1] + "). A colleague is on it."
			if strings.Contains(goal, "refund") {
				reply = "We will refund you."
			}
			name, args = "csm_ticket_reply", map[string]any{"target": ticket, "reply": reply}
		}
		args["rationale"] = "step " + fmt.Sprint(len(results))
		raw, _ := json.Marshal(args)
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c","type":"function","function":{"name":%q,"arguments":%q}}]}}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`, name, string(raw))
	}))
	defer model.Close()
	var mailed []string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mailed = append(mailed, string(body))
	}))
	defer gateway.Close()

	w := newWorld(t, hotelProvider)
	w.setup()
	w.tenant.Secrets = func(string) ([]byte, bool) { return []byte("s3cret"), true }
	now := t0
	ops := platform.Member{ID: "ops", Tenant: "hotel-a", Roles: map[string]string{platformserver.PlatformApp: platformserver.Admin, platformserver.AIApp: platformserver.AIAdmin}}
	desk, lead := w.members["desk"], w.members["manager"]
	keys := 0
	do := func(m platform.Member, app, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := w.tenant.Submit(m, &pb.Submission{TenantId: "hotel-a", PrincipalId: m.ID, Authority: app, IdempotencyKey: fmt.Sprint("h", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	tick := func(d time.Duration) {
		for end := now.Add(d); now.Before(end); now = now.Add(time.Second) {
			w.tenant.Think(now)
			w.tenant.Work(now)
			w.tenant.Dispatch(now)
		}
	}
	ticket := func(id string) csm.Ticket {
		v, _ := w.tenant.RecordOf(lead, csm.TicketType, id, now)
		x, _ := v.Record.(csm.Ticket)
		return x
	}
	flowAdmin := platform.Member{ID: "flows", Tenant: "hotel-a", Roles: map[string]string{flow.ID: flow.Admin}}
	instance := func(id string) flow.FlowInstance {
		v, _ := w.tenant.RecordOf(flowAdmin, flow.InstanceType, "csm.service-level:"+id, now)
		x, _ := v.Record.(flow.FlowInstance)
		return x
	}
	inbox := func(m platform.Member) []string {
		out, _ := w.tenant.Read(m, "inbox")
		var titles []string
		for _, x := range out.([]work.WorkTask) {
			titles = append(titles, x.Title)
		}
		return titles
	}
	w.expect(do(ops, platformserver.PlatformApp, platformserver.SchemaEndpointAdd, platformserver.EndpointType, "mail-gateway",
		map[string]any{"url": gateway.URL, "secret": "hook", "effects": []string{"csm/" + csm.EffectReply}, "allowPrivate": true}), "ok")
	w.expect(do(ops, platformserver.AIApp, platformserver.SchemaProviderAdd, platformserver.ProviderType, "lm", map[string]any{"kind": "local", "baseUrl": model.URL + "/v1"}), "ok")
	w.expect(do(ops, platformserver.AIApp, platformserver.SchemaModelEnable, platformserver.ModelType, "lm/triage", map[string]string{"access": "users"}), "ok")
	w.expect(do(ops, platformserver.PlatformApp, platformserver.SchemaSettingSet, platformserver.SettingType, "agent/model", map[string]string{"value": "lm/triage"}), "ok")
	open := func(id, subject string) {
		w.t.Helper()
		w.expect(do(desk, "csm", csm.SchemaOpen, csm.TicketType, id,
			map[string]string{"subject": subject, "customer": "anna@acme.test", "account": "ACME", "body": "The wifi in our rooms drops."}), "ok")
	}

	w.expect(do(lead, platformserver.KnowledgeApp, platformserver.DocumentType+".create", platformserver.DocumentType, "RULES",
		map[string]any{"title": "House rules", "text": "# Wifi\n\nThe wifi password is on the key card; the front desk resets it."}), "ok")

	// Grounded in the CRM and the house rules, cited: the account's opportunity is named in the reply; the
	// high priority makes it due in four hours; the mail waits for a person.
	open("T-1", "Wifi keeps dropping")
	tick(8 * time.Second)
	x := ticket("T-1")
	w.expect(fmt.Sprint(x.Status, " ", x.Category, " ", x.Priority, " ", x.Due.Sub(x.Created.At), " ", x.Replied, " | ", x.Reply),
		"answered booking high 4h0m0s agent:csm.triage | About your Board offsite: the front desk resets the wifi password (House rules). A colleague is on it.")
	agents := platform.Member{ID: "x", Tenant: "hotel-a", Roles: map[string]string{platformserver.AgentApp: platformserver.AgentAdmin}}
	cited, _ := w.tenant.Records(agents, platformserver.RunType, platform.Query{Domain: json.RawMessage(`[["goal","like","T-1"]]`)}, now)
	w.expect(fmt.Sprint(cited.Records[0].(platformserver.AgentRunRecord).Citations), "[{knowledge.document/RULES House rules 0 2}]")
	effects, _ := w.tenant.Read(ops, "effects")
	held := effects.([]platform.Effect)
	w.expect(fmt.Sprint(len(held), " ", held[0].State, " ", len(mailed)), "1 held 0")
	w.expect(do(ops, platformserver.PlatformApp, platformserver.SchemaEffectApprove, platformserver.EffectType, held[0].ID, map[string]any{}), "ok")
	tick(2 * time.Second)
	w.expect(fmt.Sprint(len(mailed), " ", strings.Contains(mailed[0], `"to":"anna@acme.test"`)), "1 true")
	w.expect(instance("T-1").State, "done")
	runOf := func(ticket string) platformserver.AgentRunRecord {
		page, _ := w.tenant.Records(agents, platformserver.RunType, platform.Query{Domain: json.RawMessage(`[["goal","like","` + ticket + ` "]]`)}, now)
		return page.Records[0].(platformserver.AgentRunRecord)
	}
	w.expect(runOf("T-1").Signals[0].Kind+" "+runOf("T-1").Signals[0].By, "approved ops") // the reply approved: an accepted outcome (ADR-0022 D9)

	// A reply discarded instead: a signal, and a memory proposed to the agent.
	// Numbers are taken only by accepted decisions (ADR-0024 D3): the refused
	// ticket before it leaves no gap.
	w.expect(do(desk, "csm", csm.SchemaOpen, csm.TicketType, "T-X", map[string]string{"subject": "Who?", "customer": "nobody"}), "ERROR_CODE_INVALID_ARGUMENT")
	open("T-4", "Wifi slow in the lobby")
	w.expect(ticket("T-1").Number+" "+ticket("T-4").Number, fmt.Sprintf("CS-%d-0001 CS-%d-0002", now.Year(), now.Year()))
	tick(8 * time.Second)
	effects, _ = w.tenant.Read(ops, "effects")
	i := slices.IndexFunc(effects.([]platform.Effect), func(e platform.Effect) bool { return e.State == "held" })
	w.expect(do(ops, platformserver.PlatformApp, platformserver.SchemaEffectDiscard, platformserver.EffectType, effects.([]platform.Effect)[i].ID, map[string]any{}), "ok")
	w.expect(runOf("T-4").Signals[0].Kind, "discarded")
	memories, _ := w.tenant.Records(agents, platformserver.MemoryType, platform.Query{}, now)
	w.expect(fmt.Sprint(len(memories.Records), " ", memories.Records[0].(platformserver.Memory).State), "1 proposed")

	// The guard: a reply promising a refund is refused; the ticket stays open.
	open("T-2", "I want a refund")
	tick(8 * time.Second)
	runs, _ := w.tenant.Records(agents, platformserver.RunType,
		platform.Query{Domain: json.RawMessage(`[["goal","like","refund"]]`)}, now)
	run := runs.Records[0].(platformserver.AgentRunRecord)
	w.expect(fmt.Sprint(ticket("T-2").Status, " ", slices.ContainsFunc(run.Steps, func(s platformserver.RunStep) bool { return strings.HasPrefix(s.Outcome, "refused by its guard") })), "triaged true")

	// Without a model's answer the desk triages by hand; nobody answers in a
	// day, so the leads are told, and a lead's reply ends it.
	open("T-3", "fail: the invoice is wrong")
	tick(8 * time.Second)
	w.expect(fmt.Sprint(slices.Contains(inbox(desk), "Triage and answer: fail: the invoice is wrong")), "true")
	now = now.Add(24 * time.Hour)
	tick(3 * time.Second)
	w.expect(fmt.Sprint(ticket("T-3").Escalated, " ", slices.Contains(inbox(lead), "Late ticket: fail: the invoice is wrong")), "true true")
	w.expect(do(lead, "csm", csm.SchemaReply, csm.TicketType, "T-3", map[string]string{"reply": "Corrected, sorry."}), "ok")
	tick(3 * time.Second)
	w.expect(fmt.Sprint(instance("T-3").State, " ", inbox(lead), " ", len(mailed)), "done [Late ticket: I want a refund] 2") // a person's reply is mailed at once; T-2 is late too

	platformserver.CheckReplay(t, w.tenant, w.journal, func() *platformserver.Tenant { return newWorld(t, hotelProvider).tenant })
}
