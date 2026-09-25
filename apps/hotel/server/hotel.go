// Package hotel is the Hotel reference slice (docs/WorkQueue.md #79): a tenant
// server that accepts reservation decisions through the kernel contract. It may
// not change the kernel; where the contract does not fit, it records friction.
package hotel

import (
	"embed"
	"encoding/json"
	"fmt"
	"maps"
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

// languages translate the app's titles and descriptions (ADR-0023).
//
//go:embed i18n
var languageFiles embed.FS

var languages = platform.LoadLanguages(languageFiles, "i18n")

const (
	RoomTypeType    = "hotel.room-type"
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
	return platform.NewCatalog(append(platform.EntityActions(Entities(nil)[0]),
		platform.Action{Schema: SchemaCreate, Target: ReservationType, Capability: "reservations", Title: "Create reservation",
			Description: "Reserve a room type for a stay; refused when the type is sold out for any night.",
			Payload:     append(stay, platform.Field{Name: "guest", Type: "string", Required: true, Description: "Guest name"}), Roles: book},
		platform.Action{Schema: SchemaModify, Target: ReservationType, Capability: "reservations", Title: "Modify stay",
			Description: "Change the room type or dates of a reservation.", Payload: stay, Roles: book},
		platform.Action{Schema: SchemaCancel, Target: ReservationType, Capability: "reservations", Title: "Cancel reservation",
			Description: "Cancel a reservation; the record stays in its history.", Payload: []platform.Field{}, Roles: []string{string(Manager)}},
	)...)
}

// Stay is a half-open range of nights [CheckIn, CheckOut), dates as YYYY-MM-DD.
type Stay struct {
	RoomType string `json:"roomType"`
	CheckIn  string `json:"checkIn"`
	CheckOut string `json:"checkOut"`
}

// Reservation is a stay of a guest in a room type (ADR-0016: the host keeps the records).
type Reservation struct {
	platform.Record
	RoomType platform.Ref[RoomType] `json:"roomType" field:"required" title:"Room type"`
	CheckIn  string                 `json:"checkIn" field:"required" title:"Check-in"`
	CheckOut string                 `json:"checkOut" field:"required" title:"Check-out"`
	Guest    string                 `json:"guest" field:"required,search"`
	Canceled bool                   `json:"canceled" field:"readonly"`
}

func (r Reservation) stay() Stay {
	return Stay{RoomType: string(r.RoomType), CheckIn: r.CheckIn, CheckOut: r.CheckOut}
}

// Entities declares the hotel's types: room types are master data a manager
// maintains (seeded from the deployment's configuration); reservations move by
// the hotel's actions.
func Entities(rooms map[string]RoomType) []platform.Entity {
	var seed []any
	for _, id := range slices.Sorted(maps.Keys(rooms)) {
		t := rooms[id]
		t.ID = id
		if t.Name == "" {
			t.Name = id
		}
		seed = append(seed, t)
	}
	return []platform.Entity{
		{Type: RoomTypeType, Title: "Room type", Model: RoomType{}, Seed: seed,
			Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{string(Manager)}, Capability: "room-types"}},
		{Type: ReservationType, Title: "Reservation", Model: Reservation{}},
	}
}

// Payloads, one per schema (version 1).
type createPayload struct {
	Stay
	Guest string `json:"guest"`
}

// Modify carries the new Stay; cancel carries nothing. Stale views are refused by
// the submission's expected revision (K4 C12), not by the payload.

// Hotel is one tenant's rules and kernel logs; its records are the host's.
type Hotel struct {
	mu       sync.Mutex
	tenant   string
	entities []platform.Entity
	facts    *kernel.FactLog
	ledger   *platform.Ledger
}

// RoomType is sellable inventory per night: physical rooms plus an overbooking
// allowance, as property management systems (OPERA, Mews) configure it.
type RoomType struct {
	platform.Record
	Name        string `json:"name" field:"required,search"`
	Rooms       int    `json:"rooms" field:"required"`
	Overbooking int    `json:"overbooking"`
	// Drill E1: sold per night (hotel rooms, serviced apartments) or per hour
	// (coworking desks and meeting rooms); MinUnits is the shortest stay.
	Hourly   bool `json:"hourly,omitempty"`
	MinUnits int  `json:"minUnits,omitempty" title:"Minimum stay"`
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

// NewHotel is a tenant's hotel whose room types start as rooms (keyed by ID).
func NewHotel(tenant string, rooms map[string]RoomType) *Hotel {
	h := &Hotel{tenant: tenant, entities: Entities(rooms),
		ledger: platform.NewLedger(tenant, Authority, Actions(), RoomTypeType, ReservationType),
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
	if record, err, ok := h.ledger.Generated(c, s, now, nil, h.entities...); ok {
		return record, err
	}
	return h.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		r, err := h.validate(c, s, c.Setting(SettingOverbooking) != "false")
		if err != nil {
			return nil, err
		}
		return func(record *pb.ChangeRecord) {
			c.Put(record, r)
			if !r.Canceled {
				h.tellOversold(c, r, now)
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
	s := r.stay()
	t, _ := platform.Get[RoomType](c, s.RoomType)
	in, out, step, _ := t.span(s)
	for slot := in; slot.Before(out); slot = slot.Add(step) {
		if used := h.used(c, t, slot, ""); used > t.Rooms {
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
	for _, r := range reservations(c) {
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
// Snapshot and Restore: the channel's facts and the decisions; reservations
// and room types are the host's records (ADR-0019 D6).
func (h *Hotel) Snapshot() (json.RawMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	facts, err := platform.SnapshotFacts(h.facts, h.tenant)
	if err != nil {
		return nil, err
	}
	return h.ledger.SnapshotWith(facts)
}

func (h *Hotel) Restore(raw json.RawMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var facts json.RawMessage
	if err := h.ledger.RestoreWith(raw, &facts); err != nil {
		return err
	}
	return platform.RestoreFacts(h.facts, h.tenant, facts)
}

func (h *Hotel) Manifest() platform.Manifest {
	return platform.Manifest{Languages: languages, ID: "hotel", Title: "Hotel", Version: "1", Actions: h.ledger.Catalog, Entities: h.entities,
		Reads: []string{"lodging-bookings"}, Inputs: map[string]bool{"channel-bookings": true},
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

// Read "lodging-bookings": the reservations as the lodging protocol shows them.
// Reservations and room types themselves are read as records (/v1/records/<type>).
func (h *Hotel) Read(c platform.Caller, _ string) (any, *kernel.Error) {
	out := []lodging.Booking{}
	for _, r := range reservations(c) {
		out = append(out, lodging.Booking{ID: r.ID, RoomType: string(r.RoomType), CheckIn: r.CheckIn, CheckOut: r.CheckOut, Guest: r.Guest, Canceled: r.Canceled})
	}
	return out, nil
}

// reservations are the tenant's reservations, by check-in, then ID.
func reservations(c platform.Caller) []Reservation {
	out, _, _ := platform.Find[Reservation](c, platform.Query{Sort: []string{"checkIn", "id"}, Archived: true})
	return out
}

// Input takes channel bookings; only a channel connector sends them.
func (h *Hotel) Input(c platform.Caller, _ string, body []byte, now time.Time) (any, *kernel.Error) {
	var b ChannelBooking
	if c.Role() != string(Channel) && !c.Replaying || json.Unmarshal(body, &b) != nil {
		return nil, denied()
	}
	return h.IngestChannelBooking(c, b, now)
}

// validate checks the domain rules and returns the reservation as it will be.
func (h *Hotel) validate(c platform.Caller, s *pb.Submission, overbooking bool) (Reservation, *kernel.Error) {
	id := s.GetTarget().GetId()
	existing, known := platform.Get[Reservation](c, id)
	invalid := fail(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	switch s.GetSchema().GetName() {
	case SchemaCreate:
		var p createPayload
		if json.Unmarshal(s.GetPayload(), &p) != nil || p.Guest == "" || !h.validStay(c, p.Stay) {
			return Reservation{}, invalid
		}
		if known {
			return Reservation{}, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
		}
		if !h.fits(c, p.Stay, "", overbooking) {
			return Reservation{}, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
		}
		return Reservation{Record: platform.Record{ID: id}, RoomType: platform.Ref[RoomType](p.RoomType), CheckIn: p.CheckIn, CheckOut: p.CheckOut, Guest: p.Guest}, nil
	case SchemaModify:
		var m Stay
		if json.Unmarshal(s.GetPayload(), &m) != nil || !h.validStay(c, m) {
			return Reservation{}, invalid
		}
		if !known || existing.Canceled {
			return Reservation{}, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
		}
		if !h.fits(c, m, id, overbooking) {
			return Reservation{}, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
		}
		existing.RoomType, existing.CheckIn, existing.CheckOut = platform.Ref[RoomType](m.RoomType), m.CheckIn, m.CheckOut
		return existing, nil
	case SchemaCancel:
		if !known || existing.Canceled {
			return Reservation{}, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
		}
		existing.Canceled = true
		return existing, nil
	}
	return Reservation{}, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
}

// validStay: an existing, not archived room type, and a stay in its unit.
func (h *Hotel) validStay(c platform.Caller, s Stay) bool {
	t, ok := platform.Get[RoomType](c, s.RoomType)
	_, _, _, whole := t.span(s)
	return ok && !t.Archived && whole && t.Rooms > 0
}

// fits reports whether every unit (night or hour) of the stay has a free room of its type,
// ignoring the reservation being modified; the overbooking allowance counts when
// the hotel sells it. Capacity allocation is domain code.
func (h *Hotel) fits(c platform.Caller, s Stay, ignore string, overbooking bool) bool {
	t, _ := platform.Get[RoomType](c, s.RoomType)
	limit := t.Rooms
	if overbooking {
		limit += t.Overbooking
	}
	in, out, step, _ := t.span(s)
	for slot := in; slot.Before(out); slot = slot.Add(step) {
		if h.used(c, t, slot, ignore) >= limit {
			return false
		}
	}
	return true
}

// used counts the reservations of room type t holding slot, except ignore.
func (h *Hotel) used(c platform.Caller, t RoomType, slot time.Time, ignore string) int {
	domain, _ := json.Marshal([]any{[]any{"roomType", "=", t.ID}, []any{"canceled", "=", false}})
	held, _, _ := platform.Find[Reservation](c, platform.Query{Domain: domain})
	n := 0
	for _, r := range held {
		if rIn, rOut, _, _ := t.span(r.stay()); r.ID != ignore && !slot.Before(rIn) && slot.Before(rOut) {
			n++
		}
	}
	return n
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
