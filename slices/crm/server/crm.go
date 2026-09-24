// Package crm is a customer and opportunity app (#91), modelled on the
// account/opportunity core of Salesforce and Dynamics 365 Sales. It knows no
// other app. It consumes the lodging protocol when a tenant has a provider
// (ADR-0011): stays booked for an opportunity are linked to it through the
// platform, and the platform's timeline tells what happens to them.
package crm

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"lodging"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver"
)

const (
	Authority       = "crm-server"
	AccountType     = "crm.account"
	OpportunityType = "crm.opportunity"

	SchemaAccount = "crm.account.create"
	SchemaOpen    = "crm.opportunity.open"
	SchemaClose   = "crm.opportunity.close"
	SchemaBook    = "crm.opportunity.book"

	Sales   Role = "sales"
	Manager Role = "sales-manager"
)

type Role string

type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // company, person
	Revision uint32 `json:"revision"`
}

type Opportunity struct {
	ID       string `json:"id"`
	Account  string `json:"account"`
	Title    string `json:"title"`
	Owner    string `json:"owner"`
	Stage    string `json:"stage"` // open, won, lost
	Revision uint32 `json:"revision"`
}

// Actions is the CRM catalog (ADR-0008).
func Actions() *platformserver.Catalog {
	both := []string{string(Sales), string(Manager)}
	return platformserver.NewCatalog(
		platformserver.Action{Schema: SchemaAccount, Target: AccountType, Capability: "accounts", Title: "Create account",
			Description: "Create a customer account: a company or a person.",
			Payload: []platformserver.Field{{Name: "name", Type: "string", Required: true, Description: "Account name"},
				{Name: "kind", Type: "string", Required: true, Description: "company or person"}}, Roles: both},
		platformserver.Action{Schema: SchemaOpen, Target: OpportunityType, Capability: "opportunities", Title: "Open opportunity",
			Description: "Open a sales opportunity for an account; the caller owns it.",
			Payload: []platformserver.Field{{Name: "account", Type: "string", Required: true, Description: "Account ID"},
				{Name: "title", Type: "string", Required: true, Description: "What is being sold"}}, Roles: both},
		platformserver.Action{Schema: SchemaClose, Target: OpportunityType, Capability: "opportunities", Title: "Close opportunity",
			Description: "Close an open opportunity as won or lost; only its owner or a sales manager.",
			Payload:     []platformserver.Field{{Name: "outcome", Type: "string", Required: true, Description: "won or lost"}}, Roles: both},
		platformserver.Action{Schema: SchemaBook, Target: OpportunityType, Capability: "stays", Title: "Book stay",
			Description: "Book a stay for an opportunity with the tenant's lodging provider and link it to the opportunity; the provider decides with your role there.",
			Payload:     lodging.Protocol().Actions[0].Payload, Roles: both, Uses: []string{platformserver.ProtocolAction(lodging.ID, "reserve")}},
	)
}

// CRM is one tenant's accounts and opportunities.
type CRM struct {
	mu            sync.Mutex
	tenant        string
	accounts      map[string]*Account
	opportunities map[string]*Opportunity
	booked        map[string]int // opportunity → stays booked, for the booking IDs
	ledger        *platformserver.Ledger
}

func New(tenant string) *CRM {
	return &CRM{tenant: tenant, accounts: map[string]*Account{}, opportunities: map[string]*Opportunity{}, booked: map[string]int{},
		ledger: platformserver.NewLedger(tenant, Authority, Actions(), AccountType, OpportunityType)}
}

func fail(code pb.ErrorCode) *kernel.Error { return &kernel.Error{Code: code} }

func (c *CRM) Submit(who platformserver.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if who.Tenant != c.tenant {
		return nil, fail(pb.ErrorCode_ERROR_CODE_POLICY_DENIED)
	}
	id := s.GetTarget().GetId()
	owns := func() bool { // closing is for the owner or a manager
		o := c.opportunities[id]
		return s.GetSchema().GetName() != SchemaClose || who.Role() == string(Manager) || o != nil && o.Owner == who.ID
	}
	return c.ledger.Receive(who, s, now, owns, func() (func(*pb.ChangeRecord), *kernel.Error) {
		invalid := fail(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		var p struct{ Name, Kind, Account, Title, Outcome, Text string }
		if json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, invalid
		}
		switch s.GetSchema().GetName() {
		case SchemaAccount:
			if strings.TrimSpace(p.Name) == "" || p.Kind != "company" && p.Kind != "person" {
				return nil, invalid
			}
			if c.accounts[id] != nil {
				return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
			}
			return func(r *pb.ChangeRecord) {
				c.accounts[id] = &Account{ID: id, Name: p.Name, Kind: p.Kind, Revision: r.GetRevision()}
			}, nil
		case SchemaOpen:
			if strings.TrimSpace(p.Title) == "" || c.accounts[p.Account] == nil {
				return nil, invalid
			}
			if c.opportunities[id] != nil {
				return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
			}
			return func(r *pb.ChangeRecord) {
				c.opportunities[id] = &Opportunity{ID: id, Account: p.Account, Title: p.Title, Owner: who.ID, Stage: "open", Revision: r.GetRevision()}
			}, nil
		case SchemaBook:
			// The stay is the provider's decision, taken as this decision's rule (K4 C10):
			// its refusal refuses the booking, and nothing is linked.
			o := c.opportunities[id]
			if o == nil {
				return nil, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
			}
			if o.Stage == "lost" {
				return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
			}
			booking := fmt.Sprintf("%s-B%d", id, c.booked[id]+1)
			stay, _, err := who.Invoke(lodging.ID, "reserve", booking, s.GetPayload(), "crm:"+s.GetIdempotencyKey(), s.GetIdempotencyKey(), now)
			if err != nil {
				return nil, err
			}
			if err := who.Link(&pb.EntityRef{Type: OpportunityType, Id: id}, stay, "crm:link:"+s.GetIdempotencyKey(), now); err != nil {
				return nil, err
			}
			return func(*pb.ChangeRecord) { c.booked[id]++ }, nil
		case SchemaClose:
			o := c.opportunities[id]
			if p.Outcome != "won" && p.Outcome != "lost" {
				return nil, invalid
			}
			if o == nil {
				return nil, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
			}
			if o.Stage != "open" {
				return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
			}
			return func(r *pb.ChangeRecord) { o.Stage, o.Revision = p.Outcome, r.GetRevision() }, nil
		}
		return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
	})
}

func (c *CRM) Declarations() []*pb.AuthorityDeclaration { return c.ledger.Declarations() }

// Manifest declares the CRM as an app (ADR-0010).
func (c *CRM) Manifest() platformserver.Manifest {
	return platformserver.Manifest{ID: "crm", Version: "1", Actions: c.ledger.Catalog, Reads: []string{"accounts", "opportunities", "customers"},
		Consumes: []platformserver.Consumption{{Protocol: lodging.ID, Optional: true}}}
}

func (c *CRM) Read(who platformserver.Caller, name string) (any, *kernel.Error) {
	switch name {
	case "accounts":
		return c.Accounts(), nil
	case "opportunities":
		return c.Opportunities(), nil
	}
	return c.customers(who)
}

// Customer is an account with its opportunities and the stays linked to them
// that the caller may see (a member without a role at the provider sees none).
type Customer struct {
	Account
	Opportunities []OpportunityStays `json:"opportunities"`
}

type OpportunityStays struct {
	Opportunity
	Stays []lodging.Booking `json:"stays"`
}

func (c *CRM) customers(who platformserver.Caller) (any, *kernel.Error) {
	var bookings []lodging.Booking
	if who.Bound(lodging.ID) {
		all, err := who.Query(lodging.ID, "bookings")
		if err != nil {
			return nil, err
		}
		bookings = all.([]lodging.Booking)
	}
	out := []Customer{}
	opportunities := c.Opportunities()
	for _, a := range c.Accounts() {
		customer := Customer{Account: a, Opportunities: []OpportunityStays{}}
		for _, o := range opportunities {
			if o.Account != a.ID {
				continue
			}
			linked := who.Links(OpportunityType + "/" + o.ID)
			stays := []lodging.Booking{}
			for _, b := range bookings {
				if slices.ContainsFunc(linked, func(e string) bool { return strings.HasSuffix(e, "/"+b.ID) }) {
					stays = append(stays, b)
				}
			}
			customer.Opportunities = append(customer.Opportunities, OpportunityStays{Opportunity: o, Stays: stays})
		}
		out = append(out, customer)
	}
	return out, nil
}

func (c *CRM) Input(platformserver.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
}

// Accounts and Opportunities are the package's public reads, sorted by ID.
func (c *CRM) Accounts() []Account {
	c.mu.Lock()
	defer c.mu.Unlock()
	return sorted(c.accounts, func(a Account) string { return a.ID })
}

func (c *CRM) Opportunities() []Opportunity {
	c.mu.Lock()
	defer c.mu.Unlock()
	return sorted(c.opportunities, func(o Opportunity) string { return o.ID })
}

func sorted[T any](m map[string]*T, key func(T) string) []T {
	out := make([]T, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	slices.SortFunc(out, func(a, b T) int { return strings.Compare(key(a), key(b)) })
	return out
}
