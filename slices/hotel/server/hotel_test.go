package hotel

import (
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

var (
	desk    = Principal{ID: "desk-1", Tenant: "hotel-a", Role: FrontDesk}
	manager = Principal{ID: "manager-1", Tenant: "hotel-a", Role: Manager}
	channel = Principal{ID: "channel-sim", Tenant: "hotel-a", Role: Channel}
	now     = time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
)

func newHotel() *Hotel {
	return NewHotel("hotel-a", map[string]RoomType{"standard": {Rooms: 1, Overbooking: 1}, "suite": {Rooms: 1}})
}

func submission(p Principal, schema, id, key string, payload any, expectedRevision ...uint32) *pb.Submission {
	raw, _ := json.Marshal(payload)
	s := &pb.Submission{TenantId: p.Tenant, PrincipalId: p.ID, Authority: Authority,
		Target: &pb.EntityRef{Type: ReservationType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1},
		IdempotencyKey: key, Payload: raw}
	if len(expectedRevision) > 0 {
		s.ExpectedRevision = &expectedRevision[0]
	}
	return s
}

func create(h *Hotel, p Principal, id, key, roomType, in, out string) (*pb.ChangeRecord, string) {
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
	other := Principal{ID: "desk-9", Tenant: "hotel-b", Role: FrontDesk}
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

func TestChannelDuplicatesCollapse(t *testing.T) {
	h := newHotel()
	b := ChannelBooking{MessageID: "ota-778", ReservationID: "r-ota-778", Guest: "OTA Guest",
		Stay: Stay{RoomType: "suite", CheckIn: "2026-11-01", CheckOut: "2026-11-02"}, SentAt: now.Add(-time.Minute)}
	first, err := h.IngestChannelBooking(channel, b, now)
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.IngestChannelBooking(channel, b, now.Add(time.Second))
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
	_, err = h.IngestChannelBooking(channel, b, now)
	expect(t, err.Error(), "ERROR_CODE_CONFLICT") // sold out: the observation stays, the decision is rejected
	if n := len(h.facts.Records("hotel-a")); n != 2 {
		t.Fatalf("observation of the refused booking was not kept: %d", n)
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
