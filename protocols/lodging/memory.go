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
// consumer needs no change when the provider does (ADR-0011).
type Memory struct {
	mu       sync.Mutex
	bookings map[string]*Booking
	ledger   *platform.Ledger
}

const Keeper = "keeper"

func NewMemory(tenant string) *Memory {
	p := Protocol()
	var actions []platform.Action
	for _, a := range p.Actions {
		a.Schema, a.Target, a.Capability, a.Roles = "memstay."+a.Schema, "memstay.booking", "stays", []string{Keeper}
		actions = append(actions, a)
	}
	return &Memory{bookings: map[string]*Booking{}, ledger: platform.NewLedger(tenant, "memstay", platform.NewCatalog(actions...), "memstay.booking")}
}

// Snapshot and Restore: the bookings and the decisions (ADR-0019 D6).
func (m *Memory) Snapshot() (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ledger.SnapshotWith(m.bookings)
}

func (m *Memory) Restore(raw json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bookings = map[string]*Booking{}
	return m.ledger.RestoreWith(raw, &m.bookings)
}

func (m *Memory) Manifest() platform.Manifest {
	return platform.Manifest{Languages: languages, ID: "memstay", Title: "Memstay", Version: "1", Actions: m.ledger.Catalog, Reads: []string{"memstay-bookings"},
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
	m.mu.Lock()
	var expired []string
	for id, b := range m.bookings {
		if b.Status == Held && b.Until < now.UTC().Format(time.DateOnly) {
			expired = append(expired, id)
		}
	}
	m.mu.Unlock()
	slices.Sort(expired)
	for _, id := range expired {
		if _, err := m.Submit(c, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: "memstay", Target: &pb.EntityRef{Type: "memstay.booking", Id: id},
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
		id, existing := s.GetTarget().GetId(), m.bookings[s.GetTarget().GetId()]
		notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		switch schema := s.GetSchema().GetName(); schema {
		case "memstay.reserve", "memstay.hold":
			if existing != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			held := schema == "memstay.hold"
			if b.RoomType == "" || b.Guest == "" || b.CheckOut <= b.CheckIn || held && b.Until == "" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			b.ID, b.Status = id, map[bool]string{true: Held, false: Booked}[held]
			if !held {
				b.Until = ""
			}
			return func(*pb.ChangeRecord) { m.bookings[id] = &b }, nil
		case "memstay.change":
			if existing == nil || !existing.Open() {
				return nil, notFound
			}
			return func(*pb.ChangeRecord) {
				existing.RoomType, existing.CheckIn, existing.CheckOut = b.RoomType, b.CheckIn, b.CheckOut
			}, nil
		case "memstay.confirm", "memstay.release":
			if existing == nil || existing.Status != Held {
				return nil, notFound
			}
			to := map[bool]string{true: Booked, false: Released}[schema == "memstay.confirm"]
			return func(*pb.ChangeRecord) { existing.Status, existing.Until = to, "" }, nil
		}
		if existing == nil || !existing.Open() {
			return nil, notFound
		}
		return func(*pb.ChangeRecord) { existing.Status = Canceled }, nil
	})
}

func (m *Memory) Read(platform.Caller, string) (any, *kernel.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Booking{}
	for _, b := range m.bookings {
		out = append(out, *b)
	}
	slices.SortFunc(out, func(a, b Booking) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}
