package lodging

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver"
)

// Memory is the smallest lodging provider: any room type, no capacity. It is the
// protocol's reference implementation and the second provider that shows a
// consumer needs no change when the provider does (ADR-0011).
type Memory struct {
	mu       sync.Mutex
	bookings map[string]*Booking
	ledger   *platformserver.Ledger
}

const Keeper = "keeper"

func NewMemory(tenant string) *Memory {
	p := Protocol()
	var actions []platformserver.Action
	for _, a := range p.Actions {
		a.Schema, a.Target, a.Capability, a.Roles = "memstay."+a.Schema, "memstay.booking", "stays", []string{Keeper}
		actions = append(actions, a)
	}
	return &Memory{bookings: map[string]*Booking{}, ledger: platformserver.NewLedger(tenant, "memstay", platformserver.NewCatalog(actions...), "memstay.booking")}
}

func (m *Memory) Manifest() platformserver.Manifest {
	return platformserver.Manifest{ID: "memstay", Version: "1", Actions: m.ledger.Catalog, Reads: []string{"memstay-bookings"},
		Provides: []platformserver.Provision{{Protocol: Protocol(),
			Actions: map[string]string{"reserve": "memstay.reserve", "change": "memstay.change", "cancel": "memstay.cancel"},
			Reads:   map[string]string{"bookings": "memstay-bookings"},
			Events:  map[string]string{"changed": "memstay.change", "canceled": "memstay.cancel"}}}}
}

func (m *Memory) Declarations() []*pb.AuthorityDeclaration { return m.ledger.Declarations() }

func (m *Memory) Input(platformserver.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (m *Memory) Submit(c platformserver.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var b Booking
		json.Unmarshal(s.GetPayload(), &b)
		id, existing := s.GetTarget().GetId(), m.bookings[s.GetTarget().GetId()]
		switch s.GetSchema().GetName() {
		case "memstay.reserve":
			if existing != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			if b.RoomType == "" || b.Guest == "" || b.CheckOut <= b.CheckIn {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			b.ID = id
			return func(*pb.ChangeRecord) { m.bookings[id] = &b }, nil
		case "memstay.change":
			if existing == nil || existing.Canceled {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			return func(*pb.ChangeRecord) {
				existing.RoomType, existing.CheckIn, existing.CheckOut = b.RoomType, b.CheckIn, b.CheckOut
			}, nil
		}
		if existing == nil || existing.Canceled {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return func(*pb.ChangeRecord) { existing.Canceled = true }, nil
	})
}

func (m *Memory) Read(platformserver.Caller, string) (any, *kernel.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Booking{}
	for _, b := range m.bookings {
		out = append(out, *b)
	}
	slices.SortFunc(out, func(a, b Booking) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}
