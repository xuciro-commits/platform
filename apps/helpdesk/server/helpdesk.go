// Package helpdesk is the helpdesk reference app (ADR-0021 D10 (2), ADR-0017's
// second proof): customers' tickets with a lifecycle, a service level kept by
// a flow, and a triage agent. The agent classifies a new ticket, grounds itself
// in what the tenant knows of the customer — their account, opportunities and
// stays, through the context graph and search, whatever apps hold them — and
// replies. Its reply is mailed only after a person approves it: the reply's
// mail is an irreversible effect, held when an agent causes it (ADR-0014 D6).
// Late tickets are escalated to the desk's leads.
package helpdesk

import (
	"embed"
	"encoding/json"
	"fmt"
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

const (
	TicketType   = "helpdesk.ticket"
	SchemaOpen   = "helpdesk.ticket.open"
	SchemaTriage = "helpdesk.ticket.triage"
	SchemaReply  = "helpdesk.ticket.reply"
	SchemaClose  = "helpdesk.ticket.close"
	SchemaLate   = "helpdesk.ticket.escalate"
	EffectReply  = "reply"
	Desk         = "desk" // answers tickets
	Lead         = "lead" // leads the desk: late tickets come to them
)

// Levels are the service levels: how soon a ticket of a priority is answered.
var Levels = map[string]time.Duration{"urgent": time.Hour, "high": 4 * time.Hour, "normal": 24 * time.Hour, "low": 72 * time.Hour}

// Ticket is a customer's request.
type Ticket struct {
	platform.Record
	Subject   string    `json:"subject" field:"required,search"`
	Body      string    `json:"body,omitempty" type:"longtext"`
	Customer  string    `json:"customer" field:"required,search" title:"Customer e-mail"`
	Account   string    `json:"account,omitempty" field:"search" title:"Customer account"` // the CRM's, by ID; the helpdesk knows no CRM
	Category  string    `json:"category,omitempty" field:"readonly" choices:"billing,booking,technical,other"`
	Priority  string    `json:"priority" field:"readonly" choices:"urgent,high,normal,low"`
	Status    string    `json:"status" field:"readonly" choices:"new,triaged,answered,closed"`
	Due       time.Time `json:"due" field:"readonly" title:"Answer due"`
	Escalated bool      `json:"escalated,omitempty" field:"readonly"`
	Reply     string    `json:"reply,omitempty" field:"readonly" type:"longtext"`
	Replied   string    `json:"replied,omitempty" field:"readonly" title:"Replied by"`
}

// Entities declares the ticket and its lifecycle.
func Entities() []platform.Entity {
	both := []string{Desk, Lead}
	return []platform.Entity{{Type: TicketType, Title: "Ticket", Model: Ticket{}, Display: "subject",
		Lifecycle: &platform.Lifecycle{Field: "status", Initial: "new",
			States: []platform.State{{Name: "new", Title: "New", Tone: "info"}, {Name: "triaged", Title: "Triaged", Tone: "info"},
				{Name: "answered", Title: "Answered", Tone: "success"}, {Name: "closed", Title: "Closed", Tone: "neutral"}},
			Transitions: []platform.Transition{
				{Name: "triage", Title: "Triage", From: []string{"new", "triaged"}, To: []string{"triaged"}, Roles: both,
					Description: "Classify a ticket; its priority sets when its answer is due.",
					Payload: []platform.Field{{Name: "category", Type: "string", Required: true, Description: "billing, booking, technical or other"},
						{Name: "priority", Type: "string", Required: true, Description: "urgent (1 hour), high (4 hours), normal (1 day) or low (3 days)"}},
					Do: triage},
				{Name: "reply", Title: "Reply", From: []string{"new", "triaged"}, To: []string{"answered"}, Roles: both,
					Description: "Answer the customer; the reply is mailed to them.",
					Payload:     []platform.Field{{Name: "reply", Type: "string", Required: true, Description: "The answer, as the customer reads it"}},
					Do:          reply, After: mail},
				{Name: "close", Title: "Close", From: []string{"answered"}, To: []string{"closed"}, Roles: both,
					Description: "Close an answered ticket."},
				{Name: "escalate", Title: "Escalate", From: []string{"new", "triaged"}, To: []string{"new", "triaged"}, Roles: []string{Lead},
					Description: "Mark a ticket late: its leads are told.", Do: escalate, After: late},
			}}}}
}

func invalid() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT} }

func triage(c platform.Caller, record any, payload json.RawMessage, _ time.Time) *kernel.Error {
	t := record.(*Ticket)
	var p struct{ Category, Priority string }
	json.Unmarshal(payload, &p)
	if _, ok := Levels[p.Priority]; !ok || !strings.Contains("billing,booking,technical,other", p.Category) || p.Category == "" {
		return invalid()
	}
	t.Category, t.Priority, t.Due = p.Category, p.Priority, t.Created.At.Add(Levels[p.Priority])
	return nil
}

func reply(c platform.Caller, record any, payload json.RawMessage, _ time.Time) *kernel.Error {
	t := record.(*Ticket)
	var p struct{ Reply string }
	json.Unmarshal(payload, &p)
	if strings.TrimSpace(p.Reply) == "" {
		return invalid()
	}
	t.Reply, t.Replied = strings.TrimSpace(p.Reply), c.ID
	return nil
}

// mail sends the reply to the customer; an agent's waits for a person (D6).
func mail(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
	t := record.(*Ticket)
	c.Emit(EffectReply, t.ID+":"+r.GetChangeId(), TicketType+"/"+t.ID,
		map[string]string{"to": t.Customer, "subject": "Re: " + t.Subject, "body": t.Reply, "ticket": t.ID}, now)
}

func escalate(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	record.(*Ticket).Escalated = true
	return nil
}

func late(c platform.Caller, _ *pb.ChangeRecord, record any, now time.Time) {
	t := record.(*Ticket)
	c.Notify(platform.Notification{Title: "Late: " + t.Subject, Body: fmt.Sprintf("Its answer was due %s.", t.Due.Format(time.DateTime)),
		Ref: TicketType + "/" + t.ID, Key: "late:" + t.ID}, now, platform.Recipient{AppRole: Lead})
}

func Actions() *platform.Catalog {
	return platform.NewCatalog(append([]platform.Action{{Schema: SchemaOpen, Target: TicketType, Capability: "tickets", Title: "Open ticket",
		Description: "Log a customer's request, from mail or the phone.", Roles: []string{Desk, Lead},
		Payload: []platform.Field{{Name: "subject", Type: "string", Required: true, Description: "Subject"},
			{Name: "body", Type: "string", Description: "What the customer wrote"},
			{Name: "customer", Type: "string", Required: true, Description: "The customer's e-mail"},
			{Name: "account", Type: "string", Description: "The customer's account ID, if known"}}}},
		platform.EntityActions(Entities()[0])...)...)
}

type App struct {
	mu     sync.Mutex
	tenant string
	ledger *platform.Ledger
}

func New(tenant string) *App {
	return &App{tenant: tenant, ledger: platform.NewLedger(tenant, "helpdesk", Actions(), TicketType)}
}

func (a *App) Snapshot() (json.RawMessage, error) { return a.ledger.Snapshot() }
func (a *App) Restore(raw json.RawMessage) error  { return a.ledger.Restore(raw) }

func (a *App) Manifest() platform.Manifest {
	return platform.Manifest{Languages: languages, ID: "helpdesk", Title: "Helpdesk", Version: "1", Actions: a.ledger.Catalog, Entities: Entities(),
		Emits: []platform.EffectKind{{Name: EffectReply, Title: "Reply to the customer",
			Description: "A ticket's reply, for the mail gateway to send to the customer.", Irreversible: true}},
		Flows: []platform.Flow{serviceLevel()}, Agents: []platform.Agent{triager()}}
}

func (a *App) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }

func (a *App) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}

func (a *App) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (a *App) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if record, err, ok := a.ledger.Generated(c, s, now, nil, Entities()...); ok {
		return record, err
	}
	return a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var p struct{ Subject, Body, Customer, Account string }
		if s.GetSchema().GetName() != SchemaOpen || json.Unmarshal(s.GetPayload(), &p) != nil || !strings.Contains(p.Customer, "@") {
			return nil, invalid()
		}
		if _, known := platform.Get[Ticket](c, s.GetTarget().GetId()); known {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		t := Ticket{Record: platform.Record{ID: s.GetTarget().GetId()}, Subject: strings.TrimSpace(p.Subject), Body: strings.TrimSpace(p.Body),
			Customer: strings.TrimSpace(p.Customer), Account: strings.TrimSpace(p.Account), Priority: "normal", Due: now.Add(Levels["normal"])}
		if err := c.Check(t); err != nil {
			return nil, err
		}
		return func(r *pb.ChangeRecord) { c.Put(r, t) }, nil
	})
}

// serviceLevel keeps a ticket's service level in two branches. One handles
// it: the triage agent classifies and answers it, or the desk does when the
// agent stops. The other keeps the time: when the ticket is still unanswered
// at its due time (which triage may move), its leads are told and asked to see
// to it. The first branch to end ends the other.
func serviceLevel() platform.Flow {
	type clock struct {
		Due time.Time `json:"due"`
	}
	ticket := func(c platform.Caller, r *platform.Run) Ticket { t, _ := platform.Get[Ticket](c, r.Key); return t }
	answered := func(c platform.Caller, r *platform.Run) bool {
		s := ticket(c, r).Status
		return s == "answered" || s == "closed"
	}
	desk := func(platform.Caller, *platform.Run) []platform.Recipient {
		return []platform.Recipient{{AppRole: Desk}}
	}
	leads := func(platform.Caller, *platform.Run) []platform.Recipient {
		return []platform.Recipient{{AppRole: Lead}}
	}
	ref := func(_ platform.Caller, r *platform.Run) string { return TicketType + "/" + r.Key }
	replied := func(_ platform.Caller, r *platform.Run, e platform.Event) bool {
		return e.Record.GetSubmission().GetTarget().GetId() == r.Key
	}
	return platform.Flow{Name: "service-level", Title: "Ticket service level", Version: 1, Owners: []string{Lead},
		Start: platform.Start{On: []string{SchemaOpen}, Begin: func(c platform.Caller, e platform.Event) (string, any, bool) {
			t, _ := platform.Get[Ticket](c, e.Record.GetSubmission().GetTarget().GetId())
			return t.ID, clock{Due: t.Due}, true
		}},
		Steps: []platform.Step{
			{Name: "service", Title: "Answered in time", Any: []string{"triage", "watch"}},
			{Name: "triage", Title: "The agent triages and answers", Next: "answered", Fault: "by-hand",
				Agent: &platform.AgentStep{Agent: "triage", To: desk, Ref: ref,
					Goal: func(c platform.Caller, r *platform.Run) string {
						t := ticket(c, r)
						return fmt.Sprintf("Triage and answer ticket %s from %s (account %s).\nSubject: %s\n%s", t.ID, t.Customer, cmpOr(t.Account, "unknown"), t.Subject, t.Body)
					}}},
			{Name: "by-hand", Title: "The desk triages and answers", Next: "answered",
				Ask: &platform.Ask{To: desk, Ref: ref, On: SchemaReply, Match: replied,
					Title: func(c platform.Caller, r *platform.Run) string { return "Triage and answer: " + ticket(c, r).Subject }}},
			{Name: "answered", Title: "Answered", Wait: &platform.Wait{Until: answered}},
			{Name: "watch", Title: "Until it is due",
				Wait: &platform.Wait{Until: func(c platform.Caller, r *platform.Run) bool {
					return answered(c, r) || !ticket(c, r).Due.Equal(platform.DataOf[clock](r).Due)
				}, At: func(_ platform.Caller, r *platform.Run) time.Time { return platform.DataOf[clock](r).Due }},
				Choose: func(c platform.Caller, r *platform.Run) (string, string) {
					t := ticket(c, r)
					switch {
					case answered(c, r):
						return "", "answered by " + t.Replied
					case !t.Due.Equal(platform.DataOf[clock](r).Due):
						r.Set(clock{Due: t.Due})
						return "watch", "now due " + t.Due.Format(time.DateTime)
					}
					return "escalate", "unanswered when due"
				}},
			{Name: "escalate", Title: "Its leads are told", Next: "lead",
				Act: &platform.Act{Action: SchemaLate, Target: func(_ platform.Caller, r *platform.Run) string { return r.Key }}},
			{Name: "lead", Title: "A lead sees to it",
				Ask: &platform.Ask{To: leads, Ref: ref, On: SchemaReply, Match: replied,
					Title: func(c platform.Caller, r *platform.Run) string { return "Late ticket: " + ticket(c, r).Subject },
					Body: func(c platform.Caller, r *platform.Run) string {
						return "Answer it, or have it answered: it was due " + ticket(c, r).Due.Format(time.DateTime)
					}}},
		}}
}

// triager is the triage agent. It may triage and reply; a reply promising
// money back is for people (its guard), and its mail waits for a person (D6).
func triager() platform.Agent {
	return platform.Agent{Name: "triage", Title: "Ticket triage",
		Instructions: `You triage and answer a customer's helpdesk ticket.
1. Find out who the customer is: search for their account ID or e-mail, and read the context of their account, opportunities and stays (bookings).
2. Look up what the house says about the matter with knowledge (house rules, FAQs), and rely on it.
3. Triage the ticket: its category (billing, booking, technical, other) and priority (urgent, high, normal, low), with helpdesk_ticket_triage.
4. Reply with helpdesk_ticket_reply: short, polite, specific to what you found (name the stay or booking concerned, and the rule you rely on). Never promise refunds, discounts or compensation; say a colleague will follow up instead.
5. Finish with a one-line summary.`,
		Tools:  []string{SchemaTriage, SchemaReply},
		Budget: platform.Budget{Steps: 10, Actions: 2},
		Guard: func(_ platform.Caller, _ platform.AgentRun, action, _ string, payload json.RawMessage) *kernel.Error {
			text := strings.ToLower(string(payload))
			if action == SchemaReply && (strings.Contains(text, "refund") || strings.Contains(text, "compensat") || strings.Contains(text, "discount")) {
				return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED} // money is for people
			}
			return nil
		},
		To: func(platform.Caller, platform.AgentRun) []platform.Recipient {
			return []platform.Recipient{{AppRole: Desk}}
		}}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
