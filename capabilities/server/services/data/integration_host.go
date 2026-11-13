package data

import (
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (o *Owner) ExecutionSources(c platform.Caller) ([]Source, *kernel.Error) {
	found, _, err := platform.Find[Source](c, platform.Query{Domain: json.RawMessage(`[["state","=","published"]]`), Sort: []string{"id"}, Limit: 200})
	if err != nil {
		return nil, err
	}
	var fixed []Source
	for _, source := range found {
		source, err := o.ExecutionSource(source)
		if err != nil {
			log.Printf("source definition refused: %v", err)
			continue
		}
		fixed = append(fixed, source)
	}
	return fixed, nil
}

func PendingExecution(c platform.Caller, typ, id string, execution *IntegrationExecution) bool {
	var current *IntegrationExecution
	switch typ {
	case SourceType:
		source, ok := platform.Get[Source](c, id)
		if !ok || source.Archived || source.State != "published" || source.Last == nil {
			return false
		}
		current = source.Last.Execution
	case PipelineType:
		pipeline, ok := platform.Get[Pipeline](c, id)
		if !ok || pipeline.Archived || pipeline.State != "published" || pipeline.Last == nil {
			return false
		}
		current = pipeline.Last.Execution
	}
	return current != nil && current.Pending && current.ID == execution.ID && current.Snapshot == execution.Snapshot && current.Member == execution.Member && slices.Equal(current.Scopes, execution.Scopes)
}

func RunOutcome(failure string, err *kernel.Error) (string, string) {
	status, detail := "ok", failure
	if failure != "" || err != nil {
		status = "failed"
		if detail == "" && err != nil {
			detail = err.Error()
		}
	}
	return status, detail
}

func SourcePullSubmission(tenant, authority string, member platform.Member, s Source, pull SourcePull, phase string, revision *uint32, now time.Time) *pb.Submission {
	key := "pulled:" + s.ID + ":" + now.UTC().Format(time.RFC3339Nano)
	if pull.Execution != nil {
		key = "source:" + pull.Execution.ID + ":" + phase
	}
	return &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: authority, IdempotencyKey: key, ExpectedRevision: revision,
		Target: &pb.EntityRef{Type: SourceType, Id: s.ID}, Schema: &pb.SchemaRef{Name: SchemaSourcePulled, Version: 1}, Payload: platform.Raw(map[string]any{"pull": pull})}
}

func PipelineRunSubmission(tenant, authority string, member platform.Member, p Pipeline, run PipelineRun, phase string, revision *uint32, now time.Time) *pb.Submission {
	key := "ran:" + p.ID + ":" + now.UTC().Format(time.RFC3339Nano)
	if run.Execution != nil {
		key = "pipeline:" + run.Execution.ID + ":" + phase
	}
	return &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: authority, IdempotencyKey: key, ExpectedRevision: revision,
		Target: &pb.EntityRef{Type: PipelineType, Id: p.ID}, Schema: &pb.SchemaRef{Name: SchemaPipelineRan, Version: 1}, Payload: platform.Raw(map[string]any{"run": run})}
}

func DatasetLoadSubmission(tenant, authority string, member platform.Member, dataset, producer string, marking platform.MarkingSet, rows []map[string]any, key, transaction string) *pb.Submission {
	if rows == nil {
		rows = []map[string]any{}
	}
	payload, _ := json.Marshal(map[string]any{"type": transaction, "rows": rows, "producer": producer, "marking": marking.Level, "compartments": marking.Compartments})
	return &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: authority, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: DatasetType, Id: dataset}, Schema: &pb.SchemaRef{Name: SchemaDatasetLoad, Version: 1}, Payload: payload}
}

func DeclaredSourceRows(info platform.EntityInfo, kind, source string, input []map[string]any, now time.Time, resolve func(string) string) ([]SourceRow, *platform.DataSourceBinding, error) {
	i := slices.IndexFunc(info.DataSources, func(candidate platform.DataSourceBinding) bool {
		return candidate.Kind == kind && candidate.Source == source
	})
	if i < 0 {
		return nil, nil, nil
	}
	binding := info.DataSources[i]
	rows, err := DeclaredRows(binding, input, now, resolve)
	return rows, &binding, err
}

func (o *Owner) MatchResolver(c platform.Caller, object, producer string, page func(offset int) ([]any, int, *kernel.Error)) *Resolver {
	rules, _, _ := platform.Find[Match](c, platform.Query{Limit: 500})
	var fixed []Match
	for _, rule := range rules {
		execution, err := o.ExecutionMatching(rule)
		if err != nil {
			log.Printf("matching definition refused: %v", err)
			continue
		}
		fixed = append(fixed, execution)
	}
	i := slices.IndexFunc(fixed, func(m Match) bool { return m.Object == object && m.State == "published" })
	if i < 0 {
		return nil
	}
	var records []map[string]any
	for offset := 0; ; offset += 500 {
		found, total, err := page(offset)
		if err != nil {
			break
		}
		for _, r := range found {
			raw, _ := json.Marshal(r)
			var fields map[string]any
			if json.Unmarshal(raw, &fields) == nil {
				records = append(records, fields)
			}
		}
		if len(found) < 500 || len(records) >= total {
			break
		}
	}
	return NewResolver(fixed[i], producer, records)
}

type RowApplication struct {
	Quarantined func() bool
	Pending     func(typ, id string, execution *IntegrationExecution) bool
	Target      func(object string) (app string, installed bool)
	Submit      func(member platform.Member, app, object string, binding *platform.DataSourceBinding, row SourceRow, key string, now time.Time) *kernel.Error
	Refused     func(app, key string) bool
}

func (a RowApplication) Apply(member platform.Member, plan IntegrationPreparation, execution *IntegrationExecution, now time.Time, pull *SourcePull) bool {
	app, installed := a.Target(plan.Object)
	for _, row := range plan.Rows {
		if a.Quarantined() || !a.Pending(plan.Type, plan.ID, execution) {
			return false
		}
		if row.Deleted && pull.Failed > 0 {
			continue
		}
		if row.Error != "" {
			pull.Fail(row.ID, row.Error)
			continue
		}
		if !installed {
			pull.Fail(row.ID, "the target object is no longer installed")
			continue
		}
		key := "integration:" + execution.ID + ":" + row.Key
		if err := a.Submit(member, app, plan.Object, plan.Binding, row, key, now); err != nil {
			if !a.Refused(app, key) {
				return false
			}
			pull.Fail(row.ID, err.Code.String()+": "+err.Message)
		} else if row.Deleted {
			pull.Deleted++
		} else {
			pull.Applied++
		}
	}
	return true
}

func (o *Owner) ConnectionMarkings(c platform.Caller, connection string) platform.MarkingSet {
	if connection == "" {
		return platform.MarkingSet{}
	}
	conn, _ := platform.Get[Connection](c, connection)
	fixed, err := o.ExecutionConnection(conn)
	if err != nil {
		return platform.MarkingSet{Level: "restricted"}
	}
	return platform.MarkingSet{Level: fixed.Marking, Compartments: fixed.Compartments}
}

func (o *Owner) ExecutionConnectionByID(c platform.Caller, id string) (Connection, error) {
	current, ok := platform.Get[Connection](c, id)
	if !ok {
		return current, fmt.Errorf("the connection is not ready")
	}
	return o.ExecutionConnection(current)
}

func (r Client) ReadSource(s Source, connection func(id string) (Connection, error), credential func(secret, scope, address string) ([]byte, bool)) ([]map[string]any, error) {
	var conn *Connection
	if s.Connection != "" {
		c, failure := connection(s.Connection)
		if failure != nil || c.State != "ready" {
			return nil, fmt.Errorf("the connection is not ready")
		}
		conn = &c
		if c.Secret != "" {
			if value, ok := credential(c.Secret, "connection/"+c.ID, c.Address); !ok || len(strings.TrimSpace(string(value))) == 0 {
				return nil, fmt.Errorf("the named connection secret is unavailable")
			}
		}
	}
	return r.Read(s, conn)
}

func (o *Owner) CollectPipelineInputs(c platform.Caller, p Pipeline, marking string, compartments []string) PipelineInputs {
	inputs := map[string]int{}
	data := map[string][]map[string]any{}
	var inputErr error
	for _, id := range append([]string{p.Input}, p.StepDatasets()...) {
		if _, seen := inputs[id]; seen {
			continue
		}
		rows, version, err := DatasetRows(c, id, 0)
		if err != nil {
			inputErr = err
			break
		}
		inputs[id], data[id] = version, rows
		if ds, ok := platform.Get[Dataset](c, id); ok {
			fixed, err := o.ExecutionDataset(ds)
			if err != nil {
				inputErr = err
				break
			}
			marking = platform.HigherMarking(marking, fixed.Marking)
			compartments = platform.JoinCompartments(compartments, fixed.Compartments)
		}
	}
	return PipelineInputs{Inputs: inputs, Data: data, Err: inputErr, Marking: marking, Compartments: compartments}
}

func DatasetOutputVersion(c platform.Caller, dataset, key string) (int, error) {
	versions, _, refusal := platform.Find[DatasetVersion](c, platform.Query{Domain: platform.Raw([][]any{{"dataset", "=", dataset}, {"requestKey", "=", key}}), Archived: true, Limit: 1})
	if refusal != nil {
		return 0, refusal
	}
	if len(versions) != 1 {
		return 0, fmt.Errorf("pipeline output version is ambiguous")
	}
	return versions[0].Version, nil
}

func PresentSourceRows(current []SourceRow) (map[string]bool, bool) {
	present := map[string]bool{}
	for _, row := range current {
		if row.Error != "" {
			return nil, false
		}
		present[row.ID] = true
	}
	return present, true
}

func SourceConnector(binding *platform.DataSourceBinding) string {
	return binding.Kind + ":" + binding.Source
}

func DeletionRows(ids []string, now time.Time) []SourceRow {
	slices.Sort(ids)
	var removed []SourceRow
	for _, id := range ids {
		removed = append(removed, SourceRow{ID: id, Verb: ".edit", Payload: json.RawMessage("{}"), Key: "delete:" + id, At: &now, Deleted: true})
	}
	return removed
}

func SourceAttributes(payload []byte, binding *platform.DataSourceBinding, row SourceRow) (map[string]json.RawMessage, []string, *kernel.Error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(payload, &values); err != nil || len(values) == 0 || row.At == nil {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source row has no mapped values or source time")
	}
	mapped := map[string]bool{}
	for _, column := range binding.Columns {
		mapped[column.Attribute] = true
	}
	attributes := make([]string, 0, len(values))
	for name := range values {
		if !mapped[name] {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source row has an undeclared attribute")
		}
		attributes = append(attributes, name)
	}
	slices.Sort(attributes)
	return values, attributes, nil
}
