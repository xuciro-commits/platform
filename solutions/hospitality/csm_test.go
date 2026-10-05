package hospitality

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
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
	"platformserver/apps/ai"
	"platformserver/apps/flow"
	"platformserver/apps/knowledge"
	"platformserver/apps/work"
	"platformserver/platform"
)

// ADR-0021 D10 (2): the helpdesk's triage agent grounds itself in what the
// CRM knows of the customer, triages and replies; its reply's mail waits for a
// person. A ticket the agent cannot take goes to the desk, and to its leads
// when it is late. A reply promising money is refused by the agent's guard.
func TestCSMTriage(t *testing.T) {
	// The model searches its own app, reads the ticket's context, tries the guest
	// account (refused: a flow's agent reads its own app, ADR-0050 D1/D2), looks
	// the matter up in the house rules, triages and replies with what it may
	// read; "fail" tickets get errors.
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []map[string]any }
		json.NewDecoder(r.Body).Decode(&req)
		if system, _ := req.Messages[0]["content"].(string); strings.HasPrefix(system, "Say in one short line") { // an app's request (ADR-0029 D3)
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Wifi drops in the rooms"}}],"usage":{"prompt_tokens":20,"completion_tokens":6}}`)
			return
		}
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
		ticket := regexp.MustCompile(`ticket ([A-Z]+-\d+)`).FindStringSubmatch(goal)[1]
		account := regexp.MustCompile(`account (\w+)`).FindStringSubmatch(goal)[1]
		name, args := "finish", map[string]any{"result": "triaged and answered"}
		switch len(results) {
		case 0:
			name, args = "search", map[string]any{"query": account}
		case 1:
			name, args = "context", map[string]any{"type": "csm.ticket", "id": ticket}
		case 2:
			// Another app's record: a run no person started may not read it (ADR-0050 D1/D2).
			name, args = "context", map[string]any{"type": "crm.account", "id": account}
		case 3:
			name, args = "knowledge", map[string]any{"query": "wifi"}
		case 4:
			name, args = "csm_ticket_triage", map[string]any{"target": ticket, "category": "booking", "priority": "high"}
		case 5:
			rule := regexp.MustCompile(`"title":"([^"]+)"`).FindStringSubmatch(results[3]) // the passage found
			reply := "About the wifi: the front desk resets the wifi password (" + rule[1] + "). A colleague is on it."
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
	ops := platform.Member{ID: "ops", Tenant: "hotel-a", Roles: map[string]string{platformserver.PlatformApp: platformserver.Admin, ai.ID: ai.Admin}}
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
	w.expect(do(ops, ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "lm", map[string]any{"kind": "local", "baseUrl": model.URL + "/v1"}), "ok")
	w.expect(do(ops, ai.ID, ai.SchemaModelEnable, ai.ModelType, "lm/triage", map[string]string{"access": "users"}), "ok")
	w.expect(do(ops, platformserver.PlatformApp, platformserver.SchemaSettingSet, platformserver.SettingType, "agent/model", map[string]string{"value": "lm/triage"}), "ok")
	w.expect(do(ops, platformserver.PlatformApp, platformserver.SchemaSettingSet, platformserver.SettingType, "ai/"+ai.SettingAppModel, map[string]string{"value": "lm/triage"}), "ok")
	open := func(id, subject string) {
		w.t.Helper()
		w.expect(do(desk, "csm", csm.SchemaOpen, csm.TicketType, id,
			map[string]string{"subject": subject, "customer": "anna@acme.test", "account": "ACME", "body": "The wifi in our rooms drops."}), "ok")
	}

	w.expect(do(lead, knowledge.ID, knowledge.DocumentType+".create", knowledge.DocumentType, "RULES",
		map[string]any{"title": "House rules", "text": "# Wifi\n\nThe wifi password is on the key card; the front desk resets it."}), "ok")

	// Grounded in its own app and the house rules, cited: the reply relies on the
	// rule it read; the guest account is refused, so nothing from another app
	// grounds it (ADR-0050 D1/D2); the high priority makes it due in four hours;
	// the mail waits for a person.
	open("T-1", "Wifi keeps dropping")
	tick(8 * time.Second)
	x := ticket("T-1")
	w.expect(fmt.Sprint(x.Status, " ", x.Category, " ", x.Priority, " ", x.Due.Sub(x.Created.At), " ", x.Replied, " | ", x.Reply),
		"answered booking high 4h0m0s agent:csm.triage | About the wifi: the front desk resets the wifi password (House rules). A colleague is on it.")
	w.expect(x.Summary, "Wifi drops in the rooms") // the app asked the tenant's model for apps, and its reply action took the answer (ADR-0029 D3)
	// An administrator of the agent app reads a run's trace only where they may
	// also read what the run read (#130): the lead holds those roles; an
	// administrator of agents alone sees that the run happened, not its content,
	// and no model call of it.
	agents := lead
	agents.Roles = maps.Clone(lead.Roles)
	agents.Roles[platformserver.AgentApp] = platformserver.AgentAdmin
	cited, _ := w.tenant.Records(agents, platformserver.RunType, platform.Query{Domain: json.RawMessage(`[["goal","like","T-1"]]`)}, now)
	citedRun := cited.Records[0].(platformserver.AgentRunRecord)
	w.expect(fmt.Sprint(citedRun.Citations), "[{knowledge.document/RULES House rules 0 3}]")
	// The guest account it tried to read: another app's record, refused, and
	// nothing from it reaches the trace (ADR-0050 D1/D2).
	if len(citedRun.Steps) < 3 || !strings.HasPrefix(citedRun.Steps[2].Outcome, "refused:") || strings.Contains(citedRun.Steps[2].Outcome, "Board") {
		t.Fatalf("a flow's agent read another app's record: %+v", citedRun.Steps)
	}
	bare := platform.Member{ID: "x", Tenant: "hotel-a", Roles: map[string]string{platformserver.AgentApp: platformserver.AgentAdmin}}
	withheld, _ := w.tenant.Records(bare, platformserver.RunType, platform.Query{Domain: json.RawMessage(`[["goal","like","T-1"]]`)}, now)
	narrow := withheld.Records[0].(platformserver.AgentRunRecord)
	w.expect(fmt.Sprint(narrow.Withheld, " ", len(narrow.Citations), " ", narrow.Result, " ", narrow.Steps[0].Outcome), "true 0  ")
	if out, err := w.tenant.TranscriptsFor(bare, narrow.ID, 20, now); err == nil || len(out) != 0 {
		t.Errorf("an administrator of agents alone read the model calls: %+v, %v", out, err)
	}
	if out, err := w.tenant.TranscriptsFor(agents, narrow.ID, 20, now); err != nil || len(out) == 0 {
		t.Errorf("the lead lost the model calls of a run it may read: %+v, %v", out, err)
	}
	effects, _ := w.tenant.Read(ops, "effects")
	held := slices.DeleteFunc(effects.([]platform.Effect), func(e platform.Effect) bool { return e.Endpoint != "mail-gateway" }) // the summary's request aside
	w.expect(fmt.Sprint(len(held), " ", held[0].State, " ", len(mailed)), "1 held 0")
	w.expect(do(desk, platformserver.PlatformApp, platformserver.SchemaEffectApprove, platformserver.EffectType, held[0].ID, map[string]any{}), "ERROR_CODE_POLICY_DENIED")
	tick(2 * time.Second)
	w.expect(fmt.Sprint(len(mailed)), "0")
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
	// The helpdesk hears it too: the ticket is unanswered again, and the desk is told (F-24).
	w.expect(ticket("T-4").Status+" "+ticket("T-4").Unsent, "triaged discarded discarded by ops")
	deskNotes, _ := w.tenant.Read(desk, "notifications")
	w.expect(fmt.Sprint(slices.ContainsFunc(deskNotes.([]platform.Notification), func(n platform.Notification) bool { return n.Title == "Reply not sent: Wifi slow in the lobby" })), "true")
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
	// The ticket's page offers the question to whom it is asked, to answer there.
	page, _ := w.tenant.RecordOf(desk, csm.TicketType, "T-3", now)
	w.expect(fmt.Sprint(len(page.Tasks), " ", page.Tasks[0].(work.WorkTask).Title), "1 Triage and answer: fail: the invoice is wrong")
	now = now.Add(24 * time.Hour)
	tick(3 * time.Second)
	w.expect(fmt.Sprint(ticket("T-3").Escalated, " ", slices.Contains(inbox(lead), "Late ticket: fail: the invoice is wrong")), "true true")
	w.expect(do(lead, "csm", csm.SchemaReply, csm.TicketType, "T-3", map[string]string{"reply": "Corrected, sorry."}), "ok")
	tick(3 * time.Second)
	w.expect(fmt.Sprint(instance("T-3").State, " ", inbox(lead), " ", len(mailed)), "done [Late ticket: I want a refund] 2") // a person's reply is mailed at once; T-2 is late too

	// The chain T-1's run belongs to (ADR-0029 D6): the ticket, its service
	// level, the triage run its step started, and the reply mail it caused.
	chainer := platform.Member{ID: "x", Tenant: "hotel-a", Roles: map[string]string{platformserver.PlatformApp: platformserver.Admin, flow.ID: flow.Admin, platformserver.AgentApp: platformserver.AgentAdmin}}
	chain, err := w.tenant.ChainOf(chainer, platformserver.RunType+"/"+runOf("T-1").ID, now)
	if err != nil {
		t.Fatal(err)
	}
	var links []string
	kind := map[string]string{}
	for _, n := range chain.Nodes {
		kind[n.Ref] = n.Kind
	}
	for _, e := range chain.Edges {
		links = append(links, kind[e.From]+">"+kind[e.To])
	}
	w.expect(fmt.Sprint(links), "[record>flow flow>run run>effect]")
	fromInstance, _ := w.tenant.ChainOf(chainer, flow.InstanceType+"/csm.service-level:T-1", now)
	w.expect(fmt.Sprint(len(fromInstance.Nodes) == len(chain.Nodes)), "true") // the same chain from the flow's side

	// The triage agent's declared cases, three runs each, dry (ADR-0029 D6): it
	// replies to an ordinary request, and its guard keeps it from promising a refund.
	w.expect(do(agents, platformserver.AgentApp, platformserver.SchemaEvalStart, platformserver.EvaluationType, "SUITE-1",
		map[string]any{"agent": "csm.triage", "model": "lm/triage", "suite": true}), "ok")
	w.tenant.Evaluate(now)
	suite, _ := w.tenant.RecordOf(agents, platformserver.EvaluationType, "SUITE-1", now)
	var cases []string
	for _, x := range suite.Record.(platformserver.Evaluation).Cases {
		cases = append(cases, fmt.Sprintf("%s %s %d/%d", x.Case, x.Verdict, x.Passes, x.Runs))
	}
	w.expect(strings.Join(cases, ", "), "replies to wifi passes 3/3, promises no refund passes 3/3")

	// An agent past its own limit stops before calling the model, and the desk takes the ticket (ADR-0029 D1).
	agent := runOf("T-1").Agent
	w.expect(do(ops, ai.ID, ai.SchemaLimitSet, ai.LimitType, "agent:"+agent, map[string]int{"dailyTokens": 1}), "ok")
	open("T-6", "Late breakfast")
	tick(8 * time.Second)
	w.expect(runOf("T-6").State+" "+runOf("T-6").Stopped, "stopped agent:"+agent+" used its 1 tokens for today")
	w.expect(fmt.Sprint(slices.Contains(inbox(desk), "Triage and answer: Late breakfast")), "true")

	platformserver.CheckReplay(t, w.tenant, w.journal, func() *platformserver.Tenant { return newWorld(t, hotelProvider).tenant })
}
