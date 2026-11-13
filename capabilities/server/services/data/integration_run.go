package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	transform "platformserver/services/data/transform"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const maxIntegrationBytes = 64 << 20

const pipelineQuarantineKept = 50

type PipelineInputs struct {
	Inputs       map[string]int
	Data         map[string][]map[string]any
	Err          error
	Marking      string
	Compartments []string
}

type Integration struct {
	Tenant             string
	Authority          string
	Rules              platform.RuleEngine
	Files              func() platform.FileStore
	Member             func(id string) (platform.Member, bool)
	Automation         func() platform.Member
	Sources            func() ([]Source, *kernel.Error)
	Quarantined        func() bool
	Pending            func(typ, id string, execution *IntegrationExecution) bool
	Target             func(object string) (string, bool)
	ReadSource         func(Source) ([]map[string]any, error)
	ConnectionMarkings func(connection string) (platform.MarkingSet, *kernel.Error)
	Source             SourcePreparation
	DeclaredRows       func(object, kind, source string, input []map[string]any, now time.Time) ([]SourceRow, *platform.DataSourceBinding, error)
	PrepareRows        func(member platform.Member, object, producer string, rows []SourceRow, now time.Time) (int, *kernel.Error)
	AbsentRows         func(object string, binding *platform.DataSourceBinding, current []SourceRow, now time.Time) ([]SourceRow, *kernel.Error)
	Refused            func(authority, key string) bool
	Submit             func(member platform.Member, submission *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error)
	SubmitSource       func(member platform.Member, submission *pb.Submission, binding *platform.DataSourceBinding, row SourceRow, now time.Time) *kernel.Error
	SubmitBackground   func(member platform.Member, submission *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error)
	ObserveRun         func(at time.Time, kind, key, app, resource, status, title, detail string)
	PipelineInputs     func(p Pipeline, marking string, compartments []string) (PipelineInputs, *kernel.Error)
	PipelinePlan       func() (PipelinePlan, *kernel.Error)
	CurrentPipeline    func(id string) (Pipeline, *kernel.Error)
	Unguarded          func(object, marking string) []string
	OutputVersion      func(dataset, key string) (int, error)
	ScheduleExists     func(id string) bool
	PipelineProject    func(id string) string
	ExecutionSchedules func() ([]platform.Schedule, *kernel.Error)
	Composite          func(member platform.Member, key string, edits []platform.CompositeEdit, now time.Time) error
}

type IntegrationPreparation struct {
	Transaction  string                      `json:"transaction,omitempty"`
	Type         string                      `json:"type"`
	ID           string                      `json:"id"`
	Rows         []SourceRow                 `json:"rows,omitempty"`
	Data         []map[string]any            `json:"data"`
	Object       string                      `json:"object,omitempty"`
	Binding      *platform.DataSourceBinding `json:"binding,omitempty"`
	Dataset      string                      `json:"dataset,omitempty"`
	Enterprise   *EnterpriseTarget           `json:"enterprise,omitempty"`
	Producer     string                      `json:"producer"`
	Marking      string                      `json:"marking,omitempty"`
	Compartments []string                    `json:"compartments,omitempty"`
	Declassified []string                    `json:"declassified,omitempty"`
}

func (g Integration) prepare(member platform.Member, revision uint32, now time.Time, plan IntegrationPreparation) (*IntegrationExecution, error) {
	raw, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxIntegrationBytes {
		return nil, fmt.Errorf("the integration preparation exceeds its retained input budget")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	if err := g.Files().Put(context.Background(), g.Tenant+"/artifacts/integration/"+digest+".json", raw, "application/json"); err != nil {
		return nil, err
	}
	identity := sha256.Sum256(platform.Raw([]any{g.Tenant, plan.Type, plan.ID, revision, now.UTC()}))
	return &IntegrationExecution{ID: fmt.Sprintf("%x", identity), Member: member.ID, Scopes: slices.Clone(member.Scopes), Pending: true, Snapshot: digest}, nil
}

func (g Integration) retained(execution *IntegrationExecution) (IntegrationPreparation, error) {
	var plan IntegrationPreparation
	digest, err := hex.DecodeString(execution.Snapshot)
	if err != nil || len(digest) != sha256.Size {
		return plan, fmt.Errorf("the integration snapshot has an invalid digest")
	}
	reader, size, err := g.Files().Get(context.Background(), g.Tenant+"/artifacts/integration/"+execution.Snapshot+".json")
	if err != nil {
		return plan, err
	}
	defer reader.Close()
	if size <= 0 || size > maxIntegrationBytes {
		return plan, fmt.Errorf("the integration snapshot exceeds its retained input budget")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxIntegrationBytes+1))
	if err != nil {
		return plan, err
	}
	if int64(len(raw)) != size || fmt.Sprintf("%x", sha256.Sum256(raw)) != execution.Snapshot {
		return plan, fmt.Errorf("the integration snapshot differs from its accepted digest")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	err = decoder.Decode(&plan)
	return plan, err
}

func (g Integration) PullSource(s Source, now time.Time) {
	pull := SourcePull{At: now, DefinitionVersion: s.ExecutionVersion()}
	member, ok := g.Member(s.Puller)
	if s.PullerScopes != "" && json.Unmarshal([]byte(s.PullerScopes), &member.Scopes) != nil {
		log.Printf("source execution scopes are invalid")
		return
	}
	var plan IntegrationPreparation
	if s.Last != nil && s.Last.Execution != nil && s.Last.Execution.Pending {
		pull = *s.Last
		member, ok = g.Member(pull.Execution.Member)
		member.Scopes = slices.Clone(pull.Execution.Scopes)
		var err error
		plan, err = g.retained(pull.Execution)
		if err != nil {
			log.Printf("source retained input unavailable: %v", err)
			return
		}
		pull.Object, pull.Marking, pull.Compartments = plan.Object, plan.Marking, slices.Clone(plan.Compartments)
	} else if !ok {
		pull.Error = "The execution member is no longer available"
	} else if raw, err := g.ReadSource(s); err != nil {
		pull.Error = err.Error()
	} else {
		marking, refusal := g.ConnectionMarkings(s.Connection)
		if refusal != nil {
			log.Printf("source connection state unavailable: %v", refusal)
			return
		}
		pull.Cursor, pull.Rows = s.Advance(raw), len(raw)
		plan = IntegrationPreparation{Object: s.Object, Dataset: s.Dataset, Producer: "source:" + s.Name, Marking: marking.Level, Compartments: marking.Compartments}
		pull.Object, pull.Marking, pull.Compartments = plan.Object, plan.Marking, slices.Clone(plan.Compartments)
		plan.Type, plan.ID = SourceType, s.ID
		if s.Stream {
			plan.Transaction = "append"
		}
		if s.Dataset != "" {
			if len(raw) > 0 || s.Cursor == "" {
				plan.Data = raw
				if plan.Data == nil {
					plan.Data = []map[string]any{}
				}
			}
		} else if (platform.MarkingSet{Level: plan.Marking, Compartments: plan.Compartments}).Guarded() {
			pull.Error = "A confidential or restricted connection feeds a dataset; use a pipeline to apply field access rules"
		} else if rows, binding, err := g.DeclaredRows(s.Object, "source", s.ID, raw, now); err != nil {
			pull.Error = err.Error()
		} else {
			plan.Binding = binding
			if binding == nil {
				rows, err = s.MapRows(raw)
				if err != nil {
					pull.Error = err.Error()
				}
				binding = &platform.DataSourceBinding{Kind: "source", Source: s.ID, Key: s.Key}
				for _, mapping := range s.Mapping {
					binding.Columns = append(binding.Columns, platform.DataSourceColumn{Column: mapping.From, Attribute: mapping.To})
				}
				for i := range rows {
					rows[i].At, rows[i].Origin = &now, rows[i].ID
				}
				plan.Binding = binding
			}
			if pull.Error == "" {
				plan.Rows = rows
				var refusal *kernel.Error
				pull.Merged, refusal = g.PrepareRows(member, s.Object, s.Name, plan.Rows, now)
				if refusal != nil {
					log.Printf("source row preparation unavailable: %v", refusal)
					return
				}
				if binding != nil && s.Since == "" {
					removed, refusal := g.AbsentRows(s.Object, binding, rows, now)
					if refusal != nil {
						log.Printf("source deletion preparation unavailable: %v", refusal)
						return
					}
					plan.Rows = append(plan.Rows, removed...)
				}
			}
		}
		if pull.Error == "" {
			execution, err := g.prepare(member, s.Revision, now, plan)
			if err != nil {
				log.Printf("source input retention failed: %v", err)
				return
			}
			pull.Execution = execution
			if err := g.SourceResult(member, s, pull, "prepared", &s.Revision, now); err != nil {
				return
			}
		}
	}
	if member.ID == "" {
		member = g.Automation()
	}
	if pull.Execution != nil {
		if !g.Pending(SourceType, s.ID, pull.Execution) {
			return
		}
		if !ok {
			pull.Error = "The execution member is no longer available"
		} else if plan.Dataset != "" {
			if plan.Data != nil {
				key := "source:" + pull.Execution.ID + ":dataset"
				if err := g.LoadDataset(member, plan.Dataset, plan.Producer, platform.MarkingSet{Level: plan.Marking, Compartments: plan.Compartments}, plan.Data, key, pull.At, plan.Transaction); err != nil {
					if !g.Refused(g.Authority, key) {
						return
					}
					pull.Error = err.Message
				} else {
					pull.Applied = len(plan.Data)
				}
			}
		} else if !g.ApplyRows(member, plan, pull.Execution, pull.At, &pull) {
			return
		}
		execution := *pull.Execution
		execution.Pending = false
		pull.Execution = &execution
	}
	_ = g.SourceResult(member, s, pull, "finished", nil, now)
}

func (g Integration) RunPipeline(p Pipeline, now time.Time) {
	run := PipelineRun{At: now, DefinitionVersion: p.ExecutionVersion()}
	member, ok := g.Member(p.Runner)
	if p.RunnerScopes != "" && json.Unmarshal([]byte(p.RunnerScopes), &member.Scopes) != nil {
		log.Printf("pipeline execution scopes are invalid")
		return
	}
	var plan IntegrationPreparation
	if p.Last != nil && p.Last.Execution != nil && p.Last.Execution.Pending {
		run = *p.Last
		member, ok = g.Member(run.Execution.Member)
		member.Scopes = slices.Clone(run.Execution.Scopes)
		var err error
		plan, err = g.retained(run.Execution)
		if err != nil {
			log.Printf("pipeline retained input unavailable: %v", err)
			return
		}
		run.Object, run.Marking, run.Compartments = plan.Object, plan.Marking, slices.Clone(plan.Compartments)
	} else if !ok {
		run.Error = "The execution member is no longer available"
	} else {
		captured, refusal := g.PipelineInputs(p, run.Marking, run.Compartments)
		if refusal != nil {
			return
		}
		inputs, data, inputErr := captured.Inputs, captured.Data, captured.Err
		run.Marking, run.Compartments = captured.Marking, captured.Compartments
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
			kept, quarantined, err := transform.Transform(g.Rules, p.Steps, p.Expectations, data[p.Input], func(id string) ([]map[string]any, error) { return data[id], nil })
			if err != nil {
				run.Error = err.Error()
			} else if run.Error == "" {
				run.Rows, run.Quarantined = len(kept)+len(quarantined), len(quarantined)
				if len(quarantined) > pipelineQuarantineKept {
					quarantined = quarantined[:pipelineQuarantineKept]
				}
				run.Quarantine = quarantined
				plan = IntegrationPreparation{Object: p.OutputObject, Dataset: p.OutputDataset, Enterprise: p.OutputEnterprise, Producer: "pipeline:" + p.Name, Marking: run.Marking, Compartments: run.Compartments, Declassified: run.Declassified, Data: kept}
				run.Object = plan.Object
				plan.Type, plan.ID = PipelineType, p.ID
				if plan.Data == nil {
					plan.Data = []map[string]any{}
				}
				if p.OutputEnterprise != nil && (platform.MarkingSet{Level: run.Marking, Compartments: run.Compartments}).Guarded() {
					run.Error = "The enterprise model is tenant-readable; confidential or restricted rows cannot land there"
				} else if p.OutputObject != "" {
					if open := g.Unguarded(p.OutputObject, run.Marking); len(open) > 0 {
						run.Error = "The input is " + run.Marking + " now; name who reads " + p.OutputObject + "'s fields " + strings.Join(open, ", ") + " first, or lower the marking"
					} else {
						rows, binding, err := g.DeclaredRows(p.OutputObject, "pipeline", p.ID, kept, now)
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
							run.Merged, refusal = g.PrepareRows(member, p.OutputObject, p.Name, plan.Rows, now)
							if refusal != nil {
								log.Printf("pipeline row preparation unavailable: %v", refusal)
								return
							}
							if binding != nil && run.Quarantined == 0 {
								removed, refusal := g.AbsentRows(p.OutputObject, binding, rows, now)
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
					execution, err := g.prepare(member, p.Revision, now, plan)
					if err != nil {
						log.Printf("pipeline input retention failed: %v", err)
						return
					}
					execution.Inputs = inputs
					run.Execution = execution
					if err := g.PipelineResult(member, p, run, "prepared", &p.Revision, now); err != nil {
						return
					}
				}
			}
		}
	}
	if member.ID == "" {
		member = g.Automation()
	}
	if run.Execution != nil {
		if !g.Pending(PipelineType, p.ID, run.Execution) {
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
			if err := g.LoadDataset(member, plan.Dataset, plan.Producer, platform.MarkingSet{Level: plan.Marking, Compartments: plan.Compartments}, plan.Data, key, run.At, "snapshot"); err != nil {
				if !g.Refused(g.Authority, key) {
					return
				}
				run.Error = err.Message
			} else {
				run.Written = len(plan.Data)
				output, err := g.OutputVersion(plan.Dataset, key)
				if err != nil {
					log.Printf("pipeline output version unavailable: %v", err)
					return
				}
				run.Output = output
			}
		} else {
			pull := SourcePull{}
			if !g.ApplyRows(member, plan, run.Execution, run.At, &pull) {
				return
			}
			run.Written, run.Deleted, run.Failed, run.Failures = pull.Applied, pull.Deleted, pull.Failed, pull.Failures
		}
		execution := *run.Execution
		execution.Pending = false
		run.Execution = &execution
	}
	_ = g.PipelineResult(member, p, run, "finished", nil, now)
}
