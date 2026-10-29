package platformserver

import (
	"encoding/json"
	"fmt"
	transform "platformserver/engine/integrations"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/studio/build"

	"platformserver/platform"
)

const pipelinePreviewSample = 100
const pipelinePreviewRows = 1000

// PipelinePreviewRequest carries the editor's draft, including unsaved steps.
type PipelinePreviewRequest struct {
	Pipeline build.Pipeline `json:"pipeline"`
}

// PipelinePreview is a sample of transformations, never an output write result.
type PipelinePreview struct {
	InputVersion int                       `json:"inputVersion"`
	InputRows    int                       `json:"inputRows"`
	SampledRows  int                       `json:"sampledRows"`
	Truncated    bool                      `json:"truncated"`
	Rows         []map[string]any          `json:"rows"`
	Quarantine   []platform.QuarantinedRow `json:"quarantine"`
	Marking      string                    `json:"marking,omitempty"`
}

// PreviewPipeline captures authorized dataset versions under the original
// record owner, then invokes the same pure transformation used by real runs.
// No staged business decision is needed: neither branch submits an output.
func (t *Tenant) PreviewPipeline(member platform.Member, request PipelinePreviewRequest, at time.Time) (PipelinePreview, *kernel.Error) {
	out := PipelinePreview{Rows: []map[string]any{}, Quarantine: []platform.QuarantinedRow{}}
	if problem := t.admits(member); problem != nil {
		return out, problem
	}
	t.mu.Lock()
	if t.quarantined() || t.app(build.ID) == nil {
		t.mu.Unlock()
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Pipeline trial is unavailable")
	}
	caller := t.caller(member, t.app(build.ID), false)
	if problem := authorizationRefusal(t.authorizeCapability(caller, build.ID, "build.pipeline.preview", []string{build.Builder, build.Integrator}, true, authorizationDetails{At: at})); problem != nil {
		t.mu.Unlock()
		return out, problem
	}
	p := request.Pipeline
	// The caller's draft is separate from the saved publication and must use
	// strict formula semantics even if the editor started from an old version.
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > 1<<20 || len(p.Steps) > 64 || len(p.Expectations) > 64 {
		t.mu.Unlock()
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Pipeline trial accepts at most 64 steps, 64 expectations and 1 MiB of definition")
	}
	p = build.Pipeline{}
	_ = json.Unmarshal(raw, &p) // detach slices and formula pointers from direct Go callers too
	if err := p.NormalizeFormulaRules(); err != nil {
		t.mu.Unlock()
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	inputs := map[string][]map[string]any{}
	var sources []string
	for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
		if _, exists := inputs[id]; exists {
			continue
		}
		data, problem := caller.ReadRecord(build.DatasetType, id, at)
		if problem != nil {
			t.mu.Unlock()
			return out, problem
		}
		var dataset build.Dataset
		if json.Unmarshal(data, &dataset) != nil || dataset.Version < 1 {
			t.mu.Unlock()
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The input dataset has no rows yet")
		}
		versionID := build.VersionID(id, dataset.Version)
		data, problem = caller.ReadRecord(build.DatasetVersionType, versionID, at)
		if problem != nil {
			t.mu.Unlock()
			return out, problem
		}
		var version build.DatasetVersion
		var rows []map[string]any
		if json.Unmarshal(data, &version) != nil || version.Archived || json.Unmarshal(version.Data, &rows) != nil {
			t.mu.Unlock()
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The input dataset version is unavailable")
		}
		sources = append(sources, build.DatasetType+"/"+id, build.DatasetVersionType+"/"+versionID+"#data")
		out.Marking = build.HigherMarking(out.Marking, dataset.Marking)
		if id == p.Input {
			out.InputRows, out.InputVersion = len(rows), dataset.Version
		}
		if len(rows) > pipelinePreviewSample {
			rows, out.Truncated = rows[:pipelinePreviewSample], true
		}
		inputs[id] = rows
	}
	problem := t.app(build.ID).(build.ConstructionDrafts).CheckPipeline(caller, p)
	t.mu.Unlock()
	if problem != nil {
		return out, problem
	}
	out.SampledRows = len(inputs[p.Input])
	rows, quarantine, err := transform.Transform(t.services.Rules, p.Steps, p.Expectations, inputs[p.Input], func(id string) ([]map[string]any, error) {
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
	// Returning sample data is still a derived read: revocation during the
	// outside-lock computation must not leave a copy of the old permissions.
	t.mu.Lock()
	problem = authorizationRefusal(t.authorizeCapability(caller, build.ID, "build.pipeline.preview", []string{build.Builder, build.Integrator}, true, authorizationDetails{At: at, Sources: sources}))
	t.mu.Unlock()
	if problem != nil {
		return PipelinePreview{}, problem
	}
	return out, nil
}
