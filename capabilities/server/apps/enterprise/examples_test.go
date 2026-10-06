package enterprise

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func TestApplyExamplePreservesExistingModelAndNamespacesCopies(t *testing.T) {
	seed := platform.OrgSeed{Units: []platform.Unit{{ID: "company", Name: "Existing enterprise", From: "2020-01-01"}}, Calendars: []platform.Calendar{{ID: "calendar", Name: "Original calendar"}}}
	e := New("examples", seed)
	before := e.Model()
	previews, err := ModelExamples()
	if err != nil || len(previews) != 4 {
		t.Fatalf("examples: %v", err)
	}
	caller := platform.As(ID, platform.Member{ID: "admin", Tenant: "examples", Roles: map[string]string{ID: Admin}})
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	submission := func(prefix, key, parent string) *pb.Submission {
		raw, _ := json.Marshal(map[string]string{"example": "hotel-v1", "name": "My hotel", "parent": parent})
		return &pb.Submission{TenantId: "examples", PrincipalId: "admin", Authority: ID, IdempotencyKey: key, Target: &pb.EntityRef{Type: ModelType, Id: prefix}, Schema: &pb.SchemaRef{Name: SchemaApplyExample, Version: 1}, Payload: raw}
	}
	sub := submission("copy-one", "apply-one", "company")
	first, refusal := e.Submit(caller, sub, now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	got := e.Model()
	if !reflect.DeepEqual(got.Elements[:len(before.Elements)], before.Elements) || !reflect.DeepEqual(got.Calendars, before.Calendars) {
		t.Fatal("applying an example changed existing data")
	}
	if len(got.Elements) <= len(before.Elements) || len(got.Views) == 0 {
		t.Fatal("example did not add an editable graph and views")
	}
	ids := map[string]bool{}
	kinds := map[string]bool{}
	for _, el := range got.Elements {
		if ids[el.ID] {
			t.Fatal("duplicate element")
		}
		ids[el.ID] = true
	}
	for _, k := range got.Kinds {
		kinds[k.ID] = true
	}
	attached := false
	for _, r := range got.Relationships {
		if !ids[r.Source] || !ids[r.Target] || r.Kind != "" && !kinds[r.Kind] {
			t.Fatalf("dangling reference: %+v", r)
		}
		attached = attached || r.Target == "company" && r.ID == "copy-one-parent"
	}
	if !attached {
		t.Fatal("example was not attached to the chosen enterprise")
	}
	visibleParent := false
	for _, id := range got.Views[0].Elements {
		visibleParent = visibleParent || id == "company"
	}
	if !visibleParent {
		t.Fatal("the applied view hides its chosen parent")
	}
	for _, v := range got.Views {
		for _, id := range v.Elements {
			if !ids[id] {
				t.Fatal("view references an unrenamed element")
			}
		}
	}
	retry, refusal := e.Submit(caller, sub, now.Add(time.Hour))
	if refusal != nil || retry.GetChangeId() != first.GetChangeId() || !reflect.DeepEqual(got, e.Model()) {
		t.Fatal("retry duplicated the example")
	}
	if _, err := e.Submit(caller, submission("copy-one", "another-key", "company"), now); err == nil || !reflect.DeepEqual(got, e.Model()) {
		t.Fatal("namespace reuse replaced data")
	}
	if _, err := e.Submit(caller, submission("bad-parent", "bad-parent", "missing"), now); err == nil || !reflect.DeepEqual(got, e.Model()) {
		t.Fatal("invalid parent partially applied a template")
	}
	if _, err := e.Submit(platform.As(ID, platform.Member{ID: "admin", Tenant: "examples"}), submission("denied", "denied", "company"), now); err == nil || !reflect.DeepEqual(got, e.Model()) {
		t.Fatal("unauthorized member applied an example")
	}
	if _, err := e.Submit(caller, submission("copy-two", "apply-two", ""), now); err != nil {
		t.Fatal(err)
	}
	second := e.Model()
	if len(second.Elements) != len(got.Elements)+len(previews[1].Model.Elements) {
		t.Fatal("a second copy was not independent")
	}
	snapshot, err := e.Snapshot()
	restored := New("examples", platform.OrgSeed{})
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, restored.Model()) {
		t.Fatal("restore lost applied examples")
	}
}
