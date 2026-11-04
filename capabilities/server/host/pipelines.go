package host

import (
	"context"
	"encoding/json"
	"log"
	"maps"
	transform "platformserver/services/data/transform"

	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) RunPipelines(now time.Time) {
	if t.businessStopped() || !t.ownerActive(build.ID) {
		return
	}
	t.mu.Lock()
	c := t.automation(build.ID, false)
	query := publishedPipelineQuery()
	query.Limit = 200
	pipelines, _, _ := platform.Find[build.Pipeline](c, query)
	versions := map[string]int{}
	changed := map[string]bool{}
	owner := t.dataOwner()
	var fixed []build.Pipeline
	for _, p := range pipelines {
		p, err := owner.ExecutionPipeline(p)
		if err != nil {
			log.Printf("pipeline definition refused: %v", err)
			continue
		}
		fixed = append(fixed, p)
		if ds, ok := platform.Get[build.Dataset](c, p.Input); ok {
			versions[p.ID] = ds.Version
		}
		if p.Last != nil && p.Last.Execution != nil {
			for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
				if ds, ok := platform.Get[build.Dataset](c, id); ok && ds.Version != p.Last.Execution.Inputs[id] {
					changed[p.ID] = true
				}
			}
		}
	}
	t.mu.Unlock()
	t.runSchedules(fixed, now)
	for i, p := range fixed {
		if current, refusal := coordinatedRecordRead(t, context.Background(), func() (build.Pipeline, *kernel.Error) {
			row, _ := platform.Get[build.Pipeline](t.automation(build.ID, false), p.ID)
			return row, nil
		}); refusal == nil {
			fixed[i].Record = current.Record
			fixed[i].Requested = current.Requested
			fixed[i].Runner = current.Runner
			fixed[i].RunnerScopes = current.RunnerScopes
		}
	}
	for _, p := range fixed {
		if p.Due(now, versions[p.ID]) || p.State == "published" && changed[p.ID] {
			t.runPipeline(p, now)
		}
	}
}

func (t *Tenant) pipelineResult(member platform.Member, p build.Pipeline, run build.PipelineRun, phase string, revision *uint32, now time.Time) *kernel.Error {
	payload := platform.Raw(map[string]any{"run": run})
	key := "ran:" + p.ID + ":" + now.UTC().Format(time.RFC3339Nano)
	if run.Execution != nil {
		key = "pipeline:" + run.Execution.ID + ":" + phase
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := t.submitAccepted(t.app(build.ID), member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key, ExpectedRevision: revision,
		Target: &pb.EntityRef{Type: build.PipelineType, Id: p.ID}, Schema: &pb.SchemaRef{Name: build.SchemaPipelineRan, Version: 1}, Payload: payload}, now, true)
	return err
}

func (t *Tenant) runPipeline(p build.Pipeline, now time.Time) {
	run := build.PipelineRun{At: now, DefinitionVersion: p.ExecutionVersion()}
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
		t.mu.Lock()
		c := t.automation(build.ID, false)
		inputs := map[string]int{}
		data := map[string][]map[string]any{}
		var inputErr error
		for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
			if _, seen := inputs[id]; seen {
				continue
			}
			rows, version, err := build.DatasetRows(c, id, 0)
			if err != nil {
				inputErr = err
				break
			}
			inputs[id], data[id] = version, rows
			if ds, ok := platform.Get[build.Dataset](c, id); ok {
				fixed, err := t.dataOwner().ExecutionDataset(ds)
				if err != nil {
					inputErr = err
					break
				}
				run.Marking = platform.HigherMarking(run.Marking, fixed.Marking)
				run.Compartments = platform.JoinCompartments(run.Compartments, fixed.Compartments)
			}
		}
		t.mu.Unlock()
		run.Input = inputs[p.Input]
		if inputErr != nil {
			run.Error = inputErr.Error()
		} else if run.Input == 0 {
			run.Error = "The input dataset has no rows yet"
		} else {
			if removed := p.Declassifications(); len(removed) > 0 {
				labels, allowed := (platform.MarkingSet{Level: run.Marking, Compartments: run.Compartments}).Declassify(member, removed)
				if !allowed {
					run.Error = "The runner needs clearance and declassification permission for the pipeline's input markings"
				} else {
					run.Marking, run.Compartments, run.Declassified = labels.Level, labels.Compartments, slices.Clone(removed)
				}
			}
			kept, quarantined, err := transform.Transform(t.services.Rules, p.Steps, p.Expectations, data[p.Input], func(id string) ([]map[string]any, error) { return data[id], nil })
			if err != nil {
				run.Error = err.Error()
			} else if run.Error == "" {
				run.Rows, run.Quarantined = len(kept)+len(quarantined), len(quarantined)
				if len(quarantined) > pipelineQuarantineKept {
					quarantined = quarantined[:pipelineQuarantineKept]
				}
				run.Quarantine = quarantined
				plan = integrationPreparation{Object: p.OutputObject, Dataset: p.OutputDataset, Enterprise: p.OutputEnterprise, Producer: "pipeline:" + p.Name, Marking: run.Marking, Compartments: run.Compartments, Declassified: run.Declassified, Data: kept}
				plan.Type, plan.ID = build.PipelineType, p.ID
				if plan.Data == nil {
					plan.Data = []map[string]any{}
				}
				if p.OutputEnterprise != nil && (platform.MarkingSet{Level: run.Marking, Compartments: run.Compartments}).Guarded() {
					run.Error = "The enterprise model is tenant-readable; confidential or restricted rows cannot land there"
				} else if p.OutputObject != "" {
					if open := t.unguarded(p.OutputObject, run.Marking); len(open) > 0 {
						run.Error = "The input is " + run.Marking + " now; name who reads " + p.OutputObject + "'s fields " + strings.Join(open, ", ") + " first, or lower the marking"
					} else {
						rows, binding, err := t.declaredRows(p.OutputObject, "pipeline", p.ID, kept, now)
						if err != nil {
							run.Error = err.Error()
						} else {
							plan.Binding = binding
							if binding == nil {
								rows = p.ObjectRows(kept)
								binding = &platform.DataSourceBinding{Kind: "pipeline", Source: p.ID, Key: p.Key}
								columns := map[string]bool{}
								for i := range rows {
									rows[i].At, rows[i].Origin = &now, rows[i].ID
									var values map[string]json.RawMessage
									_ = json.Unmarshal(rows[i].Payload, &values)
									for field := range values {
										columns[field] = true
									}
								}
								for _, column := range slices.Sorted(maps.Keys(columns)) {
									binding.Columns = append(binding.Columns, platform.DataSourceColumn{Column: column, Attribute: column})
								}
								plan.Binding = binding
							}
							plan.Rows = rows
							var refusal *kernel.Error
							run.Merged, refusal = t.prepareRows(member, p.OutputObject, p.Name, plan.Rows, now)
							if refusal != nil {
								log.Printf("pipeline row preparation unavailable: %v", refusal)
								return
							}
							if binding != nil && run.Quarantined == 0 {
								removed, refusal := t.absentSourceRows(p.OutputObject, binding, rows, now)
								if refusal != nil {
									log.Printf("pipeline deletion preparation unavailable: %v", refusal)
									return
								}
								plan.Rows = append(plan.Rows, removed...)
							}
							plan.Data = nil
						}
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
		member = t.automation(build.ID, false).Member
	}
	if run.Execution != nil {
		if !t.integrationPending(build.PipelineType, p.ID, run.Execution) {
			return
		}
		if !ok {
			run.Error = "The execution member is no longer available"
		} else if !member.CanDeclassify(plan.Declassified) {
			run.Error = "The runner no longer has the pipeline's declassification permissions"
		} else if plan.Enterprise != nil {
			run.Error = "Enterprise slice targets are retired; import organization directory records explicitly"
		} else if plan.Dataset != "" {
			key := "pipeline:" + run.Execution.ID + ":dataset"
			if err := t.loadDatasetKey(member, plan.Dataset, plan.Producer, platform.MarkingSet{Level: plan.Marking, Compartments: plan.Compartments}, plan.Data, key, run.At, "snapshot"); err != nil {
				if !t.integrationRefused(build.ID, key) {
					return
				}
				run.Error = err.Message
			} else {
				run.Written = len(plan.Data)
				t.mu.Lock()
				versions, _, err := platform.Find[build.DatasetVersion](t.automation(build.ID, false), platform.Query{
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
			pull := build.SourcePull{}
			if !t.applyRows(member, plan, run.Execution, run.At, &pull) {
				return
			}
			run.Written, run.Deleted, run.Failed, run.Failures = pull.Applied, pull.Deleted, pull.Failed, pull.Failures
		}
		execution := *run.Execution
		execution.Pending = false
		run.Execution = &execution
	}
	_ = t.pipelineResult(member, p, run, "finished", nil, now)
}

const pipelineQuarantineKept = 50

func (t *Tenant) unguarded(object, marking string) []string {
	if !build.Guarded(marking) {
		return nil
	}
	t.records.Lock()
	et := t.records.Type(object)
	t.records.Unlock()
	if et == nil {
		return nil
	}
	return build.UnguardedFields(et.Info(), nil)
}
