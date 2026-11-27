package host

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK9PackageUpgradeRefusesIncompatibleExtensions(t *testing.T) {
	seat := protocol.Seat{Subjects: []string{"package-admin"}, Member: platform.Member{ID: "package-admin", Roles: map[string]string{"platform": "admin"}}}
	source := newInvariantTenant(t, "extgate-base")
	source.accept(t, "build.object.create", build.ObjectType, "account", invariantObject("account", "account"))
	source.accept(t, "build.object.create", build.ObjectType, "lead", invariantObject("lead", "lead"))
	source.accept(t, "build.app.create", protocol.AppType, "APP", map[string]any{"name": "baseapp", "title": "Base CRM"})
	appRev := 1
	objRevs := map[string]int{"account": 1, "lead": 1}
	publishBase := func(version string, lead bool, added bool) platform.PackageDescriptor {
		t.Helper()
		var drafts []build.JointDraftRef
		var refs []platform.AssetRef
		fields := []ontology.Field{{Name: "note", Title: "Note", Type: "text"}}
		if added {
			fields = append(fields, ontology.Field{Name: "channel", Title: "Channel", Type: "text"})
		}
		for _, name := range []string{"account", "lead"} {
			if name == "lead" && !lead {
				continue
			}
			rec := source.accept(t, "build.object.edit", build.ObjectType, name, map[string]any{"fields": fields})
			objRevs[name] = int(rec.GetRevision())
			drafts = append(drafts, build.JointDraftRef{Kind: platform.AssetObject, ID: name})
			refs = append(refs, platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject." + name})
		}
		rec := source.accept(t, "build.app.edit", protocol.AppType, "APP", map[string]any{"resources": refs})
		appRev = int(rec.GetRevision())
		_ = appRev
		drafts = append(drafts, build.JointDraftRef{Kind: platform.AssetApp, ID: "APP"})
		id := invariantPublishData(t, source, source.member(t), drafts, "base-"+version)
		candidate, err := PublicationResolver().ReadCandidate(id, source.releases.Raw(id))
		if err != nil {
			t.Fatal(err)
		}
		kept := slices.DeleteFunc(slices.Clone(candidate.Assets), func(a platform.ReleaseAsset) bool {
			return !lead && a.Ref.Name == "invariantproject.lead"
		})
		desc := platform.PackageDescriptor{ID: "crm", Version: version, Namespace: "crm", Title: "CRM", Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "baseapp"}}, Assets: kept}}
		desc, err = packages.Rebind(desc, "crm")
		if err != nil {
			t.Fatal(err)
		}
		return desc
	}
	submit := func(live *invariantTenant, schema, id string, payload any) *platform.Error {
		t.Helper()
		admin, _ := live.Member("package-admin")
		req := live.submission(schema, protocol.PackageType, id, payload)
		req.Authority, req.PrincipalId = "platform", admin.ID
		_, refusal := live.Submit(admin, req, invariantClock)
		return refusal
	}
	importDesc := func(live *invariantTenant, desc platform.PackageDescriptor) {
		t.Helper()
		offer, err := live.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(desc)})
		if err != nil {
			t.Fatal(err)
		}
		if refusal := submit(live, packages.SchemaPackageImport, desc.ID, map[string]any{"id": desc.ID, "digest": offer.Digest}); refusal != nil {
			t.Fatalf("import %s@%s: %s", desc.ID, desc.Version, refusal.Message)
		}
	}
	install := func(live *invariantTenant, desc platform.PackageDescriptor) {
		t.Helper()
		importDesc(live, desc)
		if refusal := submit(live, packages.SchemaPackageInstall, desc.ID, map[string]any{"id": desc.ID, "version": desc.Version, "namespace": desc.Namespace}); refusal != nil {
			t.Fatalf("install %s@%s: %s", desc.ID, desc.Version, refusal.Message)
		}
	}
	v1 := publishBase("1.0.0", true, false)
	v2 := publishBase("1.1.0", true, true)
	removed := publishBase("1.2.0", false, true)
	incompatible := publishBase("2.0.0", true, true)

	extSource := newInvariantTenant(t, "extgate-source", seat)
	install(extSource, v1)
	ext := invariantObject("extra", "Customer extension")
	ext["extends"] = "crm.lead"
	ext["fields"] = []ontology.Field{{Name: "base", Title: "Lead", Type: "reference", Ref: "crm.lead", Required: true}, {Name: "customerfield", Title: "Customer field", Type: "text"}}
	extSource.accept(t, "build.object.create", build.ObjectType, "EXTRA", ext)
	extSource.accept(t, "build.app.create", protocol.AppType, "EXTAPP", map[string]any{"name": "customerapp", "title": "Customer extension", "resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.extra"}}})
	id := invariantPublishData(t, extSource, extSource.member(t), []build.JointDraftRef{{Kind: platform.AssetObject, ID: "EXTRA"}, {Kind: platform.AssetApp, ID: "EXTAPP"}}, "customer-extension")
	candidate, err := PublicationResolver().ReadCandidate(id, extSource.releases.Raw(id))
	if err != nil {
		t.Fatal(err)
	}
	own := slices.DeleteFunc(slices.Clone(candidate.Assets), func(a platform.ReleaseAsset) bool {
		return a.Ref.Name != "customerapp" && !strings.HasPrefix(a.Ref.Name, "invariantproject.")
	})
	extension := platform.PackageDescriptor{ID: "customer", Version: "1.0.0", Namespace: "customer", Title: "Customer extension", Requires: []string{"crm@^1.0.0"}, Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "customerapp"}}, Assets: own}}
	extension, err = packages.Rebind(extension, "customer")
	if err != nil {
		t.Fatal(err)
	}
	live := newInvariantTenant(t, "extgate-target", seat)
	install(live, v1)
	install(live, extension)
	live.accept(t, "crm.lead.create", "crm.lead", "L1", map[string]any{"note": "Base lead"})
	live.accept(t, "customer.extra.create", "customer.extra", "X1", map[string]any{"base": "L1", "customerfield": "Keep extension"})
	review := func(desc platform.PackageDescriptor) packages.PackageReview {
		t.Helper()
		catalog := live.App("platform").(interface {
			PackageViews() []protocol.PackageView
			LifecycleImpact(platform.Caller, string, []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error)
		})
		views := catalog.PackageViews()
		chosen := packages.FindView(views, desc.ID, desc.Version)
		if chosen == nil {
			t.Fatal("missing candidate")
		}
		admin, _ := live.Member("package-admin")
		c := live.caller(admin, live.App("platform"), false)
		r, err := (packages.Review{Resolver: PublicationResolver(), Blueprint: live.builders[build.ID].CompleteBlueprintAssets, Impact: func(id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
			return catalog.LifecycleImpact(c, id, after)
		}}).Of(views, chosen, false)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	checkExtension := func(live *invariantTenant, value string) {
		t.Helper()
		view, refusal := live.RecordOf(t.Context(), live.member(t), "customer.extra", "X1", invariantClock)
		if refusal != nil || !strings.Contains(string(platform.Raw(view.Record)), value) {
			t.Fatalf("extension lost: %+v %v", view, refusal)
		}
		if info, exists := live.Entity("customer.extra"); !exists || info.Extends != "crm.lead" {
			t.Fatalf("extension definition lost: %+v", info)
		}
	}
	importDesc(live, v2)
	r := review(v2)
	if len(r.Extensions) != 1 || r.Extensions[0].Issue != "" || r.Extensions[0].VersionRange != "^1.0.0" {
		t.Fatalf("compatible review: %+v", r.Extensions)
	}
	if refusal := submit(live, packages.SchemaPackageUpgrade, v2.ID, map[string]any{"id": v2.ID, "version": v2.Version}); refusal != nil {
		t.Fatalf("compatible upgrade: %s", refusal.Message)
	}
	checkExtension(live, "Keep extension")
	live.accept(t, "customer.extra.edit", "customer.extra", "X1", map[string]any{"customerfield": "Action still works"})
	for _, test := range []struct {
		desc   platform.PackageDescriptor
		reason string
	}{{removed, "removes its base"}, {incompatible, "does not satisfy this range"}} {
		importDesc(live, test.desc)
		r := review(test.desc)
		if len(r.Extensions) != 1 || !strings.Contains(r.Extensions[0].Issue, test.reason) || !strings.Contains(r.Extensions[0].Issue, "customer.extra") {
			t.Fatalf("review %s: %+v", test.desc.Version, r.Extensions)
		}
		before := live.recordImage(t)
		position := len(live.entries)
		refusal := submit(live, packages.SchemaPackageUpgrade, test.desc.ID, map[string]any{"id": test.desc.ID, "version": test.desc.Version})
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || !strings.Contains(refusal.Message, r.Extensions[0].Issue) {
			t.Fatalf("expected named refusal matching review: %+v", refusal)
		}
		if !reflect.DeepEqual(before, live.recordImage(t)) {
			t.Fatalf("refusal changed records: %v", differingTypes(before, live.recordImage(t)))
		}
		if len(live.entries) != position+1 {
			t.Fatalf("expected only sealed refusal, entries %d -> %d", position, len(live.entries))
		}
		live.sealedRefusal(t, position)
		if live.quarantined() {
			t.Fatal("tenant quarantined")
		}
		checkExtension(live, "Action still works")
		live.accept(t, "crm.lead.edit", "crm.lead", "L1", map[string]any{"note": "Writable " + test.desc.Version})
	}
	if diff := differingTypes(live.recordImage(t), live.restore(t, live.entries, seat).recordImage(t)); len(diff) != 0 {
		t.Fatalf("replay differs: %v", diff)
	}
}
