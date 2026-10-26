package platformserver

import (
	"encoding/json"
	"log"
	"platformserver/internal/host"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	enterprisemodel "platformserver/internal/enterprise"
	"platformserver/platform"
)

// Pipeline runs. A run reads the input dataset's latest version,
// executes the declared steps in memory, quarantines rows that fail the
// expectations, and writes the rest: as the output dataset's next version, or
// as the output object's own create/edit per row keyed by execution. Every write
// and the run summary are ordinary journaled inputs; replay never recomputes.

// RunPipelines runs every due pipeline of the tenant once, on the outside loop.
func (t *Tenant) RunPipelines(now time.Time) {
	if t.businessStopped() || !t.ownerActive(construction.ID) {
		return
	}
	t.mu.Lock()
	c := t.automation(construction.ID, false)
	pipelines, _, _ := platform.Find[construction.Pipeline](c, platform.Query{Domain: json.RawMessage(`[["state","=","published"]]`), Sort: []string{"id"}, Limit: 200})
	versions := map[string]int{}
	changed := map[string]bool{}
	owner := t.app(construction.ID).(host.IntegrationDefinitions)
	var fixed []construction.Pipeline
	for _, p := range pipelines {
		p, err := owner.ExecutionPipeline(p)
		if err != nil {
			log.Printf("pipeline definition refused: %v", err)
			continue
		}
		fixed = append(fixed, p)
		if ds, ok := platform.Get[construction.Dataset](c, p.Input); ok {
			versions[p.ID] = ds.Version
		}
		if p.Last != nil && p.Last.Execution != nil {
			for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
				if ds, ok := platform.Get[construction.Dataset](c, id); ok && ds.Version != p.Last.Execution.Inputs[id] {
					changed[p.ID] = true
				}
			}
		}
	}
	t.mu.Unlock()
	for _, p := range fixed {
		if p.Due(now, versions[p.ID]) || p.State == "published" && changed[p.ID] {
			t.runPipeline(p, now)
		}
	}
}

func (t *Tenant) pipelineResult(member platform.Member, p construction.Pipeline, run construction.PipelineRun, phase string, revision *uint32, now time.Time) *kernel.Error {
	payload := platform.Raw(map[string]any{"run": run})
	key := "ran:" + p.ID + ":" + now.UTC().Format(time.RFC3339Nano)
	if run.Execution != nil {
		key = "pipeline:" + run.Execution.ID + ":" + phase
	}
	_, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: construction.ID, IdempotencyKey: key, ExpectedRevision: revision,
		Target: &pb.EntityRef{Type: construction.PipelineType, Id: p.ID}, Schema: &pb.SchemaRef{Name: construction.SchemaPipelineRan, Version: 1}, Payload: payload}, now)
	return err
}

func (t *Tenant) runPipeline(p construction.Pipeline, now time.Time) {
	run := construction.PipelineRun{At: now, DefinitionVersion: p.ExecutionVersion()}
	member, ok := t.Member(p.Runner)
	if p.RunnerScopes != "" && json.Unmarshal([]byte(p.RunnerScopes), &member.Scopes) != nil {
		log.Printf("pipeline execution scopes are invalid")
		return
	}
	var plan integrationPreparation
	if p.Last != nil && p.Last.Execution != nil && p.Last.Execution.Pending {
		run = *p.Last
		member, ok = t.Member(run.Execution.Member)
		member.Scopes = slices.Clone(run.Execution.Scopes)
		var err error
		plan, err = t.integrationPreparation(run.Execution)
		if err != nil {
			log.Printf("pipeline retained input unavailable: %v", err)
			return
		}
	} else if !ok {
		run.Error = "The execution member is no longer available"
	} else {
		// Every input, including lookup/join datasets, is frozen under one read.
		t.mu.Lock()
		c := t.automation(construction.ID, false)
		inputs := map[string]int{}
		data := map[string][]map[string]any{}
		var inputErr error
		for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
			if _, seen := inputs[id]; seen {
				continue
			}
			rows, version, err := construction.DatasetRows(c, id, 0)
			if err != nil {
				inputErr = err
				break
			}
			inputs[id], data[id] = version, rows
			if ds, ok := platform.Get[construction.Dataset](c, id); ok {
				fixed, err := t.app(construction.ID).(host.IntegrationDefinitions).ExecutionDataset(ds)
				if err != nil {
					inputErr = err
					break
				}
				run.Marking = construction.HigherMarking(run.Marking, fixed.Marking)
			}
		}
		t.mu.Unlock()
		run.Input = inputs[p.Input]
		if inputErr != nil {
			run.Error = inputErr.Error()
		} else if run.Input == 0 {
			run.Error = "The input dataset has no rows yet"
		} else {
			kept, quarantined, err := p.ExecuteWithRules(t.services.Rules, data[p.Input], func(id string) ([]map[string]any, error) { return data[id], nil })
			if err != nil {
				run.Error = err.Error()
			} else {
				run.Rows, run.Quarantined = len(kept)+len(quarantined), len(quarantined)
				if len(quarantined) > pipelineQuarantineKept {
					quarantined = quarantined[:pipelineQuarantineKept]
				}
				run.Quarantine = quarantined
				plan = integrationPreparation{Object: p.OutputObject, Dataset: p.OutputDataset, Enterprise: p.OutputEnterprise, Producer: "pipeline:" + p.Name, Marking: run.Marking, Data: kept}
				plan.Type, plan.ID = construction.PipelineType, p.ID
				if plan.Data == nil {
					plan.Data = []map[string]any{}
				}
				if p.OutputEnterprise != nil && construction.Guarded(run.Marking) {
					run.Error = "The enterprise model is tenant-readable; confidential or restricted rows cannot land there"
				} else if p.OutputObject != "" {
					if open := t.unguarded(p.OutputObject, run.Marking); len(open) > 0 {
						run.Error = "The input is " + run.Marking + " now; name who reads " + p.OutputObject + "'s fields " + strings.Join(open, ", ") + " first, or lower the marking"
					} else {
						plan.Rows = p.ObjectRows(kept)
						var refusal *kernel.Error
						run.Merged, refusal = t.prepareRows(member, p.OutputObject, p.Name, plan.Rows, now)
						if refusal != nil {
							log.Printf("pipeline row preparation unavailable: %v", refusal)
							return
						}
						plan.Data = nil
					}
				}
				if run.Error == "" {
					execution, err := t.prepareIntegration(member, p.Revision, now, plan)
					if err != nil {
						log.Printf("pipeline input retention failed: %v", err)
						return
					}
					execution.Inputs = inputs
					run.Execution = execution
					if err := t.pipelineResult(member, p, run, "prepared", &p.Revision, now); err != nil {
						return
					}
				}
			}
		}
	}
	if member.ID == "" {
		member = t.automation(construction.ID, false).Member
	}
	if run.Execution != nil {
		if !t.integrationPending(construction.PipelineType, p.ID, run.Execution) {
			return
		}
		if !ok {
			run.Error = "The execution member is no longer available"
		} else if plan.Enterprise != nil {
			elements, rels, skipped := plan.Enterprise.Slice(plan.Data)
			key := "pipeline:" + run.Execution.ID + ":enterprise"
			_, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: enterprisemodel.ID, IdempotencyKey: key,
				Target: &pb.EntityRef{Type: enterprisemodel.ModelType, Id: "model"}, Schema: &pb.SchemaRef{Name: enterprisemodel.SchemaSliceSync, Version: 1}, Payload: platform.Raw(map[string]any{"source": plan.Enterprise.Source, "elements": elements, "relationships": rels})}, run.At)
			if err != nil {
				if !t.integrationRefused(enterprisemodel.ID, key) {
					return
				}
				if err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
					run.Error = "The pipeline runs as its requesting member, who needs the Enterprise admin role to land elements in the model"
				} else {
					run.Error = err.Code.String() + ": " + err.Message
				}
			} else {
				run.Written, run.Failed = len(elements), skipped
			}
		} else if plan.Dataset != "" {
			key := "pipeline:" + run.Execution.ID + ":dataset"
			if err := t.loadDatasetKey(member, plan.Dataset, plan.Producer, plan.Marking, plan.Data, key, run.At); err != nil {
				if !t.integrationRefused(construction.ID, key) {
					return
				}
				run.Error = err.Message
			} else {
				run.Written = len(plan.Data)
				t.mu.Lock()
				// The load's immutable version keeps its original operation key,
				// including after retention archives its data or later loads occur.
				versions, _, err := platform.Find[construction.DatasetVersion](t.automation(construction.ID, false), platform.Query{
					Domain: platform.Raw([][]any{{"dataset", "=", plan.Dataset}, {"requestKey", "=", key}}), Archived: true, Limit: 1,
				})
				t.mu.Unlock()
				if err != nil || len(versions) != 1 {
					log.Printf("pipeline output version unavailable: %v", err)
					return
				}
				run.Output = versions[0].Version
			}
		} else {
			pull := construction.SourcePull{}
			if !t.applyRows(member, plan, run.Execution, run.At, &pull) {
				return
			}
			run.Written, run.Failed, run.Failures = pull.Applied, pull.Failed, pull.Failures
		}
		execution := *run.Execution
		execution.Pending = false
		run.Execution = &execution
	}
	_ = t.pipelineResult(member, p, run, "finished", nil, now)
}

const pipelineQuarantineKept = 50

// unguarded are the object's fields every role reads, when marking forbids pouring into them.
func (t *Tenant) unguarded(object, marking string) []string {
	if !construction.Guarded(marking) {
		return nil
	}
	t.records.mu.Lock()
	et := t.records.types[object]
	t.records.mu.Unlock()
	if et == nil {
		return nil
	}
	return construction.UnguardedFields(et.info, nil)
}
