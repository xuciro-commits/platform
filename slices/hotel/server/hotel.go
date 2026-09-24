// Package hotel is the Hotel reference slice (docs/WorkQueue.md #79): a tenant
// server that accepts reservation decisions through the kernel contract. It may
// not change the kernel; where the contract does not fit, it records friction.
package hotel

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"lodging"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
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

// Actions is the hotel's action catalog (ADR-0008): front desk and channels
// create and modify; only managers cancel. Other packages act through it too.
func Actions() *platform.Catalog {
	stay := []platform.Field{{Name: "roomType", Type: "string", Required: true, Description: "Room type"},
		{Name: "checkIn", Type: "date", Required: true, Description: "First night (YYYY-MM-DD; hourly types YYYY-MM-DDTHH:MM)"},
		{Name: "checkOut", Type: "date", Required: true, Description: "Departure, exclusive"}}
	book := []string{string(FrontDesk), string(Manager), string(Channel)}
	return platform.NewCatalog(
		platform.Action{Schema: SchemaCreate, Target: ReservationType, Capability: "reservations", Title: "Create reservation",
			Description: "Reserve a room type for a stay; refused when the type is sold out for any night.",
			Payload:     append(stay, platform.Field{Name: "guest", Type: "string", Required: true, Description: "Guest name"}), Roles: book},
		platform.Action{Schema: SchemaModify, Target: ReservationType, Capability: "reservations", Title: "Modify stay",
			Description: "Change the room type or dates of a reservation.", Payload: stay, Roles: book},
		platform.Action{Schema: SchemaCancel, Target: ReservationType, Capability: "reservations", Title: "Cancel reservation",
			Description: "Cancel a reservation; the record stays in its history.", Payload: []platform.Field{}, Roles: []string{string(Manager)}},
	)
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
	facts        *kernel.FactLog
	ledger       *platform.Ledger
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

func NewHotel(tenant string, rooms map[string]RoomType) *Hotel {
	h := &Hotel{tenant: tenant, rooms: rooms, reservations: map[string]*Reservation{},
		ledger: platform.NewLedger(tenant, Authority, Actions(), ReservationType),
		facts:  kernel.NewFactLog(kernel.NewSchemaRegistry([]*pb.SchemaRef{{Name: channelMessageSchema, Version: 1}}, nil))}
	h.ledger.Changes.Facts = func(tenant, id string) bool {
		return slices.ContainsFunc(h.facts.Records(tenant), func(r *pb.FactRecord) bool { return r.GetFactId() == id })
	}
	return h
}

func denied() *kernel.Error                { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED} }
func fail(code pb.ErrorCode) *kernel.Error { return &kernel.Error{Code: code} }

// Submit turns a submission into a change record or rejects it, in the kernel's
// receiving order (K6 T2); the hotel supplies only its rules.
func (h *Hotel) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.Tenant != h.tenant {
		return nil, denied()
	}
	return h.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		apply, err := h.validate(s, c.Setting(SettingOverbooking) != "false")
		if err != nil {
			return nil, err
		}
		return func(record *pb.ChangeRecord) {
			apply()
			r := h.reservations[s.GetTarget().GetId()]
			r.Version = int(record.GetRevision())
			if !r.Canceled {
				h.tellOversold(c, *r, now)
			}
		}, nil
	})
}

// Settings, job and notices of the hotel (ADR-0013), the second app on them after manufacturing.
const (
	SettingOverbooking  = "allow-overbooking"
	SettingChannelNotes = "channel-booking-notice"
	SettingArrivalsLead = "arrivals-lead-days"
	JobArrivals         = "arrivals"
)

// tellOversold tells the managers about each unit of the stay sold beyond the
// physical rooms of its type (the overbooking allowance in use: a walk risk).
func (h *Hotel) tellOversold(c platform.Caller, r Reservation, now time.Time) {
	s := r.Stay
	t := h.rooms[s.RoomType]
	in, out, step, _ := t.span(s)
	for slot := in; slot.Before(out); slot = slot.Add(step) {
		if used := h.used(s.RoomType, slot, ""); used > t.Rooms {
			day := slot.Format(map[bool]string{true: "2006-01-02 15:04", false: time.DateOnly}[t.Hourly])
			c.Notify(platform.Notification{Title: fmt.Sprintf("%s oversold on %s", s.RoomType, day),
				Body: fmt.Sprintf("%d sold for %d rooms: someone may have to be walked.", used, t.Rooms),
				Ref:  ReservationType + "/" + r.ID, Key: "oversold:" + s.RoomType + ":" + day}, now, platform.Recipient{AppRole: string(Manager)})
		}
	}
}

// Run sends the front desk the arrivals of the day the lead setting names, once per day.
func (h *Hotel) Run(c platform.Caller, _ string, now time.Time) *kernel.Error {
	lead, err := strconv.Atoi(c.Setting(SettingArrivalsLead))
	if err != nil || lead <= 0 {
		return nil
	}
	day := now.UTC().AddDate(0, 0, lead).Format(time.DateOnly)
	var lines []string
	for _, r := range h.Reservations() {
		if !r.Canceled && strings.HasPrefix(r.CheckIn, day) {
			lines = append(lines, fmt.Sprintf("%s · %s · %s", r.ID, r.Guest, r.RoomType))
		}
	}
	if len(lines) > 0 {
		c.Notify(platform.Notification{Title: fmt.Sprintf("%d arrivals on %s", len(lines), day), Body: strings.Join(lines, "\n"),
			Key: "arrivals:" + day}, now, platform.Recipient{AppRole: string(FrontDesk)})
	}
	return nil
}

// ChannelConnector is the channel manager as a K8 push connector; its ID is the member it signs in as.
func ChannelConnector(id string) *pb.ConnectorDescriptor {
	return &pb.ConnectorDescriptor{ConnectorId: id, Direction: pb.ConnectorDirection_CONNECTOR_DIRECTION_PUSH,
		DataClasses: []string{ReservationType}, Heartbeat: durationpb.New(5 * time.Minute)}
}

// Declarations are the tenant's authority declarations, for edges (K5 A9).
func (h *Hotel) Declarations() []*pb.AuthorityDeclaration { return h.ledger.Declarations() }

// Manifest declares the hotel as an app (ADR-0010).
func (h *Hotel) Manifest() platform.Manifest {
	return platform.Manifest{ID: "hotel", Version: "1", Actions: h.ledger.Catalog,
		Reads: []string{"reservations", "lodging-bookings"}, Inputs: map[string]bool{"channel-bookings": true},
		Jobs: []platform.Job{{Name: JobArrivals, Title: "Send the front desk the arrivals list", Every: time.Hour}},
		Settings: []platform.Setting{
			{Name: SettingOverbooking, Title: "Sell the overbooking allowance", Type: "boolean", Default: "true",
				Description: "Sell rooms beyond the physical count up to each room type's allowance; managers are told when it is used."},
			{Name: SettingChannelNotes, Title: "Tell about channel bookings", Type: "choice", Default: "front-desk", Choices: []string{"off", "front-desk", "manager"},
				Description: "Who is notified of each booking the channel manager delivers."},
			{Name: SettingArrivalsLead, Title: "Arrivals list, days ahead", Type: "integer", Default: "1",
				Description: "The front desk receives the arrivals of the day this far ahead; 0 turns the list off."},
		},
		// The hotel sells stays to any app through the lodging protocol (ADR-0011).
		Provides: []platform.Provision{{Protocol: lodging.Protocol(),
			Actions: map[string]string{"reserve": SchemaCreate, "change": SchemaModify, "cancel": SchemaCancel},
			Reads:   map[string]string{"bookings": "lodging-bookings"},
			Events:  map[string]string{"changed": SchemaModify, "canceled": SchemaCancel}}}}
}

func (h *Hotel) Read(_ platform.Caller, name string) (any, *kernel.Error) {
	if name == "lodging-bookings" {
		out := []lodging.Booking{}
		for _, r := range h.Reservations() {
			out = append(out, lodging.Booking{ID: r.ID, RoomType: r.RoomType, CheckIn: r.CheckIn, CheckOut: r.CheckOut, Guest: r.Guest, Canceled: r.Canceled})
		}
		return out, nil
	}
	return h.Reservations(), nil
}

// Input takes channel bookings; only a channel connector sends them.
func (h *Hotel) Input(c platform.Caller, _ string, body []byte, now time.Time) (any, *kernel.Error) {
	var b ChannelBooking
	if c.Role() != string(Channel) && !c.Replaying || json.Unmarshal(body, &b) != nil {
		return nil, denied()
	}
	return h.IngestChannelBooking(c, b, now)
}

// validate checks the domain rules and returns how to apply the decision.
func (h *Hotel) validate(s *pb.Submission, overbooking bool) (func(), *kernel.Error) {
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
		if !h.fits(c.Stay, "", overbooking) {
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
		if !h.fits(m, id, overbooking) {
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
// ignoring the reservation being modified; the overbooking allowance counts when
// the hotel sells it. Capacity allocation is domain code.
func (h *Hotel) fits(s Stay, ignore string, overbooking bool) bool {
	t := h.rooms[s.RoomType]
	limit := t.Rooms
	if overbooking {
		limit += t.Overbooking
	}
	in, out, step, _ := t.span(s)
	for slot := in; slot.Before(out); slot = slot.Add(step) {
		if h.used(s.RoomType, slot, ignore) >= limit {
			return false
		}
	}
	return true
}

// used counts the reservations of roomType holding slot, except ignore.
func (h *Hotel) used(roomType string, slot time.Time, ignore string) int {
	t, n := h.rooms[roomType], 0
	for id, r := range h.reservations {
		if rIn, rOut, _, _ := t.span(r.Stay); id != ignore && !r.Canceled && r.RoomType == roomType && !slot.Before(rIn) && slot.Before(rOut) {
			n++
		}
	}
	return n
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

// IngestChannelBooking takes a delivery of the channel connector (K8, kept by
// the host), records the raw message as an observation (duplicates collapse by
// message ID), submits the booking it asks for, and tells whom the hotel's
// setting names.
func (h *Hotel) IngestChannelBooking(connector platform.Caller, b ChannelBooking, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if err := connector.Deliver(ReservationType, "", "", now); err != nil {
		return nil, err
	}
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
	record, err := h.Submit(connector, &pb.Submission{TenantId: h.tenant, PrincipalId: connector.ID, Authority: Authority,
		Target: &pb.EntityRef{Type: ReservationType, Id: b.ReservationID}, Schema: &pb.SchemaRef{Name: SchemaCreate, Version: 1},
		IdempotencyKey: "channel:" + b.MessageID, Payload: payload, EvidenceFactIds: []string{fact.GetFactId()}}, now)
	if to := connector.Setting(SettingChannelNotes); err == nil && to != "off" && to != "" {
		connector.Notify(platform.Notification{Title: "Channel booking " + b.ReservationID,
			Body: fmt.Sprintf("%s · %s · %s to %s", b.Guest, b.RoomType, b.CheckIn, b.CheckOut),
			Ref:  ReservationType + "/" + b.ReservationID, Key: "channel:" + b.ReservationID}, now, platform.Recipient{AppRole: to})
	}
	return record, err
}
