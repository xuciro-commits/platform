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
)

func TestInvariantK44EveryMemberUsesMasterDataWithoutARoleInIt(t *testing.T) {
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
	install("../../../scripts/frontend-fixtures/geo-0.1.1.json")
	install("../../../scripts/frontend-fixtures/party-0.2.1.json")
	install("../../../deploy/packages/production-provider.json")

	member, _ := live.Member("rep")
	if !slices.Contains(member.RolesIn("party.organization"), "user") {
		t.Fatalf("a member with a role only in crm uses master data as user, got %v", member.RolesIn("party.organization"))
	}
	if slices.Contains(member.RolesIn("build.batchproviderorder"), "user") {
		t.Fatal("an application package's user role is still granted only to its own members")
	}
	sub := live.submission("party.organization.create", "party.organization", "ORG-K44", map[string]any{"name": "Xihe Precision"})
	sub.Authority, sub.PrincipalId = build.ID, member.ID
	if _, refusal := live.Submit(member, sub, invariantClock); refusal != nil {
		t.Fatalf("the member creates a business partner while converting a lead: %s", refusal.Message)
	}
	if _, refusal := live.RecordOf(t.Context(), member, "party.organization", "ORG-K44", invariantClock); refusal != nil {
		t.Fatalf("the member reads it back: %s", refusal.Message)
	}
	archive := live.submission("party.organization.archive", "party.organization", "ORG-K44", map[string]any{})
	archive.Authority, archive.PrincipalId = build.ID, member.ID
	if _, refusal := live.Submit(member, archive, invariantClock); refusal == nil {
		t.Fatal("archiving stays with the steward")
	}
}
