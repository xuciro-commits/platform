// Package crmhotel is the bridge between the CRM and Hotel packages (#91), in
// the manner of Odoo's bridge modules: neither package knows the other; the
// bridge owns what their cooperation adds (which reservations serve which
// opportunity) and reaches both only through their declared actions and
// public reads.
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
	Authority   = "crm-hotel"
	LinkType    = "crmhotel.stay" // the stays of an opportunity, keyed by its ID: the bridge's own entity (K5: one authority per data class)
	SchemaBook  = "crmhotel.opportunity.book"
	packageName = "crm-hotel"
)

// Member is a person or client of a tenant composed of several packages: one
// role per package (F-21: packages each define their principal).
type Member struct {
	ID     string            `json:"id"`
	Tenant string            `json:"tenant"`
	Roles  map[string]string `json:"roles"` // package → role
}

func (m Member) TenantID() string    { return m.Tenant }
func (m Member) PrincipalID() string { return m.ID }

func (m Member) Hotel() hotel.Principal {
	return hotel.Principal{ID: m.ID, Tenant: m.Tenant, Role: hotel.Role(m.Roles["hotel"])}
}
func (m Member) CRM() crm.Principal {
	return crm.Principal{ID: m.ID, Tenant: m.Tenant, Role: crm.Role(m.Roles["crm"])}
}

// Actions: booking for an opportunity is a sales action; the stay itself is
// still the hotel's decision, taken with the caller's hotel role.
func Actions() *platformserver.Catalog {
	stay, _ := hotel.Actions().Action(hotel.SchemaCreate) // the stay is described by the hotel
	return platformserver.NewCatalog(platformserver.Action{Schema: SchemaBook, Target: LinkType, Capability: "opportunity-stays",
		Title:       "Book stay for opportunity",
		Description: "Reserve a stay for an open opportunity's customer. The hotel decides with your hotel role and its availability; the reservation is linked to the opportunity.",
		Payload:     stay.Payload,
		Roles:       []string{string(crm.Sales), string(crm.Manager)}})
}

type Bridge struct {
	mu     sync.Mutex
	tenant string
	ledger *platformserver.Ledger
	crm    *crm.CRM
	hotel  *hotel.Hotel
	stays  map[string][]string // opportunity → reservation IDs
}

func New(tenant string, c *crm.CRM, h *hotel.Hotel) *Bridge {
	return &Bridge{tenant: tenant, crm: c, hotel: h, stays: map[string][]string{},
		ledger: platformserver.NewLedger(tenant, Authority, Actions(), LinkType)}
}

// Submit books a stay: the hotel's decision is this decision's rule (K4 C10), so
// a hotel refusal (sold out, no hotel role) refuses the booking with the same
// code and links nothing. The hotel submission's key derives from this one, so
// a resend never books twice (F-22: causation cannot cross package logs).
func (b *Bridge) Submit(who Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if who.Tenant != b.tenant {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	id := s.GetTarget().GetId()
	return b.ledger.Receive(who.ID, who.Roles["crm"], s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		o, ok := b.crm.Opportunity(id)
		if !ok {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if o.Stage == "lost" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		var stay json.RawMessage
		if json.Unmarshal(s.GetPayload(), &stay) != nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		reservation := fmt.Sprintf("%s-R%d", id, len(b.stays[id])+1)
		if _, err := b.hotel.Submit(who.Hotel(), &pb.Submission{TenantId: b.tenant, PrincipalId: who.ID, Authority: hotel.Authority,
			Target: &pb.EntityRef{Type: hotel.ReservationType, Id: reservation}, Schema: &pb.SchemaRef{Name: hotel.SchemaCreate, Version: 1},
			IdempotencyKey: packageName + ":" + s.GetIdempotencyKey(), CorrelationId: s.GetIdempotencyKey(), Payload: stay}, now); err != nil {
			return nil, err
		}
		return func(*pb.ChangeRecord) { b.stays[id] = append(b.stays[id], reservation) }, nil
	})
}

func (b *Bridge) Declarations() []*pb.AuthorityDeclaration { return b.ledger.Declarations() }

// Catalog offers booking only to members the hotel would also let create a
// reservation: the action needs both packages' grants.
func (b *Bridge) Catalog(who Member) []platformserver.Action {
	if !slices.ContainsFunc(b.hotel.Catalog(who.Hotel()), func(a platformserver.Action) bool { return a.Schema == hotel.SchemaCreate }) {
		return []platformserver.Action{}
	}
	return b.ledger.Catalog.For(who.Roles["crm"])
}

// Customer is an account with its opportunities and the stays booked for them,
// read live from both packages (a hotel cancellation shows at once).
type Customer struct {
	crm.Account
	Opportunities []OpportunityStays `json:"opportunities"`
}

type OpportunityStays struct {
	crm.Opportunity
	Stays []hotel.Reservation `json:"stays"`
}

func (b *Bridge) Customers() []Customer {
	b.mu.Lock()
	defer b.mu.Unlock()
	reservations := map[string]hotel.Reservation{}
	for _, r := range b.hotel.Reservations() {
		reservations[r.ID] = r
	}
	opportunities := b.crm.Opportunities()
	var out []Customer
	for _, a := range b.crm.Accounts() {
		c := Customer{Account: a, Opportunities: []OpportunityStays{}}
		for _, o := range opportunities {
			if o.Account != a.ID {
				continue
			}
			stays := []hotel.Reservation{}
			for _, id := range b.stays[o.ID] {
				stays = append(stays, reservations[id])
			}
			c.Opportunities = append(c.Opportunities, OpportunityStays{Opportunity: o, Stays: stays})
		}
		out = append(out, c)
	}
	return out
}
