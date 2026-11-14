package build

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
)

type DispatchTarget struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type DispatchTargetHooks struct {
	Admits     func(platform.Member) *kernel.Error
	Caller     func() platform.Caller
	Automation func() platform.Caller
	Readable   func(member platform.Member, subject string, at time.Time) bool
}

func DispatchTargets(h DispatchTargetHooks, member platform.Member, id string, at time.Time) ([]DispatchTarget, *kernel.Error) {
	if refusal := h.Admits(member); refusal != nil {
		return nil, refusal
	}
	run, found := platform.Get[platform.DispatchRun](h.Caller(), id)
	if !found || member.Agent || !member.Holds(ID, run.Definition.ReviewerRole) || !h.Readable(member, run.Subject, at) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the current dispatch reviewer may choose an external target")
	}
	out := []DispatchTarget{}
	for offset := 0; ; {
		connections, total, refusal := platform.Find[dataowner.Connection](h.Automation(), platform.Query{Domain: json.RawMessage(`[["state","=","ready"]]`), Sort: []string{"title", "id"}, Limit: 500, Offset: offset})
		if refusal != nil {
			return nil, refusal
		}
		for _, connection := range connections {
			if slices.Contains([]string{"http", "odata"}, connection.Kind) {
				out = append(out, DispatchTarget{ID: connection.ID, Title: connection.Title})
			}
		}
		offset += len(connections)
		if offset >= total {
			return out, nil
		}
	}
}

func ProcessIssueNode(message string) string {
	parts := strings.SplitN(message, ": ", 2)
	if len(parts) != 2 {
		return ""
	}
	if node, ok := strings.CutPrefix(parts[0], "Node "); ok {
		return node
	}
	node, _ := strings.CutPrefix(parts[0], "step ")
	if node == parts[0] {
		return ""
	}
	return node
}

type ProcessDiagnostic struct {
	Node    string `json:"node,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ProcessDiagnostics struct {
	Valid  bool                `json:"valid"`
	Issues []ProcessDiagnostic `json:"issues"`
}

func DiagnoseProcess(owner ConstructionDrafts, c platform.Caller, process Process) ProcessDiagnostics {
	answer := ProcessDiagnostics{Valid: true, Issues: []ProcessDiagnostic{}}
	if refusal := owner.CheckProcess(c, process); refusal != nil {
		answer.Valid = false
		answer.Issues = append(answer.Issues, ProcessDiagnostic{Node: ProcessIssueNode(refusal.Message), Code: refusal.Code.String(), Message: refusal.Message})
	}
	return answer
}
