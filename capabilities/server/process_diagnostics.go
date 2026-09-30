package platformserver

type ProcessDiagnostic struct {
	Node    string `json:"node,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type ProcessDiagnostics struct {
	Valid  bool                `json:"valid"`
	Issues []ProcessDiagnostic `json:"issues"`
}
