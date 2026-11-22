package host

import (
	"context"
	"encoding/json"
	"platformserver/kernel/protocol"

	"slices"
	"strings"
	"testing"

	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK9PackageRetireAndReinstallPreserveData(t *testing.T) {
	source := newInvariantTenant(t, "package-lifecycle-source")
	defaultView := json.RawMessage(`{"columns":["note"],"layout":"table","version":1}`)
	object := invariantObject("alpha", "Alpha")
	object["view"] = defaultView
	source.accept(t, "build.object.create", build.ObjectType, "OBJ1", object)
	source.accept(t, "build.app.create", protocol.AppType, "APP1", map[string]any{
		"name": "alphaapp", "title": "Alpha application",
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.alpha"}},
	})
	candidateID := invariantPublishData(t, source, source.member(t), []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "OBJ1"},
		{Kind: platform.AssetApp, ID: "APP1"},
	}, "package-lifecycle")
	candidate, err := PublicationResolver().ReadCandidate(candidateID, source.releases.Raw(candidateID))
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

	seat := protocol.Seat{Subjects: []string{"package-admin"}, Member: platform.Member{
		ID: "package-admin", Roles: map[string]string{"platform": "admin"},
	}}
	target := newInvariantTenant(t, "package-lifecycle-target", seat)
	admin, _ := target.Member("package-admin")
	if admin.Holds(build.ID, build.Builder) {
		t.Fatal("the installation administrator must not need a builder role")
	}
	inspected, err := target.inspectPackage(context.Background(), protocol.PackageInspectRequest{Content: platform.Raw(descriptor)})
	if err != nil {
		t.Fatal(err)
	}
	submit := func(schema string, payload any) {
		t.Helper()
		submission := target.submission(schema, protocol.PackageType, descriptor.ID, payload)
		submission.Authority, submission.PrincipalId = "platform", admin.ID
		if _, refusal := target.Submit(admin, submission, invariantClock); refusal != nil {
			t.Fatalf("%s: %#v", schema, refusal)
		}
	}
	submit(packages.SchemaPackageImport, map[string]any{"id": descriptor.ID, "digest": inspected.Digest})
	taken := target.submission(packages.SchemaPackageInstall, protocol.PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version})
	taken.Authority, taken.PrincipalId = "platform", admin.ID
	if _, refusal := target.Submit(admin, taken, invariantClock); refusal == nil || !strings.Contains(refusal.Message, "already exists") {
		t.Fatalf("installing over the tenant's own invariantproject must be refused, got %#v", refusal)
	}
	submit(packages.SchemaPackageInstall, map[string]any{"id": descriptor.ID, "version": descriptor.Version, "namespace": "lifecycle"})
	if info, ok := target.Entity("lifecycle.alpha"); !ok || !slices.Equal(info.View, defaultView) {
		t.Fatalf("package installation lost the default view: %s", info.View)
	}
	views := target.App("platform").(interface{ PackageViews() []protocol.PackageView }).PackageViews()
	var receipt *protocol.PackageView
	for i := range views {
		if views[i].Descriptor.ID == descriptor.ID && views[i].Installed != nil && views[i].Installed.Version == descriptor.Version {
			receipt = &views[i]
		}
	}
	if receipt == nil || len(receipt.Plan.Steps) == 0 || receipt.Plan.Steps[0].Status != "installed" {
		t.Fatalf("install closure receipt: %+v", receipt)
	}
	words := target.Words("")
	if !slices.ContainsFunc(words.Words, func(word transport.Word) bool { return word.Text == "Alpha application" && word.App == build.ID }) {
		t.Fatalf("installed application texts are not available for translation: %+v", words)
	}

	target.accept(t, "lifecycle.alpha.create", "lifecycle.alpha", "A1", map[string]any{"note": "Keep my data"})
	impact, err := target.packageLifecycleImpact(admin, descriptor.ID, nil)
	if err != nil || len(impact.Outputs) != 1 || impact.Outputs[0].Records != 1 || !impact.Outputs[0].Retained {
		t.Fatalf("lifecycle impact must retain customer output: %+v: %v", impact, err)
	}
	objectRef := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "lifecycle.alpha"}
	if resource := resourceRecord(t, target, objectRef); resource.Archived || resource.ManagedBy != descriptor.ID || resource.Project != "lifecycle" {
		t.Fatalf("installed resource is not managed: %+v", resource)
	}

	earlyRetire := target.submission(packages.SchemaPackageRetire, protocol.PackageType, descriptor.ID, map[string]any{"id": descriptor.ID})
	earlyRetire.Authority, earlyRetire.PrincipalId = "platform", admin.ID
	if _, refusal := target.Submit(admin, earlyRetire, invariantClock); refusal == nil {
		t.Fatal("retiring an active package without draining first must be refused")
	}
	submit(packages.SchemaPackageDrain, map[string]any{"id": descriptor.ID})
	submit(packages.SchemaPackageRetire, map[string]any{"id": descriptor.ID})
	if resource := resourceRecord(t, target, objectRef); !resource.Archived {
		t.Fatalf("retirement did not archive the package resource: %+v", resource)
	}
	view, refusal := target.RecordOf(t.Context(), target.member(t), "lifecycle.alpha", "A1", invariantClock)
	if refusal != nil {
		t.Fatalf("read customer record after retirement: %v", refusal)
	}
	var record map[string]any
	if json.Unmarshal(platform.Raw(view.Record), &record) != nil || record["note"] != "Keep my data" {
		t.Fatalf("retirement changed customer data: %#v", record)
	}

	submit(packages.SchemaPackageRemoveSource, map[string]any{"id": descriptor.ID, "digest": inspected.Digest})
	submit(packages.SchemaPackageInstall, map[string]any{"id": descriptor.ID, "version": descriptor.Version})
	if resource := resourceRecord(t, target, objectRef); resource.Archived || resource.ManagedBy != descriptor.ID {
		t.Fatalf("reinstall did not restore the package resource: %+v", resource)
	}
	view, refusal = target.RecordOf(t.Context(), target.member(t), "lifecycle.alpha", "A1", invariantClock)
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
	view, refusal = replayed.RecordOf(t.Context(), replayed.member(t), "lifecycle.alpha", "A1", invariantClock)
	if refusal != nil {
		t.Fatalf("read customer record after replay: %v", refusal)
	}
	record = nil
	if json.Unmarshal(platform.Raw(view.Record), &record) != nil || record["note"] != "Keep my data" {
		t.Fatalf("replay changed customer data: %#v", record)
	}

	next := descriptor
	next.Version = "1.1.0"
	nextOffer, err := target.inspectPackage(context.Background(), protocol.PackageInspectRequest{Content: platform.Raw(next)})
	if err != nil {
		t.Fatal(err)
	}
	submit(packages.SchemaPackageImport, map[string]any{"id": next.ID, "digest": nextOffer.Digest})
	submit(packages.SchemaPackageUpgrade, map[string]any{"id": next.ID, "version": next.Version})
	submit(packages.SchemaPackageDowngrade, map[string]any{"id": next.ID, "version": descriptor.Version})
	installedVersion := func() string {
		for _, view := range target.App("platform").(interface{ PackageViews() []protocol.PackageView }).PackageViews() {
			if view.Descriptor.ID == descriptor.ID && view.Installed != nil {
				return view.Installed.Version
			}
		}
		return ""
	}
	if version := installedVersion(); version != descriptor.Version {
		t.Fatalf("downgrade installed %s, want %s", version, descriptor.Version)
	}
	submit(packages.SchemaPackageRollback, map[string]any{"id": next.ID, "version": next.Version})
	if version := installedVersion(); version != next.Version {
		t.Fatalf("rollback installed %s, want %s", version, next.Version)
	}
}
