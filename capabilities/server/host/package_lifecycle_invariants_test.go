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
	prep, prepErr := target.PackagePreparation(t.Context(), admin, descriptor.ID, descriptor.Version, invariantClock)
	if prepErr != nil || len(prep.Applications) != 1 {
		t.Fatalf("admin without builder role must read package preparation: %+v err=%#v", prep, prepErr)
	}
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

func TestInvariantK9PackageUpgradeFieldReviewAndIncompatibleGate(t *testing.T) {
	publishVersion := func(tag string, fields []map[string]any) platform.PackageDescriptor {
		t.Helper()
		source := newInvariantTenant(t, "pkg-upg-src-"+tag)
		object := invariantObject("lead", "Lead")
		object["fields"] = fields
		source.accept(t, "build.object.create", build.ObjectType, "OBJ1", object)
		source.accept(t, "build.app.create", protocol.AppType, "APP1", map[string]any{
			"name": "leadapp", "title": "Lead application",
			"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.lead"}},
		})
		candidateID := invariantPublishData(t, source, source.member(t), []build.JointDraftRef{
			{Kind: platform.AssetObject, ID: "OBJ1"},
			{Kind: platform.AssetApp, ID: "APP1"},
		}, "pkg-upg-"+tag)
		candidate, err := PublicationResolver().ReadCandidate(candidateID, source.releases.Raw(candidateID))
		if err != nil {
			t.Fatal(err)
		}
		return platform.PackageDescriptor{
			ID: "leadpkg", Version: tag, Namespace: "leadpkg", Title: "Lead package",
			Blueprint: &platform.PackageBlueprint{
				Roots:  []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "leadapp"}},
				Assets: candidate.Assets,
			},
		}
	}

	v1 := publishVersion("1.0.0", []map[string]any{
		{"name": "note", "title": "Note", "type": "text"},
	})
	v2 := publishVersion("1.1.0", []map[string]any{
		{"name": "note", "title": "Note", "type": "text"},
		{"name": "channel", "title": "Channel", "type": "text"},
		{"name": "score", "title": "Score", "type": "integer"},
	})
	v3BadType := publishVersion("1.2.0", []map[string]any{
		{"name": "note", "title": "Note", "type": "integer"},
		{"name": "channel", "title": "Channel", "type": "text"},
		{"name": "score", "title": "Score", "type": "integer"},
	})
	v4BadRemove := publishVersion("1.3.0", []map[string]any{
		{"name": "note", "title": "Note", "type": "text"},
	})

	seat := protocol.Seat{Subjects: []string{"package-admin"}, Member: platform.Member{
		ID: "package-admin", Roles: map[string]string{"platform": "admin"},
	}}
	target := newInvariantTenant(t, "pkg-upg-target", seat)
	admin, _ := target.Member("package-admin")
	submit := func(schema string, payload any) *platform.Error {
		submission := target.submission(schema, protocol.PackageType, v1.ID, payload)
		submission.Authority, submission.PrincipalId = "platform", admin.ID
		_, refusal := target.Submit(admin, submission, invariantClock)
		return refusal
	}
	importDesc := func(desc platform.PackageDescriptor) {
		t.Helper()
		preview, err := target.inspectPackage(context.Background(), protocol.PackageInspectRequest{Content: platform.Raw(desc)})
		if err != nil {
			t.Fatal(err)
		}
		if refusal := submit(packages.SchemaPackageImport, map[string]any{"id": desc.ID, "digest": preview.Digest}); refusal != nil {
			t.Fatalf("import %s: %#v", desc.Version, refusal)
		}
	}
	reviewVersion := func(version string) packages.PackageReview {
		t.Helper()
		catalog := target.App("platform").(interface {
			PackageViews() []protocol.PackageView
			LifecycleImpact(platform.Caller, string, []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error)
		})
		views := catalog.PackageViews()
		chosen := packages.FindView(views, v1.ID, version)
		if chosen == nil {
			t.Fatalf("version %s not found", version)
		}
		c := target.caller(admin, target.App("platform"), false)
		answer, err := packages.Review{
			Resolver:  PublicationResolver(),
			Blueprint: target.builders[build.ID].CompleteBlueprintAssets,
			Impact: func(id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
				return catalog.LifecycleImpact(c, id, after)
			},
		}.Of(views, chosen, false)
		if err != nil {
			t.Fatal(err)
		}
		return answer
	}

	importDesc(v1)
	if refusal := submit(packages.SchemaPackageInstall, map[string]any{"id": v1.ID, "version": v1.Version, "namespace": "leadpkg"}); refusal != nil {
		t.Fatalf("install v1: %#v", refusal)
	}
	target.accept(t, "leadpkg.lead.create", "leadpkg.lead", "L1", map[string]any{"note": "Keep lead"})

	importDesc(v2)
	rev2 := reviewVersion("1.1.0")
	if len(rev2.Problems) != 0 {
		t.Fatalf("compatible upgrade review had problems: %+v", rev2.Problems)
	}
	var addedFields []string
	for _, change := range rev2.Changes {
		for _, field := range change.Fields {
			if field.Change == "added" {
				addedFields = append(addedFields, field.Name)
			}
		}
	}
	if !slices.Contains(addedFields, "channel") || !slices.Contains(addedFields, "score") {
		t.Fatalf("upgrade review missing added fields: %+v", rev2.Changes)
	}
	if refusal := submit(packages.SchemaPackageUpgrade, map[string]any{"id": v2.ID, "version": v2.Version}); refusal != nil {
		t.Fatalf("upgrade to v2 with two optional fields failed: %#v", refusal)
	}
	view, refusal := target.RecordOf(t.Context(), target.member(t), "leadpkg.lead", "L1", invariantClock)
	if refusal != nil {
		t.Fatalf("read record after v2 upgrade: %v", refusal)
	}
	var rec map[string]any
	if json.Unmarshal(platform.Raw(view.Record), &rec) != nil || rec["note"] != "Keep lead" {
		t.Fatalf("upgrade lost existing record value: %#v", rec)
	}

	importDesc(v3BadType)
	rev3 := reviewVersion("1.2.0")
	if len(rev3.Problems) == 0 || !strings.Contains(strings.Join(rev3.Problems, ";"), "Changing field") {
		t.Fatalf("type-change upgrade must be refused in review: %+v", rev3.Problems)
	}
	if refusal := submit(packages.SchemaPackageUpgrade, map[string]any{"id": v3BadType.ID, "version": v3BadType.Version}); refusal == nil {
		t.Fatal("type-change upgrade must be refused on submit")
	}

	importDesc(v4BadRemove)
	rev4 := reviewVersion("1.3.0")
	if len(rev4.Problems) == 0 || !strings.Contains(strings.Join(rev4.Problems, ";"), "Removing field") {
		t.Fatalf("field-removal upgrade must be refused in review: %+v", rev4.Problems)
	}
	if refusal := submit(packages.SchemaPackageUpgrade, map[string]any{"id": v4BadRemove.ID, "version": v4BadRemove.Version}); refusal == nil {
		t.Fatal("field-removal upgrade must be refused on submit")
	}

	rawV1 := platform.Raw(v1)
	if _, err := target.inspectPackage(context.Background(), protocol.PackageInspectRequest{Content: rawV1, ExpectedSha256: "sha256:0000000000000000000000000000000000000000000000000000000000000000"}); err == nil {
		t.Fatal("mismatched expectedSha256 must be refused")
	}
	checked, err := target.inspectPackage(context.Background(), protocol.PackageInspectRequest{Content: rawV1})
	if err != nil || !strings.HasPrefix(checked.ArtifactSha256, "sha256:") {
		t.Fatalf("inspect must return artifactSha256: %+v err=%v", checked, err)
	}
	if _, err := target.inspectPackage(context.Background(), protocol.PackageInspectRequest{Content: rawV1, ExpectedSha256: checked.ArtifactSha256}); err != nil {
		t.Fatalf("matching expectedSha256 must succeed: %v", err)
	}
}
