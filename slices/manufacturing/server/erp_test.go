package mes

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

// #101: a finished order is confirmed to the ERP as an outbound effect; the
// ERP's answer comes back as an observation on the order (ADR-0014 D4), and a
// refusal reaches the line's supervisors. The confirmation flow (ADR-0020) drives
// it: it confirms, waits for the answer, and on a refusal asks the supervisors
// to correct and resend. The journal replays all of it without calling the ERP
// (newPlant's cleanup).
func TestOrderConfirmedToTheERP(t *testing.T) {
	calls := 0
	erpAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var c struct{ Data Confirmation }
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &c)
		w.Header().Set("Content-Type", "application/json")
		if c.Data.Planned == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"error":"no planned order to confirm against"}`))
			return
		}
		fmt.Fprintf(w, `{"confirmation":"CONF-%s-%d"}`, c.Data.Planned, c.Data.Yield)
	}))
	defer erpAPI.Close()
	p := newPlant(t)
	p.tenant.Secrets = func(string) ([]byte, bool) { return []byte("s3cret"), true }
	admin := platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{platformserver.PlatformApp: platformserver.Admin}}
	raw, _ := json.Marshal(map[string]any{"url": erpAPI.URL, "secret": "erp", "effects": []string{"mes/" + EffectConfirmation}, "allowPrivate": true})
	if _, err := p.tenant.Submit(admin, &pb.Submission{TenantId: tenant, PrincipalId: "admin", Authority: platformserver.PlatformApp, IdempotencyKey: "e1",
		Target: &pb.EntityRef{Type: platformserver.EndpointType, Id: "erp-api"}, Schema: &pb.SchemaRef{Name: platformserver.SchemaEndpointAdd, Version: 1}, Payload: raw}, t0); err != nil {
		t.Fatal(err)
	}
	expect(t, fmt.Sprint(p.DeliverPlanned(erp, PlannedPage{CursorTo: "page-1", Orders: []PlannedOrder{{ERPID: "PO-9001", Product: "P-100", Quantity: 4}}}, t0)), "<nil>")
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-1", releasePayload{Product: "P-100", Quantity: 4, SFCs: 2, Planned: "PO-9001"}, p.Planned()[0].FactID), "ok")
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-2", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ok")
	run := func(id string) {
		for _, resource := range []string{"FURNACE-1", "CNC-11", "CMM-1"} {
			expect(t, submit(p, op1, SchemaStart, SFCType, id, sfcPayload{Resource: resource}), "ok")
			expect(t, submit(p, op1, SchemaComplete, SFCType, id, sfcPayload{}), "ok")
		}
	}
	at := t0
	work := func() { // the host's owned work: the flow's deliveries and its timer
		for range 3 {
			at = at.Add(2 * time.Second)
			p.tenant.Think(at) // the flow's agent, without a model here: it stops, and the supervisors correct
			p.tenant.Work(at)
		}
	}
	flow := func(id string) platformserver.FlowInstance {
		page, _ := p.tenant.Records(platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{platformserver.FlowApp: platformserver.FlowAdmin}},
			platformserver.InstanceType, platform.Query{Domain: json.RawMessage(`[["key","=","` + id + `"]]`)}, at)
		if len(page.Records) == 0 {
			return platformserver.FlowInstance{}
		}
		return page.Records[0].(platformserver.FlowInstance)
	}
	order := func(id string) Order {
		for _, o := range p.Orders() {
			if o.ID == id {
				return o
			}
		}
		return Order{}
	}
	run("SO-1-001")
	expect(t, order("SO-1").ERP, "") // one SFC still open
	// The second lot is scrapped by two signatures; the order has ended.
	expect(t, submit(p, op1, SchemaNC, SFCType, "SO-1-002", sfcPayload{Code: "POROSITY"}), "ok")
	expect(t, submit(p, qa1, SchemaSign, SFCType, "SO-1-002", signPayload{Action: "scrap", Meaning: "reviewed"}), "ok")
	expect(t, submit(p, qa2, SchemaSign, SFCType, "SO-1-002", signPayload{Action: "scrap", Meaning: "approved"}), "ok")
	expect(t, order("SO-1").ERP, "") // the flow confirms it, as owned work
	work()
	expect(t, order("SO-1").ERP+" "+flow("SO-1").State, "sent waiting")
	p.tenant.Dispatch(at)
	work()
	expect(t, flow("SO-1").State+" "+flow("SO-1").Trace[len(flow("SO-1").Trace)-2].Detail, "done the end: the ERP confirmed it as CONF-PO-9001-2")
	o := order("SO-1")
	expect(t, o.ERP+" "+o.Confirmation, "confirmed CONF-PO-9001-2") // yield 2 of 4
	last := p.facts.Records(tenant)[len(p.facts.Records(tenant))-1].GetFact()
	expect(t, last.GetSchema().GetName()+" "+last.GetProvenance().GetConnectorId(), schemaAnswer+" erp-api")
	// An order the ERP did not plan is refused; the supervisors hear of it.
	run("SO-2-001")
	work()
	p.tenant.Dispatch(at)
	work()
	o = order("SO-2")
	expect(t, o.ERP+": "+o.ERPDetail, "refused: no planned order to confirm against")
	notes, _ := p.tenant.Read(sup.Member, "notifications")
	titles := fmt.Sprint(notes.([]platform.Notification)[0].Title, " | ", notes.([]platform.Notification)[1].Title)
	expect(t, titles, "Correct and resend SO-2 to the ERP | ERP refused the confirmation of SO-2")
	inbox := func() string {
		out, _ := p.tenant.Read(sup.Member, "inbox")
		var titles []string
		for _, task := range out.([]platformserver.WorkTask) {
			titles = append(titles, task.Title)
		}
		return fmt.Sprint(titles)
	}
	expect(t, inbox()+" "+flow("SO-2").State, "[Correct and resend SO-2 to the ERP] waiting")
	p.tenant.Dispatch(at) // settled effects are not sent again
	expect(t, fmt.Sprint(calls), "2")

	// The supervisor corrects the refused order: it fulfils a planned order the
	// ERP sent since, and its confirmation goes again under a new key.
	type resend struct {
		Planned string `json:"planned,omitempty"`
	}
	expect(t, submit(p, op1, SchemaResend, OrderType, "SO-2", resend{}), "ERROR_CODE_POLICY_DENIED")
	expect(t, submit(p, sup, SchemaResend, OrderType, "SO-1", resend{}), "ERROR_CODE_CONFLICT") // confirmed already
	expect(t, submit(p, sup, SchemaResend, OrderType, "SO-2", resend{Planned: "PO-404"}), "ERROR_CODE_INVALID_ARGUMENT")
	expect(t, fmt.Sprint(p.DeliverPlanned(erp, PlannedPage{CursorFrom: "page-1", CursorTo: "page-2", Orders: []PlannedOrder{{ERPID: "PO-9002", Product: "P-100", Quantity: 1}, {ERPID: "PO-9005", Product: "P-200", Quantity: 1}}}, t0)), "<nil>")
	// The planned order must fit: the same product (PO-9005 is for P-200), no
	// other order's (SO-1 fulfils PO-9001), and enough quantity (PO-9002 is for 1).
	expect(t, submit(p, sup, SchemaResend, OrderType, "SO-2", resend{Planned: "PO-9005"}), "ERROR_CODE_INVALID_ARGUMENT")
	expect(t, submit(p, sup, SchemaResend, OrderType, "SO-2", resend{Planned: "PO-9001"}), "ERROR_CODE_INVALID_ARGUMENT")
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-9", releasePayload{Product: "P-100", Quantity: 2, SFCs: 1, Planned: "PO-9002"}), "ERROR_CODE_INVALID_ARGUMENT")
	expect(t, submit(p, sup, SchemaResend, OrderType, "SO-2", resend{Planned: "PO-9002"}), "ok")
	expect(t, order("SO-2").ERP, "sent")
	work() // the resend closes the task; the flow waits for the answer again
	expect(t, inbox(), "[]")
	p.tenant.Dispatch(at)
	work()
	expect(t, flow("SO-2").State, "done")
	o = order("SO-2")
	expect(t, fmt.Sprint(o.ERP, " ", o.Confirmation, " ", o.Resent, " ", calls), "confirmed CONF-PO-9002-1 1 3")
	var keys []string
	for _, e := range p.tenant.Effects(t0) {
		keys = append(keys, e.Key+":"+e.State)
	}
	expect(t, fmt.Sprint(keys), "[SO-2#2:delivered SO-2:rejected SO-1:delivered]")

	// D6: the line's AI assistant corrects a refused order. Posting to the ERP
	// cannot be recalled, so the confirmation waits for a person's approval.
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-3", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ok")
	run("SO-3-001")
	work()
	p.tenant.Dispatch(at)
	expect(t, order("SO-3").ERP, "refused")
	expect(t, fmt.Sprint(p.DeliverPlanned(erp, PlannedPage{CursorFrom: "page-2", CursorTo: "page-3", Orders: []PlannedOrder{{ERPID: "PO-9003", Product: "P-100", Quantity: 1}}}, t0)), "<nil>")
	expect(t, submit(p, asst, SchemaResend, OrderType, "SO-3", resend{Planned: "PO-9003"}), "ok")
	held := p.tenant.Effects(t0)[0]
	expect(t, held.State+" "+held.Agent, "held agent-l1")
	p.tenant.Dispatch(at)
	expect(t, fmt.Sprint(calls), "4") // not sent
	adminNotes, _ := p.tenant.Read(admin, "notifications")
	expect(t, adminNotes.([]platform.Notification)[0].Title, "Approve Order confirmation to the ERP for mes.order/SO-3")
	approve := func(who platform.Member, key string) string {
		_, err := p.tenant.Submit(who, &pb.Submission{TenantId: tenant, PrincipalId: who.ID, Authority: platformserver.PlatformApp, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: platformserver.EffectType, Id: held.ID}, Schema: &pb.SchemaRef{Name: platformserver.SchemaEffectApprove, Version: 1}, Payload: []byte("{}")}, t0)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	robot := admin
	robot.ID, robot.Agent = "admin-bot", true
	expect(t, approve(robot, "a1"), "ERROR_CODE_POLICY_DENIED") // an agent cannot approve, whatever its role
	expect(t, approve(admin, "a2"), "ok")
	p.tenant.Dispatch(at)
	o = order("SO-3")
	expect(t, fmt.Sprint(o.ERP, " ", o.Confirmation, " ", calls), "confirmed CONF-PO-9003-1 5")
}

// ADR-0021 D10 (1): the ERP refuses a confirmation that names no planned order;
// the confirmation flow's agent reads the planned orders and proposes the one
// the order fulfils; a supervisor approves in the inbox, the flow resends it,
// and the ERP confirms. The agent itself acts on nothing.
func TestERPCorrectionByAgent(t *testing.T) {
	erpAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c struct{ Data Confirmation }
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &c)
		w.Header().Set("Content-Type", "application/json")
		if c.Data.Planned == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"error":"no planned order to confirm against"}`))
			return
		}
		fmt.Fprintf(w, `{"confirmation":"CONF-%s"}`, c.Data.Planned)
	}))
	defer erpAPI.Close()
	// The model reads the planned orders, then proposes the first one the goal does not name.
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []map[string]any }
		json.NewDecoder(r.Body).Decode(&req)
		goal, _ := req.Messages[1]["content"].(string)
		last := req.Messages[len(req.Messages)-1]
		name, args := "read_planned_orders", `{"rationale":"read first"}`
		if last["role"] == "tool" {
			found := ""
			for _, id := range regexp.MustCompile(`PO-\d+`).FindAllString(fmt.Sprint(last["content"]), -1) {
				if !strings.Contains(goal, id) {
					found = id
					break
				}
			}
			name, args = "finish", fmt.Sprintf(`{"result":"{\"planned\":\"%s\"}","rationale":"the planned order no other order fulfils"}`, found)
		}
		raw, _ := json.Marshal(args)
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":%q,"arguments":%s}}]}}],"usage":{"prompt_tokens":60,"completion_tokens":12}}`, name, raw)
	}))
	defer model.Close()
	p := newPlant(t)
	p.tenant.Secrets = func(string) ([]byte, bool) { return []byte("s3cret"), true }
	admin := platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{platformserver.PlatformApp: platformserver.Admin, platformserver.AIApp: platformserver.AIAdmin}}
	keys := 0
	as := func(m platform.Member, authority, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		_, err := p.tenant.Submit(m, &pb.Submission{TenantId: tenant, PrincipalId: m.ID, Authority: authority, IdempotencyKey: fmt.Sprint("x", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, t0)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	expect(t, as(admin, platformserver.PlatformApp, platformserver.SchemaEndpointAdd, platformserver.EndpointType, "erp-api",
		map[string]any{"url": erpAPI.URL, "secret": "erp", "effects": []string{"mes/" + EffectConfirmation}, "allowPrivate": true}), "ok")
	expect(t, as(admin, platformserver.AIApp, platformserver.SchemaProviderAdd, platformserver.ProviderType, "lm", map[string]any{"kind": "local", "baseUrl": model.URL + "/v1"}), "ok")
	expect(t, as(admin, platformserver.AIApp, platformserver.SchemaModelEnable, platformserver.ModelType, "lm/agent", map[string]string{"access": "users"}), "ok")
	expect(t, as(admin, platformserver.PlatformApp, platformserver.SchemaSettingSet, platformserver.SettingType, "agent/model", map[string]string{"value": "lm/agent"}), "ok")
	expect(t, fmt.Sprint(p.DeliverPlanned(erp, PlannedPage{CursorTo: "page-1", Orders: []PlannedOrder{{ERPID: "PO-9001", Product: "P-100", Quantity: 1}, {ERPID: "PO-9002", Product: "P-100", Quantity: 1}}}, t0)), "<nil>")
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-1", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1, Planned: "PO-9001"}, p.Planned()[0].FactID), "ok")
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-2", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ok") // no planned order
	for _, resource := range []string{"FURNACE-1", "CNC-11", "CMM-1"} {
		expect(t, submit(p, op1, SchemaStart, SFCType, "SO-2-001", sfcPayload{Resource: resource}), "ok")
		expect(t, submit(p, op1, SchemaComplete, SFCType, "SO-2-001", sfcPayload{}), "ok")
	}
	at := t0
	work := func(n int) {
		for range n {
			at = at.Add(2 * time.Second)
			p.tenant.Think(at)
			p.tenant.Work(at)
			p.tenant.Dispatch(at)
		}
	}
	work(8)
	inbox := func() []platformserver.WorkTask {
		out, _ := p.tenant.Read(sup.Member, "inbox")
		return out.([]platformserver.WorkTask)
	}
	tasks := inbox()
	if len(tasks) != 1 || tasks[0].Title != "Resend SO-2 to the ERP against PO-9002?" {
		t.Fatalf("inbox %+v", tasks)
	}
	runs, _ := p.tenant.Records(platform.Member{ID: "x", Tenant: tenant, Roles: map[string]string{platformserver.AgentApp: platformserver.AgentAdmin}}, platformserver.RunType, platform.Query{}, at)
	run := runs.Records[0].(platformserver.AgentRunRecord)
	expect(t, fmt.Sprint(run.State, " ", run.Steps[0].Tool, " ", run.Steps[1].Tool, " ", run.ActionsUsed), "done read_planned_orders finish 0")
	expect(t, as(sup.Member, platformserver.WorkApp, "work.task.complete", platformserver.TaskType, tasks[0].ID, map[string]string{"answer": "resend"}), "ok")
	work(6)
	var o Order
	for _, x := range p.Orders() {
		if x.ID == "SO-2" {
			o = x
		}
	}
	expect(t, fmt.Sprint(o.ERP, " ", o.Confirmation, " ", o.Planned), "confirmed CONF-PO-9002 PO-9002")
	// The supervisor's answer is kept on the run: the agent's evaluation reference (ADR-0021 D7).
	runs, _ = p.tenant.Records(platform.Member{ID: "x", Tenant: tenant, Roles: map[string]string{platformserver.AgentApp: platformserver.AgentAdmin}}, platformserver.RunType, platform.Query{}, at)
	sig := runs.Records[0].(platformserver.AgentRunRecord).Signals
	expect(t, fmt.Sprint(len(sig), " ", sig[0].Kind, " ", sig[0].By), "1 accepted sup-1")
	view, _ := p.tenant.Context(nil, OrderType, "SO-2", at) // what the agent saw of the order
	expect(t, fmt.Sprint(view.References), "[sfcs: mes.sfc/SO-2-001]")
}
