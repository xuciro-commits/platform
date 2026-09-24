// Package crm is a customer and opportunity package (#91), modelled on the
// account/opportunity core of Salesforce and Dynamics 365 Sales. It knows no
// other package; others use its declared actions and its public reads.
package crm

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

const (
	Authority       = "crm-server"
	AccountType     = "crm.account"
	OpportunityType = "crm.opportunity"

	SchemaAccount = "crm.account.create"
	SchemaOpen    = "crm.opportunity.open"
	SchemaClose   = "crm.opportunity.close"
	SchemaNote    = "crm.opportunity.note"

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
	Notes    []Note `json:"notes"` // the activity timeline, oldest first
}

// Note is an activity on an opportunity, by a person or by an app's automation.
type Note struct {
	At   time.Time `json:"at"`
	By   string    `json:"by"`
	Text string    `json:"text"`
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
		platformserver.Action{Schema: SchemaNote, Target: OpportunityType, Capability: "opportunities", Title: "Add note",
			Description: "Add an activity note to an opportunity's timeline.",
			Payload:     []platformserver.Field{{Name: "text", Type: "string", Required: true, Description: "What happened"}}, Roles: both},
	)
}

// CRM is one tenant's accounts and opportunities.
type CRM struct {
	mu            sync.Mutex
	tenant        string
	accounts      map[string]*Account
	opportunities map[string]*Opportunity
	ledger        *platformserver.Ledger
}

func New(tenant string) *CRM {
	return &CRM{tenant: tenant, accounts: map[string]*Account{}, opportunities: map[string]*Opportunity{},
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
				c.opportunities[id] = &Opportunity{ID: id, Account: p.Account, Title: p.Title, Owner: who.ID, Stage: "open", Revision: r.GetRevision(), Notes: []Note{}}
			}, nil
		case SchemaNote:
			o := c.opportunities[id]
			if strings.TrimSpace(p.Text) == "" {
				return nil, invalid
			}
			if o == nil {
				return nil, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
			}
			return func(r *pb.ChangeRecord) {
				o.Notes = append(o.Notes, Note{At: r.GetRecordedTime().AsTime(), By: who.ID, Text: p.Text})
				o.Revision = r.GetRevision()
			}, nil
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
	return platformserver.Manifest{ID: "crm", Version: "1", Actions: c.ledger.Catalog, Reads: []string{"accounts", "opportunities"}}
}

func (c *CRM) Read(_ platformserver.Caller, name string) (any, *kernel.Error) {
	if name == "accounts" {
		return c.Accounts(), nil
	}
	return c.Opportunities(), nil
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
