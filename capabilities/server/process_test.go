package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/internal/host"
	"platformserver/platform"
)

func TestTenantProcessPublicationAndRunningVersionsRecover(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		seat := func(id, role string) Seat {
			return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{construction.ID: role, flow.ID: flow.Admin}}}
		}
		tn, err := NewTenant("process", NewConsole("process", seat("builder", construction.Builder), seat("user", construction.User)), work.New("process"), flow.New("process"), build.New("process"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	member := func(id string) platform.Member { m, _ := tn.Member(id); return m }
	var journal []Entry
	tn.Record = func(e Entry) { journal = append(journal, e) }
	fail := false
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("injected append failure")
		}
		journal = append(journal, e)
		return e.Body, nil
	}
	keys := 0
	request := func(who, schema, typ, id string, payload any) *pb.Submission {
		keys++
		raw, _ := json.Marshal(payload)
		authority := construction.ID
		if typ == work.TaskType {
			authority = work.ID
		}
		return &pb.Submission{TenantId: tn.ID, PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprint(keys), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}
	}
	must := func(who, schema, typ, id string, payload any) {
		t.Helper()
		if _, err := tn.Submit(member(who), request(who, schema, typ, id, payload), at); err != nil {
			t.Fatalf("%s: %s %s", schema, err.Code, err.Message)
		}
	}
	must("builder", construction.ObjectType+".create", construction.ObjectType, "GROUP", map[string]any{"name": "group", "title": "Group", "fields": []construction.Field{{Name: "label", Title: "Label", Type: "text"}}})
	must("builder", construction.SchemaPublish, construction.ObjectType, "GROUP", map[string]any{})
	must("builder", construction.ObjectType+".create", construction.ObjectType, "O", map[string]any{"name": "visit", "title": "Visit", "fields": []construction.Field{{Name: "guest", Title: "Guest", Type: "text"}, {Name: "group", Title: "Group", Type: "reference", Ref: "build.group"}}, "states": []construction.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}, {Name: "rejected", Title: "Rejected"}}, "actions": []construction.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done"}, {Name: "reject", Title: "Reject", From: []string{"open"}, To: "rejected"}}})
	must("builder", construction.SchemaPublish, construction.ObjectType, "O", map[string]any{})
	steps := []construction.ProcessStep{{Name: "check", Title: "Check visit", Kind: "ask", Ask: construction.User, Answers: []string{"approve", "reject"}, Cases: map[string]string{"approve": "close", "reject": "reject"}}, {Name: "close", Kind: "action", Act: "close"}, {Name: "reject", Kind: "action", Act: "reject"}}
	must("builder", construction.ProcessType+".create", construction.ProcessType, "P", map[string]any{"name": "review", "title": "Visit review", "object": "build.visit", "when": "open", "steps": steps})
	// Standard edit replaces supplied nested maps/slices and keeps omitted
	// top-level fields. The old decoder retained the removed reject branch.
	patchedSteps := []construction.ProcessStep{steps[0], steps[1], steps[2]}
	patchedSteps[0].Cases = map[string]string{"approve": "close"}
	must("builder", construction.ProcessType+".edit", construction.ProcessType, "P", map[string]any{"steps": patchedSteps})
	patched, err := tn.RecordOf(member("builder"), construction.ProcessType, "P", at)
	if err != nil {
		t.Fatal(err)
	}
	draft := patched.Record.(construction.Process)
	if draft.Title != "Visit review" || len(draft.Steps[0].Cases) != 1 || draft.Steps[0].Cases["approve"] != "close" {
		t.Fatalf("standard edit retained a removed branch or lost an omitted field: %+v", draft)
	}
	must("builder", construction.ProcessType+".edit", construction.ProcessType, "P", map[string]any{"steps": steps})
	publish := request("builder", construction.SchemaProcess, construction.ProcessType, "P", map[string]any{})
	before, count := snapshot(tn), len(journal)
	fail = true
	if _, err := tn.Submit(member("builder"), publish, at); err == nil || snapshot(tn) != before || len(journal) != count {
		t.Fatal("failed append exposed a process version")
	}
	defs, _ := tn.Read(member("builder"), "flows")
	if len(defs.([]flow.FlowDefinition)) != 0 {
		t.Fatal("failed publication installed a flow")
	}
	fail = false
	first, err := tn.Submit(member("builder"), publish, at)
	if err != nil {
		t.Fatal(err)
	}
	again, err := tn.Submit(member("builder"), publish, at.Add(time.Hour))
	if err != nil || !proto.Equal(first, again) || len(journal) != count+1 {
		t.Fatal("publication retry added a version")
	}
	must("builder", construction.ObjectType+".create", construction.ObjectType, "UNRELATED", map[string]any{"name": "unrelated", "title": "Unrelated", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}})
	must("builder", construction.SchemaPublish, construction.ObjectType, "UNRELATED", map[string]any{})
	other, previewErr := tn.PreviewRelease(member("builder"), platform.AssetObject, "UNRELATED")
	if previewErr != nil {
		t.Fatal(previewErr)
	}
	if _, err := tn.SaveReleaseCandidate(member("builder"), platform.AssetObject, "UNRELATED", other.CandidateID, "other-save", at); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.ActivateRelease(member("builder"), other.CandidateID, "other-active", at); err != nil {
		t.Fatal(err)
	}
	development, bindingErr := (hostView{t: tn}).BindFlow(construction.ID, "review", 1, host.FlowBinding{})
	if bindingErr != nil || development.Dependencies == "" || development.Release != "" {
		t.Fatalf("unrelated active pointer bound a development flow: %+v %v", development, bindingErr)
	}
	activate := func() string {
		t.Helper()
		preview, err := tn.PreviewRelease(member("builder"), platform.AssetFlow, "P")
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("release preview: %+v %v", preview, err)
		}
		keys++
		if _, err := tn.SaveReleaseCandidate(member("builder"), platform.AssetFlow, "P", preview.CandidateID, fmt.Sprint(keys), at); err != nil {
			t.Fatal(err)
		}
		keys++
		if _, err := tn.ActivateRelease(member("builder"), preview.CandidateID, fmt.Sprint(keys), at); err != nil {
			t.Fatal(err)
		}
		return preview.CandidateID
	}
	release1 := activate()
	if _, err := tn.RecordOf(member("user"), construction.ProcessType, "P", at); err == nil {
		t.Fatal("user read a private process definition")
	}
	foreign := member("builder")
	foreign.Tenant = "other"
	if _, err := tn.Submit(foreign, request("builder", construction.SchemaProcess, construction.ProcessType, "P", map[string]any{}), at); err == nil {
		t.Fatal("foreign builder published a process")
	}
	if _, err := tn.Submit(member("user"), request("user", construction.SchemaProcess, construction.ProcessType, "P", map[string]any{}), at); err == nil {
		t.Fatal("user published a process")
	}
	tick := func() { at = at.Add(2 * time.Second); tn.Work(at) }
	must("user", "build.visit.create", "build.visit", "V1", map[string]any{"guest": "Ada"})
	tick()
	view, err := tn.RecordOf(member("builder"), flow.InstanceType, "build.review:V1", at)
	if err != nil || view.Record.(flow.FlowInstance).Version != 1 || view.Record.(flow.FlowInstance).State != "waiting" {
		t.Fatalf("native flow did not wait: %+v %v", view, err)
	}
	started := view.Record.(flow.FlowInstance)
	if started.Dependencies != release1 || started.Release != release1 {
		t.Fatalf("instance did not bind the exact release: %+v", started)
	}
	inbox, _ := tn.Read(member("user"), "inbox")
	tasks := inbox.([]work.WorkTask)
	if len(tasks) != 1 || tasks[0].Ref != "build.visit/V1" {
		t.Fatalf("native task missing: %+v", tasks)
	}
	for _, id := range []string{"O", "GROUP"} {
		if _, err := tn.Submit(member("builder"), request("builder", construction.ObjectType+".archive", construction.ObjectType, id, map[string]any{}), at); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
			t.Fatalf("archived workflow dependency %s: %v", id, err)
		}
		row, err := tn.RecordOf(member("builder"), construction.ObjectType, id, at)
		if err != nil || row.Record.(construction.Object).Archived {
			t.Fatal("archive refusal removed its source definition")
		}
	}
	must("builder", construction.ObjectType+".edit", construction.ObjectType, "GROUP", map[string]any{"title": "Changed group"})
	must("builder", construction.SchemaPublish, construction.ObjectType, "GROUP", map[string]any{})
	must("builder", construction.ObjectType+".edit", construction.ObjectType, "GROUP", map[string]any{"title": "Group"})
	must("builder", construction.SchemaPublish, construction.ObjectType, "GROUP", map[string]any{})
	// A waiting v1 must not silently execute a changed object's close action.
	originalActions := []construction.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done"}, {Name: "reject", Title: "Reject", From: []string{"open"}, To: "rejected"}}
	changedActions := []construction.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "rejected"}, originalActions[1]}
	must("builder", construction.ObjectType+".edit", construction.ObjectType, "O", map[string]any{"actions": changedActions})
	must("builder", construction.SchemaPublish, construction.ObjectType, "O", map[string]any{})
	// Republishing the identical source is safe, even with a waiting instance.
	must("builder", construction.ObjectType+".edit", construction.ObjectType, "O", map[string]any{"actions": originalActions})
	must("builder", construction.SchemaPublish, construction.ObjectType, "O", map[string]any{})
	// Publishing a different path leaves the already waiting instance on v1.
	must("builder", construction.ProcessType+".edit", construction.ProcessType, "P", map[string]any{"steps": []construction.ProcessStep{{Name: "reject", Kind: "action", Act: "reject"}}})
	must("builder", construction.SchemaProcess, construction.ProcessType, "P", map[string]any{})
	if _, err := (hostView{t: tn}).BindFlow(construction.ID, "review", 2, host.FlowBinding{}); err == nil {
		t.Fatal("started a changed flow under the old active release")
	}
	release2 := activate()
	if release2 == release1 || tn.procs.Check() != nil {
		t.Fatal("new release changed the old instance's dependencies")
	}
	// An unfinished or invalid draft cannot replace the saved runtime family.
	must("builder", construction.ProcessType+".edit", construction.ProcessType, "P", map[string]any{"name": "unfinished", "object": "build.missing", "steps": []construction.ProcessStep{}})
	// Snapshot restoration must install both versions before validating instances.
	raw, _, snapshotErr := tn.Snapshot(func() int64 { return 0 })
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	if snapshot(restored) != snapshot(tn) {
		t.Fatal("snapshot lost process versions/tasks")
	}
	if err := restored.procs.Check(); err != nil {
		t.Fatal(err)
	}
	// A validly decoded row still fails recovery when its binding is forged.
	for _, corrupt := range []func(*flow.FlowInstance){
		func(x *flow.FlowInstance) { x.Dependencies = release2 },
		func(x *flow.FlowInstance) { x.Release = release2 },
		func(x *flow.FlowInstance) { x.Dependencies = ""; x.Release = "" },
	} {
		x := started
		corrupt(&x)
		value, _ := json.Marshal(x)
		if _, err := restored.restoreRecords(map[string][]recordState{flow.InstanceType: {{Value: value}}}); err != nil {
			t.Fatal(err)
		}
		if err := restored.procs.Check(); err == nil {
			t.Fatal("recovery accepted a missing or forged flow binding")
		}
	}
	must("builder", construction.ProcessType+".edit", construction.ProcessType, "P", map[string]any{"name": "review", "object": "build.visit", "steps": []construction.ProcessStep{{Name: "reject", Kind: "action", Act: "reject"}}})
	must("user", "work.task.complete", work.TaskType, tasks[0].ID, map[string]string{"answer": "approve"})
	tick()
	state := func(id string) string {
		v, err := tn.RecordOf(member("builder"), "build.visit", id, at)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(v.Record)
		var row struct{ State string }
		_ = json.Unmarshal(data, &row)
		return row.State
	}
	if state("V1") != "done" {
		t.Fatal("old instance used the new reject path")
	}
	must("user", "build.visit.create", "build.visit", "V2", map[string]any{"guest": "Lin"})
	tick()
	if state("V2") != "rejected" {
		t.Fatal("new instance did not use version 2")
	}
	v2, err := tn.RecordOf(member("builder"), flow.InstanceType, "build.review:V2", at)
	if err != nil || v2.Record.(flow.FlowInstance).Version != 2 || v2.Record.(flow.FlowInstance).Dependencies != release2 || v2.Record.(flow.FlowInstance).Release != release2 {
		t.Fatal("new instance did not pin version 2")
	}
	must("builder", construction.ObjectType+".edit", construction.ObjectType, "GROUP", map[string]any{"title": "Changed group"})
	must("builder", construction.SchemaPublish, construction.ObjectType, "GROUP", map[string]any{})
	if _, err := tn.Submit(member("builder"), request("builder", construction.ObjectType+".archive", construction.ObjectType, "O", map[string]any{}), at); err == nil {
		t.Fatal("archived a source still required by its installed workflow after all runs ended")
	}
	if _, err := tn.Submit(member("builder"), request("builder", construction.ObjectType+".archive", construction.ObjectType, "UNRELATED", map[string]any{}), at); err == nil {
		t.Fatal("archived an installed data class still retained by records/history")
	}
	must("builder", construction.ObjectType+".create", construction.ObjectType, "DRAFT-ARCHIVE", map[string]any{"name": "draftarchive", "title": "Draft", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}})
	must("builder", construction.ObjectType+".archive", construction.ObjectType, "DRAFT-ARCHIVE", map[string]any{})

	definitions, _ := tn.Read(member("builder"), "flows")
	encodedDefinitions, _ := json.Marshal(definitions)
	for index, bad := range []map[string]any{
		{"name": "badbranch", "steps": []construction.ProcessStep{{Name: "ask", Kind: "ask", Ask: construction.User, Answers: []string{"yes"}, Cases: map[string]string{"no": "ask"}}}},
		{"name": "badrole", "steps": []construction.ProcessStep{{Name: "ask", Kind: "ask", Ask: "outsider"}}},
		{"name": "badaction", "steps": []construction.ProcessStep{{Name: "act", Kind: "action", Act: "missing"}}},
		{"name": "badpath", "steps": []construction.ProcessStep{{Name: "act", Kind: "action", Act: "close", Next: "missing"}}},
		{"name": "duplicate", "steps": []construction.ProcessStep{{Name: "act", Kind: "action", Act: "close"}, {Name: "act", Kind: "action", Act: "reject"}}},
		{"name": "review", "steps": []construction.ProcessStep{{Name: "act", Kind: "action", Act: "close"}}},
	} {
		id := fmt.Sprint("BAD", index)
		bad["title"], bad["object"], bad["when"] = "Bad review", "build.visit", "open"
		must("builder", construction.ProcessType+".create", construction.ProcessType, id, bad)
		if _, err := tn.Submit(member("builder"), request("builder", construction.SchemaProcess, construction.ProcessType, id, map[string]any{}), at); err == nil {
			t.Fatal("invalid process published")
		}
		row, _ := tn.RecordOf(member("builder"), construction.ProcessType, id, at)
		if row.Record.(construction.Process).Version != 0 || row.Record.(construction.Process).State != "draft" {
			t.Fatal("refusal exposed partial publication")
		}
		after, _ := tn.Read(member("builder"), "flows")
		raw, _ := json.Marshal(after)
		if string(raw) != string(encodedDefinitions) {
			t.Fatal("refusal changed installed process definitions")
		}
	}
	CheckReplay(t, tn, journal, compose)
}

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
