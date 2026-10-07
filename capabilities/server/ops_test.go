package platformserver

import (
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

// A builder declares an agent over a published object and an alert rule on it
// (ADR-0077): the agent appears among the tenant's agents with the object's
// action as its tool once published, and the rule tells the role's holders
// once when a record comes to match.
func TestBuilderAgentsAndAlerts(t *testing.T) {
	dana := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana", Roles: map[string]string{build.ID: build.Builder}}}
	wes := Seat{Subjects: []string{"wes"}, Member: platform.Member{ID: "wes", Roles: map[string]string{build.ID: "desk"}}}
	tn, err := NewTenant("ops", NewConsole("ops", dana, wes), ai.New("ops"), work.New("ops"), NewAgents("ops"), build.New("ops"))
	if err != nil {
		t.Fatal(err)
	}
	member, _ := tn.Member("dana")
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	submit := func(key, schema, typ, target, payload string) *pb.ChangeRecord {
		t.Helper()
		r, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: typ, Id: target}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, at)
		if err != nil {
			t.Fatalf("%s: %s %s", key, err.Code, err.Message)
		}
		return r
	}
	refused := func(key, schema, typ, target, payload, why string) {
		t.Helper()
		if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: typ, Id: target}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, at); err == nil || !strings.Contains(err.Message, why) {
			t.Fatalf("%s: want refusal about %q, got %v", key, why, err)
		}
	}
	submit("obj", build.ObjectType+".create", build.ObjectType, "O1", `{"name":"order","title":"Order","fields":[{"name":"qty","title":"Quantity","type":"integer"},{"name":"due","title":"Due","type":"text"}],
		"access":[{"role":"desk","read":"all","create":true,"edit":true}],
		"states":[{"name":"open","title":"Open"},{"name":"late","title":"Late"}],"actions":[{"name":"expedite","title":"Expedite","from":["open","late"],"to":"open"}]}`)
	submit("obj-publish", build.SchemaPublish, build.ObjectType, "O1", `{}`)

	// Agent: a tool must be a published action; a checkpoint one of its tools.
	refused("agent-bad", build.AgentType+".create", build.AgentType, "A1", `{"name":"expediter","title":"Expediter","description":"Chases late orders","instructions":"Find late orders and expedite them.","tools":["build.order.ship"]}`, "not an action")
	submit("agent", build.AgentType+".create", build.AgentType, "A1", `{"name":"expediter","title":"Expediter","description":"Chases late orders","instructions":"Find late orders and expedite them.","tools":["build.order.expedite"],"checkpoints":["build.order.expedite"],"handoffRole":"desk","actions":2,
		"cases":[{"name":"asks first","goal":"Expedite order o-1","ref":"build.order/o-1","asks":true}]}`)
	before, _ := tn.agents.Read(platform.NewCaller(runtime{tn}, member, AgentApp, false, false), "agents")
	if infos := before.([]AgentInfo); len(infos) != 0 {
		t.Fatalf("a draft agent is not installed: %+v", infos)
	}
	submit("agent-publish", build.SchemaAgentPublish, build.AgentType, "A1", `{}`)
	after, _ := tn.agents.Read(platform.NewCaller(runtime{tn}, member, AgentApp, false, false), "agents")
	infos := after.([]AgentInfo)
	if len(infos) != 1 || infos[0].ID != "build.expediter" || infos[0].Budget.Actions != 2 || infos[0].Budget.Steps != 10 || infos[0].Tools[0] != "build.order.expedite" {
		t.Fatalf("published agent %+v", infos)
	}
	if d := tn.agents.def("build.expediter"); d == nil || len(d.Cases) != 1 || d.Cases[0].Check(platform.CaseRun{Result: "done", Asked: false}) == "" || d.Cases[0].Check(platform.CaseRun{Result: "done", Asked: true}) != "" {
		t.Fatalf("the agent's case must require asking")
	}

	// Alert: told once per record when quantity passes 100, to the desk.
	refused("alert-bad", build.AlertType+".create", build.AlertType, "R1", `{"name":"big","title":"Big order","object":"build.order","field":"size","operator":">","value":"100","message":"Check capacity.","active":true}`, "no field")
	submit("alert", build.AlertType+".create", build.AlertType, "R1", `{"name":"big","title":"Big order","object":"build.order","field":"qty","operator":">","value":"100","message":"Check capacity.","role":"desk","active":true}`)
	submit("o1", "build.order.create", "build.order", "o-1", `{"qty":50,"due":"2026-10-20"}`)
	if n := tn.notificationsFor("wes"); len(n) != 0 {
		t.Fatalf("50 is not over 100: %+v", n)
	}
	submit("o1-edit", "build.order.edit", "build.order", "o-1", `{"qty":150}`)
	n := tn.notificationsFor("wes")
	if len(n) != 1 || n[0].Title != "Big order" || n[0].Ref != "build.order/o-1" || n[0].Body != "Check capacity." {
		t.Fatalf("the desk is told once: %+v", n)
	}
	submit("o1-edit2", "build.order.edit", "build.order", "o-1", `{"qty":160}`)
	if n := tn.notificationsFor("wes"); len(n) != 1 {
		t.Fatalf("still once per record: %+v", n)
	}
	if n := tn.notificationsFor("dana"); len(n) != 0 {
		t.Fatalf("the builder is not the desk: %+v", n)
	}
}
