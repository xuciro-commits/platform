package host

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK36PackagesWithTheSameBareObjectNameInstallSideBySide(t *testing.T) {
	candidates := map[string][]platform.ReleaseAsset{}
	for _, id := range []string{"sales", "support"} {
		source := newInvariantTenant(t, "k36-"+id)
		source.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("contact", "Contact"))
		source.accept(t, "build.app.create", protocol.AppType, "APP1", map[string]any{
			"name": id + "app", "title": id,
			"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.contact"}},
		})
		candidateID := invariantPublishData(t, source, source.member(t), []build.JointDraftRef{{Kind: platform.AssetObject, ID: "OBJ1"}, {Kind: platform.AssetApp, ID: "APP1"}}, "k36-"+id)
		candidate, err := PublicationResolver().ReadCandidate(candidateID, source.releases.Raw(candidateID))
		if err != nil {
			t.Fatal(err)
		}
		candidates[id] = candidate.Assets
	}
	seat := protocol.Seat{Subjects: []string{"package-admin"}, Member: platform.Member{ID: "package-admin", Roles: map[string]string{"platform": "admin"}}}
	target := newInvariantTenant(t, "k36-target", seat)
	admin, _ := target.Member("package-admin")
	install := func(id string) {
		t.Helper()
		descriptor := platform.PackageDescriptor{ID: id, Version: "1.0.0", Namespace: id, Title: id,
			Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: id + "app"}}, Assets: candidates[id]}}
		inspected, err := target.InspectPackage(context.Background(), protocol.PackageInspectRequest{Content: platform.Raw(descriptor)})
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range []struct {
			schema  string
			payload map[string]any
		}{{packages.SchemaPackageImport, map[string]any{"id": id, "digest": inspected.Digest}}, {packages.SchemaPackageInstall, map[string]any{"id": id, "version": "1.0.0", "namespace": id}}} {
			submission := target.submission(step.schema, protocol.PackageType, id, step.payload)
			submission.Authority, submission.PrincipalId = "platform", admin.ID
			if _, refusal := target.Submit(admin, submission, invariantClock); refusal != nil {
				t.Fatalf("%s %s: %s", id, step.schema, refusal.Message)
			}
		}
	}
	install("sales")
	install("support")
	drafts := map[string]bool{}
	for _, id := range []string{"sales", "support"} {
		if _, ok := target.Entity(id + ".contact"); !ok {
			t.Fatalf("%s.contact was not installed", id)
		}
		resource := resourceRecord(t, target, platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: id + ".contact"})
		if resource.ManagedBy != id || resource.Draft == "" {
			t.Fatalf("%s.contact resource: %+v", id, resource)
		}
		drafts[resource.Draft] = true
	}
	if len(drafts) != 2 {
		t.Fatalf("both packages share one object draft: %v", drafts)
	}
	target.accept(t, "support.contact.create", "support.contact", "C1", map[string]any{"note": "Kept apart"})
	if _, ok := target.held("sales.contact/C1"); ok {
		t.Fatal("a record of one package leaked into the other")
	}
	before := target.recordImage(t)
	if after := target.restore(t, target.entries, seat).recordImage(t); !reflect.DeepEqual(before, after) {
		raw, _ := json.Marshal(drafts)
		t.Fatalf("replay must install both packages again with drafts %s", raw)
	}
}
