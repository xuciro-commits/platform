package hotel

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver"
)

var (
	desk    = as("desk-1", "hotel-a", FrontDesk)
	manager = as("manager-1", "hotel-a", Manager)
	channel = as("channel-sim", "hotel-a", Channel)
	now     = time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
)

func newHotel() *Hotel {
	return NewHotel("hotel-a", map[string]RoomType{"standard": {Rooms: 1, Overbooking: 1}, "suite": {Rooms: 1}})
}

func submission(p platformserver.Caller, schema, id, key string, payload any, expectedRevision ...uint32) *pb.Submission {
	raw, _ := json.Marshal(payload)
	s := &pb.Submission{TenantId: p.Tenant, PrincipalId: p.ID, Authority: Authority,
		Target: &pb.EntityRef{Type: ReservationType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1},
		IdempotencyKey: key, Payload: raw}
	if len(expectedRevision) > 0 {
		s.ExpectedRevision = &expectedRevision[0]
	}
	return s
}

func create(h *Hotel, p platformserver.Caller, id, key, roomType, in, out string) (*pb.ChangeRecord, string) {
	r, err := h.Submit(p, submission(p, SchemaCreate, id, key,
		map[string]string{"roomType": roomType, "checkIn": in, "checkOut": out, "guest": "Guest " + id}), now)
	if err != nil {
		return nil, err.Error()
	}
	return r, "ok"
}

func expect(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestCapacityConflictRespectsOverbookingPerNight(t *testing.T) {
	h := newHotel()
	_, got := create(h, desk, "r1", "k1", "standard", "2026-10-01", "2026-10-03")
	expect(t, got, "ok")
	_, got = create(h, desk, "r2", "k2", "standard", "2026-10-02", "2026-10-04") // uses the overbooking allowance
	expect(t, got, "ok")
	_, got = create(h, desk, "r3", "k3", "standard", "2026-10-02", "2026-10-03")
	expect(t, got, "ERROR_CODE_CONFLICT")
	_, got = create(h, desk, "r4", "k4", "standard", "2026-10-03", "2026-10-05") // r1 has left
	expect(t, got, "ok")
	_, got = create(h, desk, "r5", "k5", "suite", "2026-10-03", "2026-10-02")
	expect(t, got, "ERROR_CODE_INVALID_ARGUMENT")
}

func TestReplayReturnsOriginalEvenWhenFull(t *testing.T) {
	h := newHotel()
	first, _ := create(h, desk, "r1", "k1", "suite", "2026-10-01", "2026-10-02")
	again, got := create(h, desk, "r1", "k1", "suite", "2026-10-01", "2026-10-02")
	expect(t, got, "ok")
	if again.GetChangeId() != first.GetChangeId() || len(h.Reservations()) != 1 {
		t.Fatal("replay applied twice")
	}
	_, got = create(h, desk, "r1", "k1", "suite", "2026-10-01", "2026-10-03")
	expect(t, got, "ERROR_CODE_IDEMPOTENCY_CONFLICT")
}

func TestRolesAndVersions(t *testing.T) {
	h := newHotel()
	create(h, desk, "r1", "k1", "suite", "2026-10-01", "2026-10-02")
	_, err := h.Submit(desk, submission(desk, SchemaCancel, "r1", "k2", map[string]int{}, 1), now)
	expect(t, err.Error(), "ERROR_CODE_POLICY_DENIED")
	_, err = h.Submit(desk, submission(desk, SchemaModify, "r1", "k3",
		map[string]any{"roomType": "suite", "checkIn": "2026-10-01", "checkOut": "2026-10-03"}, 1), now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.Submit(manager, submission(manager, SchemaCancel, "r1", "k4", map[string]int{}, 1), now)
	expect(t, err.Error(), "ERROR_CODE_CONFLICT") // stale version
	if _, err = h.Submit(manager, submission(manager, SchemaCancel, "r1", "k5", map[string]int{}, 2), now); err != nil {
		t.Fatal(err)
	}
	_, got := create(h, desk, "r2", "k6", "suite", "2026-10-01", "2026-10-03") // canceled stay frees the room
	expect(t, got, "ok")
}

func TestTenantPrincipalAndAuthorityAreChecked(t *testing.T) {
	h := newHotel()
	other := as("desk-9", "hotel-b", FrontDesk)
	_, got := create(h, other, "r1", "k1", "suite", "2026-10-01", "2026-10-02")
	expect(t, got, "ERROR_CODE_POLICY_DENIED")
	s := submission(desk, SchemaCreate, "r1", "k2", map[string]string{})
	s.PrincipalId = "manager-1" // claims someone else
	_, err := h.Submit(desk, s, now)
	expect(t, err.Error(), "ERROR_CODE_POLICY_DENIED")
	s = submission(desk, SchemaCreate, "r1", "k3", map[string]string{"roomType": "suite", "checkIn": "2026-10-01", "checkOut": "2026-10-02", "guest": "G"})
	s.Authority = "desk-laptop"
	_, err = h.Submit(desk, s, now)
	expect(t, err.Error(), "ERROR_CODE_NOT_AUTHORITY")
}

// hotelTenant runs the hotel on the host with its channel connector, journaling every input.
func hotelTenant(t *testing.T, journal *[]platformserver.Entry) (*Hotel, *platformserver.Tenant) {
	seat := func(id string, roles map[string]string) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{id}, Member: platformserver.Member{ID: id, Roles: roles}}
	}
	h := newHotel()
	tn, err := platformserver.NewTenant("hotel-a", platformserver.NewDirectory("hotel-a",
		seat("desk-1", map[string]string{"hotel": string(FrontDesk)}), seat("desk-2", map[string]string{"hotel": string(FrontDesk)}),
		seat("manager-1", map[string]string{"hotel": string(Manager), platformserver.PlatformApp: platformserver.Admin}),
		seat("channel-sim", map[string]string{"hotel": string(Channel)})), h)
	if err == nil {
		err = tn.Connect(ChannelConnector("channel-sim"))
	}
	if err != nil {
		t.Fatal(err)
	}
	tn.Record = func(e platformserver.Entry) { *journal = append(*journal, e) }
	return h, tn
}

func deliver(tn *platformserver.Tenant, b ChannelBooking, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	raw, _ := json.Marshal(b)
	out, err := tn.Input(channel.Member, "channel-bookings", raw, at)
	record, _ := out.(*pb.ChangeRecord)
	return record, err
}

func TestChannelDuplicatesCollapse(t *testing.T) {
	var journal []platformserver.Entry
	h, tn := hotelTenant(t, &journal)
	b := ChannelBooking{MessageID: "ota-778", ReservationID: "r-ota-778", Guest: "OTA Guest",
		Stay: Stay{RoomType: "suite", CheckIn: "2026-11-01", CheckOut: "2026-11-02"}, SentAt: now.Add(-time.Minute)}
	first, err := deliver(tn, b, now)
	if err != nil {
		t.Fatal(err)
	}
	again, err := deliver(tn, b, now.Add(time.Second))
	if err != nil || again.GetChangeId() != first.GetChangeId() {
		t.Fatalf("duplicate delivery was not idempotent: %v", err)
	}
	if ev := first.GetSubmission().GetEvidenceFactIds(); len(ev) != 1 || ev[0] != h.facts.Records("hotel-a")[0].GetFactId() {
		t.Fatalf("booking decision does not name its channel observation: %v", ev)
	}
	if n := len(h.facts.Records("hotel-a")); n != 1 || len(h.Reservations()) != 1 {
		t.Fatalf("facts %d, reservations %d", n, len(h.Reservations()))
	}
	b.MessageID, b.ReservationID = "ota-779", "r-ota-779"
	_, err = deliver(tn, b, now)
	expect(t, err.Error(), "ERROR_CODE_CONFLICT") // sold out: the observation stays, the decision is rejected
	if n := len(h.facts.Records("hotel-a")); n != 2 {
		t.Fatalf("observation of the refused booking was not kept: %d", n)
	}
	if c := tn.Connectors(now); c[0].LastError == nil || c[0].LastError.Error != "ERROR_CODE_CONFLICT" {
		t.Fatalf("the refused delivery is not shown on the connector: %+v", c)
	}
	// Without the connector the channel's member cannot deliver.
	_, bare := hotelTenant(t, &journal)
	other := platformserver.Member{ID: "channel-2", Tenant: "hotel-a", Roles: map[string]string{"hotel": string(Channel)}}
	raw, _ := json.Marshal(b)
	if _, err := bare.Input(other, "channel-bookings", raw, now); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		t.Fatalf("an unconnected channel delivered: %v", err)
	}
}

// #98: the hotel is the second app on the platform's operations (ADR-0013):
// settings, notifications addressed by app role, a scheduled job and the connector.
func TestHotelUsesPlatformOperations(t *testing.T) {
	var journal []platformserver.Entry
	_, tn := hotelTenant(t, &journal)
	member := func(id string) platformserver.Member {
		return platformserver.Member{ID: id, Tenant: "hotel-a", Roles: map[string]string{"hotel": map[string]string{"desk-1": string(FrontDesk),
			"desk-2": string(FrontDesk), "manager-1": string(Manager)}[id], platformserver.PlatformApp: map[string]string{"manager-1": platformserver.Admin}[id]}}
	}
	inbox := func(tn *platformserver.Tenant, id string) []string {
		out, err := tn.Read(member(id), "notifications")
		if err != nil {
			t.Fatal(err)
		}
		titles := []string{}
		for _, n := range out.([]platformserver.Notification) {
			titles = append(titles, n.Title)
		}
		return titles
	}
	keys := 0
	book := func(id, roomType, in, out string) string {
		keys++
		raw, _ := json.Marshal(map[string]string{"roomType": roomType, "checkIn": in, "checkOut": out, "guest": "Guest " + id})
		_, err := tn.Submit(member("desk-1"), &pb.Submission{TenantId: "hotel-a", PrincipalId: "desk-1", Authority: Authority, IdempotencyKey: fmt.Sprint("b", keys),
			Target: &pb.EntityRef{Type: ReservationType, Id: id}, Schema: &pb.SchemaRef{Name: SchemaCreate, Version: 1}, Payload: raw}, now)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	set := func(name, value string) {
		keys++
		_, err := tn.Submit(member("manager-1"), &pb.Submission{TenantId: "hotel-a", PrincipalId: "manager-1", Authority: platformserver.PlatformApp,
			IdempotencyKey: fmt.Sprint("s", keys), Target: &pb.EntityRef{Type: platformserver.SettingType, Id: "hotel/" + name},
			Schema: &pb.SchemaRef{Name: platformserver.SchemaSettingSet, Version: 1}, Payload: []byte(`{"value":"` + value + `"}`)}, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	// The channel's booking reaches the front desk, then, once the setting says so, the managers.
	if _, err := deliver(tn, ChannelBooking{MessageID: "m1", ReservationID: "ota-1", Guest: "Ana", Stay: Stay{RoomType: "suite", CheckIn: "2026-10-01", CheckOut: "2026-10-02"}}, now); err != nil {
		t.Fatal(err)
	}
	set(SettingChannelNotes, "manager")
	if _, err := deliver(tn, ChannelBooking{MessageID: "m2", ReservationID: "ota-2", Guest: "Bo", Stay: Stay{RoomType: "suite", CheckIn: "2026-10-05", CheckOut: "2026-10-06"}}, now); err != nil {
		t.Fatal(err)
	}
	expect(t, fmt.Sprint(inbox(tn, "desk-1"), inbox(tn, "desk-2"), inbox(tn, "manager-1")), "[Channel booking ota-1] [Channel booking ota-1] [Channel booking ota-2]")
	// Overbooking is the hotel's to switch; when it is used, managers hear of it.
	expect(t, book("r1", "standard", "2026-10-01", "2026-10-02"), "ok")
	set(SettingOverbooking, "false")
	expect(t, book("r2", "standard", "2026-10-01", "2026-10-02"), "ERROR_CODE_CONFLICT")
	set(SettingOverbooking, "true")
	expect(t, book("r2", "standard", "2026-10-01", "2026-10-02"), "ok")
	expect(t, inbox(tn, "manager-1")[0], "standard oversold on 2026-10-01")
	// The arrivals list reaches the front desk once a day.
	tn.Work(time.Date(2026, 9, 30, 6, 0, 0, 0, time.UTC))
	tn.Work(time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC))
	expect(t, fmt.Sprint(inbox(tn, "desk-2")), "[3 arrivals on 2026-10-01 Channel booking ota-1]")
	platformserver.CheckReplay(t, tn, journal, func() *platformserver.Tenant { var j []platformserver.Entry; _, x := hotelTenant(t, &j); return x })
	// A replay of the journal tells everyone the same.
	var again []platformserver.Entry
	_, replayed := hotelTenant(t, &again)
	if err := replayed.Replay(journal); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"desk-1", "desk-2", "manager-1"} {
		if fmt.Sprint(inbox(replayed, id)) != fmt.Sprint(inbox(tn, id)) {
			t.Fatalf("%s after replay: %v, before %v", id, inbox(replayed, id), inbox(tn, id))
		}
	}
}

// Drill E1: serviced apartments (long stays) and coworking (hourly) reuse the
// reservation lifecycle; only the domain's capacity unit changed.
func TestDrillE1ApartmentsAndCoworking(t *testing.T) {
	h := NewHotel("hotel-a", map[string]RoomType{
		"apartment":    {Rooms: 1, MinUnits: 28},
		"meeting-room": {Rooms: 1, Hourly: true},
	})
	_, got := create(h, desk, "a1", "k1", "apartment", "2026-10-01", "2026-10-04")
	expect(t, got, "ERROR_CODE_INVALID_ARGUMENT") // shorter than the minimum stay
	_, got = create(h, desk, "a2", "k2", "apartment", "2026-10-01", "2026-11-01")
	expect(t, got, "ok")
	_, got = create(h, desk, "m1", "k3", "meeting-room", "2026-10-01T09:00", "2026-10-01T11:00")
	expect(t, got, "ok")
	_, got = create(h, desk, "m2", "k4", "meeting-room", "2026-10-01T11:00", "2026-10-01T12:00")
	expect(t, got, "ok")
	_, got = create(h, desk, "m3", "k5", "meeting-room", "2026-10-01T10:00", "2026-10-01T12:00")
	expect(t, got, "ERROR_CODE_CONFLICT")
	_, got = create(h, desk, "m4", "k6", "meeting-room", "2026-10-01T12:30", "2026-10-01T13:00")
	expect(t, got, "ERROR_CODE_INVALID_ARGUMENT") // not whole hours
	_, err := h.Submit(manager, submission(manager, SchemaCancel, "m1", "k7", map[string]int{}, 1), now)
	if err != nil {
		t.Fatal(err)
	}
	_, got = create(h, desk, "m5", "k8", "meeting-room", "2026-10-01T10:00", "2026-10-01T11:00")
	expect(t, got, "ok")
}

func as(id, tenant string, role Role) platformserver.Caller {
	return platformserver.As("hotel", platformserver.Member{ID: id, Tenant: tenant, Roles: map[string]string{"hotel": string(role)}})
}
