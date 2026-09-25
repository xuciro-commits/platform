// Package hr is the HR reference app (ADR-0017 D7): people's leave requests,
// approved along the organisation — the manager, and the department head above
// five days. It is mostly declarations: the entity, its lifecycle and its
// approval chain; the rules are the few only HR knows.
package hr

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const (
	LeaveType    = "hr.leave"
	SchemaCreate = "hr.leave.create"
	SchemaSubmit = "hr.leave.submit"
	SchemaCancel = "hr.leave.cancel"
	Employee     = "employee"
	HR           = "hr"
	// Structure is where approvers are found: a manager of the requester's
	// unit, a head of it or of a unit above (ADR-0012).
	Structure = "management"
)

// Leave is a request for days off.
type Leave struct {
	platform.Record
	Employee string `json:"employee" field:"readonly,search"`
	Kind     string `json:"kind" field:"required" choices:"vacation,sick,unpaid"`
	From     string `json:"from" field:"required" type:"date" title:"First day"`
	Until    string `json:"until" field:"required" type:"date" title:"Last day"`
	Days     int    `json:"days" field:"readonly"`
	Note     string `json:"note,omitempty" type:"longtext"`
	State    string `json:"state" field:"readonly" choices:"draft,approved,canceled"`
}

// Entities declares the leave, its lifecycle and the approval of submitting it.
func Entities() []platform.Entity {
	return []platform.Entity{{Type: LeaveType, Title: "Leave request", Model: Leave{}, Display: "id",
		Scope: platform.Scope{Owner: "employee", Levels: map[string]string{Employee: platform.ScopeOwn, HR: platform.ScopeTenant}},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "info"}, {Name: "approved", Title: "Approved", Tone: "success"}, {Name: "canceled", Title: "Canceled", Tone: "neutral"}},
			Transitions: []platform.Transition{
				{Name: "submit", Title: "Submit for approval", From: []string{"draft"}, To: []string{"approved"}, Roles: []string{Employee},
					Description: "Ask for the leave; it is approved once the manager (and, above five days, the department head) agree.",
					Approval: &platform.Approval{Levels: []platform.ApprovalLevel{
						{Title: "Manager", Structure: Structure, Role: "manager", Due: 48 * time.Hour},
						{Title: "Department head", Structure: Structure, Role: "head", Due: 48 * time.Hour, When: longerThan(5)},
					}},
					Do: own},
				{Name: "cancel", Title: "Cancel", From: []string{"draft", "approved"}, To: []string{"canceled"}, Roles: []string{Employee, HR},
					Description: "Cancel a leave request: your own, or any when you work in HR.", Do: ownOrHR},
			}}}}
}

// longerThan applies an approval level to leaves of more than days.
func longerThan(days int) func(platform.Caller, *pb.Submission) bool {
	return func(c platform.Caller, s *pb.Submission) bool {
		l, _ := platform.Get[Leave](c, s.GetTarget().GetId())
		return l.Days > days
	}
}

func own(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	if record.(*Leave).Employee != c.ID {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return nil
}

func ownOrHR(c platform.Caller, record any, payload json.RawMessage, now time.Time) *kernel.Error {
	if c.Role() == HR || c.Replaying {
		return nil
	}
	return own(c, record, payload, now)
}

func Actions() *platform.Catalog {
	return platform.NewCatalog(append([]platform.Action{{Schema: SchemaCreate, Target: LeaveType, Capability: "leave", Title: "Draft leave request",
		Description: "Draft a request for days off, from its first to its last day.", Roles: []string{Employee},
		Payload: []platform.Field{{Name: "kind", Type: "string", Required: true, Description: "vacation, sick or unpaid"},
			{Name: "from", Type: "date", Required: true, Description: "First day"}, {Name: "until", Type: "date", Required: true, Description: "Last day"},
			{Name: "note", Type: "string", Description: "For the approvers"}}}},
		platform.EntityActions(Entities()[0])...)...)
}

type App struct {
	mu     sync.Mutex
	tenant string
	ledger *platform.Ledger
}

func New(tenant string) *App {
	return &App{tenant: tenant, ledger: platform.NewLedger(tenant, "hr", Actions(), LeaveType)}
}

// Snapshot and Restore: its records are the host's; the ledger is its own (ADR-0019 D6).
func (a *App) Snapshot() (json.RawMessage, error) { return a.ledger.Snapshot() }

func (a *App) Restore(raw json.RawMessage) error { return a.ledger.Restore(raw) }

func (a *App) Manifest() platform.Manifest {
	return platform.Manifest{ID: "hr", Title: "HR", Version: "1", Actions: a.ledger.Catalog, Entities: Entities()}
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
		var p struct{ Kind, From, Until, Note string }
		invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		if s.GetSchema().GetName() != SchemaCreate || json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, invalid
		}
		if _, known := platform.Get[Leave](c, s.GetTarget().GetId()); known {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		from, err1 := time.Parse(time.DateOnly, p.From)
		until, err2 := time.Parse(time.DateOnly, p.Until)
		if err1 != nil || err2 != nil || until.Before(from) {
			return nil, invalid
		}
		leave := Leave{Record: platform.Record{ID: s.GetTarget().GetId()}, Employee: c.ID, Kind: p.Kind, From: p.From, Until: p.Until,
			Days: int(until.Sub(from).Hours()/24) + 1, Note: strings.TrimSpace(p.Note)}
		if err := c.Check(leave); err != nil {
			return nil, err
		}
		return func(r *pb.ChangeRecord) { c.Put(r, leave) }, nil
	})
}
