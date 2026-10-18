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
	// activation may not change the held action under it (ADR-0011 D4).
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
	// (delegate → approver); DecidedBy says who did (approver → delegate) (ADR-0038).
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
