package platformserver

import "platformserver/internal/construction"

import (
	"fmt"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// The store caps each read page at 500. Definitions and running dependencies
// on later pages must participate in publication checks too.
func TestProcessChecksBeyondFirstReadPage(t *testing.T) {
	seat := Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{construction.ID: construction.Builder}}}
	codeFlow := platform.Flow{Name: "review", Title: "Review", Version: 1,
		Start: platform.Start{On: []string{"shop.order.place"}, Begin: func(platform.Caller, platform.Event) (string, any, bool) { return "", nil, false }},
		Steps: []platform.Step{{Name: "wait", Wait: &platform.Wait{Until: func(_ platform.Caller, r *platform.Run) bool { return r.ID == "P0500" }}}}}
	tn, err := NewTenant("paged", NewConsole("paged", seat), work.New("paged"), flow.New("paged"), build.New("paged"), newShop("paged", codeFlow))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	put := func(app, typ, id string, revision uint32, value any) {
		t.Helper()
		c := platform.NewCaller(runtime{tn}, platform.Member{ID: "fixture", Tenant: tn.ID}, app, true, true)
		change := &pb.ChangeRecord{ChangeId: fmt.Sprintf("%s:%s:%d", typ, id, revision), Revision: revision,
			Submission: &pb.Submission{Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + ".create"}}}
		if err := tn.records.put(c, change, value); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index <= 500; index++ {
		id := fmt.Sprintf("P%04d", index)
		name := fmt.Sprintf("draft%d", index)
		subject := "build.other/record"
		if index == 500 {
			name, subject = "duplicate", "build.source/record"
		}
		put(construction.ID, construction.ProcessType, id, 1, construction.Process{Record: platform.Record{ID: id}, Name: name, Title: name, Object: "build.source", When: "open", State: "draft"})
		put(flow.ID, flow.InstanceType, id, 1, flow.FlowInstance{Record: platform.Record{ID: id}, Flow: "shop.review", Subject: subject, State: "waiting", Version: 1})
	}
	member, _ := tn.Member("builder")
	submit := func(schema, key string, payload string) *kernel.Error {
		_, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: construction.ID, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: construction.ProcessType, Id: "NEW"}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, at)
		return err
	}
	if err := submit(construction.ProcessType+".create", "create", `{"name":"duplicate","title":"Duplicate","object":"build.source","when":"open"}`); err != nil {
		t.Fatal(err)
	}
	if err := submit(construction.SchemaProcess, "publish", `{}`); err == nil || !strings.Contains(err.Message, "already used") {
		t.Fatalf("ignored a duplicate name after row 500: %v", err)
	}
	procs := tn.app(flow.ID).(*flow.Flows)
	if running, err := procs.HasRunningDependency("build.source"); err != nil || !running {
		t.Fatalf("ignored a waiting instance after row 500: %v", err)
	}
	put(flow.ID, flow.InstanceType, "P0500", 2, flow.FlowInstance{Record: platform.Record{ID: "P0500"}, Flow: "shop.review", Subject: "build.source/record", State: "waiting", Version: 2})
	if err := procs.Check(); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("recovery skipped the instance beyond row 500: %v", err)
	}
	put(flow.ID, flow.InstanceType, "P0500", 3, flow.FlowInstance{Record: platform.Record{ID: "P0500"}, Flow: "shop.review", Subject: "build.source/record", State: "waiting", Version: 1,
		Tokens: []flow.Token{{ID: 1, Step: "wait", Waits: "wait"}}})
	tn.Work(at)
	completed, ok := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), "P0500")
	if !ok || completed.State != "done" {
		t.Fatal("native timers skipped the waiting instance beyond row 500")
	}
	if running, err := procs.HasRunningDependency("build.source"); err != nil || running {
		t.Fatalf("terminal instance blocked the source: %v", err)
	}
}
