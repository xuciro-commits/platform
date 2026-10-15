package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

const ConfirmationType = "build.confirmation"
const ConfirmationAnswerSchema = ConfirmationType + ".answer"

// Confirmation declares coordination after a single authority accepts its
// transaction. Each party's request, reply and compensation stays independent.
type Confirmation struct {
	Parties         []ConfirmationParty `json:"parties"`
	DeadlineSeconds int                 `json:"deadlineSeconds"`
	OnPartial       string              `json:"onPartial"`
	OnExpire        string              `json:"onExpire"`
	OnComplete      string              `json:"onComplete"`
	OnPartialAction string              `json:"onPartialAction,omitempty"`
	ReviewerRole    string              `json:"reviewerRole"`
}

type ConfirmationParty struct {
	Name                string            `json:"name"`
	Title               string            `json:"title"`
	Writeback           string            `json:"writeback,omitempty"`
	Protocol            string            `json:"protocol,omitempty"`
	Action              string            `json:"action,omitempty"`
	Target              string            `json:"target,omitempty"`
	CompensateWriteback string            `json:"compensateWriteback,omitempty"`
	CompensateAction    string            `json:"compensateAction,omitempty"`
	Reconcile           *ConfirmationRead `json:"reconcile,omitempty"`
}

type ConfirmationRead struct {
	Source       string          `json:"source"`
	KeyField     string          `json:"keyField"`
	ResultField  string          `json:"resultField"`
	Expected     json.RawMessage `json:"expected"`
	EverySeconds int             `json:"everySeconds"`
}

func (c Confirmation) Check() error {
	if len(c.Parties) < 1 || len(c.Parties) > 8 || c.DeadlineSeconds < 1 || c.DeadlineSeconds > 604800 || !slices.Contains([]string{"hold", "proceed-with-flag", "reject"}, c.OnPartial) || !slices.Contains([]string{"hold", "escalate", "compensate"}, c.OnExpire) || c.ReviewerRole == "" || c.OnComplete == "" {
		return fmt.Errorf("Confirmation needs 1–8 parties, a bounded deadline, completion action, reviewer and explicit partial/expiry policies")
	}
	if c.OnPartial != "hold" && c.OnPartialAction == "" {
		return fmt.Errorf("Partial confirmation needs an explicit outcome action")
	}
	seen := map[string]bool{}
	for _, p := range c.Parties {
		if !functionName.MatchString(p.Name) || p.Title == "" || seen[p.Name] || (p.Writeback == "") == (p.Protocol == "") || p.Protocol != "" && p.Action == "" {
			return fmt.Errorf("Each confirmation party needs a unique name, title and one declared transport")
		}
		seen[p.Name] = true
		if c.OnExpire == "compensate" && p.CompensateWriteback == "" && p.CompensateAction == "" {
			return fmt.Errorf("Compensation must be declared for every party")
		}
		if r := p.Reconcile; r != nil {
			if r.Source == "" || r.KeyField == "" || r.ResultField == "" || !json.Valid(r.Expected) || len(r.Expected) > 4096 || r.EverySeconds < 1 || r.EverySeconds > 86400 {
				return fmt.Errorf("Reconciliation needs a source, match/result fields, expected value and bounded period")
			}
		}
	}
	return nil
}

type ConfirmationRun struct {
	Record
	Subject         string                 `json:"subject" field:"readonly,search" source:"record"`
	Action          string                 `json:"action" field:"readonly" source:"record"`
	Member          string                 `json:"member" field:"readonly" source:"record"`
	State           string                 `json:"state" field:"readonly" choices:"pending,partial,confirmed,expired,rejected,compensating,compensated" source:"record"`
	Deadline        time.Time              `json:"deadline" field:"readonly"`
	Partial         bool                   `json:"partial" field:"readonly"`
	Definition      Confirmation           `json:"definition" field:"readonly,aside" type:"json"`
	Release         string                 `json:"release,omitempty" field:"readonly" source:"record"`
	Assets          []ReleaseAsset         `json:"assets" field:"readonly,aside" type:"json"`
	Input           json.RawMessage        `json:"input" field:"readonly,aside" type:"json"`
	Parties         []ConfirmationProgress `json:"parties" field:"readonly" type:"json"`
	Reviewers       []string               `json:"reviewers" field:"readonly"`
	Completion      string                 `json:"completion,omitempty" field:"readonly" source:"record"`
	CompletionError string                 `json:"completionError,omitempty" field:"readonly" source:"record"`
	Withheld        bool                   `json:"withheld,omitempty" field:"readonly"`
}

type ConfirmationProgress struct {
	Name                string                   `json:"name"`
	Title               string                   `json:"title"`
	State               string                   `json:"state"`
	Intent              string                   `json:"intent"`
	Key                 string                   `json:"key"`
	At                  time.Time                `json:"at,omitzero"`
	Answer              json.RawMessage          `json:"answer,omitempty"`
	Reason              string                   `json:"reason,omitempty"`
	Request             json.RawMessage          `json:"request,omitempty"`
	RequestVersion      int                      `json:"requestVersion,omitempty"`
	Source              json.RawMessage          `json:"source,omitempty"`
	Connection          json.RawMessage          `json:"connection,omitempty"`
	SourceVersion       string                   `json:"sourceVersion,omitempty"`
	CompensationRequest json.RawMessage          `json:"compensationRequest,omitempty"`
	Compensation        string                   `json:"compensation,omitempty"`
	CompensationState   string                   `json:"compensationState,omitempty"`
	ReconcileDue        time.Time                `json:"reconcileDue,omitzero"`
	Differences         []ConfirmationDifference `json:"differences,omitempty"`
}

type ConfirmationDifference struct {
	At       time.Time       `json:"at"`
	Matched  bool            `json:"matched"`
	Observed json.RawMessage `json:"observed,omitempty"`
	Error    string          `json:"error,omitempty"`
}
