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

func TestInvariantK38PackagesAttachTheirOwnFieldsToOnePerson(t *testing.T) {
	const tenant = "invariant-k38"
	people := map[string]platform.Member{}
	var seats []protocol.Seat
	for id, roles := range map[string]map[string]string{
		"hr-1": {build.ID: "hr"}, "sales-1": {build.ID: "sales"}, "org-admin": {enterprisemodel.ID: enterprisemodel.Admin},
	} {
		people[id] = platform.Member{ID: id, Tenant: tenant, Roles: roles}
		seats = append(seats, protocol.Seat{Subjects: []string{id}, Member: people[id]})
	}
	slices.SortFunc(seats, func(a, b protocol.Seat) int { return strings.Compare(a.Member.ID, b.Member.ID) })
	apps := func() []platform.Service {
		return []platform.Service{enterprisemodel.New(tenant, actions.NewAuthority, platform.OrgSeed{Units: []platform.Unit{{ID: "TEAM", Name: "Team", Kind: "team"}}})}
	}
	live := newInvariantTenantWithApps(t, tenant, apps(), seats...)
	as := func(who, schema, authority, typ, id string, payload any) *platform.Error {
		t.Helper()
		sub := live.submission(schema, typ, id, payload)
		sub.PrincipalId, sub.Authority = who, authority
		_, refusal := live.Submit(people[who], sub, invariantClock)
		return refusal
	}
	for _, id := range []string{"P-LI", "P-WU"} {
		if refusal := as("org-admin", enterprisemodel.SchemaElementAdd, enterprisemodel.ID, enterprisemodel.ElementType, id, map[string]any{"stereotype": enterprisemodel.Person, "name": id}); refusal != nil {
			t.Fatal(refusal.Message)
		}
	}
	base := ontology.Field{Name: ontology.BaseField, Title: "Person", Type: "reference", Ref: enterprisemodel.ElementType, Stereotype: enterprisemodel.Person, Required: true}
	facet := func(id, name, role string, field ontology.Field) {
		t.Helper()
		live.accept(t, "build.object.create", build.ObjectType, id, map[string]any{"name": name, "title": name, "extends": enterprisemodel.ElementType,
			"fields": []ontology.Field{base, field}, "access": []ontology.Access{{Role: role, Read: "all", Create: true, Edit: true}}})
		live.accept(t, "build.object.publish", build.ObjectType, id, nil)
	}
	facet("EMPLOYEE", "employee", "hr", ontology.Field{Name: "idnumber", Title: "ID number", Type: "text"})
	facet("CONTACT", "salesprofile", "sales", ontology.Field{Name: "mobile", Title: "Mobile", Type: "text"})
	const employee, profile = "invariantproject.employee", "invariantproject.salesprofile"

	if refusal := as("hr-1", employee+".create", build.ID, employee, "E1", map[string]any{"base": "P-LI", "idnumber": "110101"}); refusal != nil {
		t.Fatalf("hr attaches its fields to a person: %s", refusal.Message)
	}
	if refusal := as("sales-1", profile+".create", build.ID, profile, "S1", map[string]any{"base": "P-LI", "mobile": "138"}); refusal != nil {
		t.Fatalf("sales attaches its own fields to the same person: %s", refusal.Message)
	}
	for name, refusal := range map[string]*platform.Error{
		"a second employee record for one person": as("hr-1", employee+".create", build.ID, employee, "E2", map[string]any{"base": "P-LI"}),
		"an employee record without a person":     as("hr-1", employee+".create", build.ID, employee, "E3", map[string]any{"idnumber": "x"}),
		"an employee record on a team":            as("hr-1", employee+".create", build.ID, employee, "E4", map[string]any{"base": "TEAM"}),
		"moving the record to another person":     as("hr-1", employee+".edit", build.ID, employee, "E1", map[string]any{"base": "P-WU"}),
		"sales creating an employee record":       as("sales-1", employee+".create", build.ID, employee, "E5", map[string]any{"base": "P-WU"}),
	} {
		if refusal == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
	if _, refusal := live.RecordOf(t.Context(), people["sales-1"], employee, "E1", invariantClock); refusal == nil {
		t.Fatal("sales must not read the person's employee fields")
	}
	view, refusal := live.RecordOf(t.Context(), people["hr-1"], employee, "E1", invariantClock)
	if refusal != nil {
		t.Fatal(refusal.Message)
	}
	var record map[string]any
	_ = json.Unmarshal(platform.Raw(view.Record), &record)
	if record["base"] != "P-LI" || record["idnumber"] != "110101" {
		t.Fatalf("the employee record keeps its person: %v", record)
	}
	before := live.recordImage(t)
	stored := make([]Entry, len(live.entries))
	raw, _ := json.Marshal(live.entries)
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	fresh := newInvariantTenantReplayWithApps(t, tenant, apps(), seats...)
	if err := fresh.recoverEntries(stored); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if after := fresh.recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must rebuild both packages' records on the person")
	}
}
