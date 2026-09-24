// Package crm is a customer and opportunity app (#91), modelled on the
// account/opportunity core of Salesforce and Dynamics 365 Sales. It knows no
// other app. It consumes the lodging protocol when a tenant has a provider
// (ADR-0011): stays booked for an opportunity are linked to it through the
// platform, and the platform's timeline tells what happens to them.
package crm

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"lodging"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
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

// Account and Opportunity are the CRM's entity types (ADR-0016): the host
// keeps their records and gives them lists, record pages and history.
type Account struct {
	platform.Record
	Name string `json:"name" field:"required,search"`
	Kind string `json:"kind" field:"required" choices:"company,person"`
}

type Opportunity struct {
	platform.Record
	Account platform.Ref[Account] `json:"account" field:"required"`
	Title   string                `json:"title" field:"required,search"`
	Owner   string                `json:"owner" field:"readonly"`
	Stage   string                `json:"stage" field:"readonly" choices:"open,won,lost"`
	Booked  int                   `json:"booked" field:"readonly" title:"Stays booked"`
}

// Entities declares the CRM's types. Accounts are master data with generated
// create, edit and archive actions (crm.account.create keeps its schema, so
// journals replay); opportunities move by the CRM's own actions, and a sales
// member sees their own (Salesforce's private default), a manager all of them.
func Entities() []platform.Entity {
	both := []string{string(Sales), string(Manager)}
	return []platform.Entity{
		{Type: AccountType, Title: "Account", Model: Account{},
			Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: both, Capability: "accounts"}},
		{Type: OpportunityType, Title: "Opportunity", Model: Opportunity{},
			Scope: platform.Scope{Owner: "owner", Levels: map[string]string{string(Sales): platform.ScopeOwn}}},
	}
}

// Actions is the CRM catalog (ADR-0008).
func Actions() *platform.Catalog {
	both := []string{string(Sales), string(Manager)}
	return platform.NewCatalog(append(platform.StandardActions(Entities()[0]),
		platform.Action{Schema: SchemaOpen, Target: OpportunityType, Capability: "opportunities", Title: "Open opportunity",
			Description: "Open a sales opportunity for an account; the caller owns it.",
			Payload: []platform.Field{{Name: "account", Type: "string", Required: true, Description: "Account ID"},
				{Name: "title", Type: "string", Required: true, Description: "What is being sold"}}, Roles: both},
		platform.Action{Schema: SchemaClose, Target: OpportunityType, Capability: "opportunities", Title: "Close opportunity",
			Description: "Close an open opportunity as won or lost; only its owner or a sales manager.",
			Payload:     []platform.Field{{Name: "outcome", Type: "string", Required: true, Description: "won or lost"}}, Roles: both},
		platform.Action{Schema: SchemaBook, Target: OpportunityType, Capability: "stays", Title: "Book stay",
			Description: "Book a stay for an opportunity with the tenant's lodging provider and link it to the opportunity; the provider decides with your role there.",
			Payload:     lodging.Protocol().Actions[0].Payload, Roles: both, Uses: []string{platform.ProtocolAction(lodging.ID, "reserve")}},
	)...)
}

// CRM is one tenant's sales rules; its records are the host's.
type CRM struct {
	mu     sync.Mutex
	tenant string
	ledger *platform.Ledger
}

func New(tenant string) *CRM {
	return &CRM{tenant: tenant, ledger: platform.NewLedger(tenant, Authority, Actions(), AccountType, OpportunityType)}
}

func fail(code pb.ErrorCode) *kernel.Error { return &kernel.Error{Code: code} }

func (c *CRM) Submit(who platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if who.Tenant != c.tenant {
		return nil, fail(pb.ErrorCode_ERROR_CODE_POLICY_DENIED)
	}
	if record, err, ok := c.ledger.Standard(who, s, now, nil, Entities()...); ok {
		return record, err
	}
	id := s.GetTarget().GetId()
	o, known := platform.Get[Opportunity](who, id)
	owns := func() bool { // closing is for the owner or a manager
		return s.GetSchema().GetName() != SchemaClose || who.Role() == string(Manager) || known && o.Owner == who.ID
	}
	return c.ledger.Receive(who, s, now, owns, func() (func(*pb.ChangeRecord), *kernel.Error) {
		invalid := fail(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		var p struct{ Account, Title, Outcome string }
		if json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, invalid
		}
		switch s.GetSchema().GetName() {
		case SchemaOpen:
			if known {
				return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
			}
			o = Opportunity{Record: platform.Record{ID: id}, Account: platform.Ref[Account](p.Account), Title: strings.TrimSpace(p.Title), Owner: who.ID, Stage: "open"}
			if err := who.Check(o); err != nil {
				return nil, invalid
			}
		case SchemaBook:
			// The stay is the provider's decision, taken as this decision's rule (K4 C10):
			// its refusal refuses the booking, and nothing is linked.
			if !known {
				return nil, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
			}
			if o.Stage == "lost" {
				return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
			}
			booking := fmt.Sprintf("%s-B%d", id, o.Booked+1)
			stay, _, err := who.Invoke(lodging.ID, "reserve", booking, s.GetPayload(), "crm:"+s.GetIdempotencyKey(), s.GetIdempotencyKey(), now)
			if err != nil {
				return nil, err
			}
			if err := who.Link(&pb.EntityRef{Type: OpportunityType, Id: id}, stay, "crm:link:"+s.GetIdempotencyKey(), now); err != nil {
				return nil, err
			}
			o.Booked++
		case SchemaClose:
			if p.Outcome != "won" && p.Outcome != "lost" {
				return nil, invalid
			}
			if !known {
				return nil, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
			}
			if o.Stage != "open" {
				return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
			}
			o.Stage = p.Outcome
		default:
			return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
		}
		return func(r *pb.ChangeRecord) { who.Put(r, o) }, nil
	})
}

func (c *CRM) Declarations() []*pb.AuthorityDeclaration { return c.ledger.Declarations() }

// Manifest declares the CRM as an app (ADR-0010).
func (c *CRM) Manifest() platform.Manifest {
	return platform.Manifest{ID: "crm", Version: "1", Actions: c.ledger.Catalog, Reads: []string{"customers"}, Entities: Entities(),
		Consumes: []platform.Consumption{{Protocol: lodging.ID, Optional: true}}}
}

// Read "customers": accounts with their opportunities and stays. Plain lists
// of accounts and opportunities are the platform's (/v1/records/<type>).
func (c *CRM) Read(who platform.Caller, name string) (any, *kernel.Error) {
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

// customers shows each opportunity with the stays linked to it, from whichever
// provider holds them: a stay booked before the tenant switched providers stays.
func (c *CRM) customers(who platform.Caller) (any, *kernel.Error) {
	results, err := who.Query(lodging.ID, "bookings")
	if err != nil {
		return nil, err
	}
	bookings := map[string]lodging.Booking{} // "<type>/<id>" → booking
	for _, a := range results {
		for _, b := range a.Result.([]lodging.Booking) {
			bookings[a.Type+"/"+b.ID] = b
		}
	}
	out := []Customer{}
	opportunities := platform.Records[Opportunity](who)
	for _, a := range platform.Records[Account](who) {
		customer := Customer{Account: a, Opportunities: []OpportunityStays{}}
		for _, o := range opportunities {
			if string(o.Account) != a.ID {
				continue
			}
			stays := []lodging.Booking{}
			for _, e := range who.Links(OpportunityType + "/" + o.ID) {
				if b, ok := bookings[e]; ok {
					stays = append(stays, b)
				}
			}
			customer.Opportunities = append(customer.Opportunities, OpportunityStays{Opportunity: o, Stays: stays})
		}
		out = append(out, customer)
	}
	return out, nil
}

func (c *CRM) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
}
