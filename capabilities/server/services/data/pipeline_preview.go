package data

import (
	"encoding/json"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	transform "platformserver/services/data/transform"
)

const pipelinePreviewSample = 100
const pipelinePreviewRows = 1000

type PipelinePreviewRequest struct {
	Pipeline Pipeline `json:"pipeline"`
}

type PipelinePreview struct {
	InputVersion int                       `json:"inputVersion"`
	InputRows    int                       `json:"inputRows"`
	SampledRows  int                       `json:"sampledRows"`
	Truncated    bool                      `json:"truncated"`
	Rows         []map[string]any          `json:"rows"`
	Quarantine   []platform.QuarantinedRow `json:"quarantine"`
	Marking      string                    `json:"marking,omitempty"`
	Compartments []string                  `json:"compartments,omitempty"`
}

func (o *Owner) PreviewPipeline(caller platform.Caller, request PipelinePreviewRequest, at time.Time, rules platform.RuleEngine, authorize func(sources []string) *kernel.Error) (PipelinePreview, *kernel.Error) {
	out := PipelinePreview{Rows: []map[string]any{}, Quarantine: []platform.QuarantinedRow{}}
	p := request.Pipeline
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > 1<<20 || len(p.Steps) > 64 || len(p.Expectations) > 64 {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Pipeline trial accepts at most 64 steps, 64 expectations and 1 MiB of definition")
	}
	p = Pipeline{}
	_ = json.Unmarshal(raw, &p)
	if err := p.NormalizeFormulaRules(); err != nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	inputs := map[string][]map[string]any{}
	var sources []string
	for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
		if _, exists := inputs[id]; exists {
			continue
		}
		data, problem := caller.ReadRecord(DatasetType, id, at)
		if problem != nil {
			return out, problem
		}
		var dataset Dataset
		if json.Unmarshal(data, &dataset) != nil || dataset.Version < 1 {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The input dataset has no rows yet")
		}
		versionID := VersionID(id, dataset.Version)
		data, problem = caller.ReadRecord(DatasetVersionType, versionID, at)
		if problem != nil {
			return out, problem
		}
		var version DatasetVersion
		var rows []map[string]any
		if json.Unmarshal(data, &version) != nil || version.Archived || json.Unmarshal(version.Data, &rows) != nil {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The input dataset version is unavailable")
		}
		sources = append(sources, DatasetType+"/"+id, DatasetVersionType+"/"+versionID+"#data")
		out.Marking = platform.HigherMarking(out.Marking, dataset.Marking)
		out.Compartments = platform.JoinCompartments(out.Compartments, dataset.Compartments)
		if id == p.Input {
			out.InputRows, out.InputVersion = len(rows), dataset.Version
		}
		if len(rows) > pipelinePreviewSample {
			rows, out.Truncated = rows[:pipelinePreviewSample], true
		}
		inputs[id] = rows
	}
	if problem := o.CheckPipeline(caller, p); problem != nil {
		return out, problem
	}
	out.SampledRows = len(inputs[p.Input])
	rows, quarantine, err := transform.Transform(rules, p.Steps, p.Expectations, inputs[p.Input], func(id string) ([]map[string]any, error) {
		rows, exists := inputs[id]
		if !exists {
			return nil, fmt.Errorf("The trial dataset was not captured")
		}
		return rows, nil
	}, pipelinePreviewRows)
	if err != nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	if rows != nil {
		out.Rows = rows
	}
	if quarantine != nil {
		out.Quarantine = quarantine
	}
	if raw, err := json.Marshal(out); err != nil || len(raw) > 1<<20 {
		return PipelinePreview{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Pipeline trial output exceeds 1 MiB")
	}
	if problem := authorize(sources); problem != nil {
		return PipelinePreview{}, problem
	}
	return out, nil
}
