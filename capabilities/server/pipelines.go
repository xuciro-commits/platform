package platformserver

import (
	"encoding/json"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/enterprise"
	"platformserver/platform"
)

// Pipeline runs (ADR-0071). A run reads the input dataset's latest version,
// executes the declared steps in memory, quarantines rows that fail the
// expectations, and writes the rest: as the output dataset's next version, or
// as the output object's own create/edit per row keyed by content. Every write
// and the run summary are ordinary journaled inputs; replay never recomputes.

// RunPipelines runs every due pipeline of the tenant once, on the outside loop.
func (t *Tenant) RunPipelines(now time.Time) {
	if t.quarantined() {
		return
	}
	t.mu.Lock()
	c := t.automation(build.ID, false)
	pipelines, _, _ := platform.Find[build.Pipeline](c, platform.Query{Domain: json.RawMessage(`[["state","=","published"]]`), Sort: []string{"id"}, Limit: 200})
	versions := map[string]int{}
	for _, p := range pipelines {
		if ds, ok := platform.Get[build.Dataset](c, p.Input); ok {
			versions[p.ID] = ds.Version
		}
	}
	t.mu.Unlock()
	for _, p := range pipelines {
		if p.Due(now, versions[p.ID]) {
			t.runPipeline(p, now)
		}
	}
}

func (t *Tenant) runPipeline(p build.Pipeline, now time.Time) {
	run := build.PipelineRun{At: now}
	member, ok := t.Member(p.Runner)
	if !ok {
		run.Error = "The publishing member is no longer available"
	} else {
		// Read everything under the lock, compute outside it, write through Submit.
		t.mu.Lock()
		c := t.automation(build.ID, false)
		rows, version, err := build.DatasetRows(c, p.Input, 0)
		others := map[string][]map[string]any{}
		for _, s := range p.Steps {
			if s.Dataset != "" {
				others[s.Dataset], _, _ = build.DatasetRows(c, s.Dataset, 0)
			}
		}
		for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
			if ds, ok := platform.Get[build.Dataset](c, id); ok {
				run.Marking = build.HigherMarking(run.Marking, ds.Marking)
			}
		}
		t.mu.Unlock()
		run.Input = version
		if err != nil {
			run.Error = err.Error()
		} else if version == 0 {
			run.Error = "The input dataset has no rows yet"
		} else {
			kept, quarantined, execErr := p.Execute(rows, func(id string) ([]map[string]any, error) { return others[id], nil })
			if execErr != nil {
				run.Error = execErr.Error()
			} else {
				run.Rows, run.Quarantined = len(kept)+len(quarantined), len(quarantined)
				if len(quarantined) > pipelineQuarantineKept {
					quarantined = quarantined[:pipelineQuarantineKept]
				}
				run.Quarantine = quarantined
				if p.OutputEnterprise != nil {
					elements, rels, skipped := p.OutputEnterprise.Slice(kept)
					payload, _ := json.Marshal(map[string]any{"source": p.OutputEnterprise.Source, "elements": elements, "relationships": rels})
					_, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: enterprise.ID, IdempotencyKey: "sync:" + p.ID + ":" + now.UTC().Format(time.RFC3339Nano),
						Target: &pb.EntityRef{Type: enterprise.ModelType, Id: "model"}, Schema: &pb.SchemaRef{Name: enterprise.SchemaSliceSync, Version: 1}, Payload: payload}, now)
					switch {
					case err != nil && err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED:
						run.Error = "The pipeline runs as its publisher, who needs the Enterprise admin role to land elements in the model"
					case err != nil:
						run.Error = err.Code.String() + ": " + err.Message
					default:
						run.Written, run.Failed = len(elements), skipped
					}
				} else if p.OutputDataset != "" {
					if err := t.loadDataset(member, p.OutputDataset, "pipeline:"+p.Name, run.Marking, kept, now); err != nil {
						run.Error = err.Message
					} else {
						run.Written = len(kept)
						t.mu.Lock()
						if ds, ok := platform.Get[build.Dataset](t.automation(build.ID, false), p.OutputDataset); ok {
							run.Output = ds.Version
						}
						t.mu.Unlock()
					}
				} else if open := t.unguarded(p.OutputObject, run.Marking); len(open) > 0 {
					run.Error = "The input is " + run.Marking + " now; name who reads " + p.OutputObject + "'s fields " + strings.Join(open, ", ") + " first, or lower the marking"
				} else {
					pull := build.SourcePull{}
					t.applyRows(member, p.OutputObject, p.Name, p.ObjectRows(kept), now, &pull)
					run.Written, run.Failed, run.Failures, run.Merged = pull.Applied, pull.Failed, pull.Failures, pull.Merged
				}
			}
		}
	}
	payload, _ := json.Marshal(map[string]any{"run": run})
	if member.ID == "" {
		member = t.automation(build.ID, false).Member
	}
	t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "ran:" + p.ID + ":" + now.UTC().Format(time.RFC3339Nano),
		Target: &pb.EntityRef{Type: build.PipelineType, Id: p.ID}, Schema: &pb.SchemaRef{Name: build.SchemaPipelineRan, Version: 1}, Payload: payload}, now)
}

const pipelineQuarantineKept = 50

// unguarded are the object's fields every role reads, when marking forbids pouring into them (ADR-0075).
func (t *Tenant) unguarded(object, marking string) []string {
	if !build.Guarded(marking) {
		return nil
	}
	t.records.mu.Lock()
	et := t.records.types[object]
	t.records.mu.Unlock()
	if et == nil {
		return nil
	}
	return build.UnguardedFields(et.info, nil)
}
