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

func TestApplicationRunsPreserveBindingsAndOriginalReadPermissions(t *testing.T) {
	const id = "application-runs"
	compose := func() (*Tenant, error) {
		stockApp := &computeStock{stock: newStock(id)}
		return NewTenant(id, NewConsole(id,
			Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, PlatformApp: Admin, flow.ID: flow.Admin, AgentApp: AgentAdmin, "stock": "clerk", "shop": "clerk", "desk": "clerk"}}},
			Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}},
			Seat{Subjects: []string{"stranger"}, Member: platform.Member{ID: "stranger"}}), build.New(id), work.New(id), flow.New(id), NewAgents(id), stockApp, newShop(id, platform.Flow{Name: "hold", Title: "Original hold", Version: 7, Start: platform.Start{On: []string{"shop.order.place"}, Begin: func(_ platform.Caller, e platform.Event) (string, any, bool) {
			return e.Record.GetSubmission().GetTarget().GetId(), map[string]any{}, true
		}}, Steps: []platform.Step{{Name: "wait", Wait: &platform.Wait{On: "shop.order.ship", Match: func(_ platform.Caller, r *platform.Run, e platform.Event) bool {
			return e.Record.GetSubmission().GetTarget().GetId() == r.Key
		}}}}}), newDesk(id))
	}
	tn, err := compose()
	if err != nil {
		t.Fatal(err)
	}
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	stranger, _ := tn.Member("stranger")
	entries := []Entry{}
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	key := 0
	submit := func(authority, typ, target, verb string, payload any) {
		t.Helper()
		key++
		_, err := tn.Submit(builder, &pb.Submission{TenantId: id, PrincipalId: builder.ID, Authority: authority, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: target}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, now)
		if err != nil {
			t.Fatal(typ, verb, err)
		}
	}
	submit(build.ID, build.ObjectType, "object", "create", map[string]any{"name": "note", "title": "Note", "access": []build.Access{{Role: build.User, Read: "all"}}, "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}})
	submit(build.ID, build.ObjectType, "object", "publish", map[string]any{})
	submit(build.ID, build.PageType, "page", "create", map[string]any{"name": "notes", "title": "Notes", "object": "build.note", "sections": []build.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"note"}}}})
	submit(build.ID, build.PageType, "page", "publish", map[string]any{})
	submit(build.ID, build.ProcessType, "hold", "create", map[string]any{"name": "hold", "title": "Original hold", "manual": true, "steps": []build.ProcessStep{{Name: "wait", Kind: "wait", UntilSeconds: 86400}}})
	submit(build.ID, build.ProcessType, "hold", "publish", map[string]any{})
	resource := platform.AssetRef{App: "build", Kind: platform.AssetFlow, Name: "build.hold"}
	compute := platform.AssetRef{App: "stock", Kind: platform.AssetCompute, Name: "double"}
	for _, name := range []string{"first", "shared"} {
		submit(build.ID, build.AppType, name, "create", map[string]any{"name": name, "title": name, "pages": []string{"notes"}, "resources": []platform.AssetRef{resource, compute}})
		submit(build.ID, build.AppType, name, "publish", map[string]any{})
	}
	appRef := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: "first"}
	candidate, err := tn.ReleaseCandidate([]platform.AssetRef{appRef})
	if err != nil {
		t.Fatal(err)
	}
	raw := candidate.Bytes
	tn.releaseCandidates = map[string]json.RawMessage{candidate.ID: raw}
	tn.activeRelease = candidate.ID
	submit(build.ID, build.ProcessType, "hold", "run", map[string]string{"key": "order"})
	tn.Work(now.Add(time.Second))
	call, refusal := tn.InvokeOperation(builder, platform.OperationRequest{App: "stock", Name: "double", Key: "compute-original", Inputs: json.RawMessage(`{"value":4}`)}, now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	for _, dispatch := range tn.operationDispatches(now.Add(2 * time.Second)) {
		dispatch()
	}
	result, refusal := tn.ReadOperation(builder, call.ID)
	if refusal != nil {
		t.Fatal(refusal)
	}
	if result.Definition != call.Definition || result.Release != candidate.ID || result.OwnerVersion != "1" {
		t.Fatalf("compute binding=%+v, call=%+v", result, call)
	}
	submit(build.ID, "build.note", "N1", "create", map[string]string{"note": "original source"})
	// The start is now an immutable accepted row, without running a model.
	submit(AgentApp, RunType, "run", "start", map[string]string{"agent": "desk.triage", "goal": "Review note", "ref": "build.note/N1"})
	// Changing the current app must not hide runs bound to its older closure.
	submit(build.ID, build.AppType, "first", "edit", map[string]any{"resources": []platform.AssetRef{}})
	submit(build.ID, build.AppType, "first", "publish", map[string]any{})
	page, refusal := tn.ApplicationRuns(builder, appRef, 0, 50, now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	kinds := map[string]ApplicationRun{}
	for _, run := range page.Runs {
		kinds[run.Kind] = run
	}
	if kinds["flow"].Version != "1" || kinds["flow"].Release != candidate.ID || kinds["compute"].Version != "code:1" || kinds["compute"].Release != candidate.ID {
		t.Fatalf("original bindings missing: %+v", page)
	}
	if kinds["agent"].Version == "" || kinds["agent"].Release != candidate.ID {
		t.Fatalf("agent startup binding missing: %+v", page)
	}
	if !strings.Contains(strings.Join(kinds["compute"].Shared, ","), "shared") {
		t.Fatalf("shared resource undisclosed: %+v", page)
	}
	restricted, refusal := tn.ApplicationRuns(reader, appRef, 0, 50, now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	if restricted.Total != 0 {
		t.Fatalf("another person's runtime leaked: %+v", restricted)
	}
	if _, refusal := tn.ApplicationRuns(stranger, appRef, 0, 50, now); refusal == nil {
		t.Fatal("inaccessible app was exposed")
	}
	// No current-registry definition is substituted during accepted start replay.
	var start Entry
	kind := ""
	for _, entry := range entries {
		var envelope struct {
			Kind string
			Row  acceptedRow
			Rows []acceptedRow
		}
		_ = json.Unmarshal(entry.Body, &envelope)
		matched := envelope.Row.Type == RunType && envelope.Row.ID == "run"
		for _, row := range envelope.Rows {
			matched = matched || row.Type == RunType && row.ID == "run"
		}
		if matched {
			start, kind = entry, envelope.Kind
		}
	}
	if len(start.Body) == 0 {
		t.Fatal("agent start was not persisted as an accepted result")
	}
	fresh, err := compose()
	if err != nil {
		t.Fatal(err)
	}
	fresh.app(AgentApp).(*Agents).defs["desk.triage"].Instructions = "Changed after the original start"
	if kind == "record-batch" {
		if _, err := fresh.applyAcceptedBatch(fresh.app(AgentApp).(platform.ResultApp).AcceptedLedger(), start.Body); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := fresh.applyAcceptedResult(fresh.app(AgentApp).(platform.ResultApp).AcceptedLedger(), start.Body); err != nil {
			t.Fatal(err)
		}
	}
	saved, ok := fresh.Held("agent.run/run")
	if !ok {
		t.Fatal("captured start missing after restore")
	}
	restored := saved.(AgentRunRecord)
	if restored.DefinitionVersion != kinds["agent"].Version || restored.Release != candidate.ID {
		t.Fatalf("startup metadata was reinterpreted: %+v", restored)
	}

}
