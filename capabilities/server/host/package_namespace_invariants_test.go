package host

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	appauthoring "platformserver/services/apps/authoring"
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

func TestInvariantK36SnapshotRestorePreservesNamespacedPackagePagesAndExtensionApps(t *testing.T) {
	baseSource := newInvariantTenant(t, "k36-snap-base")
	baseSource.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("account", "Account"))
	tmpl := appauthoring.ListDetailPage("account", "Accounts", "", platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.account"}, []string{"note"}, []string{"note"}, nil)
	baseSource.accept(t, "build.page.create", protocol.PageType, "PAGE1", map[string]any{
		"format": tmpl.Format, "name": "account", "title": "Accounts", "object": "invariantproject.account",
		"dataBindings": tmpl.DataBindings, "actionTriggers": tmpl.ActionTriggers, "scopeVisibility": tmpl.ScopeVisibility, "widgetSpec": tmpl.WidgetSpec,
	})
	baseSource.accept(t, "build.app.create", protocol.AppType, "APP1", map[string]any{
		"name": "crm", "title": "CRM", "pages": []string{"account"},
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.account"}},
	})
	baseCandidateID := invariantPublishData(t, baseSource, baseSource.member(t), []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "OBJ1"},
		{Kind: platform.AssetPage, ID: "PAGE1"},
		{Kind: platform.AssetApp, ID: "APP1"},
	}, "k36-snap-base")
	baseCandidate, err := PublicationResolver().ReadCandidate(baseCandidateID, baseSource.releases.Raw(baseCandidateID))
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range baseCandidate.Assets {
		if a.Ref.Kind == platform.AssetPage && a.Ref.Name == "account" {
			var body map[string]any
			if err := json.Unmarshal(a.Body, &body); err != nil {
				t.Fatal(err)
			}
			body["name"] = "crm.account"
			baseCandidate.Assets[i].Ref.Name = "crm.account"
			baseCandidate.Assets[i].Body = platform.Raw(body)
		}
		if a.Ref.Kind == platform.AssetApp && a.Ref.Name == "crm" {
			var body map[string]any
			if err := json.Unmarshal(a.Body, &body); err != nil {
				t.Fatal(err)
			}
			body["pages"] = []string{"crm.account"}
			for r, req := range a.Requires {
				if req.Kind == platform.AssetPage && req.Name == "account" {
					baseCandidate.Assets[i].Requires[r].Name = "crm.account"
				}
			}
			baseCandidate.Assets[i].Body = platform.Raw(body)
		}
	}
	baseDesc := platform.PackageDescriptor{
		ID: "crm", Version: "1.0.0", Namespace: "crm", Title: "CRM",
		Blueprint: &platform.PackageBlueprint{
			Roots:  []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "crm"}},
			Assets: baseCandidate.Assets,
		},
	}

	extSource := newInvariantTenant(t, "k36-snap-ext")
	extSource.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("leadext", "Lead extension"))
	extSource.accept(t, "build.app.create", protocol.AppType, "APP1", map[string]any{
		"name": "leadext", "title": "Lead extension",
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.leadext"}},
	})
	extCandidateID := invariantPublishData(t, extSource, extSource.member(t), []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "OBJ1"},
		{Kind: platform.AssetApp, ID: "APP1"},
	}, "k36-snap-ext")
	extCandidate, err := PublicationResolver().ReadCandidate(extCandidateID, extSource.releases.Raw(extCandidateID))
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range extCandidate.Assets {
		if a.Ref.Kind == platform.AssetApp && a.Ref.Name == "leadext" {
			var body map[string]any
			if err := json.Unmarshal(a.Body, &body); err != nil {
				t.Fatal(err)
			}
			body["name"] = "crmext.leadext"
			extCandidate.Assets[i].Ref.Name = "crmext.leadext"
			extCandidate.Assets[i].Body = platform.Raw(body)
		}
	}
	extDesc := platform.PackageDescriptor{
		ID: "crmext", Version: "0.1.0", Namespace: "crmext", Title: "CRM Extension", Requires: []string{"crm"},
		Blueprint: &platform.PackageBlueprint{
			Roots:  []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "crmext.leadext"}},
			Assets: extCandidate.Assets,
		},
	}

	seat := protocol.Seat{Subjects: []string{"package-admin"}, Member: platform.Member{
		ID: "package-admin", Roles: map[string]string{"platform": "admin"},
	}}
	live := newInvariantTenant(t, "k36-snapshot-restore", seat)
	admin, _ := live.Member("package-admin")
	for _, desc := range []platform.PackageDescriptor{baseDesc, extDesc} {
		inspected, err := live.InspectPackage(context.Background(), protocol.PackageInspectRequest{Content: platform.Raw(desc)})
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range []struct {
			schema  string
			payload map[string]any
		}{
			{packages.SchemaPackageImport, map[string]any{"id": desc.ID, "digest": inspected.Digest}},
			{packages.SchemaPackageInstall, map[string]any{"id": desc.ID, "version": desc.Version, "namespace": desc.Namespace}},
		} {
			submission := live.submission(step.schema, protocol.PackageType, desc.ID, step.payload)
			submission.Authority, submission.PrincipalId = "platform", admin.ID
			if _, refusal := live.Submit(admin, submission, invariantClock); refusal != nil {
				t.Fatalf("%s %s: %s", desc.ID, step.schema, refusal.Message)
			}
		}
	}

	snap, seq, err := live.Snapshot(func() int64 { return int64(len(live.entries)) })
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	fresh := newInvariantTenantSetup(t, live.ID, nil)
	if err := fresh.recoverSnapshot(snap, seq); err != nil {
		t.Fatalf("recoverSnapshot failed: %v", err)
	}
	if fresh.quarantined() {
		t.Fatalf("recoverSnapshot quarantined tenant: %s", fresh.Fault.Load().Reason)
	}
	if !reflect.DeepEqual(live.recordImage(t), fresh.recordImage(t)) {
		t.Fatal("restored snapshot record image differs from live tenant")
	}
}
