package manufacturing

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

	"erpadapter"
	"mes"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

// external is the plant with the adapter to an ERP outside (ADR-0024 7d), that
// ERP faked over HTTP: it confirms with "CONF-<order>-<yield>" and refuses
// PO-9004, which it has locked.
type external struct {
	t       *testing.T
	tn      *platformserver.Tenant
	journal []platformserver.Entry
	calls   int
	now     time.Time
	keys    int
}

var externalSeats = []platformserver.Seat{
	Seat("sup", "sup-1", map[string]string{"mes": string(mes.Supervisor), erpadapter.ID: erpadapter.Planner, platformserver.PlatformApp: platformserver.Admin,
		platformserver.AIApp: platformserver.AIAdmin, platformserver.FlowApp: platformserver.FlowAdmin, platformserver.AgentApp: platformserver.AgentAdmin}, "plant-sz"),
	Seat("op", "op-l1", map[string]string{"mes": string(mes.Operator)}, "L1"),
	Seat("qa1", "qa-1", map[string]string{"mes": string(mes.Quality)}),
	Seat("qa2", "qa-2", map[string]string{"mes": string(mes.Quality)}),
	func() platformserver.Seat { // the line's AI assistant, a planner in the adapter too
		s := Seat("asst", "agent-l1", map[string]string{"mes": string(mes.Assistant), erpadapter.ID: erpadapter.Planner}, "L1")
		s.Agent = true
		return s
	}(),
	Seat("erp", "erp", map[string]string{erpadapter.ID: erpadapter.Connector}),
}

func buildExternal(t *testing.T) *platformserver.Tenant {
	tn, err := NewTenant(tenant, erpadapter.New(tenant), externalSeats...)
	if err == nil {
		err = tn.Connect(erpadapter.Poll("erp"))
	}
	if err != nil {
		t.Fatal(err)
	}
	tn.Secrets = func(string) ([]byte, bool) { return []byte("s3cret"), true }
	return tn
}

func newExternal(t *testing.T) *external {
	x := &external{t: t, now: time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x.calls++
		var c struct{ Data erpadapter.Confirmation }
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &c)
		w.Header().Set("Content-Type", "application/json")
		if c.Data.Order == "PO-9004" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"error":"PO-9004 is locked in the ERP"}`))
			return
		}
		fmt.Fprintf(w, `{"confirmation":"CONF-%s-%g"}`, c.Data.Order, c.Data.Yield)
	}))
	t.Cleanup(fake.Close)
	x.tn = buildExternal(t)
	x.tn.Record = func(e platformserver.Entry) { x.journal = append(x.journal, e) }
	x.expect("endpoint", x.do("sup-1", platformserver.PlatformApp, platformserver.SchemaEndpointAdd, platformserver.EndpointType, "erp-api",
		map[string]any{"url": fake.URL, "secret": "erp", "effects": []string{erpadapter.ID + "/" + erpadapter.EffectConfirmation}, "allowPrivate": true}), "ok")
	t.Cleanup(func() {
		platformserver.CheckReplay(t, x.tn, x.journal, func() *platformserver.Tenant { return buildExternal(t) })
	})
	return x
}

func (x *external) member(id string) platform.Member { m, _ := x.tn.Member(id); return m }

func (x *external) do(who, authority, schema, typ, id string, payload any) string {
	x.keys++
	raw, _ := json.Marshal(payload)
	if _, err := x.tn.Submit(x.member(who), &pb.Submission{TenantId: tenant, PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprint("k", x.keys),
		Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, x.now); err != nil {
		return err.Error()
	}
	return "ok"
}

func (x *external) expect(what string, got any, want string) {
	x.t.Helper()
	if fmt.Sprint(got) != want {
		x.t.Fatalf("%s: got %v, want %s", what, got, want)
	}
}

// poll delivers a page of the ERP's planned orders, as its poller does.
func (x *external) poll(from, to string, orders ...erpadapter.Planned) {
	x.t.Helper()
	raw, _ := json.Marshal(erpadapter.Page{CursorFrom: from, CursorTo: to, Orders: orders})
	if _, err := x.tn.Input(x.member("erp"), "planned-orders", raw, x.now); err != nil {
		x.t.Fatal(err)
	}
}

// work runs the host's owned work: the flow's deliveries and timers, its agent, the effects.
func (x *external) work(rounds int) {
	for range rounds {
		x.now = x.now.Add(2 * time.Second)
		x.tn.Think(x.now)
		x.tn.Work(x.now)
		x.tn.Dispatch(x.now)
		x.tn.Work(x.now)
	}
}

// make releases a shop order of P-100 and runs its SFCs through the routing;
// scrapped SFCs are scrapped by two quality signatures at the first operation.
func (x *external) make(shop, planned string, quantity, sfcs, scrapped int) {
	x.t.Helper()
	x.expect("release "+shop, x.do("sup-1", mes.ID, mes.SchemaRelease, mes.OrderType, shop,
		map[string]any{"product": "P-100", "quantity": quantity, "sfcs": sfcs, "planned": planned}), "ok")
	for n := 1; n <= sfcs; n++ {
		sfc := fmt.Sprintf("%s-%03d", shop, n)
		if n > sfcs-scrapped {
			x.expect("nc", x.do("op-l1", mes.ID, mes.SchemaNC, mes.SFCType, sfc, map[string]string{"code": "POROSITY"}), "ok")
			x.expect("reviewed", x.do("qa-1", mes.ID, mes.SchemaSign, mes.SFCType, sfc, map[string]string{"action": "scrap", "meaning": "reviewed"}), "ok")
			x.expect("approved", x.do("qa-2", mes.ID, mes.SchemaSign, mes.SFCType, sfc, map[string]string{"action": "scrap", "meaning": "approved"}), "ok")
			continue
		}
		for _, resource := range []string{"FURNACE-1", "CNC-11", "CMM-1"} {
			x.expect("start", x.do("op-l1", mes.ID, mes.SchemaStart, mes.SFCType, sfc, map[string]string{"resource": resource}), "ok")
			x.expect("complete", x.do("op-l1", mes.ID, mes.SchemaComplete, mes.SFCType, sfc, map[string]string{}), "ok")
		}
	}
}

func (x *external) order(id string) mes.Order {
	v, err := x.tn.RecordOf(x.member("sup-1"), mes.OrderType, id, x.now)
	if err != nil {
		x.t.Fatalf("%s: %v", id, err)
	}
	return v.Record.(mes.Order)
}

func (x *external) flow(key string) platformserver.FlowInstance {
	page, _ := x.tn.Records(x.member("sup-1"), platformserver.InstanceType, platform.Query{Domain: json.RawMessage(`[["key","=","` + key + `"]]`)}, x.now)
	if len(page.Records) == 0 {
		return platformserver.FlowInstance{}
	}
	return page.Records[0].(platformserver.FlowInstance)
}

func (x *external) inbox() string {
	out, _ := x.tn.Read(x.member("sup-1"), "inbox")
	var titles []string
	for _, task := range out.([]platformserver.WorkTask) {
		titles = append(titles, task.Title)
	}
	return fmt.Sprint(titles)
}

// ADR-0024 7d (and #101 before it): the plant confirms a finished order through
// production.orders/1 to the adapter, which sends it to the ERP outside; the
// ERP's number comes back on the order. A refusal reaches the line's
// supervisors, who correct and resend it against a planned order that fits. An
// order without a planned order is refused by the plant itself; the line's AI
// assistant corrects it, and the confirmation it caused waits for a person
// (ADR-0014 D6). The journal replays all of it without calling the ERP.
func TestConfirmedToAnERPOutside(t *testing.T) {
	x := newExternal(t)
	x.poll("", "page-1", erpadapter.Planned{ID: "PO-9001", Product: "P-100", Quantity: 4}, erpadapter.Planned{ID: "PO-9004", Product: "P-100", Quantity: 1})
	x.make("SO-1", "PO-9001", 4, 2, 1)
	x.work(1)
	x.expect("sent", x.order("SO-1").ERP, "sent")
	x.work(3)
	f := x.flow("SO-1")
	x.expect("flow", f.State+" "+f.Trace[len(f.Trace)-2].Detail, "done the end: the ERP confirmed it as CONF-PO-9001-2")
	o := x.order("SO-1")
	x.expect("the ERP's number", o.ERP+" "+o.Confirmation, "confirmed CONF-PO-9001-2") // yield 2 of 4

	// The ERP refuses PO-9004; the supervisors hear of it and are asked to correct it.
	x.make("SO-2", "PO-9004", 1, 1, 0)
	x.work(4)
	o = x.order("SO-2")
	x.expect("refused", o.ERP+": "+o.ERPDetail, "refused: PO-9004 is locked in the ERP")
	notes, _ := x.tn.Read(x.member("sup-1"), "notifications")
	x.expect("told", notes.([]platform.Notification)[0].Title+" | "+notes.([]platform.Notification)[1].Title,
		"Correct and resend SO-2 to the ERP | ERP refused the confirmation of SO-2")
	x.expect("asked", x.inbox()+" "+x.flow("SO-2").State, "[Correct and resend SO-2 to the ERP] waiting")

	// The planned order named in a correction must fit: known, the same product
	// (PO-9005 is for P-200), open (PO-9001 is confirmed), and enough quantity.
	resend := func(who, planned string) string {
		return x.do(who, mes.ID, mes.SchemaResend, mes.OrderType, "SO-2", map[string]string{"planned": planned})
	}
	x.expect("an operator", resend("op-l1", ""), "ERROR_CODE_POLICY_DENIED")
	x.expect("confirmed already", x.do("sup-1", mes.ID, mes.SchemaResend, mes.OrderType, "SO-1", map[string]string{}), "ERROR_CODE_CONFLICT")
	x.expect("unknown", resend("sup-1", "PO-404"), "ERROR_CODE_INVALID_ARGUMENT")
	x.poll("page-1", "page-2", erpadapter.Planned{ID: "PO-9002", Product: "P-100", Quantity: 1}, erpadapter.Planned{ID: "PO-9005", Product: "P-200", Quantity: 1})
	x.expect("another product", resend("sup-1", "PO-9005"), "ERROR_CODE_INVALID_ARGUMENT")
	x.expect("confirmed", resend("sup-1", "PO-9001"), "ERROR_CODE_INVALID_ARGUMENT")
	x.expect("too few", x.do("sup-1", mes.ID, mes.SchemaRelease, mes.OrderType, "SO-9",
		map[string]any{"product": "P-100", "quantity": 2, "sfcs": 1, "planned": "PO-9002"}), "ERROR_CODE_INVALID_ARGUMENT")
	x.expect("resend", resend("sup-1", "PO-9002"), "ok")
	x.expect("sent again", x.order("SO-2").ERP, "sent")
	x.work(4)
	x.expect("the task closed", x.inbox()+" "+x.flow("SO-2").State, "[] done")
	o = x.order("SO-2")
	x.expect("accepted", fmt.Sprint(o.ERP, " ", o.Confirmation, " ", o.Resent, " ", x.calls), "confirmed CONF-PO-9002-1 1 3")
	var keys []string
	for _, e := range x.tn.Effects(x.now) {
		keys = append(keys, e.Key+":"+e.State)
	}
	x.expect("effects", keys, "[PO-9002#1:delivered PO-9004#1:rejected PO-9001#1:delivered]")

	// D6: the assistant corrects an order released without a planned order. The
	// confirmation cannot be recalled, so it waits for a person's approval.
	x.make("SO-3", "", 1, 1, 0)
	x.work(2)
	x.expect("refused by the plant", x.order("SO-3").ERP+": "+x.order("SO-3").ERPDetail, "refused: no planned order to confirm against")
	x.poll("page-2", "page-3", erpadapter.Planned{ID: "PO-9003", Product: "P-100", Quantity: 1})
	x.expect("the assistant resends", x.do("agent-l1", mes.ID, mes.SchemaResend, mes.OrderType, "SO-3", map[string]string{"planned": "PO-9003"}), "ok")
	held := x.tn.Effects(x.now)[0]
	x.expect("held", held.State+" "+held.Agent, "held agent-l1")
	x.work(2)
	x.expect("not sent", x.calls, "3")
	admin, _ := x.tn.Read(x.member("sup-1"), "notifications")
	x.expect("asked to approve", admin.([]platform.Notification)[0].Title, "Approve Production order confirmation for erpadapter.order/PO-9003")
	approve := func(who platform.Member) string {
		x.keys++
		_, err := x.tn.Submit(who, &pb.Submission{TenantId: tenant, PrincipalId: who.ID, Authority: platformserver.PlatformApp, IdempotencyKey: fmt.Sprint("a", x.keys),
			Target: &pb.EntityRef{Type: platformserver.EffectType, Id: held.ID}, Schema: &pb.SchemaRef{Name: platformserver.SchemaEffectApprove, Version: 1}, Payload: []byte("{}")}, x.now)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	robot := x.member("sup-1")
	robot.ID, robot.Agent = "sup-bot", true
	x.expect("an agent approves", approve(robot), "ERROR_CODE_POLICY_DENIED") // whatever its role
	x.expect("the supervisor approves", approve(x.member("sup-1")), "ok")
	x.work(3)
	o = x.order("SO-3")
	x.expect("confirmed after approval", fmt.Sprint(o.ERP, " ", o.Confirmation, " ", x.calls), "confirmed CONF-PO-9003-1 4")
}

// ADR-0021 D10 (1): an order released without a planned order is refused; the
// confirmation flow's agent reads the planned orders and proposes the one the
// order fulfils; a supervisor approves in the inbox, the flow resends it, and
// the ERP outside confirms. The agent itself acts on nothing.
func TestCorrectedByTheAgent(t *testing.T) {
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
	x := newExternal(t)
	x.expect("provider", x.do("sup-1", platformserver.AIApp, platformserver.SchemaProviderAdd, platformserver.ProviderType, "lm", map[string]any{"kind": "local", "baseUrl": model.URL + "/v1"}), "ok")
	x.expect("model", x.do("sup-1", platformserver.AIApp, platformserver.SchemaModelEnable, platformserver.ModelType, "lm/agent", map[string]string{"access": "users"}), "ok")
	x.expect("agents' model", x.do("sup-1", platformserver.PlatformApp, platformserver.SchemaSettingSet, platformserver.SettingType, "agent/model", map[string]string{"value": "lm/agent"}), "ok")
	x.poll("", "page-1", erpadapter.Planned{ID: "PO-9001", Product: "P-100", Quantity: 1}, erpadapter.Planned{ID: "PO-9002", Product: "P-100", Quantity: 1})
	x.expect("SO-1", x.do("sup-1", mes.ID, mes.SchemaRelease, mes.OrderType, "SO-1", map[string]any{"product": "P-100", "quantity": 1, "sfcs": 1, "planned": "PO-9001"}), "ok")
	x.make("SO-2", "", 1, 1, 0)
	x.work(8)
	x.expect("the agent's proposal", x.inbox(), "[Resend SO-2 to the ERP against PO-9002?]")
	agents := platform.Member{ID: "x", Tenant: tenant, Roles: map[string]string{platformserver.AgentApp: platformserver.AgentAdmin}}
	runs, _ := x.tn.Records(agents, platformserver.RunType, platform.Query{}, x.now)
	run := runs.Records[0].(platformserver.AgentRunRecord)
	x.expect("its run", fmt.Sprint(run.State, " ", run.Steps[0].Tool, " ", run.Steps[1].Tool, " ", run.ActionsUsed), "done read_planned_orders finish 0")
	out, _ := x.tn.Read(x.member("sup-1"), "inbox")
	x.expect("resend", x.do("sup-1", platformserver.WorkApp, "work.task.complete", platformserver.TaskType, out.([]platformserver.WorkTask)[0].ID, map[string]string{"answer": "resend"}), "ok")
	x.work(6)
	o := x.order("SO-2")
	x.expect("confirmed", fmt.Sprint(o.ERP, " ", o.Confirmation, " ", o.Planned), "confirmed CONF-PO-9002-1 PO-9002")
	// The supervisor's answer is kept on the run: the agent's evaluation reference (ADR-0021 D7).
	runs, _ = x.tn.Records(agents, platformserver.RunType, platform.Query{}, x.now)
	sig := runs.Records[0].(platformserver.AgentRunRecord).Signals
	x.expect("signal", fmt.Sprint(len(sig), " ", sig[0].Kind, " ", sig[0].By), "1 accepted sup-1")
}
