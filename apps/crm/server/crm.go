// Package crm is a customer and opportunity app (#91), modelled on the
// account/opportunity core of Salesforce and Dynamics 365 Sales. It knows no
// other app. It consumes the lodging protocol when a tenant has a provider
// (ADR-0011): stays booked and rooms held for an opportunity are linked to it
// through the platform, and the platform's timeline tells what happens to them.
package crm

import (
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

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
	ID              = "crm"
	AccountType     = "crm.account"
	OpportunityType = "crm.opportunity"

	SchemaAccount = "crm.account.create"
	SchemaOpen    = "crm.opportunity.open"
	SchemaClose   = "crm.opportunity.close"
	SchemaBook    = "crm.opportunity.book"
	SchemaPlan    = "crm.opportunity.plan"
	SchemaAnswer  = "crm.opportunity.answer"

	Sales   Role = "sales"
	Manager Role = "sales-manager"
)

type Role string

// Account and Opportunity are the CRM's entity types (ADR-0016): the host
// keeps their records and gives them lists, record pages and history.
type Account struct {
	platform.Record
	Name string `json:"name" field:"required,search" help:"The customer's legal or everyday name" example:"Acme Corp"`
	Kind string `json:"kind" field:"required" choices:"company,person" help:"Whether the customer is an organisation or one person"`
	// Phone is personal data when the customer is a person: its reads are audited (ADR-0028 D4).
	Phone string `json:"phone,omitempty" personal:"contact" help:"The customer's phone number"`
}

type Opportunity struct {
	platform.Record
	Account platform.Ref[Account] `json:"account" field:"required"`
	Title   string                `json:"title" field:"required,search" help:"What is being sold, in the customer's words" example:"Board offsite, 12 rooms"`
	Owner   string                `json:"owner" field:"readonly" help:"The salesperson who owns it; only they and managers may close it"`
	// Margin is the expected margin: sales managers read and set it (ADR-0028 D3).
	Margin float64 `json:"margin,omitempty" type:"decimal" title:"Expected margin" read:"sales-manager" write:"sales-manager" help:"The margin the company expects, in percent; only sales managers see it"`
	Stage  string  `json:"stage" field:"readonly" choices:"open,won,lost" help:"open while it is being worked on; won or lost once closed"`
	// The group's block (ADR-0026 D6), as a hotel sales system keeps it: planned
	// rooms are held until a cutoff date; won, they are confirmed; lost, or past
	// the cutoff, they are released.
	Rooms    int    `json:"rooms,omitempty" field:"readonly" title:"Group rooms"`
	RoomType string `json:"roomType,omitempty" field:"readonly" title:"Room type"`
	Arrive   string `json:"arrive,omitempty" field:"readonly" type:"date"`
	Depart   string `json:"depart,omitempty" field:"readonly" type:"date"`
	Cutoff   string `json:"cutoff,omitempty" field:"readonly" type:"date" help:"The last day the rooms are held without being confirmed"`
	Block    string `json:"block,omitempty" field:"readonly" title:"Group block" choices:"holding,held,confirming,confirmed,releasing,released,failed" help:"Where the group's rooms stand with the provider"`
	Plans    int    `json:"plans,omitempty" field:"readonly"` // how many blocks were planned: the current one's rooms carry its number
	Stays    []Stay `json:"stays" field:"readonly" title:"Rooms and stays"`
}

// Stay is one room the opportunity asked of the lodging provider: held for the
// group's block, or booked on its own; its status is the provider's last answer.
type Stay struct {
	Booking string `json:"booking" title:"Booking"`
	Kind    string `json:"kind" choices:"hold,booking"`
	Plan    int    `json:"plan,omitempty"`
	Status  string `json:"status" choices:"asked,held,booked,refused,released,canceled" help:"asked until the provider, or a person, answers"`
	Detail  string `json:"detail,omitempty"`
	Manual  bool   `json:"manual,omitempty" help:"No provider was bound: a person asks the hotel and records its answers"`
}

// Entities declares the CRM's types. Accounts are master data with generated
// create, edit and archive actions (crm.account.create keeps its schema, so
// journals replay); opportunities move by the CRM's own actions, and a sales
// member sees their own (Salesforce's private default), a manager all of them.
func Entities() []platform.Entity {
	both := []string{string(Sales), string(Manager)}
	return []platform.Entity{
		{Type: AccountType, Title: "Account", Model: Account{}, Synonyms: "customer,client",
			Description: "A customer the company sells to: an organisation or a person.",
			Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: both, Capability: "accounts"}},
		{Type: OpportunityType, Title: "Opportunity", Model: Opportunity{}, Synonyms: "deal,lead",
			Description: "A chance to sell something to an account, followed until it is won or lost; stays for a group can be booked through the lodging protocol.",
			Scope:       platform.Scope{Owner: "owner", Levels: map[string]string{string(Sales): platform.ScopeOwn}},
			Standard:    platform.Standard{Edit: true, Roles: []string{string(Manager)}, Capability: "opportunities"}},
	}
}

// Actions is the CRM catalog (ADR-0008).
func Actions() *platform.Catalog {
	both := []string{string(Sales), string(Manager)}
	return platform.NewCatalog(append(append(platform.EntityActions(Entities()[0]), platform.EntityActions(Entities()[1])...),
		platform.Action{Schema: SchemaOpen, Target: OpportunityType, New: true, Capability: "opportunities", Title: "Open opportunity",
			Description: "Open a sales opportunity for an account; the caller owns it.",
			Payload: []platform.Field{{Name: "account", Type: "string", Required: true, Description: "Account ID", Ref: AccountType},
				{Name: "title", Type: "string", Required: true, Description: "What is being sold"}}, Roles: both},
		platform.Action{Schema: SchemaClose, Target: OpportunityType, Capability: "opportunities", Title: "Close opportunity",
			Description: "Close an open opportunity as won or lost; only its owner or a sales manager. Won with rooms held, it is won once the provider confirms them all, and stays open if it cannot.",
			Payload:     []platform.Field{{Name: "outcome", Type: "string", Required: true, Description: "won or lost", Choices: []string{"won", "lost"}}}, Roles: both},
		platform.Action{Schema: SchemaPlan, Target: OpportunityType, Capability: "stays", Title: "Plan group stay",
			Description: "Hold the rooms a group needs until a cutoff date: won, they are confirmed; lost, or past the cutoff, they are released. Refused at once when the provider cannot hold such a room; when it cannot hold them all, the block fails and the rooms held are given back.",
			Payload: []platform.Field{{Name: "rooms", Type: "integer", Required: true, Description: "Rooms, 1 to 20"},
				{Name: "roomType", Type: "string", Required: true, Description: "The provider's room type"},
				{Name: "arrive", Type: "date", Required: true, Description: "First night"}, {Name: "depart", Type: "date", Required: true, Description: "Departure"},
				{Name: "cutoff", Type: "date", Required: true, Description: "The last day the rooms are held"}},
			Roles: both},
		platform.Action{Schema: SchemaBook, Target: OpportunityType, Capability: "stays", Title: "Book stay",
			Description: "Book a stay for an opportunity with the tenant's lodging provider and link it to the opportunity; the provider decides with your role there.",
			Payload:     lodging.Protocol().Actions[0].Payload, Roles: both, Uses: []string{platform.ProtocolAction(lodging.ID, "reserve")}},
		platform.Action{Schema: SchemaAnswer, Target: OpportunityType, Capability: "stays", Title: "Record the provider's answer",
			Description: "Record how the lodging provider answered for one of the opportunity's rooms: the platform does it when a provider is bound; a person does it when the hotel answers by phone or email.",
			Payload:     platform.AnswerFields(), Roles: both},
	)...)
}

// CRM is one tenant's sales rules; its records are the host's.
type CRM struct {
	mu     sync.Mutex
	tenant string
	ledger *platform.Ledger
}

func New(tenant string) *CRM {
	return &CRM{tenant: tenant, ledger: platform.NewLedger(tenant, ID, Actions(), AccountType, OpportunityType)}
}

func fail(code pb.ErrorCode) *kernel.Error { return &kernel.Error{Code: code} }

func (c *CRM) Submit(who platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if who.Tenant != c.tenant {
		return nil, fail(pb.ErrorCode_ERROR_CODE_POLICY_DENIED)
	}
	if record, err, ok := c.ledger.Generated(who, s, now, nil, Entities()...); ok {
		return record, err
	}
	id := s.GetTarget().GetId()
	o, known := platform.Get[Opportunity](who, id)
	owns := func() bool { // closing and planning are for the owner or a manager
		schema := s.GetSchema().GetName()
		return schema != SchemaClose && schema != SchemaPlan || who.Role() == string(Manager) || known && o.Owner == who.ID
	}
	return c.ledger.Receive(who, s, now, owns, func() (func(*pb.ChangeRecord), *kernel.Error) {
		invalid := fail(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		var p struct {
			Account, Title, Outcome, RoomType, Arrive, Depart, Cutoff string
			Rooms                                                     int
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, invalid
		}
		if s.GetSchema().GetName() != SchemaOpen && !known {
			return nil, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
		}
		// asks requests each stay of the provider once the decision is accepted
		// (ADR-0026 D2); unbound, nothing is asked and a person records the answers.
		var asks []platform.Request
		ask := func(action, booking string, payload any) {
			asks = append(asks, platform.Request{Protocol: lodging.ID, Action: action, Target: booking, Payload: payload, Reply: SchemaAnswer})
		}
		switch s.GetSchema().GetName() {
		case SchemaOpen:
			if known {
				return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
			}
			o = Opportunity{Record: platform.Record{ID: id}, Account: platform.Ref[Account](p.Account), Title: strings.TrimSpace(p.Title), Owner: who.ID, Stage: "open", Stays: []Stay{}}
			if err := who.Check(o); err != nil {
				return nil, invalid
			}
		case SchemaBook:
			// The provider decides; it is asked first, so that what it cannot sell is refused at once.
			if o.Stage == "lost" {
				return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
			}
			booking := fmt.Sprintf("%s-B%d", id, len(o.Stays)+1)
			bound, err := probe(who, "reserve", booking, s.GetPayload(), now)
			if err != nil {
				return nil, err
			}
			o.Stays = append(o.Stays, Stay{Booking: booking, Kind: "booking", Status: "asked", Manual: !bound})
			if bound {
				ask("reserve", booking, json.RawMessage(s.GetPayload()))
			}
		case SchemaPlan:
			if o.Stage != "open" || o.Block == "holding" || o.Block == "held" || o.Block == "confirming" || o.Block == "releasing" {
				return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT) // one block at a time; a failed or released one may be planned again
			}
			if p.Rooms < 1 || p.Rooms > 20 || strings.TrimSpace(p.RoomType) == "" || p.Depart <= p.Arrive || p.Cutoff == "" || p.Cutoff >= p.Arrive {
				return nil, invalid
			}
			o.Rooms, o.RoomType, o.Arrive, o.Depart, o.Cutoff, o.Block = p.Rooms, p.RoomType, p.Arrive, p.Depart, p.Cutoff, "holding"
			o.Plans++
			hold := map[string]string{"roomType": p.RoomType, "checkIn": p.Arrive, "checkOut": p.Depart, "until": p.Cutoff}
			for i := range p.Rooms {
				booking := fmt.Sprintf("%s-H%d", id, len(o.Stays)+1)
				hold["guest"] = fmt.Sprintf("%s, room %d", o.Title, i+1)
				bound, err := probe(who, "hold", booking, hold, now)
				if err != nil {
					return nil, err
				}
				o.Stays = append(o.Stays, Stay{Booking: booking, Kind: "hold", Plan: o.Plans, Status: "asked", Manual: !bound})
				if bound {
					ask("hold", booking, maps.Clone(hold))
				}
			}
			if err := who.Check(o); err != nil {
				return nil, invalid
			}
		case SchemaClose:
			if p.Outcome != "won" && p.Outcome != "lost" {
				return nil, invalid
			}
			if o.Stage != "open" || o.Block == "confirming" {
				return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
			}
			// Won with rooms held, it stays open until the provider confirms them
			// all, and is won then; a failed confirmation leaves it open (F-40).
			// Lost, the rooms are given back.
			won := p.Outcome == "won"
			if o.Block != "held" || !won {
				o.Stage = p.Outcome
			}
			if o.Block == "held" || !won && o.Block == "failed" { // lost, what a failed confirmation left held is given back too
				action := map[bool]string{true: "confirm", false: "release"}[won]
				for _, st := range o.block() {
					if st.Manual || st.Status != "held" {
						continue
					}
					if won { // the provider would confirm each room, or it is refused at once
						if _, err := probe(who, action, st.Booking, struct{}{}, now); err != nil {
							return nil, err
						}
					}
					ask(action, st.Booking, struct{}{})
				}
				o.Block = map[bool]string{true: "confirming", false: "releasing"}[won]
			}
		case SchemaAnswer:
			var a platform.Answer
			if json.Unmarshal(s.GetPayload(), &a) != nil {
				return nil, invalid
			}
			i := slices.IndexFunc(o.Stays, func(st Stay) bool { return st.Booking == a.Call })
			if i < 0 {
				return nil, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
			}
			status, ok := answered(o.Stays[i], a)
			if !ok {
				return nil, invalid
			}
			o.Stays[i].Status, o.Stays[i].Detail = status, a.Code
			asks = append(asks, o.settle(o.Stays[i])...)
			return func(r *pb.ChangeRecord) {
				who.Put(r, o)
				if a.Ref != "" && a.Outcome == "accepted" && (a.Action == "reserve" || a.Action == "hold") {
					who.Link(&pb.EntityRef{Type: OpportunityType, Id: id}, refOf(a.Ref), "crm:link:"+a.Call, now)
				}
				for _, q := range asks {
					who.Request(r, q)
				}
			}, nil
		default:
			return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
		}
		return func(r *pb.ChangeRecord) {
			who.Put(r, o)
			for _, q := range asks {
				who.Request(r, q)
			}
		}, nil
	})
}

// probe asks the provider whether it would take the stay; bound is false when
// the tenant has no provider, and the stay then waits for a person's answer.
func probe(who platform.Caller, action, booking string, payload any, now time.Time) (bound bool, err *kernel.Error) {
	err = who.Probe(lodging.ID, action, booking, payload, now)
	if err != nil && err.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		return false, nil
	}
	return err == nil, err
}

// answered is a stay's status after the provider's answer to what it was asked.
func answered(st Stay, a platform.Answer) (string, bool) {
	switch {
	case a.Outcome == "released" || a.Outcome == "accepted" && a.Action == "release":
		return "released", st.Status == "held"
	case a.Outcome == "refused":
		return map[bool]string{true: "refused", false: st.Status}[st.Status == "asked"], st.Status != "released"
	case a.Outcome != "accepted":
		return "", false
	case a.Action == "hold":
		return "held", st.Status == "asked"
	case a.Action == "reserve", a.Action == "confirm":
		return "booked", st.Status == "asked" || st.Status == "held"
	}
	return "", false
}

// block are the current block's rooms.
func (o Opportunity) block() []Stay {
	var out []Stay
	for _, st := range o.Stays {
		if st.Kind == "hold" && st.Plan == o.Plans {
			out = append(out, st)
		}
	}
	return out
}

// settle moves the block on after an answer about one of its rooms, and says
// what to ask the provider next: a block that cannot be held whole is given
// back, a room held after that too.
func (o *Opportunity) settle(changed Stay) []platform.Request {
	if changed.Kind != "hold" || changed.Plan != o.Plans {
		return nil
	}
	release := func(st Stay) platform.Request {
		return platform.Request{Protocol: lodging.ID, Action: "release", Target: st.Booking, Payload: struct{}{}, Reply: SchemaAnswer}
	}
	count := map[string]int{}
	for _, st := range o.block() {
		count[st.Status]++
	}
	all := func(status string) bool { return count[status] == len(o.block()) }
	switch {
	case o.Block == "failed" && changed.Status == "held" && !changed.Manual:
		return []platform.Request{release(changed)}
	case o.Block == "holding" && (changed.Status == "refused" || changed.Status == "released"):
		o.Block = "failed"
		var out []platform.Request
		for _, st := range o.block() {
			if st.Status == "held" && !st.Manual {
				out = append(out, release(st))
			}
		}
		return out
	case o.Block == "holding" && all("held"):
		o.Block = "held"
	case o.Block == "confirming" && all("booked"):
		o.Block, o.Stage = "confirmed", "won"
	case o.Block == "confirming" && changed.Status != "booked":
		o.Block = "failed" // still open: planned again, or closed lost
	case (o.Block == "held" || o.Block == "releasing") && count["released"]+count["refused"] == len(o.block()):
		o.Block = "released"
	}
	return nil
}

func refOf(ref string) *pb.EntityRef {
	t, id, _ := strings.Cut(ref, "/")
	return &pb.EntityRef{Type: t, Id: id}
}

// Handle hears the provider release a held room on its own, past the cutoff,
// and records it as the provider's answer.
func (c *CRM) Handle(who platform.Caller, e platform.Event) *kernel.Error {
	booking := e.Record.GetSubmission().GetTarget().GetId()
	opp := booking[:max(strings.LastIndex(booking, "-"), 0)]
	o, known := platform.Get[Opportunity](who, opp)
	if !known || !slices.ContainsFunc(o.Stays, func(st Stay) bool { return st.Booking == booking && st.Status == "held" }) {
		return nil // not ours, or already answered
	}
	payload, _ := json.Marshal(platform.Answer{Call: booking, Action: "release", Outcome: "released"})
	_, err := c.Submit(who, &pb.Submission{TenantId: c.tenant, PrincipalId: who.ID, Authority: ID, Target: &pb.EntityRef{Type: OpportunityType, Id: opp},
		Schema: &pb.SchemaRef{Name: SchemaAnswer, Version: 1}, IdempotencyKey: "released:" + booking, Payload: payload}, e.Record.GetRecordedTime().AsTime())
	return err
}

func (c *CRM) Declarations() []*pb.AuthorityDeclaration { return c.ledger.Declarations() }

// Manifest declares the CRM as an app (ADR-0010).
// Snapshot and Restore: its records are the host's; the ledger is its own (ADR-0019 D6).
func (c *CRM) Snapshot() (json.RawMessage, error) { return c.ledger.Snapshot() }

func (c *CRM) Restore(raw json.RawMessage) error { return c.ledger.Restore(raw) }

func (c *CRM) Manifest() platform.Manifest {
	return platform.Manifest{Languages: languages, ID: ID, Title: "CRM", Version: "1", Actions: c.ledger.Catalog, Reads: []string{"customers"}, Entities: Entities(),
		Agents:     []platform.Agent{Assistant()},
		Consumes:   []platform.Consumption{{Protocol: lodging.ID, Optional: true}},
		Subscribes: []string{platform.ProtocolAction(lodging.ID, "released")}}
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
	Bookings []lodging.Booking `json:"bookings"` // as the providers hold them
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
			customer.Opportunities = append(customer.Opportunities, OpportunityStays{Opportunity: o, Bookings: stays})
		}
		out = append(out, customer)
	}
	return out, nil
}

func (c *CRM) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
}

// Assistant is the CRM's assistant (ADR-0021): asked about an account or an
// opportunity, it reads what the tenant knows and drafts the change — a new
// opportunity, a plan for rooms, an outcome — that the member confirms.
func Assistant() platform.Agent {
	return platform.Agent{Name: "assistant", Title: "Sales assistant",
		Instructions: `You help a salesperson with their accounts and opportunities. Read the record's context, and search when you need another record. When the goal asks for a change, make it with the one action that fits — open an opportunity, plan a group stay (rooms, room type, arrival, departure and the cutoff date until which the rooms are held), or close an opportunity won or lost — and the salesperson confirms it. When the goal only asks a question, finish with the answer. Never guess an ID: search for it.`,
		Tools:        []string{SchemaOpen, SchemaPlan, SchemaClose, "read:customers"},
		Budget:       platform.Budget{Steps: 8, Actions: 2}}
}
