package protocol

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"time"
)

type WorkHost interface {
	Action(string) (string, platform.Action, bool)
	Member(string) (platform.Member, bool)
	Caller(platform.Caller, platform.Member, string) platform.Caller
	Automation(platform.Caller, string) platform.Caller
	Holding(string, string) []string
	Calendar(string, platform.Date) platform.Calendar
	Units(string, string, platform.Date) []string
	Holders(string, string, string, platform.Date) []string
	Recipients(platform.Caller, time.Time, []platform.Recipient) []string
	Attempt(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error)
	Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error)
	ActiveRelease() string
	ApprovalRelease(string, string, string) (string, *platform.Error)
	ApprovalExecution(platform.Caller, ApprovalRequest) (WorkHost, *platform.Error)
	Seen(platform.Caller, ...string)
}

const (
	WorkApp                   = "work"
	WorkTaskType              = "work.task"
	WorkApprovalType          = "work.approval"
	WorkViewType              = "work.view"
	WorkViewStateVersion      = 1
	MonitoringViewType        = "monitoring.view"
	WorkApprovalRequestSchema = "work.approval.request"
)

type ApprovalOwner interface {
	platform.App
	RequestApproval(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error)
}

type ApprovalRequest struct {
	platform.Record
	Action     string         `json:"action" field:"readonly,search" source:"record"`
	Title      string         `json:"title" field:"readonly,search" source:"record"`
	App        string         `json:"app" field:"readonly" source:"record"`
	Target     string         `json:"target" field:"readonly,search" source:"record"`
	Requester  string         `json:"requester" field:"readonly" source:"record"`
	Submission string         `json:"submission" field:"readonly" type:"longtext"`
	Level      int            `json:"level" field:"readonly"`
	Levels     []ApprovalStep `json:"levels" field:"readonly"`
	State      string         `json:"state" field:"readonly" choices:"pending,approved,rejected,refused,withdrawn"`
	Outcome    string         `json:"outcome,omitempty" field:"readonly" source:"user-input"`
	RejectedBy string         `json:"rejectedBy,omitempty" field:"readonly" title:"Rejected by" source:"record"`
	Release    string         `json:"release,omitempty" field:"readonly" source:"record"`
}

type ApprovalStep struct {
	Title     string            `json:"title" source:"record"`
	Approvers []string          `json:"approvers"`
	All       bool              `json:"all,omitempty"`
	Count     int               `json:"count,omitempty"`
	Approved  []string          `json:"approved"`
	Due       time.Time         `json:"due,omitzero"`
	Delegates map[string]string `json:"delegates,omitempty"`
	DecidedBy map[string]string `json:"decidedBy,omitempty"`
}

type WorkDelegation struct {
	platform.Record
	From  string        `json:"from" field:"readonly" title:"Delegated by"`
	To    string        `json:"to" field:"required" title:"Delegate"`
	Start platform.Date `json:"start" field:"required" type:"date" title:"First day"`
	End   platform.Date `json:"end" field:"required" type:"date" title:"Last day"`
}

type WorkTask struct {
	platform.Record
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
	Answers    []string  `json:"answers,omitempty" field:"readonly"`
	Answer     string    `json:"answer,omitempty" field:"readonly"`
}

type SavedView struct {
	platform.Record
	Title  string `json:"title" field:"required,search" source:"record"`
	Entity string `json:"entity" field:"required,readonly" source:"record"`
	State  string `json:"state" type:"longtext" source:"record"`
	Owner  string `json:"owner" field:"readonly" source:"record"`
}

func CheckWorkViewState(raw string) error {
	var state struct {
		Version int `json:"version"`
	}
	if json.Unmarshal([]byte(raw), &state) != nil {
		return fmt.Errorf("saved view state must be a JSON object")
	}
	if state.Version != WorkViewStateVersion {
		return fmt.Errorf("saved view state version %d is not supported", state.Version)
	}
	return nil
}

type AcceptedEventPlan struct {
	App         string            `json:"app"`
	Changed     []string          `json:"changed"`
	Names       []string          `json:"names,omitempty"`
	Subscribers []string          `json:"subscribers,omitempty"`
	Effects     []platform.Effect `json:"effects,omitempty"`
	Observed    bool              `json:"observed,omitempty"`
	Hops        int               `json:"hops,omitempty"`
	Stopped     []string          `json:"stopped,omitempty"`
}

type ScheduledTask struct {
	Applications []platform.AssetBinding `json:"applications,omitempty"`
	ID           string                  `json:"id" source:"record"`
	Kind         string                  `json:"kind" source:"record"`
	App          string                  `json:"app" source:"record"`
	Title        string                  `json:"title" source:"record"`
	State        string                  `json:"state" source:"record"`
	Attempts     int                     `json:"attempts"`
	Last         time.Time               `json:"last,omitzero"`
	Due          time.Time               `json:"due,omitzero"`
	Error        string                  `json:"error,omitempty"`
	Withheld     bool                    `json:"withheld,omitempty"`
}

type ScheduledTaskState struct {
	ScheduledTask
	Since    int                `json:"since,omitempty"`
	EventApp string             `json:"eventApp,omitempty"`
	Event    json.RawMessage    `json:"event,omitempty"`
	Hops     int                `json:"hops,omitempty"`
	Changed  []string           `json:"changed,omitempty"`
	Plan     *AcceptedEventPlan `json:"plan,omitempty"`
}
