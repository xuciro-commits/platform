package platformserver

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"testing"
	"time"
)

func TestAgentTargetLookupReadsTheStagedAndControlRecordView(t *testing.T) {
	tenant := definitionOwnerTenant(t, "system")
	member, _ := tenant.Member("builder")
	staged := tenant.newStagedDecision()
	app := tenant.app("build")
	caller := platform.NewCaller(staged, member, "build", false, false)
	submission := &pb.Submission{TenantId: tenant.ID, PrincipalId: member.ID, Authority: "build", IdempotencyKey: "private-target", Target: &pb.EntityRef{Type: "build.object", Id: "private"}, Schema: &pb.SchemaRef{Name: "build.object.create", Version: 1}, Payload: platform.Raw(map[string]any{"name": "privateobject", "title": "Private", "fields": []map[string]any{{"name": "title", "title": "Title", "type": "text"}}})}
	if _, refusal := platform.Decide(caller, app, submission, time.Now()); refusal != nil {
		t.Fatal(refusal)
	}
	if _, exists := tenant.held("build.object/private"); exists {
		t.Fatal("the staged row leaked into live state")
	}
	if _, exists := (hostView{t: tenant, decision: staged}).Record("build.object/private"); !exists {
		t.Fatal("agent lookup missed the staged row")
	}
	if _, exists := (hostView{t: staged.controlHost()}).Record("build.object/private"); !exists {
		t.Fatal("control lookup missed the same staged row")
	}
}
