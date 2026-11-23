package host

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	enterprisemodel "platformserver/services/security/organization"
)

func TestInvariantK43FacetsOnPeopleAreReadThroughTheSignedInPersonAndCarryTheirName(t *testing.T) {
	const tenant, typ = "invariant-k43", "invariantproject.profile"
	seed := platform.OrgSeed{
		Units: []platform.Unit{{ID: "TEAM", Name: "Team", Kind: "team"}, {ID: "OTHER", Name: "Other", Kind: "team"}},
		Memberships: []platform.Membership{
			{Party: "member:lead-1", Unit: "TEAM", Role: platform.ManagerRole},
			{Party: "member:staff-1", Unit: "TEAM", Role: "member"},
			{Party: "member:outsider-1", Unit: "OTHER", Role: "member"},
		},
	}
	people := map[string]platform.Member{}
	var seats []protocol.Seat
	for id, roles := range map[string]map[string]string{
		"staff-1": {build.ID: "staff"}, "outsider-1": {build.ID: "staff"}, "lead-1": {build.ID: "lead"}, "hr-1": {build.ID: "hr"},
		"org-admin": {enterprisemodel.ID: enterprisemodel.Admin},
	} {
		people[id] = platform.Member{ID: id, Tenant: tenant, Roles: roles}
		seats = append(seats, protocol.Seat{Subjects: []string{id}, Member: people[id]})
	}
	slices.SortFunc(seats, func(a, b protocol.Seat) int { return strings.Compare(a.Member.ID, b.Member.ID) })
	apps := func() []platform.Service {
		return []platform.Service{enterprisemodel.New(tenant, actions.NewAuthority, seed)}
	}
	live := newInvariantTenantWithApps(t, tenant, apps(), seats...)
	as := func(who, schema, authority, target, id string, payload any) {
		t.Helper()
		sub := live.submission(schema, target, id, payload)
		sub.PrincipalId, sub.Authority = who, authority
		if _, refusal := live.Submit(people[who], sub, invariantClock); refusal != nil {
			t.Fatalf("%s %s/%s: %s", schema, target, id, refusal.Message)
		}
	}
	for id, name := range map[string]string{"P-A": "Anna Li", "P-B": "Bo Wang", "P-C": "Chen Yu"} {
		as("org-admin", enterprisemodel.SchemaElementAdd, enterprisemodel.ID, enterprisemodel.ElementType, id, map[string]any{"stereotype": enterprisemodel.Person, "name": name})
	}
	for id, link := range map[string][2]string{"R-A": {"member:staff-1", "P-A"}, "R-C": {"member:outsider-1", "P-C"}} {
		as("org-admin", enterprisemodel.SchemaRelationshipAdd, enterprisemodel.ID, enterprisemodel.RelationshipType, id, map[string]any{"stereotype": enterprisemodel.SignsInAs, "source": link[0], "target": link[1]})
	}
	name := &platform.Expr{Op: "path", Kind: "text", Args: []platform.Expr{{Kind: "reference", Ref: ontology.BaseField}}, Path: []string{"name"}}
	live.accept(t, "build.object.create", build.ObjectType, "PROFILE", map[string]any{"name": "profile", "title": "Profile", "extends": enterprisemodel.ElementType,
		"scope": map[string]any{"owner": ontology.BaseField},
		"fields": []ontology.Field{
			{Name: ontology.BaseField, Title: "Person", Type: "reference", Ref: enterprisemodel.ElementType, Stereotype: enterprisemodel.Person, Required: true},
			{Name: "fullname", Title: "Full name", Type: "text", From: name},
			{Name: "note", Title: "Note", Type: "text"},
		},
		"access": []ontology.Access{{Role: "staff", Read: "own"}, {Role: "lead", Read: "team"}, {Role: "hr", Read: "all", Create: true, Edit: true}}})
	live.accept(t, "build.object.publish", build.ObjectType, "PROFILE", nil)
	for id, person := range map[string]string{"PR-A": "P-A", "PR-B": "P-B", "PR-C": "P-C"} {
		as("hr-1", typ+".create", build.ID, typ, id, map[string]any{ontology.BaseField: person, "note": id})
	}

	read := func(tenant *invariantTenant, who, id string) (map[string]any, bool) {
		t.Helper()
		view, refusal := tenant.RecordOf(t.Context(), people[who], typ, id, invariantClock)
		if refusal != nil {
			return nil, false
		}
		var record map[string]any
		_ = json.Unmarshal(platform.Raw(view.Record), &record)
		return record, true
	}
	if record, ok := read(live, "staff-1", "PR-A"); !ok || record["fullname"] != "Anna Li" {
		t.Fatalf("a member reads the facet on the person they sign in as, named after the person: %v", record)
	}
	for who, id := range map[string]string{"staff-1": "PR-B", "outsider-1": "PR-A", "lead-1": "PR-C"} {
		if _, ok := read(live, who, id); ok {
			t.Fatalf("%s must not read %s", who, id)
		}
	}
	if _, ok := read(live, "lead-1", "PR-A"); !ok {
		t.Fatal("a manager reads the facets on the people their team members sign in as")
	}
	before := live.recordImage(t)
	fresh := newInvariantTenantReplayWithApps(t, tenant, apps(), seats...)
	stored := make([]Entry, len(live.entries))
	raw, _ := json.Marshal(live.entries)
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if err := fresh.recoverEntries(stored); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if after := fresh.recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must rebuild the facets with their carried names")
	}
	if _, ok := read(fresh, "staff-1", "PR-A"); !ok {
		t.Fatal("after replay the member still reads their own facet")
	}
}
