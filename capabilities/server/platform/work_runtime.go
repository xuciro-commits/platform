package platform

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"time"
)

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

type ApprovalRequest struct {
	Record
	Action     string         `json:"action" field:"readonly,search"`
	Title      string         `json:"title" field:"readonly,search"`
	App        string         `json:"app" field:"readonly"`
	Target     string         `json:"target" field:"readonly,search"` // "<type>/<id>"
	Requester  string         `json:"requester" field:"readonly"`
	Submission string         `json:"submission" field:"readonly" type:"longtext"` // the held submission, as protojson
	Level      int            `json:"level" field:"readonly"`                      // the level deciding now
	Levels     []ApprovalStep `json:"levels" field:"readonly"`
	State      string         `json:"state" field:"readonly" choices:"pending,approved,rejected,refused,withdrawn"`
	Outcome    string         `json:"outcome,omitempty" field:"readonly"` // why a request was refused when it ran, or rejected
	RejectedBy string         `json:"rejectedBy,omitempty" field:"readonly" title:"Rejected by"`
	// Release is the tenant's active release when the request opened; a later
	// activation may not change the held action under it (ADR-0039 D4).
	Release string `json:"release,omitempty" field:"readonly"`
}

type ApprovalStep struct {
	Title     string    `json:"title"`
	Approvers []string  `json:"approvers"`
	All       bool      `json:"all,omitempty"`
	Count     int       `json:"count,omitempty"`
	Approved  []string  `json:"approved"`
	Due       time.Time `json:"due,omitzero"`
	// Delegates may decide for an approver away on the request's day
	// (delegate → approver); DecidedBy says who did (approver → delegate) (ADR-0028 D11).
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
	Title      string    `json:"title" field:"readonly,search"`
	Body       string    `json:"body,omitempty" field:"readonly" type:"longtext"`
	Ref        string    `json:"ref,omitempty" field:"readonly"`
	App        string    `json:"app" field:"readonly"`
	Key        string    `json:"key,omitempty" field:"readonly"`
	Candidates []string  `json:"candidates" field:"readonly"`
	Assignee   string    `json:"assignee,omitempty" field:"readonly"`
	Due        time.Time `json:"due,omitzero" field:"readonly"`
	State      string    `json:"state" field:"readonly" choices:"open,done,canceled"`
	Answers    []string  `json:"answers,omitempty" field:"readonly"` // what the person may answer (a flow's Ask)
	Answer     string    `json:"answer,omitempty" field:"readonly"`
}

type SavedView struct {
	Record
	Title  string `json:"title" field:"required,search"`
	Entity string `json:"entity" field:"required,readonly"`
	State  string `json:"state" type:"longtext"`
	Owner  string `json:"owner" field:"readonly"`
}
