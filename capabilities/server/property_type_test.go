package platformserver

import (
	"errors"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestPropertyVersionObjectBindingFreezesAndRecovers(t *testing.T) {
	const tenant = "shared-property-state"
	compose := func() *Tenant {
		tn, err := NewTenant(tenant, NewConsole(tenant, Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New(tenant))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Now().UTC()
	var entries []Entry
	failAppend := false
	key := 0
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if failAppend {
			return nil, errors.New("append failed")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	submit := func(m platform.Member, typ, id, verb string, payload any) *kernel.Error {
		key++
		_, err := tn.Submit(m, &pb.Submission{TenantId: tenant, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		return err
	}
	must := func(typ, id, verb string, payload any) {
		t.Helper()
		if err := submit(builder, typ, id, verb, payload); err != nil {
			t.Fatal(typ, verb, err.Message)
		}
	}
	must(build.PropertyTypeType, "qty", "create", map[string]any{"name": "quantity", "title": "Quantity", "description": "Shared measured quantity", "type": "integer"})

	propPreview, problem := tn.PreviewRelease(builder, platform.AssetPropertyType, "qty")
	if problem != nil || propPreview.Diagnostic != "" {
		t.Fatal(propPreview, problem)
	}
	if _, problem = tn.PreviewRelease(reader, platform.AssetPropertyType, "qty"); problem == nil {
		t.Fatal("reader inspected a private draft")
	}
	if _, problem = tn.SaveReleaseCandidate(builder, platform.AssetPropertyType, "qty", propPreview.CandidateID, "save-property", at); problem != nil {
		t.Fatal(problem)
	}
	must(build.PropertyTypeType, "qty", "edit", map[string]any{"title": "Later property draft"})
	if _, problem = tn.ActivateRelease(builder, propPreview.CandidateID, "activate-property", at); problem != nil {
		t.Fatal(problem)
	}

	binding := platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetPropertyType, Name: "quantity"}, SourceVersion: "1.property-1"}
	fields := []build.Field{{Name: "planned", Title: "Quantity", Type: "integer", Property: &binding, Required: true}}
	must(build.ObjectType, "object", "create", map[string]any{"name": "job", "title": "Job", "fields": fields})
	preview, err := tn.PreviewRelease(builder, platform.AssetObject, "object")
	if err != nil || preview.Diagnostic != "" {
		t.Fatal(preview, err)
	}
	pinned := false
	for _, ref := range preview.Included {
		if ref == binding.Ref {
			pinned = true
		}
	}
	if !pinned {
		t.Fatal("property not frozen as an object dependency")
	}
	if _, err = tn.SaveReleaseCandidate(builder, platform.AssetObject, "object", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	must(build.PropertyTypeType, "qty", "edit", map[string]any{"title": "New quantity"})
	must(build.PropertyTypeType, "qty", "publish", map[string]any{})
	must(build.ObjectType, "object", "edit", map[string]any{"title": "Later object draft"})
	if _, err = tn.ActivateRelease(builder, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
	}
	must("build.job", "J1", "create", map[string]any{"planned": 3})
	for _, invalid := range []build.Field{{Name: "planned", Title: "Quantity", Type: "decimal", Property: &binding}, {Name: "planned", Title: "Local override", Type: "integer", Property: &binding}} {
		if submit(builder, build.ObjectType, "wrong"+invalid.Type, "create", map[string]any{"name": "wrong" + invalid.Type, "title": "Wrong", "fields": []build.Field{invalid}}) == nil {
			t.Fatal("incompatible property override accepted")
		}
	}
	if submit(reader, build.PropertyTypeType, "forbidden", "create", map[string]any{"name": "forbidden", "title": "Hidden", "description": "Hidden", "type": "text"}) == nil {
		t.Fatal("ordinary reader authored a property")
	}

	must(build.PropertyTypeType, "secret", "create", map[string]any{"name": "secret", "title": "Secret", "description": "Private scalar meaning", "type": "text"})
	must(build.PropertyTypeType, "secret", "publish", map[string]any{})
	secretBinding := platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetPropertyType, Name: "secret"}, SourceVersion: "1.property-1"}
	must(build.ObjectType, "privateobject", "create", map[string]any{"name": "privatejob", "title": "Private job", "fields": []build.Field{{Name: "label", Title: "Label", Type: "text"}, {Name: "code", Title: "Secret", Type: "text", Property: &secretBinding, Read: []string{build.Builder}}}})
	must(build.ObjectType, "privateobject", "publish", map[string]any{})
	for _, d := range tn.Definitions(reader) {
		if d.Ref.Kind == platform.AssetPropertyType && d.Ref.Name == "secret" {
			t.Fatal("unused property discovered by member")
		}
	}
	missing := binding
	missing.SourceVersion = "1.property-99"
	if submit(builder, build.ObjectType, "missing", "create", map[string]any{"name": "missing", "title": "Missing", "fields": []build.Field{{Name: "value", Title: "Quantity", Type: "integer", Property: &missing}}}) == nil {
		t.Fatal("missing property version accepted")
	}
	if submit(builder, build.PropertyTypeType, "qty", "edit", map[string]any{"type": "decimal"}) == nil {
		if submit(builder, build.PropertyTypeType, "qty", "publish", map[string]any{}) == nil {
			t.Fatal("published scalar type changed")
		}
	}
	must(build.PropertyTypeType, "secret", "edit", map[string]any{"title": "Later secret"})
	failAppend = true
	if submit(builder, build.PropertyTypeType, "secret", "publish", map[string]any{}) == nil {
		t.Fatal("failed append accepted")
	}
	failAppend = false
	for _, d := range tn.Definitions(builder) {
		if d.Ref.Kind == platform.AssetPropertyType && d.Ref.Name == "secret" && d.Version != "1.property-1" {
			t.Fatal("failed append installed property")
		}
	}
	CheckReplay(t, tn, entries, compose)
	raw, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err = restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	for _, current := range []*Tenant{tn, restored} {
		m, _ := current.Member("reader")
		seen := false
		for _, d := range current.Definitions(m) {
			if d.Entity != nil && d.Ref.Name == "build.job" {
				f, ok := d.Entity.Field("planned")
				if !ok || f.Title != "Quantity" || f.Property == nil || *f.Property != binding {
					t.Fatal("field binding drifted")
				}
				seen = true
			}
			if d.Ref == binding.Ref && (d.PropertyType != nil || d.PropertyVersions[binding.SourceVersion].Title != "Quantity") {
				t.Fatal("unconsumed latest version leaked or retained meaning missing")
			}
		}
		if !seen {
			t.Fatal("bound object missing")
		}
		view, problem := current.RecordOf(m, "build.job", "J1", at)
		if problem != nil || view.Record == nil {
			t.Fatal("original object reader unavailable", problem)
		}
	}
}
