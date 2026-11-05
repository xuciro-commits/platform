package platform

import "time"

type Observation struct {
	ID           string            `json:"id"`
	At           time.Time         `json:"at"`
	Level        string            `json:"level" enum:"debug,info,warn,error"`
	Source       string            `json:"source" enum:"request,submit,run,worker,data,app"`
	Tenant       string            `json:"tenant"`
	Member       string            `json:"member,omitempty"`
	App          string            `json:"app,omitempty"`
	Route        string            `json:"route,omitempty"`
	RequestID    string            `json:"requestId,omitempty"`
	TraceID      string            `json:"traceId,omitempty"`
	SpanID       string            `json:"spanId,omitempty"`
	RunID        string            `json:"runId,omitempty"`
	Resource     string            `json:"resource,omitempty"`
	SubmissionID string            `json:"submissionId,omitempty"`
	ChangeID     string            `json:"changeId,omitempty"`
	Outcome      string            `json:"outcome,omitempty" enum:"ok,refused,failed,pending"`
	Message      string            `json:"message"`
	Attributes   map[string]string `json:"attributes,omitempty"`
}

type ObservationQuery struct {
	Tenant    string
	From      time.Time
	To        time.Time
	Level     string
	Source    string
	App       string
	Member    string
	RequestID string
	TraceID   string
	RunID     string
	Resource  string
	Search    string
	Cursor    string
	Limit     int
}

type ObservationLogCoverage struct {
	RetainedFrom time.Time `json:"retainedFrom,omitzero"`
	Filtered     bool      `json:"filtered,omitempty"`
	Sampled      bool      `json:"sampled,omitempty"`
	Truncated    bool      `json:"truncated,omitempty"`
	Dropped      int64     `json:"dropped"`
}

type ObservationPage struct {
	Entries    []Observation          `json:"entries"`
	NextCursor string                 `json:"nextCursor,omitempty"`
	HasMore    bool                   `json:"hasMore"`
	Coverage   ObservationLogCoverage `json:"coverage"`
}
