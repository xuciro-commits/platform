package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

const DispatchType = "build.dispatch"
const DispatchAnswerSchema = DispatchType + ".answer"
const DispatchReviewSchema = DispatchType + ".review"
const DispatchTickSchema = DispatchType + ".tick"

// DispatchCommand belongs to the object's declared command table. Its
// writeback supplies the request format; addresses and credentials stay in
// the selected connection. ReceiverDeduplicates is an explicit dependency
// on the receiver, never a guarantee made by the platform.
type DispatchCommand struct {
	Name                 string `json:"name"`
	Title                string `json:"title"`
	EndpointKind         string `json:"endpointKind"`
	Writeback            string `json:"writeback"`
	Idempotent           bool   `json:"idempotent"`
	ReceiverDeduplicates bool   `json:"receiverDeduplicates,omitempty"`
	Compensate           string `json:"compensate,omitempty"`
}

func (c DispatchCommand) SafeRetry() bool { return c.Idempotent || c.ReceiverDeduplicates }

func (c DispatchCommand) Check() error {
	if !functionName.MatchString(c.Name) || strings.TrimSpace(c.Title) == "" || !functionName.MatchString(c.EndpointKind) || c.Writeback == "" || c.Compensate != "" && !functionName.MatchString(c.Compensate) {
		return fmt.Errorf("Dispatch commands need a name, title, endpoint kind and declared writeback")
	}
	return nil
}

type Dispatch struct {
	Command         string       `json:"command"`
	Target          string       `json:"target"`
	Payload         []TxnSet     `json:"payload,omitempty"`
	Key             string       `json:"key"`
	DeadlineSeconds int          `json:"deadlineSeconds"`
	Readback        DispatchRead `json:"readback"`
	OnStuck         string       `json:"onStuck"`
	OnFail          string       `json:"onFail"`
	ReviewerRole    string       `json:"reviewerRole"`
}

// Exactly one published source or observation supplies the status. States
// maps the external value to accepted/running/completed/stuck/failed/unknown.
type DispatchRead struct {
	Name         string            `json:"name"`
	Source       string            `json:"source,omitempty"`
	Observation  string            `json:"observation,omitempty"`
	Record       string            `json:"record,omitempty"`
	KeyField     string            `json:"keyField,omitempty"`
	ResultField  string            `json:"resultField,omitempty"`
	ReasonField  string            `json:"reasonField,omitempty"`
	States       map[string]string `json:"states"`
	EverySeconds int               `json:"everySeconds"`
}

func (d Dispatch) Check(command DispatchCommand) error {
	if err := command.Check(); err != nil {
		return err
	}
	if d.Command != command.Name || d.Target == "" || d.ReviewerRole == "" || d.DeadlineSeconds < 1 || d.DeadlineSeconds > 604800 {
		return fmt.Errorf("Dispatch needs a declared command, external target, bounded deadline and reviewer role")
	}
	if d.Key != "$subject" && !strings.HasPrefix(d.Key, "record.") {
		return fmt.Errorf("Dispatch business key must come from the subject record")
	}
	for _, policy := range []string{d.OnStuck, d.OnFail} {
		if !slices.Contains([]string{"manual", "escalate", "compensate", "retry"}, policy) {
			return fmt.Errorf("Dispatch needs explicit stuck and failure policies")
		}
		if policy == "retry" && !command.SafeRetry() {
			return fmt.Errorf("A command that cannot be safely resent cannot use automatic retry")
		}
		if policy == "compensate" && command.Compensate == "" {
			return fmt.Errorf("Dispatch compensation must name a declared reverse command")
		}
	}
	r := d.Readback
	methods := 0
	for _, source := range []string{r.Source, r.Observation, r.Record} {
		if source != "" {
			methods++
		}
	}
	if !functionName.MatchString(r.Name) || methods != 1 || (r.Source != "" || r.Record != "") && (r.KeyField == "" || r.ResultField == "") || r.EverySeconds < 1 || r.EverySeconds > 86400 || len(r.States) == 0 || len(r.States) > 64 || len(d.Payload) > 64 {
		return fmt.Errorf("Dispatch needs one named status read, status mapping and bounded period")
	}
	for value, state := range r.States {
		if value == "" || !slices.Contains([]string{"accepted", "running", "completed", "stuck", "failed", "unknown"}, state) {
			return fmt.Errorf("Dispatch status mapping contains an unknown state")
		}
	}
	return nil
}

type DispatchRun struct {
	Record
	Subject             string            `json:"subject" field:"readonly,search" source:"record"`
	Action              string            `json:"action" field:"readonly" source:"record"`
	Member              string            `json:"member" field:"readonly" source:"record"`
	Command             DispatchCommand   `json:"command" field:"readonly" type:"json"`
	Definition          Dispatch          `json:"definition" field:"readonly,aside" type:"json"`
	Target              string            `json:"target" field:"readonly" source:"record"`
	Key                 string            `json:"key" field:"readonly" source:"record"`
	State               string            `json:"state" field:"readonly" choices:"pending,accepted,running,completed,stuck,failed,unknown,cancelled,compensating,compensated" source:"record"`
	Reason              string            `json:"reason,omitempty" field:"readonly" source:"record"`
	StartedAt           time.Time         `json:"startedAt" field:"readonly"`
	StateAt             time.Time         `json:"stateAt" field:"readonly"`
	Deadline            time.Time         `json:"deadline" field:"readonly"`
	ReadDue             time.Time         `json:"readDue" field:"readonly"`
	Readback            []DispatchReading `json:"readback" field:"readonly" type:"json"`
	Attempts            []DispatchAttempt `json:"attempts" field:"readonly" type:"json"`
	Attempt             int               `json:"attempt" field:"readonly"`
	Intent              string            `json:"intent" field:"readonly" source:"record"`
	Request             json.RawMessage   `json:"request" field:"readonly,aside" type:"json"`
	Source              json.RawMessage   `json:"source,omitempty" field:"readonly,aside" type:"json"`
	Connection          json.RawMessage   `json:"connection,omitempty" field:"readonly,aside" type:"json"`
	Observation         json.RawMessage   `json:"observation,omitempty" field:"readonly,aside" type:"json"`
	Device              string            `json:"device,omitempty" field:"readonly" source:"record"`
	CompensationCommand *DispatchCommand  `json:"compensationCommand,omitempty" field:"readonly,aside" type:"json"`
	CompensationRequest json.RawMessage   `json:"compensationRequest,omitempty" field:"readonly,aside" type:"json"`
	Compensating        bool              `json:"compensating,omitempty" field:"readonly"`
	Release             string            `json:"release,omitempty" field:"readonly" source:"record"`
	Assets              []ReleaseAsset    `json:"assets" field:"readonly,aside" type:"json"`
	Input               json.RawMessage   `json:"input" field:"readonly,aside" type:"json"`
	Reviewers           []string          `json:"reviewers" field:"readonly"`
	Escalated           bool              `json:"escalated,omitempty" field:"readonly"`
	PolicyApplied       bool              `json:"policyApplied,omitempty" field:"readonly"`
	Withheld            bool              `json:"withheld,omitempty" field:"readonly"`
}

type DispatchAttempt struct {
	Number       int             `json:"number"`
	Intent       string          `json:"intent"`
	Target       string          `json:"target"`
	State        string          `json:"state"`
	At           time.Time       `json:"at"`
	Reason       string          `json:"reason,omitempty"`
	Answer       json.RawMessage `json:"answer,omitempty"`
	Compensation bool            `json:"compensation,omitempty"`
}

type DispatchReading struct {
	At       time.Time       `json:"at"`
	State    string          `json:"state"`
	Value    string          `json:"value,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Observed json.RawMessage `json:"observed,omitempty"`
}

type DispatchTransport interface {
	FreezeDispatch(Caller, string, string, Dispatch, time.Time) (string, []ReleaseAsset, error)
	QueueDispatch(Caller, *pb.ChangeRecord, DispatchRun, time.Time) *kernel.Error
	ResolveDispatchDelivery(Caller, DispatchRun, time.Time) *kernel.Error
	CheckDispatchRead(DispatchRun, time.Time) *kernel.Error
	RetargetDispatch(Caller, DispatchRun, string, time.Time) (json.RawMessage, json.RawMessage, *kernel.Error)
}

type DispatchRuntime interface{ DispatchTransport() DispatchTransport }

func (c Caller) DispatchTransport() DispatchTransport {
	rt, ok := c.rt.(DispatchRuntime)
	if !ok || !c.Staging() || c.Replaying {
		return nil
	}
	return rt.DispatchTransport()
}

func PendingDispatches(c Caller) ([]DispatchRun, *kernel.Error) {
	var all []DispatchRun
	for {
		rows, count, err := Find[DispatchRun](c, Query{Domain: json.RawMessage(`[["state","!=","completed"],["state","!=","cancelled"],["state","!=","compensated"]]`), Sort: []string{"id"}, Limit: 500, Offset: len(all)})
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if len(all) >= count {
			return all, nil
		}
		if len(rows) == 0 {
			return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The dispatch inventory changed while being read")
		}
	}
}
