package host

import (
	"context"
	"encoding/json"
	"testing"

	"platformserver/platform"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
)

func TestInvariantPackageRetireAndReinstallPreserveData(t *testing.T) {
	source := newInvariantTenant(t, "package-lifecycle-source")
	source.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
	source.accept(t, "build.app.create", build.AppType, "APP1", map[string]any{
		"name": "alphaapp", "title": "Alpha application",
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "build.alpha"}},
	})
	candidateID := invariantPublishData(t, source, source.member(t), []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "OBJ1"},
		{Kind: platform.AssetApp, ID: "APP1"},
	}, "package-lifecycle")
	candidate, err := platform.ReadCandidate(candidateID, source.releases.Raw(candidateID))
	if err != nil {
		t.Fatal(err)
	}
	descriptor := platform.PackageDescriptor{
		ID: "lifecycle", Version: "1.0.0", Namespace: "lifecycle", Title: "Lifecycle",
		Blueprint: &platform.PackageBlueprint{
			Roots:  []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "alphaapp"}},
			Assets: candidate.Assets,
		},
	}

	seat := platform.Seat{Subjects: []string{"package-admin"}, Member: platform.Member{
		ID: "package-admin", Roles: map[string]string{"platform": "admin", build.ID: build.Builder},
	}}
	target := newInvariantTenant(t, "package-lifecycle-target", seat)
	admin, _ := target.Member("package-admin")
	inspected, err := target.inspectPackage(context.Background(), platform.PackageInspectRequest{Content: platform.Raw(descriptor)})
	if err != nil {
		t.Fatal(err)
	}
	submit := func(schema string, payload any) {
		t.Helper()
		submission := target.submission(schema, platform.PackageType, descriptor.ID, payload)
		submission.Authority, submission.PrincipalId = "platform", admin.ID
		if _, refusal := target.Submit(admin, submission, invariantClock); refusal != nil {
			t.Fatalf("%s: %#v", schema, refusal)
		}
	}
	submit(packages.SchemaPackageImport, map[string]any{"id": descriptor.ID, "digest": inspected.Digest})
	submit(packages.SchemaPackageInstall, map[string]any{"id": descriptor.ID, "version": descriptor.Version})

	target.accept(t, "build.alpha.create", "build.alpha", "A1", map[string]any{"note": "Keep my data"})
	objectRef := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.alpha"}
	if resource := resourceRecord(t, target, objectRef); resource.Archived || resource.ManagedBy != descriptor.ID || resource.Project != "package:"+descriptor.ID {
		t.Fatalf("installed resource is not managed: %+v", resource)
	}

	submit(packages.SchemaPackageDrain, map[string]any{"id": descriptor.ID})
	submit(packages.SchemaPackageRetire, map[string]any{"id": descriptor.ID})
	if resource := resourceRecord(t, target, objectRef); !resource.Archived {
		t.Fatalf("retirement did not archive the package resource: %+v", resource)
	}
	view, refusal := target.RecordOf(target.member(t), "build.alpha", "A1", invariantClock)
	if refusal != nil {
		t.Fatalf("read customer record after retirement: %v", refusal)
	}
	var record map[string]any
	if json.Unmarshal(platform.Raw(view.Record), &record) != nil || record["note"] != "Keep my data" {
		t.Fatalf("retirement changed customer data: %#v", record)
	}

	submit(packages.SchemaPackageInstall, map[string]any{"id": descriptor.ID, "version": descriptor.Version})
	if resource := resourceRecord(t, target, objectRef); resource.Archived || resource.ManagedBy != descriptor.ID {
		t.Fatalf("reinstall did not restore the package resource: %+v", resource)
	}
	view, refusal = target.RecordOf(target.member(t), "build.alpha", "A1", invariantClock)
	if refusal != nil {
		t.Fatalf("read customer record after reinstall: %v", refusal)
	}
	record = nil
	if json.Unmarshal(platform.Raw(view.Record), &record) != nil || record["note"] != "Keep my data" {
		t.Fatalf("reinstall changed customer data: %#v", record)
	}
	appRef := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: "alphaapp"}
	if resource := resourceRecord(t, target, appRef); resource.Archived || resource.ManagedBy != descriptor.ID {
		t.Fatalf("reinstall did not restore the application resource: %+v", resource)
	}
	replayed := target.restore(t, target.entries, seat)
	if resource := resourceRecord(t, replayed, objectRef); resource.Archived || resource.ManagedBy != descriptor.ID {
		t.Fatalf("replay did not preserve the restored package resource: %+v", resource)
	}
	view, refusal = replayed.RecordOf(replayed.member(t), "build.alpha", "A1", invariantClock)
	if refusal != nil {
		t.Fatalf("read customer record after replay: %v", refusal)
	}
	record = nil
	if json.Unmarshal(platform.Raw(view.Record), &record) != nil || record["note"] != "Keep my data" {
		t.Fatalf("replay changed customer data: %#v", record)
	}
}
