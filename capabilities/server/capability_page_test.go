package platformserver

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestNestedPageFormBindings(t *testing.T) {
	const tenant = "nested-form"
	compose := func() *Tenant {
		tn, err := NewTenant(tenant, NewConsole(tenant, Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"user"}, Member: platform.Member{ID: "user", Roles: map[string]string{build.ID: build.User}}}), build.New(tenant))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	var journal []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { journal = append(journal, e); return e.Body, nil }
	builder, _ := tn.Member("builder")
	user, _ := tn.Member("user")
	at := time.Now().UTC()
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		if _, problem := tn.Submit(builder, &pb.Submission{TenantId: tenant, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: typ + id + verb, Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at); problem != nil {
			t.Fatal(problem.Message)
		}
	}
	for _, object := range []struct {
		name   string
		fields []build.Field
	}{
		{"item", []build.Field{{Name: "size", Title: "Size", Type: "integer", Required: true}}},
		{"root", []build.Field{{Name: "name", Title: "Name", Type: "text", Required: true}}},
		{"line", []build.Field{{Name: "root", Title: "Root", Type: "reference", Ref: "build.root", Inverse: "lines", Required: true}, {Name: "item", Title: "Item", Type: "reference", Ref: "build.item", Required: true}}},
		{"task", []build.Field{{Name: "line", Title: "Line", Type: "reference", Ref: "build.line", Inverse: "tasks", Required: true}, {Name: "size", Title: "Size", Type: "integer", Required: true}}},
	} {
		submit(build.ObjectType, object.name, "create", map[string]any{"name": object.name, "title": object.name, "fields": object.fields})
		submit(build.ObjectType, object.name, "publish", map[string]any{})
	}
	ref := func(name string) platform.AssetRef {
		return platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build." + name}
	}
	page := platform.Page{Name: "nested", Object: ref("root"), Layout: "composed", Selections: []platform.SelectionVariable{{Name: "root", Object: ref("root")}, {Name: "line", Object: ref("line")}, {Name: "task", Object: ref("task")}}, Sections: []platform.Section{
		{Widget: "table", Selection: "root", Fields: []string{"name"}},
		{Widget: "table", Object: ref("line"), Selection: "line", ParentSelection: "root", Relation: "lines", Fields: []string{"item"}},
		{Widget: "table", Object: ref("task"), Selection: "task", ParentSelection: "line", Relation: "tasks", Fields: []string{"size"}},
		{Widget: "form", Object: ref("task"), ParentSelection: "line", Relation: "tasks", Inputs: map[string]platform.Binding{"size": {Source: "subject", Path: []string{"item", "size"}}}},
	}}
	sections := make([]build.Section, len(page.Sections))
	for i, section := range page.Sections {
		sections[i] = build.Section{Widget: section.Widget, Object: section.Object.Name, Selection: section.Selection, ParentSelection: section.ParentSelection, Relation: section.Relation, Fields: section.Fields, Inputs: section.Inputs}
	}
	submit(build.PageType, "P", "create", map[string]any{"name": page.Name, "title": "Nested form", "object": page.Object.Name, "sections": sections, "selections": page.Selections})
	submit(build.PageType, "P", "publish", map[string]any{})
	submit("build.item", "I", "create", map[string]any{"size": 50})
	submit("build.root", "R", "create", map[string]any{"name": "Root"})
	submit("build.line", "L", "create", map[string]any{"root": "R", "item": "I"})
	request := platform.CapabilityInvocation{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: "build.task.create"}, Key: "bound-create", Target: "T", Record: "build.line/L", Inputs: platform.Raw(map[string]any{"line": "L", "size": 999}), Bindings: page.Sections[3].Inputs}
	if _, problem := tn.InvokeCapability(user, request, at); problem != nil {
		t.Fatal(problem.Message)
	}
	row, _ := tn.RecordOf(user, "build.task", "T", at)
	raw, _ := json.Marshal(row.Record)
	var values map[string]any
	_ = json.Unmarshal(raw, &values)
	if values["size"] != float64(50) || values["line"] != "L" {
		t.Fatalf("form did not use the scoped source: %s", raw)
	}
	wrong := page
	wrong.Sections = slices.Clone(page.Sections)
	wrong.Sections[3].Inputs = map[string]platform.Binding{"size": {Source: "subject", Path: []string{"root", "name"}}}
	root, _ := tn.entity("build.root")
	if err := tn.checkSections(wrong, root); err == nil {
		t.Fatal("an incompatible record field was accepted as the required integer")
	}
	CheckReplay(t, tn, journal, compose)
	// Revoke only the leaf field: the parent and referenced record stay readable.
	tn.records.types["build.item"].info.Fields[0].Read = []string{build.Builder}
	request.Key, request.Target = "denied-create", "DENIED"
	if _, problem := tn.InvokeCapability(user, request, at); problem == nil {
		t.Fatal("a hidden leaf supplied a business write")
	}
	for _, definition := range tn.Definitions(user) {
		if definition.Ref.Kind == platform.AssetPage && definition.Ref.Name == page.Name && slices.ContainsFunc(definition.Page.Sections, func(s platform.Section) bool { return s.Widget == "form" }) {
			t.Fatal("the private form binding remained discoverable")
		}
	}
	if _, problem := tn.RecordOf(user, "build.task", "DENIED", at); problem == nil {
		t.Fatal("a refused binding left a task behind")
	}
}

type pageComputeStock struct{ *computeStock }

func (s pageComputeStock) Manifest() platform.Manifest {
	m := s.computeStock.Manifest()
	m.Entities[1].Seed = []any{Item{Record: platform.Record{ID: "I"}, Name: "Source", Qty: 4, Owner: "ana"}}
	return m
}

func TestPageComputeBindingRetainsScopedSources(t *testing.T) {
	app := pageComputeStock{&computeStock{stock: newStock("page-compute")}}
	member := platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}
	tn, err := NewTenant("page-compute", NewConsole("page-compute", Seat{Subjects: []string{"ana"}, Member: member}), app)
	if err != nil {
		t.Fatal(err)
	}
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) { return entry.Body, nil }
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	member, _ = tn.Member("ana")
	if _, refusal := tn.RecordOf(member, "stock.item", "I", now); refusal != nil {
		t.Fatalf("source setup: %s; rows=%v", refusal.Message, tn.records.types["stock.item"].rows)
	}
	ref := platform.AssetRef{App: "stock", Kind: platform.AssetCompute, Name: "double"}
	q := platform.CapabilityInvocation{Ref: ref, Key: "page-call", Inputs: json.RawMessage(`{}`), Record: "stock.item/I", Bindings: map[string]platform.Binding{"value": {Source: "subject", Path: []string{"qty"}}}}
	result, refusal := tn.InvokeCapability(member, q, now)
	if refusal != nil || result.State != "pending" || result.Call == "" || app.calls != 0 {
		t.Fatalf("page call did not create the canonical pending operation: %+v %v", result, refusal)
	}
	for _, execute := range tn.operationDispatches(now) {
		execute()
	}
	answer, refusal := tn.ReadOperation(member, result.Call)
	if refusal != nil || answer.State != "completed" || string(answer.Output) != `{"doubled":8}` || app.calls != 1 {
		t.Fatalf("page result: %+v %v", answer, refusal)
	}
	var retained operationBinding
	for _, effect := range tn.outbound {
		if effect.ID == result.Call {
			_ = json.Unmarshal([]byte(effect.Body), &retained)
		}
	}
	if !slices.Contains(retained.Call.Sources, "stock.item/I") || !slices.Contains(retained.Call.Sources, "stock.item/I#qty") {
		t.Fatal("host did not derive record and field source labels")
	}
	for i := range tn.records.types["stock.item"].info.Fields {
		if tn.records.types["stock.item"].info.Fields[i].Name == "qty" {
			tn.records.types["stock.item"].info.Fields[i].Read = []string{"lead"}
		}
	}
	if _, refusal := tn.ReadOperation(member, result.Call); refusal == nil {
		t.Fatal("stored page result escaped a revoked source field")
	}
	if _, refusal := tn.InvokeCapability(member, platform.CapabilityInvocation{Ref: ref, Key: "private", Inputs: json.RawMessage(`{}`), Record: "stock.item/I", Bindings: map[string]platform.Binding{"value": {Source: "subject", Path: []string{"secret"}}}}, now); refusal == nil {
		t.Fatal("page binding read a private field")
	}
}
