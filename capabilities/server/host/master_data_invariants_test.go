package host

import (
	"encoding/json"
	"os"
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
	admin := protocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin", build.ID: ontology.BuilderRole}}}
	rep := protocol.Seat{Subjects: []string{"rep"}, Member: platform.Member{ID: "rep", Roles: map[string]string{"crm": "salesrep"}}}
	live := newInvariantTenant(t, "invariant-k44", admin, rep)
	install := func(path string) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var descriptor platform.PackageDescriptor
		if err := json.Unmarshal(raw, &descriptor); err != nil {
			t.Fatal(err)
		}
		member, _ := live.Member("admin")
		preview, err := live.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: raw})
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
			sub.Authority, sub.PrincipalId = "platform", member.ID
			if _, refusal := live.Submit(member, sub, invariantClock); refusal != nil {
				t.Fatalf("%s %s: %s", step.schema, descriptor.ID, refusal.Message)
			}
			live.Work(invariantClock)
		}
	}
	install(fixturePath(t, "frontend-fixtures/geo-0.1.2.json"))
	install(fixturePath(t, "frontend-fixtures/party-0.2.2.json"))

	member, _ := live.Member("rep")
	if roles := member.RolesIn("party.organization"); len(roles) != 0 {
		t.Fatalf("installing master data must not inject any implicit role, got %v", roles)
	}
	create := live.submission("party.organization.create", "party.organization", "ORG-K44", map[string]any{"name": "Xihe Precision"})
	create.Authority, create.PrincipalId = build.ID, member.ID
	if _, refusal := live.Submit(member, create, invariantClock); refusal == nil {
		t.Fatal("a member without an explicit party role must not create a business partner")
	}

	adminMember, _ := live.Member("admin")
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
