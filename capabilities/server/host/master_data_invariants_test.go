package host

import (
	"slices"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	"platformserver/services/security/control"
)

func TestInvariantK44MasterDataRequiresExplicitGrantAndRevokes(t *testing.T) {
	source := newInvariantTenant(t, "invariant-k44-source")
	org := invariantObject("organization", "Organization")
	org["display"] = "name"
	org["fields"] = []ontology.Field{{Name: "name", Title: "Name", Type: "text", Required: true}}
	org["access"] = []ontology.Access{
		{Role: "user", Read: "all", Create: true, Edit: true},
		{Role: "steward", Read: "all", Create: true, Edit: true, Archive: true},
	}
	source.accept(t, "build.object.create", build.ObjectType, "ORG", org)
	source.accept(t, "build.app.create", protocol.AppType, "PARTYAPP", map[string]any{
		"name":      "partyapp",
		"title":     "Business partners",
		"surface":   "master-data",
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.organization"}},
	})
	cid := invariantPublishData(t, source, source.member(t), []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "ORG"},
		{Kind: platform.AssetApp, ID: "PARTYAPP"},
	}, "k44-party")
	candidate, err := PublicationResolver().ReadCandidate(cid, source.releases.Raw(cid))
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := packages.Rebind(platform.PackageDescriptor{
		ID: "party", Version: "0.2.2", Namespace: "party", Title: "Business partners",
		Blueprint: &platform.PackageBlueprint{
			Roots:  []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "partyapp"}},
			Assets: candidate.Assets,
		},
	}, "party")
	if err != nil {
		t.Fatal(err)
	}

	admin := protocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin", build.ID: ontology.BuilderRole}}}
	rep := protocol.Seat{Subjects: []string{"rep"}, Member: platform.Member{ID: "rep", Roles: map[string]string{"crm": "salesrep"}}}
	live := newInvariantTenant(t, "invariant-k44", admin, rep)
	adminMember, _ := live.Member("admin")
	preview, err := live.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(descriptor)})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		schema  string
		payload map[string]any
	}{
		{packages.SchemaPackageImport, map[string]any{"id": descriptor.ID, "digest": preview.Digest}},
		{packages.SchemaPackageInstall, map[string]any{"id": descriptor.ID, "version": descriptor.Version}},
	} {
		sub := live.submission(step.schema, protocol.PackageType, descriptor.ID, step.payload)
		sub.Authority, sub.PrincipalId = "platform", adminMember.ID
		if _, refusal := live.Submit(adminMember, sub, invariantClock); refusal != nil {
			t.Fatalf("%s %s: %s", step.schema, descriptor.ID, refusal.Message)
		}
	}

	member, _ := live.Member("rep")
	if roles := member.RolesIn("party.organization"); len(roles) != 0 {
		t.Fatalf("installing master data must not inject any implicit role, got %v", roles)
	}
	create := live.submission("party.organization.create", "party.organization", "ORG-K44", map[string]any{"name": "Xihe Precision"})
	create.Authority, create.PrincipalId = build.ID, member.ID
	if _, refusal := live.Submit(member, create, invariantClock); refusal == nil {
		t.Fatal("a member without an explicit party role must not create a business partner")
	}

	grant := live.submission(control.SchemaGrant, control.MemberType, "rep", map[string]any{"app": "party", "role": "user"})
	grant.Authority, grant.PrincipalId = "platform", adminMember.ID
	if _, refusal := live.Submit(adminMember, grant, invariantClock); refusal != nil {
		t.Fatalf("grant party user: %s", refusal.Message)
	}

	member, _ = live.Member("rep")
	if !slices.Contains(member.RolesIn("party.organization"), "user") {
		t.Fatalf("expected explicit party user role, got %v", member.RolesIn("party.organization"))
	}
	create2 := live.submission("party.organization.create", "party.organization", "ORG-K44", map[string]any{"name": "Xihe Precision"})
	create2.Authority, create2.PrincipalId = build.ID, member.ID
	if _, refusal := live.Submit(member, create2, invariantClock); refusal != nil {
		t.Fatalf("explicitly granted member creates a business partner: %s", refusal.Message)
	}
	if _, refusal := live.RecordOf(t.Context(), member, "party.organization", "ORG-K44", invariantClock); refusal != nil {
		t.Fatalf("explicitly granted member reads it back: %s", refusal.Message)
	}
	archive := live.submission("party.organization.archive", "party.organization", "ORG-K44", map[string]any{})
	archive.Authority, archive.PrincipalId = build.ID, member.ID
	if _, refusal := live.Submit(member, archive, invariantClock); refusal == nil {
		t.Fatal("archiving stays with the steward")
	}

	revoke := live.submission(control.SchemaRevoke, control.MemberType, "rep", map[string]any{"app": "party", "role": "user"})
	revoke.Authority, revoke.PrincipalId = "platform", adminMember.ID
	if _, refusal := live.Submit(adminMember, revoke, invariantClock); refusal != nil {
		t.Fatalf("revoke party user: %s", refusal.Message)
	}

	member, _ = live.Member("rep")
	if roles := member.RolesIn("party.organization"); len(roles) != 0 {
		t.Fatalf("revoking party user must remove access immediately, got %v", roles)
	}
	if _, refusal := live.RecordOf(t.Context(), member, "party.organization", "ORG-K44", invariantClock); refusal == nil {
		t.Fatal("revoked member must not read the business partner")
	}
}
