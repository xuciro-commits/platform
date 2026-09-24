package crmhotel

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"crm"
	"hotel"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
)

var (
	t0    = time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	seats = []platformserver.Seat{
		{Subjects: []string{"sales"}, Member: platformserver.Member{ID: "sales-1", Roles: map[string]string{"crm": "sales", "crm-hotel": "sales", "hotel": "front-desk"}}},
		{Subjects: []string{"sales-only"}, Member: platformserver.Member{ID: "sales-2", Roles: map[string]string{"crm": "sales"}}},
		{Subjects: []string{"desk"}, Member: platformserver.Member{ID: "desk-1", Roles: map[string]string{"hotel": "front-desk"}}},
		{Subjects: []string{"manager"}, Member: platformserver.Member{ID: "manager-1", Roles: map[string]string{"crm": "sales-manager", "crm-hotel": "sales-manager", "hotel": "manager"}}},
	}
)

type world struct {
	t       *testing.T
	tenant  *platformserver.Tenant
	members map[string]platformserver.Member
	journal []platformserver.Entry
}

func newWorld(t *testing.T) *world {
	tn, err := NewTenant("hotel-a", map[string]hotel.RoomType{"suite": {Rooms: 1}}, seats...)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, tenant: tn, members: map[string]platformserver.Member{}}
	tn.Record = func(e platformserver.Entry) { w.journal = append(w.journal, e) }
	for _, s := range seats {
		w.members[s.Subjects[0]] = platformserver.Member{ID: s.ID, Tenant: "hotel-a", Roles: s.Roles}
	}
	return w
}

func (w *world) submit(who, app, schema, targetType, id, key string, payload any) string {
	raw, _ := json.Marshal(payload)
	_, err := w.tenant.Submit(w.members[who], &pb.Submission{TenantId: "hotel-a", PrincipalId: w.members[who].ID, Authority: app,
		Target: &pb.EntityRef{Type: targetType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, Payload: raw}, t0)
	if err != nil {
		return err.Error()
	}
	return "ok"
}

func (w *world) book(who, key, checkIn string) string {
	return w.submit(who, App, SchemaBook, LinkType, "OPP-1", key,
		map[string]string{"roomType": "suite", "checkIn": checkIn, "checkOut": "2026-10-03", "guest": "Acme board"})
}

func (w *world) read(name string) any {
	out, _ := w.tenant.Read(w.members["manager"], name)
	return out
}

func (w *world) stays() []hotel.Reservation {
	return w.read("customers").([]Customer)[0].Opportunities[0].Stays
}

func (w *world) expect(got, want string) {
	w.t.Helper()
	if got != want {
		w.t.Fatalf("got %s, want %s", got, want)
	}
}

func TestBookingCrossesAppsThroughTheHost(t *testing.T) {
	w := newWorld(t)
	w.expect(w.submit("sales", crm.Authority, crm.SchemaAccount, crm.AccountType, "ACME", "a", map[string]string{"name": "Acme Corp", "kind": "company"}), "ok")
	w.expect(w.submit("sales", crm.Authority, crm.SchemaOpen, crm.OpportunityType, "OPP-1", "o", map[string]string{"account": "ACME", "title": "Board offsite"}), "ok")
	w.expect(w.book("sales", "b-1", "2026-10-01"), "ok")
	w.expect(w.book("sales", "b-1", "2026-10-01"), "ok") // a resend
	if n := len(w.read("reservations").([]hotel.Reservation)); n != 1 {
		t.Fatalf("a resend booked again: %d reservations", n)
	}
	if s := w.stays(); len(s) != 1 || s[0].ID != "OPP-1-R1" || s[0].Guest != "Acme board" {
		t.Fatalf("customer view: %+v", s)
	}
	// The hotel's rules decide: the only suite is taken, so the bridge links nothing.
	w.expect(w.book("sales", "b-2", "2026-10-02"), "ERROR_CODE_CONFLICT")
	// Each app checks its own role: no hotel role, or no CRM role, is refused.
	w.expect(w.book("sales-only", "b-3", "2026-10-05"), "ERROR_CODE_POLICY_DENIED")
	w.expect(w.book("desk", "b-4", "2026-10-05"), "ERROR_CODE_POLICY_DENIED")
	// The hotel cancels on its own; the customer view reads it live.
	w.expect(w.submit("manager", hotel.Authority, hotel.SchemaCancel, hotel.ReservationType, "OPP-1-R1", "c-1", struct{}{}), "ok")
	if !w.stays()[0].Canceled {
		t.Fatal("the cancellation does not show on the customer")
	}
	// The bridge subscribed to hotel cancellations: the opportunity's timeline tells.
	notes := w.read("customers").([]Customer)[0].Opportunities[0].Notes
	if len(notes) != 1 || notes[0].By != "app:crm-hotel" || notes[0].Text != "The hotel canceled stay OPP-1-R1 (manager-1)." {
		t.Fatalf("opportunity notes %+v", notes)
	}
	w.expect(w.submit("sales", crm.Authority, crm.SchemaClose, crm.OpportunityType, "OPP-1", "l-1", map[string]string{"outcome": "lost"}), "ok")
	w.expect(w.book("sales", "b-5", "2026-10-06"), "ERROR_CODE_CONFLICT")

	// One journal across the apps replays the whole tenant, the hotel decision
	// the bridge caused included, although only the bridge input was recorded.
	again := newWorld(t)
	if err := again.tenant.Replay(w.journal); err != nil {
		t.Fatal(err)
	}
	view := func(w *world) string { return fmt.Sprint(w.read("customers"), w.read("reservations")) }
	if view(again) != view(w) {
		t.Fatalf("replayed tenant differs:\n%s\n%s", view(w), view(again))
	}
	if kinds := fmt.Sprint(len(w.journal)); kinds != "6" {
		t.Fatalf("journal holds %s entries, want the 6 accepted top-level inputs", kinds)
	}
}

func TestMemberCatalogFollowsBothGrants(t *testing.T) {
	w := newWorld(t)
	catalog := func(who string) []string {
		var out []string
		for _, a := range w.tenant.Catalog(w.members[who]) {
			out = append(out, a.Schema)
		}
		return out
	}
	if got := catalog("sales"); !slices.Equal(got, []string{hotel.SchemaCreate, hotel.SchemaModify, crm.SchemaAccount, crm.SchemaOpen, crm.SchemaClose, crm.SchemaNote, SchemaBook}) {
		t.Fatalf("sales catalog %v", got)
	}
	if slices.Contains(catalog("sales-only"), SchemaBook) || slices.Contains(catalog("desk"), SchemaBook) {
		t.Fatal("booking is offered without both the CRM and the hotel grant")
	}
}
