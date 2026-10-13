package authz

import (
	"slices"
	"testing"
)

func TestOperationLayerOrderAndNarrowing(t *testing.T) {
	pass := Gate{Allowed: true, Reason: "owner permits"}
	context := &Context{Tenant: pass, Principal: pass, Row: pass, Fields: pass, Derived: pass, Markings: pass}
	request := Request{Kind: "write", Subject: Subject{ID: "ann", App: "build", Roles: []string{"builder"}}, Permission: "build.order.edit", Allowed: []string{"builder"}, Context: context}
	verdict := Decide(request)
	var names []string
	for _, layer := range verdict.Layers {
		names = append(names, layer.Name)
	}
	if !verdict.Allow || !slices.Equal(names, []string{"tenant", "principal", "deny", "verb", "row", "fields", "derived", "markings", "allow"}) {
		t.Fatalf("layer order: %+v", verdict)
	}
	context.Row = Gate{Reason: "row scope excludes this record"}
	allow := Policy{ID: "broad", Effect: "allow", Permission: "*"}
	if got := Decide(request, allow); got.Allow || got.Layers[len(got.Layers)-1].Name != "row" {
		t.Fatalf("allow widened row scope: %+v", got)
	}
	request.Subject.Automation = true
	if got := Decide(request, allow); got.Allow {
		t.Fatal("automation widened row scope")
	}
	context.Row = pass
	deny := Policy{ID: "freeze", Effect: "deny", Permission: "*"}
	if got := Decide(request, deny, allow); got.Allow || got.Policy != "freeze" {
		t.Fatal("automation bypassed explicit deny")
	}
	request.Kind = "related"
	if got := Decide(request); got.Allow || got.Reason != "Unknown authorization kind" {
		t.Fatal("unknown kind accepted")
	}
}
