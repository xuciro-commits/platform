package data

import (
	"cmp"
	"time"

	"platformserver/platform"
	"platformserver/services/resources/monitoring"
)

func (o *Owner) SourceRuns(r *monitoring.Runs, sources []Source, coverage *monitoring.MonitoringCoverage, now time.Time) {
	for _, source := range sources {
		if source.Last == nil && !source.Requested {
			continue
		}
		run := monitoring.MonitoringRun{Type: "sync", Source: "source", ID: "source:" + source.ID, Title: source.Title, ResourceRef: &platform.AssetRef{App: App, Kind: platform.AssetSource, Name: source.Name}, RecordRef: SourceType + "/" + source.ID, Status: "unknown", OwnerStatus: source.State, When: source.Changed.At}
		if source.Last != nil {
			run.When, run.Status = source.Last.At, "ok"
			if source.Last.Error != "" || source.Last.Failed > 0 {
				run.Status = "failed"
			}
		}
		if source.Requested || source.Last != nil && source.Last.Execution != nil && source.Last.Execution.Pending {
			run.Status = "pending"
		}
		if !source.Archived && source.State == "published" && !source.Stream {
			r.Retry(&run, SourceType+".pull", SourceType, source.ID, "rerun")
			fixed, fixedErr := o.ExecutionSource(source)
			if fixedErr != nil {
				coverage.Limitation = "definition-unavailable"
			} else if every, parseErr := time.ParseDuration(fixed.Every); parseErr == nil && every > 0 && source.Last != nil && now.After(source.Last.At.Add(every)) {
				r.Delayed()
				run.Delayed = true
			}
		}
		r.Add(run)
	}
}

func PipelineRuns(r *monitoring.Runs, pipelines []Pipeline) {
	for _, pipeline := range pipelines {
		if pipeline.Last == nil && !pipeline.Requested {
			continue
		}
		run := monitoring.MonitoringRun{Type: "build", Source: "pipeline", ID: "pipeline:" + pipeline.ID, Title: pipeline.Title, ResourceRef: &platform.AssetRef{App: App, Kind: platform.AssetPipeline, Name: pipeline.Name}, RecordRef: PipelineType + "/" + pipeline.ID, When: pipeline.Changed.At, Status: "unknown", OwnerStatus: pipeline.State}
		if pipeline.Last != nil {
			run.When, run.Status = pipeline.Last.At, "ok"
			if pipeline.Last.Error != "" || pipeline.Last.Failed > 0 {
				run.Status = "failed"
			} else if pipeline.Last.Quarantined > 0 {
				run.Status = "warning"
			}
		}
		if pipeline.Requested || pipeline.Last != nil && pipeline.Last.Execution != nil && pipeline.Last.Execution.Pending {
			run.Status = "pending"
		}
		if !pipeline.Archived && pipeline.State == "published" {
			r.Retry(&run, PipelineType+".run", PipelineType, pipeline.ID, "rerun")
		}
		r.Add(run)
	}
}

func DatasetRuns(r *monitoring.Runs, datasets []Dataset) {
	for _, dataset := range datasets {
		for _, projection := range dataset.Projections {
			status := "ok"
			if projection.Paused != "" || projection.Refused > 0 {
				status = "failed"
			}
			r.Add(monitoring.MonitoringRun{Type: "build", Source: "projection", ID: "projection:" + dataset.ID + ":" + projection.Object, Title: cmp.Or(dataset.Title, dataset.ID), ResourceRef: &platform.AssetRef{App: App, Kind: platform.AssetDataset, Name: dataset.Name}, RecordRef: DatasetType + "/" + dataset.ID, When: projection.At, Status: status})
		}
	}
}
