package host

import (
	"encoding/json"
	"reflect"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	enterprisemodel "platformserver/services/security/organization"
)

func TestInvariantK34WorkdaysCountTheMembersCalendar(t *testing.T) {
	const tenant, typ = "invariant-k34", "invariantproject.absence"
	seed := platform.OrgSeed{
		Calendars: []platform.Calendar{{ID: "office", Name: "Office", Workdays: []int{1, 2, 3, 4, 5}, Holidays: []string{"2026-11-25"}},
			{ID: "plant", Name: "Plant", Workdays: []int{1, 2, 3, 4, 5, 6}}},
		Units: []platform.Unit{{ID: "TEAM", Name: "Team", Kind: "team", Calendar: "office"}, {ID: "PLANT", Name: "Plant", Kind: "team", Calendar: "plant"}},
		Memberships: []platform.Membership{{Party: "member:builder", Unit: "TEAM", Role: "member", Primary: true},
			{Party: "member:other", Unit: "PLANT", Role: "member", Primary: true}},
	}
	other := platform.Member{ID: "other", Tenant: tenant, Roles: map[string]string{build.ID: ontology.BuilderRole}}
	seat := protocol.Seat{Subjects: []string{other.ID}, Member: other}
	live := newInvariantTenantWithApps(t, tenant, []platform.Service{enterprisemodel.New(tenant, actions.NewAuthority, seed)}, seat)
	date := func(name string) map[string]any { return map[string]any{"op": "", "kind": "date", "ref": name} }
	live.accept(t, "build.object.create", build.ObjectType, "ABSENCE", map[string]any{"name": "absence", "title": "Absence",
		"fields": []map[string]any{{"name": "fromdate", "title": "From", "type": "date", "required": true}, {"name": "todate", "title": "To", "type": "date", "required": true},
			{"name": "days", "title": "Days", "type": "integer", "formulaExpr": map[string]any{"op": "workdays", "kind": "integer", "args": []any{date("fromdate"), date("todate")}}}}})
	live.accept(t, "build.object.publish", build.ObjectType, "ABSENCE", nil)

	days := func(who platform.Member, id, from, to string) any {
		t.Helper()
		sub := live.submission(typ+".create", typ, id, map[string]any{"fromdate": from, "todate": to})
		sub.PrincipalId = who.ID
		if _, refusal := live.Submit(who, sub, invariantClock); refusal != nil {
			t.Fatalf("%s %s: %s", who.ID, id, refusal.Message)
		}
		view, refusal := live.RecordOf(t.Context(), who, typ, id, invariantClock)
		if refusal != nil {
			t.Fatal(refusal.Message)
		}
		var rec map[string]any
		_ = json.Unmarshal(platform.Raw(view.Record), &rec)
		return rec["days"]
	}
	builder := live.member(t)
	if got := days(builder, "A1", "2026-11-23", "2026-11-29"); got != float64(4) {
		t.Fatalf("a week in the office calendar has four working days around its holiday, got %v", got)
	}
	if got := days(builder, "A2", "2026-11-28", "2026-11-29"); got != float64(0) {
		t.Fatalf("a weekend has no working days, got %v", got)
	}
	if got := days(other, "A3", "2026-11-23", "2026-11-29"); got != float64(6) {
		t.Fatalf("a member of the plant counts its Monday-to-Saturday week without the office holiday, got %v", got)
	}
	if got := days(builder, "A4", "2026-11-26", "2026-11-25"); got != float64(0) {
		t.Fatalf("an end before the start counts no working days, got %v", got)
	}
	before := live.recordImage(t)
	if after := live.restore(t, live.entries, seat).recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must read the counted days back without the calendar")
	}
}
