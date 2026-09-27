package lodging

import (
	"embed"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// languages translate the app's titles and descriptions (ADR-0023).
//
//go:embed i18n
var languageFiles embed.FS

var languages = platform.LoadLanguages(languageFiles, "i18n")

// Memory is the smallest lodging provider: any room type, no capacity. It is the
// protocol's reference implementation and the second provider that shows a
// consumer needs no change when the provider does (ADR-0011). Its stays are
// records of the host, like any app's (#129): a record page lists them.
type Memory struct {
	mu     sync.Mutex
	ledger *platform.Ledger
}

const (
	Keeper   = "keeper"
	StayType = "memstay.booking"
)

// Stay is a booking memstay keeps.
type Stay struct {
	platform.Record
	RoomType string `json:"roomType" field:"required,search" title:"Room type"`
	CheckIn  string `json:"checkIn" field:"required" type:"date" title:"Check-in"`
	CheckOut string `json:"checkOut" field:"required" type:"date" title:"Check-out"`
	Guest    string `json:"guest" field:"required,search"`
	Status   string `json:"status" field:"readonly" choices:"held,booked,canceled,released"`
	Until    string `json:"until,omitempty" type:"date" title:"Held until"`
}

func (s Stay) booking() Booking {
	return Booking{ID: s.ID, RoomType: s.RoomType, CheckIn: s.CheckIn, CheckOut: s.CheckOut, Guest: s.Guest, Status: s.Status, Until: s.Until}
}

func stays() []platform.Entity {
	return []platform.Entity{{Type: StayType, Title: "Memstay booking", Model: Stay{}, Display: "guest"}}
}

func NewMemory(tenant string) *Memory {
	p := Protocol()
	var actions []platform.Action
	for _, a := range p.Actions {
		a.Schema, a.Target, a.Capability, a.Roles = "memstay."+a.Schema, StayType, "stays", []string{Keeper}
		actions = append(actions, a)
	}
	return &Memory{ledger: platform.NewLedger(tenant, "memstay", platform.NewCatalog(actions...), StayType)}
}

// Snapshot and Restore: its records are the host's; the ledger is its own (ADR-0019 D6).
func (m *Memory) Snapshot() (json.RawMessage, error) { return m.ledger.Snapshot() }

func (m *Memory) Restore(raw json.RawMessage) error { return m.ledger.Restore(raw) }

func (m *Memory) Manifest() platform.Manifest {
	return platform.Manifest{Languages: languages, ID: "memstay", Title: "Memstay", Version: "1", Actions: m.ledger.Catalog, Entities: stays(), Reads: []string{"memstay-bookings"},
		Jobs: []platform.Job{{Name: JobHolds, Title: "Release holds past their date", Every: time.Hour}},
		Provides: []platform.Provision{{Protocol: Protocol(),
			Actions: map[string]string{"reserve": "memstay.reserve", "change": "memstay.change", "cancel": "memstay.cancel",
				"hold": "memstay.hold", "confirm": "memstay.confirm", "release": "memstay.release"},
			Reads: map[string]string{"bookings": "memstay-bookings"},
			Events: map[string]string{"changed": "memstay.change", "canceled": "memstay.cancel",
				"confirmed": "memstay.confirm", "released": "memstay.release"}}}}
}

// JobHolds releases the holds whose last day has passed.
const JobHolds = "holds"

// Run releases every hold whose last day is before now's day, as the provider.
func (m *Memory) Run(c platform.Caller, _ string, now time.Time) *kernel.Error {
	var expired []string
	for _, b := range platform.Records[Stay](c) {
		if b.Status == Held && b.Until < now.UTC().Format(time.DateOnly) {
			expired = append(expired, b.ID)
		}
	}
	slices.Sort(expired)
	for _, id := range expired {
		if _, err := m.Submit(c, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: "memstay", Target: &pb.EntityRef{Type: StayType, Id: id},
			Schema: &pb.SchemaRef{Name: "memstay.release", Version: 1}, IdempotencyKey: "expired:" + id, Payload: []byte("{}")}, now); err != nil {
			return err
		}
	}
	return nil
}

func (m *Memory) Declarations() []*pb.AuthorityDeclaration { return m.ledger.Declarations() }

func (m *Memory) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (m *Memory) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var b Booking
		json.Unmarshal(s.GetPayload(), &b)
		id := s.GetTarget().GetId()
		existing, known := platform.Get[Stay](c, id)
		notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		put := func(st Stay) (func(*pb.ChangeRecord), *kernel.Error) {
			return func(r *pb.ChangeRecord) { c.Put(r, st) }, nil
		}
		switch schema := s.GetSchema().GetName(); schema {
		case "memstay.reserve", "memstay.hold":
			if known {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			held := schema == "memstay.hold"
			if b.RoomType == "" || b.Guest == "" || b.CheckOut <= b.CheckIn || held && b.Until == "" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			st := Stay{Record: platform.Record{ID: id}, RoomType: b.RoomType, CheckIn: b.CheckIn, CheckOut: b.CheckOut, Guest: b.Guest,
				Status: map[bool]string{true: Held, false: Booked}[held]}
			if held {
				st.Until = b.Until
			}
			return put(st)
		case "memstay.change":
			if !known || !existing.booking().Open() {
				return nil, notFound
			}
			existing.RoomType, existing.CheckIn, existing.CheckOut = b.RoomType, b.CheckIn, b.CheckOut
			return put(existing)
		case "memstay.confirm", "memstay.release":
			if !known || existing.Status != Held {
				return nil, notFound
			}
			existing.Status, existing.Until = map[bool]string{true: Booked, false: Released}[schema == "memstay.confirm"], ""
			return put(existing)
		}
		if !known || !existing.booking().Open() {
			return nil, notFound
		}
		existing.Status = Canceled
		return put(existing)
	})
}

func (m *Memory) Read(c platform.Caller, _ string) (any, *kernel.Error) {
	out := []Booking{}
	for _, b := range platform.Records[Stay](c) {
		out = append(out, b.booking())
	}
	slices.SortFunc(out, func(a, b Booking) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}
