package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/enterprise"
	"platformserver/platform"
)

// ADR-0078 §2 / ADR-0079 §4, the closing block: tenant settings that bind
// (sign-in domains seat strangers, session hours end sign-ins, a second
// factor is demanded), a unit-bound grant that narrows what is read,
// offboarding that hands over and ends the tenure.
func TestTenancyBlock(t *testing.T) {
	seat := func(id string, roles map[string]string) Seat {
		return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: roles}}
	}
	org := enterprise.New("t-1", platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Name: "Site", Kind: "site"}},
		Units:       []platform.Unit{{ID: "plant", Kind: "plant"}, {ID: "L1", Kind: "line"}, {ID: "L2", Kind: "line"}},
		Edges:       []platform.Edge{{Structure: "site", Unit: "L1", Parent: "plant"}, {Structure: "site", Unit: "L2", Parent: "plant"}},
		Memberships: []platform.Membership{{Party: "member:lead", Unit: "plant", Role: "lead"}, {Party: "member:boss", Unit: "plant", Role: "lead"}}})
	tn, err := NewTenant("t-1", NewConsole("t-1", seat("ana", map[string]string{PlatformApp: Admin, "stock": "clerk"}), seat("bo", map[string]string{"stock": "clerk"}),
		seat("lead", map[string]string{"stock": "lead"}), seat("boss", map[string]string{"stock": "lead"})), org, newStock("t-1"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewHost(Tokens(map[string]string{"ana-token": "ana", "bo-token": "bo", "new-token": "user:new@acme.test", "out-token": "user:x@other.test"}), tn)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	h.Now = func() time.Time { return now }
	d := consoleOf(tn)
	seq := 0
	submit := func(who platform.Member, app, schema, typ, id string, payload any) string {
		t.Helper()
		seq++
		raw, _ := json.Marshal(payload)
		_, err := tn.Submit(who, &pb.Submission{TenantId: "t-1", PrincipalId: who.ID, Authority: app, IdempotencyKey: fmt.Sprint("tb", seq),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	member := func(id string) platform.Member { m, _ := tn.member(id); return m }
	whoIs := func(token string) (platform.Member, bool) {
		r := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		m, _, ok := h.member(r)
		return m, ok
	}
	ana := member("ana")
	set := func(name, value string) {
		t.Helper()
		if got := submit(ana, PlatformApp, SchemaSettingSet, SettingType, PlatformApp+"/"+name, map[string]string{"value": value}); got != "ok" {
			t.Fatal(got)
		}
	}

	// Sign-in domains: a stranger of the domain is seated on first sign-in, holding nothing; others stay out.
	if _, ok := whoIs("new-token"); ok {
		t.Fatal("a stranger was admitted before any domain was listed")
	}
	set(SettingDomains, "Acme.test, partner.example")
	joined, ok := whoIs("new-token")
	if !ok || joined.ID != "new" || len(joined.RolesIn("stock")) != 0 || joined.Status != "" {
		t.Fatalf("self-join: %+v %v", joined, ok)
	}
	if again, ok := whoIs("new-token"); !ok || again.ID != "new" {
		t.Fatal("the joined member is not recognised on the next request")
	}
	if _, ok := whoIs("out-token"); ok {
		t.Fatal("a stranger of another domain was admitted")
	}
	if d.JoinID("user:new@acme.test") != "new-2" {
		t.Fatal("a taken id is not numbered")
	}

	// Session hours: a sign-in older than the setting is refused until the provider is asked again.
	set(SettingSessionHours, "2")
	if _, ok := whoIs("bo-token"); !ok {
		t.Fatal("bo cannot sign in")
	}
	now = now.Add(3 * time.Hour)
	if _, ok := whoIs("bo-token"); ok {
		t.Fatal("a sign-in past its hours was kept")
	}
	if _, ok := whoIs("bo-token"); ok { // the spent credential stays refused; only a fresh one from the provider signs in
		t.Fatal("a spent credential was admitted again")
	}
	h.authenticate = Tokens(map[string]string{"ana-token": "ana", "bo-token-2": "bo"})
	if _, ok := whoIs("bo-token-2"); !ok {
		t.Fatal("a fresh credential was refused")
	}

	// Second factor: without the provider's word nobody signs in; with it, those attested.
	set(SettingMFA, "true")
	if _, ok := whoIs("bo-token-2"); ok {
		t.Fatal("a sign-in without a second factor was admitted")
	}
	h.SecondFactor = func(credential string) bool { return credential == "bo-token-2" }
	if _, ok := whoIs("bo-token-2"); !ok {
		t.Fatal("an attested sign-in was refused")
	}
	if _, ok := whoIs("ana-token"); ok {
		t.Fatal("an unattested sign-in was admitted")
	}
	set(SettingMFA, "false")

	// A unit-bound grant narrows reading to that unit; held outright, the whole of what one belongs to.
	if got := submit(member("lead"), "stock", "stock.bin.create", "stock.bin", "B1", map[string]any{"code": "A-01"}); got != "ok" {
		t.Fatal(got)
	}
	for i, unit := range []string{"L1", "L2"} {
		if got := submit(member("lead"), "stock", "stock.item.create", "stock.item", fmt.Sprint("U", i), map[string]any{"name": "Part " + unit, "qty": 1, "line": unit, "kind": "part", "bin": "B1"}); got != "ok" {
			t.Fatal(got)
		}
	}
	count := func(id string) int {
		newcomer, _ := d.Member("user:new@acme.test")
		page, err := tn.Records(newcomer, "stock.item", platform.Query{}, now)
		if err != nil {
			t.Fatalf("%s reads: %s", id, err.Error())
		}
		return page.Total
	}
	if got := submit(ana, PlatformApp, SchemaGrant, MemberType, "new", map[string]string{"app": "stock", "role": "lead", "unit": "L2", "structure": "site"}); got != "ok" {
		t.Fatal(got)
	}
	if n := count("new"); n != 1 {
		t.Fatalf("unit-bound grant reads %d items, want the one in L2", n)
	}
	if got := submit(ana, PlatformApp, SchemaGrant, MemberType, "new", map[string]string{"app": "stock", "role": "lead", "unit": "plant", "structure": "site"}); got != "ok" {
		t.Fatal(got)
	}
	if n := count("new"); n != 2 {
		t.Fatalf("a grant on the plant reads %d items, want both lines", n)
	}

	// Offboarding with a successor: delegations pass on, the successor is told, the enterprise tenure ends today.
	if got := submit(member("lead"), PlatformApp, SchemaDelegate, MemberType, "bo", map[string]string{"app": "stock", "until": "2027-01-01"}); got != "ok" {
		t.Fatal(got)
	}
	if got := submit(ana, PlatformApp, SchemaOffboard, MemberType, "lead", map[string]string{"successor": "ghost"}); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("an unknown successor was accepted: %s", got)
	}
	if got := submit(ana, PlatformApp, SchemaOffboard, MemberType, "lead", map[string]string{"successor": "boss", "reason": "left the company"}); got != "ok" {
		t.Fatal(got)
	}
	for _, g := range member("bo").Grants {
		if g.By == "lead" {
			t.Fatalf("delegation still stands in the leaver's name: %+v", g)
		}
	}
	if !strings.Contains(fmt.Sprint(member("bo").Grants), "boss") {
		t.Fatalf("delegation did not pass to the successor: %+v", member("bo").Grants)
	}
	if notes := tn.notificationsFor("boss"); len(notes) == 0 || !strings.Contains(notes[0].Title, "lead") {
		t.Fatalf("the successor was not told: %+v", notes)
	}
	tn.Work(now) // the enterprise model hears of it as owned work
	org = tn.app(enterprise.ID).(*enterprise.Enterprise)
	ended := false
	for _, r := range org.Model().Relationships {
		if r.Source == "member:lead" && r.Stereotype == enterprise.Membership {
			if r.Until != now.UTC().Format(time.DateOnly) {
				t.Fatalf("tenure not ended: %+v", r)
			}
			ended = true
		}
	}
	if !ended {
		t.Fatal("no membership of the leaver found")
	}
	if units := tn.directory.Units("member:lead", "", now.AddDate(0, 0, 1).UTC().Format(time.DateOnly)); len(units) != 0 {
		t.Fatalf("the leaver still belongs tomorrow: %v", units)
	}
}
