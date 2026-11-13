package monitoring

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

type MonitoringRun struct {
	Type               string                  `json:"type" enum:"sync,build,automation,function,delivery"`
	ID                 string                  `json:"id"`
	Source             string                  `json:"source" enum:"source,pipeline,code,projection,function,compute,work,effect,flow,agent"`
	Title              string                  `json:"title"`
	ResourceRef        *platform.AssetRef      `json:"resourceRef,omitempty"`
	RecordRef          string                  `json:"recordRef,omitempty"`
	When               time.Time               `json:"when,omitzero"`
	Status             string                  `json:"status" enum:"ok,failed,pending,running,warning,paused,cancelled,unknown"`
	OwnerStatus        string                  `json:"ownerStatus,omitempty"`
	Retryable          bool                    `json:"retryable"`
	RetrySchema        string                  `json:"retrySchema,omitempty"`
	RetryTarget        *MonitoringRetryTarget  `json:"retryTarget,omitempty"`
	RetrySchemaVersion *platform.SchemaVersion `json:"retrySchemaVersion,omitempty"`
	RetryKind          string                  `json:"retryKind,omitempty" enum:"retry,rerun"`
	Delayed            bool                    `json:"delayed,omitempty"`
}

type MonitoringRetryTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type MonitoringAlert struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Object  string `json:"object,omitempty"`
	Repeat  bool   `json:"repeat"`
	Message string `json:"message"`
	Role    string `json:"role"`
}

type MonitoringCoverage struct {
	Source     string `json:"source"`
	Readable   bool   `json:"readable"`
	Basis      string `json:"basis" enum:"latest,current,retained"`
	Limitation string `json:"limitation,omitempty"`
}

type MonitoringOverview struct {
	FailedRuns       int `json:"failedRuns"`
	DelayedSyncs     int `json:"delayedSyncs"`
	FailedDeliveries int `json:"failedDeliveries"`
}

type MonitoringRunPage struct {
	Runs     []MonitoringRun      `json:"runs"`
	Total    int                  `json:"total"`
	Offset   int                  `json:"offset"`
	Limit    int                  `json:"limit"`
	Overview MonitoringOverview   `json:"overview"`
	Coverage []MonitoringCoverage `json:"coverage"`
}

type MonitoringRunFilter struct {
	Type, Status  string
	Offset, Limit int
}

func (f MonitoringRunFilter) Valid() bool {
	return f.Offset >= 0 && f.Limit >= 1 && f.Limit <= 100 &&
		(f.Type == "" || slices.Contains([]string{"sync", "build", "automation", "function", "delivery"}, f.Type)) &&
		(f.Status == "" || slices.Contains([]string{"ok", "failed", "pending", "running", "warning", "paused", "cancelled", "unknown"}, f.Status))
}

type Runs struct {
	page     MonitoringRunPage
	filter   MonitoringRunFilter
	visible  map[platform.AssetRef]bool
	readable func(string) bool
	seen     map[string]bool
	Retry    func(run *MonitoringRun, schema, typ, id, kind string)
}

func NewRuns(filter MonitoringRunFilter, visible map[platform.AssetRef]bool, readable func(string) bool, retry func(*MonitoringRun, string, string, string, string)) *Runs {
	return &Runs{page: MonitoringRunPage{Runs: []MonitoringRun{}, Offset: filter.Offset, Limit: filter.Limit, Coverage: []MonitoringCoverage{}}, filter: filter, visible: visible, readable: readable, seen: map[string]bool{}, Retry: retry}
}

func (r *Runs) Add(run MonitoringRun) {
	if run.Title == "" {
		run.Title = run.ID
	}
	if run.ResourceRef != nil && !r.visible[*run.ResourceRef] {
		run.ResourceRef = nil
	}
	if run.RecordRef != "" {
		allowed, known := r.seen[run.RecordRef]
		if !known {
			allowed = r.readable(run.RecordRef)
			r.seen[run.RecordRef] = allowed
		}
		if !allowed {
			run.RecordRef = ""
		}
	}
	if run.Status == "failed" {
		r.page.Overview.FailedRuns++
		if run.Type == "delivery" {
			r.page.Overview.FailedDeliveries++
		}
	}
	if (r.filter.Type == "" || run.Type == r.filter.Type) && (r.filter.Status == "" || run.Status == r.filter.Status) {
		r.page.Runs = append(r.page.Runs, run)
	}
}

func (r *Runs) Delayed() { r.page.Overview.DelayedSyncs++ }

func (r *Runs) Cover(coverage ...MonitoringCoverage) {
	r.page.Coverage = append(r.page.Coverage, coverage...)
}

func (r *Runs) Partial() MonitoringRunPage { return r.page }

func (r *Runs) Page() MonitoringRunPage {
	out := r.page
	slices.SortFunc(out.Runs, func(a, b MonitoringRun) int {
		if order := b.When.Compare(a.When); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	out.Total = len(out.Runs)
	start := min(r.filter.Offset, out.Total)
	out.Runs = out.Runs[start : start+min(r.filter.Limit, out.Total-start)]
	return out
}

func RunResourceApp(run MonitoringRun) string {
	if run.ResourceRef != nil {
		return run.ResourceRef.App
	}
	return ""
}

func EffectStatus(state string) string {
	switch state {
	case "delivered":
		return "ok"
	case "failed", "rejected":
		return "failed"
	case "pending", "retrying":
		return "pending"
	case "held":
		return "paused"
	case "discarded", "compensated":
		return "cancelled"
	default:
		return "unknown"
	}
}

func (r *Runs) Flows(flows []platform.FlowInstance, retrySchema string) {
	for _, flow := range flows {
		app, _, _ := strings.Cut(flow.Flow, ".")
		run := MonitoringRun{Type: "automation", Source: "flow", ID: "flow:" + flow.ID, Title: cmp.Or(flow.Title, flow.Flow), ResourceRef: &platform.AssetRef{App: app, Kind: platform.AssetFlow, Name: flow.Flow}, RecordRef: platform.FlowInstanceType + "/" + flow.ID, When: flow.Changed.At, Status: "unknown", OwnerStatus: flow.State}
		if run.When.IsZero() {
			run.When = flow.Created.At
		}
		switch flow.State {
		case "running", "compensating":
			run.Status = "running"
		case "waiting":
			run.Status = "pending"
		case "done":
			run.Status = "ok"
		case "stuck", "compensated":
			run.Status = "failed"
		case "canceled":
			run.Status = "cancelled"
		}
		if flow.State == "stuck" && !flow.Archived {
			r.Retry(&run, retrySchema, platform.FlowInstanceType, flow.ID, "retry")
		}
		r.Add(run)
	}
}

func (r *Runs) Agents(agents []platform.AgentRunRecord) {
	for _, agent := range agents {
		app, name, _ := strings.Cut(agent.Agent, ".")
		run := MonitoringRun{Type: "function", Source: "agent", ID: "agent:" + agent.ID, Title: cmp.Or(agent.Title, agent.Agent), ResourceRef: &platform.AssetRef{App: app, Kind: platform.AssetAgent, Name: name}, RecordRef: platform.RunType + "/" + agent.ID, When: agent.Changed.At, Status: "unknown", OwnerStatus: agent.State}
		if run.When.IsZero() {
			run.When = agent.Created.At
		}
		switch agent.State {
		case "running":
			run.Status = "running"
		case "waiting":
			run.Status = "pending"
		case "done":
			run.Status = "ok"
		case "stopped":
			run.Status = "failed"
			if strings.HasPrefix(agent.Stopped, "stopped by ") {
				run.Status = "cancelled"
			}
		}
		r.Add(run)
	}
}

func FromIndex(entries []platform.RunIndexEntry, filter MonitoringRunFilter, coverage []MonitoringCoverage) MonitoringRunPage {
	out := MonitoringRunPage{Runs: []MonitoringRun{}, Offset: filter.Offset, Limit: filter.Limit, Coverage: coverage}
	for _, entry := range entries {
		var run MonitoringRun
		if json.Unmarshal(entry.Payload, &run) != nil {
			continue
		}
		if run.ID == "" {
			run.ID = entry.ID
		}
		if run.Delayed {
			out.Overview.DelayedSyncs++
		}
		if run.Status == "failed" {
			out.Overview.FailedRuns++
			if run.Type == "delivery" {
				out.Overview.FailedDeliveries++
			}
		}
		if (filter.Type == "" || run.Type == filter.Type) && (filter.Status == "" || run.Status == filter.Status) {
			out.Runs = append(out.Runs, run)
		}
	}
	out.Total = len(out.Runs)
	start := min(filter.Offset, out.Total)
	out.Runs = out.Runs[start:min(start+filter.Limit, out.Total)]
	return out
}
