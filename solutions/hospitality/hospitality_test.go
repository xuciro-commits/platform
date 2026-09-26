package hospitality

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"crm"
	"lodging"
	"pms"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

var (
	t0    = time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	seats = []platformserver.Seat{
		{Subjects: []string{"sales"}, Member: platform.Member{ID: "sales-1", Roles: map[string]string{"crm": "sales", "pms": "front-desk", "memstay": lodging.Keeper}}},
		{Subjects: []string{"sales-only"}, Member: platform.Member{ID: "sales-2", Roles: map[string]string{"crm": "sales"}}},
		{Subjects: []string{"desk"}, Member: platform.Member{ID: "desk-1", Roles: map[string]string{"pms": "front-desk", "csm": "desk"}}},
		{Subjects: []string{"manager"}, Member: platform.Member{ID: "manager-1", Roles: map[string]string{"crm": "sales-manager", "pms": "manager", "memstay": lodging.Keeper, "csm": "lead", "knowledge": "editor"}}},
	}
)

type world struct {
	t       *testing.T
	tenant  *platformserver.Tenant
	members map[string]platform.Member
	journal []platformserver.Entry
}

func newWorld(t *testing.T, providers ...func(string) platform.App) *world {
	var apps []platform.App
	for _, p := range providers {
		apps = append(apps, p("hotel-a"))
	}
	tn, err := Compose("hotel-a", apps, seats...)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, tenant: tn, members: map[string]platform.Member{}}
	tn.Record = func(e platformserver.Entry) { w.journal = append(w.journal, e) }
	for _, s := range seats {
		w.members[s.Subjects[0]] = platform.Member{ID: s.ID, Tenant: "hotel-a", Roles: s.Roles}
	}
	w.members["admin"] = platform.Member{ID: "admin-1", Tenant: "hotel-a", Roles: map[string]string{platformserver.PlatformApp: platformserver.Admin}}
	return w
}

func hotelProvider(id string) platform.App {
	return pms.New(id, map[string]pms.RoomType{"suite": {Rooms: 1}})
}

func memoryProvider(id string) platform.App { return lodging.NewMemory(id) }

func (w *world) submit(who, app, schema, targetType, id, key string, payload any) string {
	raw, _ := json.Marshal(payload)
	_, err := w.tenant.Submit(w.members[who], &pb.Submission{TenantId: "hotel-a", PrincipalId: w.members[who].ID, Authority: app,
		Target: &pb.EntityRef{Type: targetType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, Payload: raw}, t0)
	if err != nil {
		return err.Error()
	}
	return "ok"
}

func (w *world) setup() {
	w.expect(w.submit("sales", crm.ID, crm.SchemaAccount, crm.AccountType, "ACME", "a", map[string]string{"name": "Acme Corp", "kind": "company"}), "ok")
	w.expect(w.submit("sales", crm.ID, crm.SchemaOpen, crm.OpportunityType, "OPP-1", "o", map[string]string{"account": "ACME", "title": "Board offsite"}), "ok")
}

func (w *world) book(who, key, roomType, checkIn string) string {
	return w.submit(who, crm.ID, crm.SchemaBook, crm.OpportunityType, "OPP-1", key,
		map[string]string{"roomType": roomType, "checkIn": checkIn, "checkOut": "2026-10-03", "guest": "Acme board"})
}

// reservations are the hotel's reservation records, as the member may read them (ADR-0016).
func (w *world) reservations(who string) []any {
	page, err := w.tenant.Records(w.members[who], pms.ReservationType, platform.Query{Archived: true}, t0)
	if err != nil {
		panic(err)
	}
	return page.Records
}

func (w *world) read(who, name string) any {
	out, err := w.tenant.Read(w.members[who], name)
	if err != nil {
		w.t.Fatalf("read %s as %s: %v", name, who, err)
	}
	return out
}

func (w *world) stays(who string) []lodging.Booking {
	return w.read(who, "customers").([]crm.Customer)[0].Opportunities[0].Stays
}

func (w *world) timeline(who, entity string) []string {
	var out []string
	for _, n := range w.read(who, "timeline").([]platformserver.Note) {
		if n.Entity == entity {
			out = append(out, n.By+": "+n.Text)
		}
	}
	return out
}

func (w *world) expect(got, want string) {
	w.t.Helper()
	if got != want {
		w.t.Fatalf("got %s, want %s", got, want)
	}
}

func TestStaysThroughTheLodgingProtocol(t *testing.T) {
	w := newWorld(t, hotelProvider)
	w.setup()
	w.expect(w.book("sales", "b-1", "suite", "2026-10-01"), "ok")
	w.expect(w.book("sales", "b-1", "suite", "2026-10-01"), "ok") // a resend
	if n := len(w.reservations("manager")); n != 1 {
		t.Fatalf("a resend booked again: %d reservations", n)
	}
	if s := w.stays("sales"); len(s) != 1 || s[0].ID != "OPP-1-B1" || s[0].Guest != "Acme board" {
		t.Fatalf("customer view: %+v", s)
	}
	// The provider decides: the only suite is taken, so nothing is linked.
	w.expect(w.book("sales", "b-2", "suite", "2026-10-02"), "ERROR_CODE_CONFLICT")
	// Each app checks its own role: no hotel role, or no CRM role, is refused.
	w.expect(w.book("sales-only", "b-3", "suite", "2026-10-05"), "ERROR_CODE_POLICY_DENIED")
	w.expect(w.book("desk", "b-4", "suite", "2026-10-05"), "ERROR_CODE_POLICY_DENIED")
	// A member who may not see hotel data sees the opportunity without its stays.
	if s := w.stays("sales-only"); len(s) != 0 {
		t.Fatalf("sales-only sees stays %+v", s)
	}
	// The hotel cancels on its own; the platform timeline tells the opportunity, as
	// the protocol's event, through the link — no app in between.
	w.expect(w.submit("manager", pms.ID, pms.SchemaCancel, pms.ReservationType, "OPP-1-B1", "c-1", struct{}{}), "ok")
	if !w.stays("sales")[0].Canceled {
		t.Fatal("the cancellation does not show on the customer")
	}
	told := w.timeline("sales", "crm.opportunity/OPP-1")
	if !slices.Equal(told, []string{"app:pms: Booking canceled (pms.reservation/OPP-1-B1, by manager-1)"}) {
		t.Fatalf("opportunity timeline %v", told)
	}
	w.expect(w.submit("sales", crm.ID, crm.SchemaClose, crm.OpportunityType, "OPP-1", "l-1", map[string]string{"outcome": "lost"}), "ok")
	w.expect(w.book("sales", "b-5", "suite", "2026-10-06"), "ERROR_CODE_CONFLICT")

	// One journal replays the tenant: the reservation and link the booking caused,
	// and the timeline its cancellation caused, although only inputs were recorded.
	again := newWorld(t, hotelProvider)
	if err := again.tenant.Replay(w.journal); err != nil {
		t.Fatal(err)
	}
	view := func(w *world) string {
		return fmt.Sprint(w.read("manager", "customers"), w.reservations("manager"), w.read("manager", "links"), w.timeline("sales", "crm.opportunity/OPP-1"))
	}
	if view(again) != view(w) {
		t.Fatalf("replayed tenant differs:\n%s\n%s", view(w), view(again))
	}
}

// ADR-0011: another provider of the protocol plugs in with no change to the CRM.
func TestAnyLodgingProviderServesTheCRM(t *testing.T) {
	w := newWorld(t, memoryProvider)
	w.setup()
	w.expect(w.book("sales", "b-1", "loft", "2026-10-01"), "ok")
	if s := w.stays("sales"); len(s) != 1 || s[0].RoomType != "loft" {
		t.Fatalf("stays from the memory provider: %+v", s)
	}
	w.expect(w.submit("manager", "memstay", "memstay.cancel", "memstay.booking", "OPP-1-B1", "c-1", struct{}{}), "ok")
	if told := w.timeline("sales", "crm.opportunity/OPP-1"); len(told) != 1 || !strings.HasPrefix(told[0], "app:memstay: Booking canceled") {
		t.Fatalf("timeline %v", told)
	}
}

// #99: with two providers an administrator chooses where new stays go; the stays
// the other provider holds stay on the opportunity, and the choice replays.
func TestAdministratorChoosesTheProvider(t *testing.T) {
	w := newWorld(t, hotelProvider, memoryProvider)
	w.setup()
	w.expect(w.book("sales", "b-1", "suite", "2026-10-01"), "ok") // the hotel is bound first
	bind := func(who, key, provider string) string {
		return w.submit(who, platformserver.PlatformApp, platformserver.SchemaProtocolBind, platformserver.ProtocolType, lodging.ID, key,
			map[string]string{"provider": provider})
	}
	w.expect(bind("manager", "p-1", "memstay"), "ERROR_CODE_POLICY_DENIED")
	w.expect(bind("admin", "p-2", "crm"), "ERROR_CODE_INVALID_ARGUMENT")
	w.expect(bind("admin", "p-3", "memstay"), "ok")
	w.expect(w.book("sales", "b-2", "loft", "2026-10-01"), "ok") // the hotel sells no lofts: this went to memstay
	stays := func(w *world) string {
		var out []string
		for _, s := range w.stays("sales") {
			out = append(out, s.ID+" "+s.RoomType)
		}
		return fmt.Sprint(out)
	}
	w.expect(stays(w), "[OPP-1-B1 suite OPP-1-B2 loft]") // in the order they were linked, from both providers
	w.expect(w.submit("manager", pms.ID, pms.SchemaCancel, pms.ReservationType, "OPP-1-B1", "c-1", struct{}{}), "ok")
	if told := w.timeline("sales", "crm.opportunity/OPP-1"); len(told) != 1 || !strings.HasPrefix(told[0], "app:pms: Booking canceled") {
		t.Fatalf("the earlier provider's event no longer reaches the opportunity: %v", told)
	}
	platformserver.CheckReplay(t, w.tenant, w.journal, func() *platformserver.Tenant { return newWorld(t, hotelProvider, memoryProvider).tenant })
	again := newWorld(t, hotelProvider, memoryProvider)
	if err := again.tenant.Replay(w.journal); err != nil {
		t.Fatal(err)
	}
	if p := again.tenant.Protocols()[0]; p.ID != lodging.ID || p.Bound != "memstay" || stays(again) != stays(w) {
		t.Fatalf("after replay: bound %s, stays %s", p.Bound, stays(again))
	}
}

func TestCatalogFollowsTheProvidersGrants(t *testing.T) {
	w := newWorld(t, hotelProvider)
	catalog := func(who string) []string {
		var out []string
		for _, a := range w.tenant.Catalog(w.members[who]) {
			if !strings.HasPrefix(a.Schema, "platform.") && !strings.HasPrefix(a.Schema, "work.") && !strings.HasPrefix(a.Schema, "agent.") { // offered to every member
				out = append(out, a.Schema)
			}
		}
		return out
	}
	if got := catalog("sales"); !slices.Equal(got, []string{pms.SchemaCreate, pms.SchemaModify, crm.SchemaAccount, "crm.account.edit", "crm.account.archive", crm.SchemaOpen, crm.SchemaClose, crm.SchemaPlan, crm.SchemaBook}) {
		t.Fatalf("sales catalog %v", got)
	}
	if slices.Contains(catalog("sales-only"), crm.SchemaBook) {
		t.Fatal("booking is offered without the provider's grant")
	}
	// Without any provider the optional protocol is unbound and booking is not offered.
	bare, err := platformserver.NewTenant("t", platformserver.NewConsole("t"), platformserver.NewRelations("t"), platformserver.NewFlows("t"), platformserver.NewAgents("t"), crm.New("t"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range bare.Catalog(platform.Member{ID: "x", Tenant: "t", Roles: map[string]string{"crm": "sales"}}) {
		if a.Schema == crm.SchemaBook {
			t.Fatal("booking offered with no lodging provider")
		}
	}
}
