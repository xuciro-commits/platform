package build

import (
	"cmp"
	"time"

	"platformserver/platform"
	"platformserver/services/resources/monitoring"
)

func CodeRuns(r *monitoring.Runs, codes []Code, compilations map[string]time.Time) {
	for _, code := range codes {
		if code.Call == "" {
			continue
		}
		run := monitoring.MonitoringRun{Type: "build", Source: "code", ID: "code:" + code.ID, Title: code.Title, ResourceRef: &platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: code.Name}, RecordRef: CodeType + "/" + code.ID, When: code.Changed.At, Status: "unknown", OwnerStatus: code.State}
		if at, known := compilations[code.Call]; known {
			run.When = at
		}
		switch code.State {
		case "compiling":
			run.Status = "pending"
		case "compiled", "published":
			run.Status = "ok"
		case "failed":
			run.Status = "failed"
			if !code.Archived {
				r.Retry(&run, SchemaCodeCompile, CodeType, code.ID, "rerun")
			}
		}
		r.Add(run)
	}
}

func FunctionRuns(r *monitoring.Runs, functions []FunctionRun) {
	for _, function := range functions {
		status := "unknown"
		switch function.State {
		case "pending":
			status = "pending"
		case "ready":
			status = "ok"
		case "rejected":
			status = "failed"
		}
		r.Add(monitoring.MonitoringRun{Type: "function", Source: "function", ID: "function:" + function.ID, Title: function.Function, ResourceRef: &platform.AssetRef{App: cmp.Or(function.App, ID), Kind: platform.AssetFunction, Name: function.Function}, RecordRef: FunctionCallType + "/" + function.ID, When: function.Created.At, Status: status, OwnerStatus: function.State})
	}
}
