package platform

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"slices"
	"strings"
	"time"
)

// AlertRule is the portable notification configuration, independent of its
// editable owner record and of notifications already delivered.
type AlertRule struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Object   AssetRef `json:"object"`
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Value    string   `json:"value,omitempty"`
	Message  string   `json:"message"`
	Role     string   `json:"role,omitempty"`
	Every    bool     `json:"every,omitempty"`
	Active   bool     `json:"active"`
}

func (a AlertRule) Check() error {
	if a.Name == "" || strings.TrimSpace(a.Title) == "" || a.Object.Check() != nil || a.Object.Kind != AssetObject || a.Field == "" || strings.TrimSpace(a.Message) == "" || !slices.Contains([]string{"=", "!=", "<", "<=", ">", ">=", "empty", "not empty"}, a.Operator) {
		return fmt.Errorf("alert needs a name, title, object, field, operator and message")
	}
	return nil
}

// Assignment asks people to do something about a record: a task in
// their inbox, due by a time, escalated when it is late. To lists who may take
// it; Key keeps one open task per app and key.
type Assignment struct {
	Close bool // close this owner's existing task identified by Key
	Title string
	Body  string
	Ref   string // the record it is about, "<type>/<id>"
	To    []Recipient
	Due   time.Time
	Key   string
	// Answers are what the person may answer (a flow's Ask); none: "done".
	Answers []string
}

// Assign creates a task as part of the decision r (inside its input, so
// replay creates it again).
func (c Caller) Assign(r *pb.ChangeRecord, a Assignment) *kernel.Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Assign(c, r, a)
}

// WorkHost provides the original approval/task owner's actual collaborators.
type WorkHost interface {
	Action(string) (string, Action, bool)
	Member(string) (Member, bool)
	Caller(Caller, Member, string) Caller
	Automation(Caller, string) Caller
	Holding(string, string) []string
	Calendar(string, Date) Calendar
	Units(string, string, Date) []string
	Holders(string, string, string, Date) []string
	Recipients(Caller, time.Time, []Recipient) []string
	Attempt(Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
	Submit(Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
	ActiveRelease() string
	ApprovalRelease(string, string, string) (string, *kernel.Error)
	ApprovalExecution(Caller, ApprovalRequest) (WorkHost, *kernel.Error)
	Seen(Caller, ...string)
}

type AttachedWork interface{ AttachWork(WorkHost) }

type Tasks interface {
	Assign(c Caller, r *pb.ChangeRecord, a Assignment) *kernel.Error
	Close(c Caller, r *pb.ChangeRecord, id string)
}

// Observer sees each accepted input's events inside the input, before any
// delivery, and again in replay: the platform's own views derived from events
// (the timeline) show them in the same input. events are the protocol events
// the decision is.

const (
	WorkApp                   = "work"
	WorkTaskType              = "work.task"
	WorkApprovalType          = "work.approval"
	WorkViewType              = "work.view"
	WorkApprovalRequestSchema = "work.approval.request"
)

// ApprovalOwner creates the original owner's held approval, inside the
// caller's staged transaction after the host's private policy trial.
type ApprovalOwner interface {
	App
	RequestApproval(Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
}

// ApprovalRetirementOwner handles the original pending request through Work's
// own ledger. The host validates the exact private migration directive first.
type ApprovalRetirementOwner interface {
	App
	RetireApproval(Caller, string, string, time.Time) *kernel.Error
}

type ApprovalRetirementRuntime interface {
	CheckApprovalRetirement(Caller, string, string) (ApprovalRetirement, *kernel.Error)
}

type ApprovalRetirement struct{ Actor, Redirect, Release string }

func (c Caller) CheckApprovalRetirement(migration, approval string) (ApprovalRetirement, *kernel.Error) {
	runtime, ok := c.rt.(ApprovalRetirementRuntime)
	if !ok || !c.Staging() || c.Replaying {
		return ApprovalRetirement{}, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Approval retirement requires a private migration")
	}
	return runtime.CheckApprovalRetirement(c, migration, approval)
}

type ApprovalRequest struct {
	Record
	Action     string         `json:"action" field:"readonly,search" source:"record"`
	Title      string         `json:"title" field:"readonly,search" source:"record"`
	App        string         `json:"app" field:"readonly" source:"record"`
	Target     string         `json:"target" field:"readonly,search" source:"record"` // "<type>/<id>"
	Requester  string         `json:"requester" field:"readonly" source:"record"`
	Submission string         `json:"submission" field:"readonly" type:"longtext"` // the held submission, as protojson
	Level      int            `json:"level" field:"readonly"`                      // the level deciding now
	Levels     []ApprovalStep `json:"levels" field:"readonly"`
	State      string         `json:"state" field:"readonly" choices:"pending,approved,rejected,refused,withdrawn,redirected"`
	Outcome    string         `json:"outcome,omitempty" field:"readonly" source:"user-input"` // refusal or the approver's note
	RejectedBy string         `json:"rejectedBy,omitempty" field:"readonly" title:"Rejected by" source:"record"`
	// Release is the tenant's active release when the request opened; a later
	// activation may not change the held action under it.
	Release         string `json:"release,omitempty" field:"readonly" source:"record"`
	RedirectAction  string `json:"redirectAction,omitempty" field:"readonly" source:"record" title:"Redirect action"`
	RedirectRelease string `json:"redirectRelease,omitempty" field:"readonly" source:"record" title:"Redirect release"`
	RedirectTo      string `json:"redirectTo,omitempty" field:"readonly" source:"record" title:"Redirect to"`
	RedirectedBy    string `json:"redirectedBy,omitempty" field:"readonly" source:"record" title:"Redirected by"`
}

type ApprovalStep struct {
	Title     string    `json:"title" source:"record"`
	Approvers []string  `json:"approvers"`
	All       bool      `json:"all,omitempty"`
	Count     int       `json:"count,omitempty"`
	Approved  []string  `json:"approved"`
	Due       time.Time `json:"due,omitzero"`
	// Delegates may decide for an approver away on the request's day
	// (delegate → approver); DecidedBy says who did (approver → delegate).
	Delegates map[string]string `json:"delegates,omitempty"`
	DecidedBy map[string]string `json:"decidedBy,omitempty"`
}

type WorkDelegation struct {
	Record
	From  string `json:"from" field:"readonly" title:"Delegated by"`
	To    string `json:"to" field:"required" title:"Delegate"`
	Start Date   `json:"start" field:"required" type:"date" title:"First day"`
	End   Date   `json:"end" field:"required" type:"date" title:"Last day"`
}

type WorkTask struct {
	Record
	Title      string    `json:"title" field:"readonly,search" source:"generated"`
	Body       string    `json:"body,omitempty" field:"readonly" type:"longtext" source:"generated"`
	Withheld   bool      `json:"withheld,omitempty" field:"readonly"`
	Ref        string    `json:"ref,omitempty" field:"readonly" source:"record"`
	App        string    `json:"app" field:"readonly" source:"record"`
	Key        string    `json:"key,omitempty" field:"readonly" source:"record"`
	Candidates []string  `json:"candidates" field:"readonly"`
	Assignee   string    `json:"assignee,omitempty" field:"readonly" source:"record"`
	Due        time.Time `json:"due,omitzero" field:"readonly"`
	State      string    `json:"state" field:"readonly" choices:"open,done,canceled"`
	Answers    []string  `json:"answers,omitempty" field:"readonly"` // what the person may answer (a flow's Ask)
	Answer     string    `json:"answer,omitempty" field:"readonly"`
}

type SavedView struct {
	Record
	Title  string `json:"title" field:"required,search" source:"record"`
	Entity string `json:"entity" field:"required,readonly" source:"record"`
	State  string `json:"state" type:"longtext"`
	Owner  string `json:"owner" field:"readonly" source:"record"`
}

type AcceptedEventPlan struct {
	App         string   `json:"app"`
	Changed     []string `json:"changed"`
	Names       []string `json:"names,omitempty"`
	Subscribers []string `json:"subscribers,omitempty"`
	Effects     []Effect `json:"effects,omitempty"`
	Observed    bool     `json:"observed,omitempty"`
	Hops        int      `json:"hops,omitempty"`
	Stopped     []string `json:"stopped,omitempty"`
}

// ScheduledTask is the saved scheduling outcome; live callbacks stay in the scheduler.
type ScheduledTask struct {
	Applications []AssetBinding `json:"applications,omitempty"`
	ID           string         `json:"id" source:"record"`
	Kind         string         `json:"kind" source:"record"` // delivery, job
	App          string         `json:"app" source:"record"`  // the subscriber, or the job's app
	Title        string         `json:"title" source:"record"`
	State        string         `json:"state" source:"record"` // queued, retrying, failed, scheduled
	Attempts     int            `json:"attempts"`
	Last         time.Time      `json:"last,omitzero"`
	Due          time.Time      `json:"due,omitzero"`
	Error        string         `json:"error,omitempty"`
	Withheld     bool           `json:"withheld,omitempty"`
}

type ScheduledTaskState struct {
	ScheduledTask
	Since    int                `json:"since,omitempty"`
	EventApp string             `json:"eventApp,omitempty"` // the event it delivers
	Event    json.RawMessage    `json:"event,omitempty"`
	Hops     int                `json:"hops,omitempty"`
	Changed  []string           `json:"changed,omitempty"` // the records its decision put
	Plan     *AcceptedEventPlan `json:"plan,omitempty"`    // accepted-result delivery routing
}
