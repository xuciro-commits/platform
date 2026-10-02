package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestFlowQueryVersionFreezesWaitsAndRecovers(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("query-flow", NewConsole("query-flow", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, flow.ID: flow.Admin}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), work.New("query-flow"), flow.New("query-flow"), build.New("query-flow"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	key := 0
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, schema string, payload any) {
		t.Helper()
		key++
		if _, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, at); err != nil {
			t.Fatalf("%s: %s", schema, err.Message)
		}
	}
	submit(build.ObjectType, "O", build.ObjectType+".create", map[string]any{"name": "note", "title": "Note", "fields": []build.Field{{Name: "label", Title: "Label", Type: "text"}, {Name: "bucket", Title: "Bucket", Type: "text"}}})
	submit(build.ObjectType, "O", build.SchemaPublish, struct{}{})
	submit("build.note", "A", "build.note.create", map[string]string{"label": "FIRST", "bucket": "A"})
	submit("build.note", "B", "build.note.create", map[string]string{"label": "SECOND", "bucket": "B"})
	submit(build.QueryType, "Q", build.QueryType+".create", map[string]any{"name": "notes", "title": "Original notes", "description": "Reusable notes", "limit": 50, "object": "build.note", "domain": json.RawMessage(`[["bucket","=","A"]]`)})
	submit(build.QueryType, "Q", build.SchemaQuery, struct{}{})
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetQuery, Name: "notes"}
	descriptor, problem := tn.DescribeCapability(reader, ref, 1)
	if problem != nil || descriptor.Revision != 1 || descriptor.Version != "1.query-1" || descriptor.Input == nil || descriptor.Output == nil {
		t.Fatalf("retained ports: %+v %v", descriptor, problem)
	}
	for _, version := range []int{0, 99} {
		if _, err := tn.InvokeCapability(reader, platform.CapabilityInvocation{Ref: ref, Version: version, Inputs: json.RawMessage(`{}`), Key: fmt.Sprintf("bad-%d", version)}, at); err == nil {
			t.Fatal("query fell back to an unpinned or missing version")
		}
	}
	if _, err := tn.InvokeCapability(reader, platform.CapabilityInvocation{Ref: ref, Version: 1, Inputs: json.RawMessage(`{"unknown":3}`), Key: "bad-input"}, at); err == nil {
		t.Fatal("query accepted unknown inputs")
	}
	steps := []build.ProcessStep{{Name: "pause", Kind: "wait", UntilSeconds: 60, Next: "read"}, {Name: "read", Kind: "query", App: build.ID, Query: "notes", QueryVersion: 1, Next: "done"}, {Name: "done", Kind: "end", Value: &platform.Binding{Source: "step", Step: "read"}}}
	owner := tn.app(build.ID).(*build.Build)
	for _, version := range []int{0, 99, -1} {
		bad := append([]build.ProcessStep(nil), steps...)
		bad[1].QueryVersion = version
		if owner.CheckProcess(build.Process{Name: "bad", Title: "Bad", Manual: true, Steps: bad}) == nil {
			t.Fatal("compiler accepted invalid query version", version)
		}
	}
	bad := append([]build.ProcessStep(nil), steps...)
	bad[1].Inputs = map[string]platform.Binding{"unknown": {Source: "literal", Value: json.RawMessage(`3`)}}
	if owner.CheckProcess(build.Process{Name: "bad", Title: "Bad", Manual: true, Steps: bad}) == nil {
		t.Fatal("compiler accepted unknown query input")
	}
	submit(build.ProcessType, "P", build.ProcessType+".create", map[string]any{"name": "reuse", "title": "Reuse notes", "manual": true, "steps": steps})
	preview, err := tn.PreviewRelease(builder, platform.AssetFlow, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetFlow, "P", preview.CandidateID, "save-flow", at)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := platform.ReadCandidate(saved, tn.releaseCandidates[saved])
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range candidate.Assets {
		if a.Ref.Kind != platform.AssetFlow {
			continue
		}
		var d platform.FlowReleaseDescriptor
		_ = json.Unmarshal(a.Body, &d)
		if len(d.Queries) != 1 || d.Queries[0].SourceVersion != "1.query-1" {
			t.Fatal("candidate lost precise query", string(a.Body))
		}
		corrupt := append([]platform.ReleaseAsset(nil), candidate.Assets...)
		corrupt[i].Requires = nil
		if _, err := platform.Candidate([]platform.AssetRef{a.Ref}, corrupt); err == nil {
			t.Fatal("candidate accepted an omitted query dependency")
		}
	}
	// Neither mutable workflow edits nor a newer query contaminate the saved candidate.
	changed := append([]build.ProcessStep(nil), steps...)
	changed[1].QueryVersion = 99
	submit(build.ProcessType, "P", build.ProcessType+".edit", map[string]any{"steps": changed})
	submit(build.QueryType, "Q", build.QueryType+".edit", map[string]any{"title": "Later notes", "domain": json.RawMessage(`[["bucket","=","B"]]`)})
	submit(build.QueryType, "Q", build.SchemaQuery, struct{}{})
	if _, err := tn.ActivateRelease(builder, saved, "activate-flow", at); err != nil {
		t.Fatal(err)
	}
	submit(build.ProcessType, "P", build.SchemaProcessRun, map[string]string{"key": "waiting"})
	tn.Work(at)
	view, problem := tn.RecordOf(builder, flow.InstanceType, "build.reuse:waiting", at)
	if problem != nil {
		t.Fatal(problem)
	}
	waiting := view.Record.(flow.FlowInstance)
	if waiting.State != "waiting" || waiting.Dependencies == "" || waiting.Release != saved {
		t.Fatalf("flow not retained: %+v", waiting)
	}
	// Advance the installed query again while the old flow is waiting.
	submit(build.QueryType, "Q", build.QueryType+".edit", map[string]any{"title": "Latest notes", "domain": json.RawMessage(`[["bucket","=","B"]]`)})
	submit(build.QueryType, "Q", build.SchemaQuery, struct{}{})
	call, problem := tn.InvokeCapability(reader, platform.CapabilityInvocation{Ref: ref, Version: 1, Inputs: json.RawMessage(`{}`), Key: "old-query"}, at)
	if problem != nil || !strings.Contains(string(call.Result), "FIRST") || strings.Contains(string(call.Result), "SECOND") {
		t.Fatalf("retained query switched: %s %+v", call.Result, problem)
	}
	snapshotBytes, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(snapshotBytes); err != nil {
		t.Fatal(err)
	}
	resumedEntries := append([]Entry(nil), entries...)
	restored.Record = func(e Entry) { resumedEntries = append(resumedEntries, e) }
	restored.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		resumedEntries = append(resumedEntries, e)
		return e.Body, nil
	}
	for second := 61; second < 65; second++ {
		restored.Work(at.Add(time.Duration(second) * time.Second))
	}
	view, problem = restored.RecordOf(builder, flow.InstanceType, "build.reuse:waiting", at.Add(65*time.Second))
	if problem != nil {
		t.Fatal(problem)
	}
	done := view.Record.(flow.FlowInstance)
	if done.State != "done" || done.Dependencies != waiting.Dependencies || !strings.Contains(string(done.Outputs["read"]), "FIRST") || strings.Contains(string(done.Outputs["read"]), "SECOND") {
		t.Fatalf("restored flow changed query: %+v", done)
	}
	CheckReplay(t, restored, resumedEntries, compose)
	CheckReplay(t, tn, entries, compose)
	// Hidden predicates are not a discoverable or callable query port.
	for second := 61; second < 65; second++ {
		tn.Work(at.Add(time.Duration(second) * time.Second))
	}
	submit(build.ObjectType, "O", build.ObjectType+".edit", map[string]any{"fields": []build.Field{{Name: "label", Title: "Label", Type: "text"}, {Name: "bucket", Title: "Bucket", Type: "text", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "O", build.SchemaPublish, struct{}{})
	if _, err := tn.DescribeCapability(reader, ref, 1); err == nil {
		t.Fatal("hidden predicate disclosed retained ports")
	}
	if _, err := tn.InvokeCapability(reader, platform.CapabilityInvocation{Ref: ref, Version: 1, Inputs: json.RawMessage(`{}`), Key: "hidden-query"}, at); err == nil {
		t.Fatal("hidden predicate query was invoked")
	}
	submit(build.ObjectType, "C", build.ObjectType+".create", map[string]any{"name": "child", "title": "Child", "fields": []build.Field{{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.note"}}})
	submit(build.ObjectType, "C", build.SchemaPublish, struct{}{})
	submit(build.QueryType, "CHILD", build.QueryType+".create", map[string]any{"name": "children", "title": "Children", "description": "Children of one note", "object": "build.child", "by": "parent", "limit": 50})
	submit(build.QueryType, "CHILD", build.SchemaQuery, struct{}{})
	for _, inputs := range []map[string]platform.Binding{nil, {"for": {Source: "literal", Value: json.RawMessage(`3`)}}} {
		p := build.Process{Name: "inputprobe", Title: "Input probe", Manual: true, Steps: []build.ProcessStep{{Name: "read", Kind: "query", App: build.ID, Query: "children", QueryVersion: 1, Inputs: inputs}}}
		if owner.CheckProcess(p) == nil {
			t.Fatal("compiler accepted a missing or wrongly typed for input")
		}
	}
	childRef := platform.AssetRef{App: build.ID, Kind: platform.AssetQuery, Name: "children"}
	if _, problem := tn.InvokeCapability(builder, platform.CapabilityInvocation{Ref: childRef, Version: 1, Inputs: json.RawMessage(`{"for":3}`), Key: "typed-query"}, at); problem == nil {
		t.Fatal("runtime accepted a numeric record reference")
	}
}
