// Package hotel is the Hotel reference slice (docs/WorkQueue.md #79): a tenant
// server that accepts reservation decisions through the kernel contract. It may
// not change the kernel; where the contract does not fit, it records friction.
package hotel

import (
	"encoding/json"
	"slices"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

const (
	ReservationType = "hotel.reservation"
	Authority       = "hotel-server"
	SchemaCreate    = "hotel.reservation.create"
	SchemaModify    = "hotel.reservation.modify"
	SchemaCancel    = "hotel.reservation.cancel"
)

// Role is domain data (K6: org structure is not kernel).
type Role string

const (
	FrontDesk Role = "front-desk"
	Manager   Role = "manager"
	Channel   Role = "channel"
)

type Principal struct {
	ID     string
	Tenant string
	Role   Role
}

// Policy is the policy hook: one evaluation of (principal, action, target).
type Policy func(p Principal, action string, target *pb.EntityRef) bool

// DefaultPolicy: front desk and channels create and modify; only managers cancel.
func DefaultPolicy(p Principal, action string, _ *pb.EntityRef) bool {
	switch action {
	case SchemaCreate, SchemaModify:
		return slices.Contains([]Role{FrontDesk, Manager, Channel}, p.Role)
	case SchemaCancel:
		return p.Role == Manager
	}
	return false
}

// Stay is a half-open range of nights [CheckIn, CheckOut), dates as YYYY-MM-DD.
type Stay struct {
	RoomType string `json:"roomType"`
	CheckIn  string `json:"checkIn"`
	CheckOut string `json:"checkOut"`
}

type Reservation struct {
	ID       string `json:"id"`
	Stay            // embedded
	Guest    string `json:"guest"`
	Version  int    `json:"version"`
	Canceled bool   `json:"canceled"`
}

// Payloads, one per schema (version 1).
type createPayload struct {
	Stay
	Guest string `json:"guest"`
}

// Modify carries the new Stay; cancel carries nothing. Stale views are refused by
// the submission's expected revision (K4 C12), not by the payload.

// Hotel is one tenant: its rooms, reservations and kernel logs.
type Hotel struct {
	mu           sync.Mutex
	tenant       string
	rooms        map[string]RoomType
	reservations map[string]*Reservation
	changes      *kernel.ChangeLog
	facts        *kernel.FactLog
	authorities  *kernel.Authorities
	policy       Policy
	declaration  *pb.AuthorityDeclaration
}

// RoomType is sellable inventory per night: physical rooms plus an overbooking
// allowance, as property management systems (OPERA, Mews) configure it.
type RoomType struct {
	Rooms       int `json:"rooms"`
	Overbooking int `json:"overbooking"`
	// Drill E1: sold per night (hotel rooms, serviced apartments) or per hour
	// (coworking desks and meeting rooms); MinUnits is the shortest stay.
	Hourly   bool `json:"hourly,omitempty"`
	MinUnits int  `json:"minUnits,omitempty"`
}

// span parses a stay in its room type's unit: dates per night, "YYYY-MM-DDTHH:MM" per hour.
func (t RoomType) span(s Stay) (in, out time.Time, step time.Duration, ok bool) {
	layout, step := time.DateOnly, 24*time.Hour
	if t.Hourly {
		layout, step = "2006-01-02T15:04", time.Hour
	}
	in, err1 := time.Parse(layout, s.CheckIn)
	out, err2 := time.Parse(layout, s.CheckOut)
	whole := out.Sub(in)%step == 0
	return in, out, step, err1 == nil && err2 == nil && out.After(in) && whole && int(out.Sub(in)/step) >= max(t.MinUnits, 1)
}

func NewHotel(tenant string, rooms map[string]RoomType, policy Policy) *Hotel {
	schemas := []*pb.SchemaRef{{Name: SchemaCreate, Version: 1}, {Name: SchemaModify, Version: 1}, {Name: SchemaCancel, Version: 1}}
	registry := kernel.NewSchemaRegistry(schemas, nil)
	h := &Hotel{tenant: tenant, rooms: rooms, reservations: map[string]*Reservation{},
		changes: kernel.NewChangeLog(registry), authorities: kernel.NewAuthorities(Authority), policy: policy,
		facts: kernel.NewFactLog(kernel.NewSchemaRegistry([]*pb.SchemaRef{{Name: channelMessageSchema, Version: 1}}, nil))}
	h.declaration = &pb.AuthorityDeclaration{TenantId: tenant, DataClass: ReservationType,
		Kind: pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, AuthorityId: Authority, Epoch: 1}
	h.authorities.Declare(h.declaration)
	h.changes.Facts = func(tenant, id string) bool {
		return slices.ContainsFunc(h.facts.Records(tenant), func(r *pb.FactRecord) bool { return r.GetFactId() == id })
	}
	return h
}

func denied() *kernel.Error                { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED} }
func fail(code pb.ErrorCode) *kernel.Error { return &kernel.Error{Code: code} }

// Submit turns a submission into a change record or rejects it, in the kernel's
// receiving order (K6 T2); the hotel supplies only its policy and domain rules.
func (h *Hotel) Submit(p Principal, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.Tenant != h.tenant {
		return nil, denied()
	}
	receiver := kernel.Receiver{Changes: h.changes, Authorities: h.authorities,
		Policy: func(_ kernel.Caller, s *pb.Submission) bool {
			return h.policy(p, s.GetSchema().GetName(), s.GetTarget())
		}}
	var apply func()
	record, err := receiver.Receive(kernel.Caller{Tenant: p.Tenant, Principal: p.ID}, s, now, func() *kernel.Error {
		var err *kernel.Error
		apply, err = h.validate(s)
		return err
	})
	if err == nil && apply != nil {
		apply()
		h.reservations[s.GetTarget().GetId()].Version = int(record.GetRevision())
	}
	return record, err
}

// Declarations are the tenant's authority declarations, for edges (K5 A9).
func (h *Hotel) Declarations() []*pb.AuthorityDeclaration {
	return []*pb.AuthorityDeclaration{h.declaration}
}

// validate checks the domain rules and returns how to apply the decision.
func (h *Hotel) validate(s *pb.Submission) (func(), *kernel.Error) {
	id := s.GetTarget().GetId()
	existing := h.reservations[id]
	invalid := fail(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	switch s.GetSchema().GetName() {
	case SchemaCreate:
		var c createPayload
		if json.Unmarshal(s.GetPayload(), &c) != nil || c.Guest == "" || !h.validStay(c.Stay) {
			return nil, invalid
		}
		if existing != nil {
			return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
		}
		if !h.fits(c.Stay, "") {
			return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
		}
		return func() { h.reservations[id] = &Reservation{ID: id, Stay: c.Stay, Guest: c.Guest} }, nil
	case SchemaModify:
		var m Stay
		if json.Unmarshal(s.GetPayload(), &m) != nil || !h.validStay(m) {
			return nil, invalid
		}
		if existing == nil || existing.Canceled {
			return nil, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
		}
		if !h.fits(m, id) {
			return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
		}
		return func() { existing.Stay = m }, nil
	case SchemaCancel:
		if existing == nil || existing.Canceled {
			return nil, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
		}
		return func() { existing.Canceled = true }, nil
	}
	return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
}

func (h *Hotel) validStay(s Stay) bool {
	t := h.rooms[s.RoomType]
	_, _, _, ok := t.span(s)
	return ok && t.Rooms > 0
}

// fits reports whether every unit (night or hour) of the stay has a free room of its type,
// ignoring the reservation being modified. Capacity allocation is domain code.
func (h *Hotel) fits(s Stay, ignore string) bool {
	t := h.rooms[s.RoomType]
	in, out, step, _ := t.span(s)
	for slot := in; slot.Before(out); slot = slot.Add(step) {
		used := 0
		for id, r := range h.reservations {
			if rIn, rOut, _, _ := t.span(r.Stay); id != ignore && !r.Canceled && r.RoomType == s.RoomType && !slot.Before(rIn) && slot.Before(rOut) {
				used++
			}
		}
		if used >= t.Rooms+t.Overbooking {
			return false
		}
	}
	return true
}

func (h *Hotel) Reservations() []Reservation {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Reservation, 0, len(h.reservations))
	for _, r := range h.reservations {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b Reservation) int {
		if a.CheckIn != b.CheckIn {
			if a.CheckIn < b.CheckIn {
				return -1
			}
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	return out
}

// Channel connector input (K2 observation, then a decision by the channel principal).

const channelMessageSchema = "hotel.channel.booking"

type ChannelBooking struct {
	MessageID     string `json:"messageId"`
	ReservationID string `json:"reservationId"`
	Stay
	Guest  string    `json:"guest"`
	SentAt time.Time `json:"sentAt"`
}

// IngestChannelBooking records the raw message as an observation (duplicates
// collapse by message ID) and submits the booking it asks for.
func (h *Hotel) IngestChannelBooking(connector Principal, b ChannelBooking, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	raw, _ := json.Marshal(b)
	h.mu.Lock()
	fact, err := h.facts.Record(&pb.Fact{TenantId: h.tenant, Kind: pb.FactKind_FACT_KIND_OBSERVATION,
		Subject: &pb.EntityRef{Type: ReservationType, Id: b.ReservationID}, Attribute: "channel-booking",
		Schema: &pb.SchemaRef{Name: channelMessageSchema, Version: 1}, IdempotencyKey: b.MessageID, Payload: raw,
		Provenance: &pb.Provenance{Source: &pb.Provenance_ConnectorId{ConnectorId: connector.ID}, SourceTime: timestamppb.New(b.SentAt)}}, now)
	h.mu.Unlock()
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(createPayload{Stay: b.Stay, Guest: b.Guest})
	return h.Submit(connector, &pb.Submission{TenantId: h.tenant, PrincipalId: connector.ID, Authority: Authority,
		Target: &pb.EntityRef{Type: ReservationType, Id: b.ReservationID}, Schema: &pb.SchemaRef{Name: SchemaCreate, Version: 1},
		IdempotencyKey: "channel:" + b.MessageID, Payload: payload, EvidenceFactIds: []string{fact.GetFactId()}}, now)
}

// RecordJSON renders a change record with Protobuf JSON names.
func RecordJSON(r *pb.ChangeRecord) json.RawMessage {
	out, _ := protojson.Marshal(r)
	return out
}
