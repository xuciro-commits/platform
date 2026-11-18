package build

import (
	"platformserver/kernel/protocol"
	"strings"

	"platformserver/platform"
)

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

func DiagnoseProcess(owner ConstructionDrafts, c platform.Caller, process protocol.Process) ProcessDiagnostics {
	answer := ProcessDiagnostics{Valid: true, Issues: []ProcessDiagnostic{}}
	if refusal := owner.CheckProcess(c, process); refusal != nil {
		answer.Valid = false
		answer.Issues = append(answer.Issues, ProcessDiagnostic{Node: ProcessIssueNode(refusal.Message), Code: refusal.Code.String(), Message: refusal.Message})
	}
	return answer
}
