package authz

import "testing"

func TestDecideOrder(t *testing.T) {
	s := Subject{ID: "ann", App: "mes", Roles: []string{"viewer", "planner"}}
	if v := Decide(Request{Subject: s, Permission: "mes.order.release", Allowed: []string{"planner"}}); !v.Allow || v.Rule != "role" || v.Role != "planner" {
		t.Fatalf("second role not honoured: %+v", v)
	}
	if v := Decide(Request{Subject: s, Permission: "mes.order.release", Allowed: []string{"admin"}}); v.Allow || v.Rule != "none" || v.Reason == "" {
		t.Fatalf("%+v", v)
	}
	if v := Decide(Request{Subject: s, Permission: "mes.order.read", Resource: "mes.order/1", Allowed: []string{"admin"}, Related: true}); !v.Allow || v.Rule != "relation" {
		t.Fatalf("%+v", v)
	}
	deny := Policy{ID: "freeze", Effect: "deny", Permission: "mes.order.*", When: func(r Request) bool { return r.Attributes["status"] == "closed" }}
	closed := Request{Subject: s, Permission: "mes.order.release", Allowed: []string{"planner"}, Attributes: map[string]string{"status": "closed"}}
	if v := Decide(closed, deny); v.Allow || v.Policy != "freeze" {
		t.Fatalf("deny did not win: %+v", v)
	}
	closed.Subject.Replaying = true
	if v := Decide(closed, deny); !v.Allow || v.Rule != "bypass" {
		t.Fatalf("replay must bypass: %+v", v)
	}
	allow := Policy{ID: "night", Effect: "allow", Permission: "mes.order.release"}
	if v := Decide(Request{Subject: Subject{ID: "bob", App: "mes"}, Permission: "mes.order.release", Allowed: []string{"planner"}}, allow); !v.Allow || v.Rule != "policy" {
		t.Fatalf("%+v", v)
	}
	if Widest([]string{"own", "unit"}) != "unit" || Widest(nil) != "none" || Widest([]string{"none", "tenant"}) != "tenant" {
		t.Fatal("widest")
	}
}
