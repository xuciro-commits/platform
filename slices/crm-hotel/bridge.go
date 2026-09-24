// Package crmhotel is the bridge between the CRM and Hotel apps (#91, ADR-0009),
// in the manner of Odoo's bridge modules: neither app knows the other; the bridge
// owns what their cooperation adds (which reservations serve which opportunity)
// and reaches both only through the host, by action and read name (ADR-0010).
package crmhotel

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"crm"
	"hotel"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver"
)

const (
	App        = "crm-hotel"
	LinkType   = "crmhotel.stay" // the stays of an opportunity, keyed by its ID: the bridge's own entity (K5: one authority per data class)
	SchemaBook = "crmhotel.opportunity.book"
)

// Actions: booking for an opportunity is granted in this app (roles sales and
// sales-manager, like the CRM's); the stay itself is
// still the hotel's decision, so it is offered only to members who may create
// a reservation there (Uses).
func Actions() *platformserver.Catalog {
	stay, _ := hotel.Actions().Action(hotel.SchemaCreate) // the stay is described by the hotel
	return platformserver.NewCatalog(platformserver.Action{Schema: SchemaBook, Target: LinkType, Capability: "opportunity-stays",
		Title:       "Book stay for opportunity",
		Description: "Reserve a stay for an open opportunity's customer. The hotel decides with your hotel role and its availability; the reservation is linked to the opportunity.",
		Payload:     stay.Payload, Roles: []string{string(crm.Sales), string(crm.Manager)}, Uses: []string{hotel.SchemaCreate}})
}

type Bridge struct {
	mu     sync.Mutex
	tenant string
	ledger *platformserver.Ledger
	stays  map[string][]string // opportunity → reservation IDs
}

func New(tenant string) *Bridge {
	return &Bridge{tenant: tenant, stays: map[string][]string{}, ledger: platformserver.NewLedger(tenant, App, Actions(), LinkType)}
}

func (b *Bridge) Manifest() platformserver.Manifest {
	return platformserver.Manifest{ID: App, Version: "1", Actions: b.ledger.Catalog, Reads: []string{"customers"}, Requires: []string{"crm", "hotel"},
		Subscribes: []string{hotel.SchemaCancel, hotel.SchemaModify}}
}

func (b *Bridge) Declarations() []*pb.AuthorityDeclaration { return b.ledger.Declarations() }

func (b *Bridge) Input(platformserver.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// Submit books a stay: the hotel's decision is this decision's rule (K4 C10), so
// a hotel refusal (sold out, no hotel role) refuses the booking with the same
// code and links nothing. The hotel submission's key derives from this one, so
// a resend never books twice (F-22: causation cannot cross app logs).
func (b *Bridge) Submit(c platformserver.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if c.Tenant != b.tenant {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	id := s.GetTarget().GetId()
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		opportunities, err := c.Read("crm", "opportunities")
		if err != nil {
			return nil, err
		}
		var found *crm.Opportunity
		for _, o := range opportunities.([]crm.Opportunity) {
			if o.ID == id {
				found = &o
			}
		}
		if found == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if found.Stage == "lost" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		var stay json.RawMessage
		if json.Unmarshal(s.GetPayload(), &stay) != nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		reservation := fmt.Sprintf("%s-R%d", id, len(b.stays[id])+1)
		if _, err := c.Submit("hotel", &pb.Submission{TenantId: b.tenant, PrincipalId: c.ID, Authority: hotel.Authority,
			Target: &pb.EntityRef{Type: hotel.ReservationType, Id: reservation}, Schema: &pb.SchemaRef{Name: hotel.SchemaCreate, Version: 1},
			IdempotencyKey: App + ":" + s.GetIdempotencyKey(), CorrelationId: s.GetIdempotencyKey(), Payload: stay}, now); err != nil {
			return nil, err
		}
		return func(*pb.ChangeRecord) { b.stays[id] = append(b.stays[id], reservation) }, nil
	})
}

// Handle tells the opportunity when the hotel cancels or changes one of its
// stays: a note on its timeline, by the bridge's automation (#94). The hotel
// knows nothing of opportunities; a stay the bridge did not book is ignored.
func (b *Bridge) Handle(c platformserver.Caller, e platformserver.Event) *kernel.Error {
	s := e.Record.GetSubmission()
	reservation := s.GetTarget().GetId()
	b.mu.Lock()
	opportunity := ""
	for o, stays := range b.stays {
		if slices.Contains(stays, reservation) {
			opportunity = o
		}
	}
	b.mu.Unlock()
	if opportunity == "" {
		return nil
	}
	text := fmt.Sprintf("The hotel canceled stay %s (%s).", reservation, s.GetPrincipalId())
	if s.GetSchema().GetName() == hotel.SchemaModify {
		var stay hotel.Stay
		json.Unmarshal(s.GetPayload(), &stay)
		text = fmt.Sprintf("The hotel changed stay %s to %s, %s → %s (%s).", reservation, stay.RoomType, stay.CheckIn, stay.CheckOut, s.GetPrincipalId())
	}
	payload, _ := json.Marshal(map[string]string{"text": text})
	_, err := c.Submit("crm", &pb.Submission{TenantId: b.tenant, PrincipalId: c.ID, Authority: crm.Authority,
		Target: &pb.EntityRef{Type: crm.OpportunityType, Id: opportunity}, Schema: &pb.SchemaRef{Name: crm.SchemaNote, Version: 1},
		IdempotencyKey: App + ":event:" + e.Record.GetChangeId(), Payload: payload}, e.Record.GetRecordedTime().AsTime())
	return err
}

// Customer is an account with its opportunities and the stays booked for them,
// read live from both apps (a hotel cancellation shows at once).
type Customer struct {
	crm.Account
	Opportunities []OpportunityStays `json:"opportunities"`
}

type OpportunityStays struct {
	crm.Opportunity
	Stays []hotel.Reservation `json:"stays"`
}

// Read "customers".
func (b *Bridge) Read(c platformserver.Caller, _ string) (any, *kernel.Error) {
	accounts, err1 := c.Read("crm", "accounts")
	opportunities, err2 := c.Read("crm", "opportunities")
	all, err3 := c.Read("hotel", "reservations")
	for _, err := range []*kernel.Error{err1, err2, err3} {
		if err != nil {
			return nil, err
		}
	}
	reservations := map[string]hotel.Reservation{}
	for _, r := range all.([]hotel.Reservation) {
		reservations[r.ID] = r
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []Customer{}
	for _, a := range accounts.([]crm.Account) {
		customer := Customer{Account: a, Opportunities: []OpportunityStays{}}
		for _, o := range opportunities.([]crm.Opportunity) {
			if o.Account != a.ID {
				continue
			}
			stays := []hotel.Reservation{}
			for _, id := range b.stays[o.ID] {
				stays = append(stays, reservations[id])
			}
			customer.Opportunities = append(customer.Opportunities, OpportunityStays{Opportunity: o, Stays: stays})
		}
		out = append(out, customer)
	}
	return out, nil
}

// NewTenant is the composed sales software for one tenant: the directory, the
// two apps and their bridge (requirements point to apps enabled before).
func NewTenant(id string, rooms map[string]hotel.RoomType, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	return platformserver.NewTenant(id, platformserver.NewDirectory(id, seats...), hotel.NewHotel(id, rooms), crm.New(id), New(id))
}
