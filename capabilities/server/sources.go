package platformserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"platformserver/internal/host"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

// Data source pulls. A pull is an import whose
// file comes from an endpoint: the host fetches the answer on the I/O lane,
// outside every lock and decision, then submits each mapped row as the
// publisher - the target's own generated create or edit, keyed by the source,
// a frozen execution and row position - and finally the pull's summary on the source. Each
// of those is an ordinary journaled input; a replay never fetches.

// PullSources pulls every due source of the tenant once; it runs on the
// five-second outside loop beside evaluations and embeddings.
func (t *Tenant) PullSources(now time.Time) {
	if t.businessStopped() || !t.ownerActive(construction.ID) {
		return
	}
	sources, refusal := coordinatedRecordRead(t, context.Background(), func() ([]construction.Source, *kernel.Error) {
		found, _, err := platform.Find[construction.Source](t.automation(construction.ID, false), platform.Query{Domain: json.RawMessage(`[["state","=","published"]]`), Sort: []string{"id"}, Limit: 200})
		if err != nil {
			return nil, err
		}
		owner := t.app(construction.ID).(host.IntegrationDefinitions)
		var fixed []construction.Source
		for _, source := range found {
			source, err := owner.ExecutionSource(source)
			if err != nil {
				log.Printf("source definition refused: %v", err)
				continue
			}
			fixed = append(fixed, source)
		}
		return fixed, nil
	})
	if refusal != nil {
		log.Printf("source preparation unavailable: %v", refusal)
		return
	}
	for _, source := range sources {
		if source.Due(now) {
			t.pullSource(source, now)
		}
	}
	t.ReconcileConfirmations(now)
	t.ReadDispatches(now)
	t.EvaluateObservations(now)
}

// integrationPreparation is immutable input and resolved output, held by the
// existing file owner rather than copied into every accepted result.
type integrationPreparation struct {
	Type       string                         `json:"type"`
	ID         string                         `json:"id"`
	Rows       []construction.SourceRow       `json:"rows,omitempty"`
	Data       []map[string]any               `json:"data"`
	Object     string                         `json:"object,omitempty"`
	Dataset    string                         `json:"dataset,omitempty"`
	Enterprise *construction.EnterpriseTarget `json:"enterprise,omitempty"`
	Producer   string                         `json:"producer"`
	Marking    string                         `json:"marking,omitempty"`
}

func (t *Tenant) prepareIntegration(member platform.Member, revision uint32, now time.Time, plan integrationPreparation) (*construction.IntegrationExecution, error) {
	raw, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxFlowFrameBytes {
		return nil, fmt.Errorf("the integration preparation exceeds its retained input budget")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	if err := t.files().Put(context.Background(), t.ID+"/artifacts/integration/"+digest+".json", raw, "application/json"); err != nil {
		return nil, err
	}
	identity := sha256.Sum256(platform.Raw([]any{t.ID, plan.Type, plan.ID, revision, now.UTC()}))
	return &construction.IntegrationExecution{ID: fmt.Sprintf("%x", identity), Member: member.ID, Scopes: slices.Clone(member.Scopes), Pending: true, Snapshot: digest}, nil
}

// integrationPending reads the already accepted execution, without journaling
// another artificial resume operation. A paused/replaced source cannot start
// more work, and the definition/actor of an accepted input stays fixed.
func (t *Tenant) integrationPending(typ, id string, execution *construction.IntegrationExecution) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.automation(construction.ID, false)
	var current *construction.IntegrationExecution
	switch typ {
	case construction.SourceType:
		source, ok := platform.Get[construction.Source](c, id)
		if !ok || source.Archived || source.State != "published" || source.Last == nil {
			return false
		}
		current = source.Last.Execution
	case construction.PipelineType:
		pipeline, ok := platform.Get[construction.Pipeline](c, id)
		if !ok || pipeline.Archived || pipeline.State != "published" || pipeline.Last == nil {
			return false
		}
		current = pipeline.Last.Execution
	}
	return current != nil && current.Pending && current.ID == execution.ID && current.Snapshot == execution.Snapshot && current.Member == execution.Member && slices.Equal(current.Scopes, execution.Scopes)
}

func (t *Tenant) integrationPreparation(execution *construction.IntegrationExecution) (integrationPreparation, error) {
	var plan integrationPreparation
	digest, err := hex.DecodeString(execution.Snapshot)
	if err != nil || len(digest) != sha256.Size {
		return plan, fmt.Errorf("the integration snapshot has an invalid digest")
	}
	reader, size, err := t.files().Get(context.Background(), t.ID+"/artifacts/integration/"+execution.Snapshot+".json")
	if err != nil {
		return plan, err
	}
	defer reader.Close()
	if size <= 0 || size > maxFlowFrameBytes {
		return plan, fmt.Errorf("the integration snapshot exceeds its retained input budget")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxFlowFrameBytes+1))
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

func (t *Tenant) sourceResult(member platform.Member, s construction.Source, pull construction.SourcePull, phase string, revision *uint32, now time.Time) *kernel.Error {
	payload := platform.Raw(map[string]any{"pull": pull})
	key := "pulled:" + s.ID + ":" + now.UTC().Format(time.RFC3339Nano)
	if pull.Execution != nil {
		key = "source:" + pull.Execution.ID + ":" + phase
	}
	_, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: construction.ID, IdempotencyKey: key, ExpectedRevision: revision,
		Target: &pb.EntityRef{Type: construction.SourceType, Id: s.ID}, Schema: &pb.SchemaRef{Name: construction.SchemaSourcePulled, Version: 1}, Payload: payload}, now)
	return err
}

func (t *Tenant) pullSource(s construction.Source, now time.Time) {
	pull := construction.SourcePull{At: now, DefinitionVersion: s.ExecutionVersion()}
	member, ok := t.Member(s.Puller)
	if s.PullerScopes != "" && json.Unmarshal([]byte(s.PullerScopes), &member.Scopes) != nil {
		log.Printf("source execution scopes are invalid")
		return
	}
	var plan integrationPreparation
	if s.Last != nil && s.Last.Execution != nil && s.Last.Execution.Pending {
		pull = *s.Last
		member, ok = t.Member(pull.Execution.Member)
		member.Scopes = slices.Clone(pull.Execution.Scopes)
		var err error
		plan, err = t.integrationPreparation(pull.Execution)
		if err != nil {
			log.Printf("source retained input unavailable: %v", err)
			return
		}
	} else if !ok {
		pull.Error = "The execution member is no longer available"
	} else if raw, err := t.readSource(s); err != nil {
		pull.Error = err.Error()
	} else {
		marking, refusal := recordRead(t, context.Background(), func() (string, *kernel.Error) { return t.connectionMarking(s.Connection), nil })
		if refusal != nil {
			log.Printf("source connection state unavailable: %v", refusal)
			return
		}
		pull.Cursor, pull.Rows = s.Advance(raw), len(raw)
		plan = integrationPreparation{Object: s.Object, Dataset: s.Dataset, Producer: "source:" + s.Name, Marking: marking}
		plan.Type, plan.ID = construction.SourceType, s.ID
		if s.Dataset != "" {
			if len(raw) > 0 || s.Cursor == "" {
				plan.Data = raw
				if plan.Data == nil {
					plan.Data = []map[string]any{}
				}
			}
		} else if construction.Guarded(plan.Marking) {
			pull.Error = "A confidential or restricted connection feeds a dataset; use a pipeline to apply field access rules"
		} else if rows, err := s.MapRows(raw); err != nil {
			pull.Error = err.Error()
		} else {
			plan.Rows = rows
			var refusal *kernel.Error
			pull.Merged, refusal = t.prepareRows(member, s.Object, s.Name, plan.Rows, now)
			if refusal != nil {
				log.Printf("source row preparation unavailable: %v", refusal)
				return
			}
		}
		if pull.Error == "" {
			execution, err := t.prepareIntegration(member, s.Revision, now, plan)
			if err != nil {
				log.Printf("source input retention failed: %v", err)
				return
			}
			pull.Execution = execution
			if err := t.sourceResult(member, s, pull, "prepared", &s.Revision, now); err != nil {
				return
			}
		}
	}
	if member.ID == "" {
		member = t.automation(construction.ID, false).Member
	}
	if pull.Execution != nil {
		if !t.integrationPending(construction.SourceType, s.ID, pull.Execution) {
			return
		}
		if !ok {
			pull.Error = "The execution member is no longer available"
		} else if plan.Dataset != "" {
			if plan.Data != nil {
				key := "source:" + pull.Execution.ID + ":dataset"
				if err := t.loadDatasetKey(member, plan.Dataset, plan.Producer, plan.Marking, plan.Data, key, pull.At); err != nil {
					if !t.integrationRefused(construction.ID, key) {
						return
					}
					pull.Error = err.Message
				} else {
					pull.Applied = len(plan.Data)
				}
			}
		} else if !t.applyRows(member, plan, pull.Execution, pull.At, &pull) {
			return
		}
		execution := *pull.Execution
		execution.Pending = false
		pull.Execution = &execution
	}
	_ = t.sourceResult(member, s, pull, "finished", nil, now)
}

// prepareRows fixes matching and create/edit selection before accepting the input.
func (t *Tenant) prepareRows(member platform.Member, object, producer string, rows []construction.SourceRow, now time.Time) (int, *kernel.Error) {
	return recordRead(t, context.Background(), func() (int, *kernel.Error) {
		merged := 0
		if resolver := t.resolver(member, object, producer, now); resolver != nil {
			for i := range rows {
				resolver.Resolve(&rows[i])
			}
			merged = resolver.Merged
		}
		t.records.mu.Lock()
		defer t.records.mu.Unlock()
		et := t.records.types[object]
		seen := map[string]bool{}
		for i := range rows {
			row := &rows[i]
			row.Key = fmt.Sprintf("row:%d", i)
			row.Verb = ".create"
			if seen[row.ID] || et != nil && et.rows.Get(row.ID) != nil {
				row.Verb = ".edit"
			}
			if row.Error == "" {
				seen[row.ID] = true
			}
		}
		return merged, nil
	})
}

// integrationRefused distinguishes a saved business refusal from an append
// failure. Only saved answers can finish a row; an uncommitted row stays pending.
func (t *Tenant) integrationRefused(authority, key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.committed.refusal(authority + "/" + key)
	return ok
}

func (t *Tenant) applyRows(member platform.Member, plan integrationPreparation, execution *construction.IntegrationExecution, now time.Time, pull *construction.SourcePull) bool {
	t.records.mu.Lock()
	et := t.records.types[plan.Object]
	t.records.mu.Unlock()
	for _, row := range plan.Rows {
		if t.quarantined() || !t.integrationPending(plan.Type, plan.ID, execution) {
			return false
		}
		if row.Error != "" {
			pull.Fail(row.ID, row.Error)
			continue
		}
		if et == nil {
			pull.Fail(row.ID, "the target object is no longer installed")
			continue
		}
		key := "integration:" + execution.ID + ":" + row.Key
		_, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: et.info.App, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: plan.Object, Id: row.ID}, Schema: &pb.SchemaRef{Name: plan.Object + row.Verb, Version: 1}, Payload: row.Payload}, now)
		if err != nil {
			if !t.integrationRefused(et.info.App, key) {
				return false
			}
			pull.Fail(row.ID, err.Code.String()+": "+err.Message)
		} else {
			pull.Applied++
		}
	}
	return true
}

// connectionMarking is what a connection classifies everything read through it as.
func (t *Tenant) connectionMarking(connection string) string {
	if connection == "" {
		return ""
	}
	conn, _ := platform.Get[construction.Connection](t.automation(construction.ID, false), connection)
	fixed, err := t.app(construction.ID).(host.IntegrationDefinitions).ExecutionConnection(conn)
	if err != nil {
		return "restricted"
	}
	return fixed.Marking
}

// resolver is the object's published matching rule over its
// records as they are now, or nil when rows land as they come.
func (t *Tenant) resolver(member platform.Member, object, producer string, now time.Time) *construction.Resolver {
	rules, _, _ := platform.Find[construction.Match](t.automation(construction.ID, false), platform.Query{Limit: 500})
	owner := t.app(construction.ID).(host.IntegrationDefinitions)
	var fixed []construction.Match
	for _, rule := range rules {
		execution, err := owner.ExecutionMatching(rule)
		if err != nil {
			log.Printf("matching definition refused: %v", err)
			continue
		}
		fixed = append(fixed, execution)
	}
	rules = fixed
	i := slices.IndexFunc(rules, func(m construction.Match) bool { return m.Object == object && m.State == "published" })
	if i < 0 {
		return nil
	}
	var records []map[string]any
	for offset := 0; ; offset += 500 {
		page, err := t.recordsRead(member, object, platform.Query{Limit: 500, Offset: offset, Sort: []string{"id"}}, now)
		if err != nil {
			break
		}
		for _, r := range page.Records {
			raw, _ := json.Marshal(r)
			var fields map[string]any
			if json.Unmarshal(raw, &fields) == nil {
				records = append(records, fields)
			}
		}
		if len(page.Records) < 500 || len(records) >= page.Total {
			break
		}
	}
	return construction.NewResolver(rules[i], producer, records)
}

// loadDataset appends rows as the dataset's next version, one journaled input.
func (t *Tenant) loadDataset(member platform.Member, dataset, producer, marking string, rows []map[string]any, now time.Time) *kernel.Error {
	return t.loadDatasetKey(member, dataset, producer, marking, rows, "load:"+dataset+":"+producer+":"+now.UTC().Format(time.RFC3339Nano), now)
}

func (t *Tenant) loadDatasetKey(member platform.Member, dataset, producer, marking string, rows []map[string]any, key string, now time.Time) *kernel.Error {
	if rows == nil {
		rows = []map[string]any{}
	}
	payload, _ := json.Marshal(map[string]any{"rows": rows, "producer": producer, "marking": marking})
	_, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: construction.ID, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: construction.DatasetType, Id: dataset}, Schema: &pb.SchemaRef{Name: construction.SchemaDatasetLoad, Version: 1}, Payload: payload}, now)
	return err
}

// readSource reads the source's rows by its profile: json and csv
// fetch one body, odata walks an entity set's pages, table queries a database.
func (t *Tenant) readSource(s construction.Source) ([]map[string]any, error) {
	var conn *construction.Connection
	if s.Connection != "" {
		t.mu.Lock()
		c, ok := platform.Get[construction.Connection](t.automation(construction.ID, false), s.Connection)
		if ok {
			var err error
			c, err = t.app(construction.ID).(host.IntegrationDefinitions).ExecutionConnection(c)
			if err != nil {
				ok = false
			}
		}
		t.mu.Unlock()
		if !ok || c.State != "ready" {
			return nil, fmt.Errorf("the connection is not ready")
		}
		conn = &c
		if c.Secret != "" {
			if value, ok := t.credentialHeader(c.Secret, "connection/"+c.ID, c.Address); !ok || len(strings.TrimSpace(string(value))) == 0 {
				return nil, fmt.Errorf("the named connection secret is unavailable")
			}
		}
	}
	return t.readSourceWithConnection(s, conn)
}

func (t *Tenant) readSourceWithConnection(s construction.Source, conn *construction.Connection) ([]map[string]any, error) {
	switch s.Profile {
	case "odata":
		return t.readOData(s, *conn)
	case "table":
		return t.readTable(s, *conn)
	}
	url, header, private := s.URL, s.Header, s.AllowPrivate
	if conn != nil {
		url, private = conn.Resolve(s.URL), conn.AllowPrivate
		header = t.authorization(*conn, url)
		if conn.Secret != "" && header == "" {
			return nil, fmt.Errorf("The connection credential is not authorized for this target")
		}
	}
	accept := "application/json"
	if s.Profile == "csv" {
		accept = "text/csv, text/plain"
	}
	body, err := t.fetchSource(url, header, private, accept)
	if err != nil {
		return nil, err
	}
	return s.Decode(body)
}

// authorization is the Authorization header a connection's secret gives, or none.
func (t *Tenant) authorization(c construction.Connection, target ...string) string {
	address := c.Address
	if len(target) > 0 {
		address = target[0]
	}
	if c.Secret == "" {
		return ""
	}
	if v, ok := t.credentialHeader(c.Secret, "connection/"+c.ID, address); ok && len(strings.TrimSpace(string(v))) > 0 {
		return "Authorization: " + strings.TrimSpace(string(v))
	}
	return ""
}

// fetchSource reads an answer with the guarded outbound sender: private
// addresses only when allowed, one bounded body.
func (t *Tenant) fetchSource(url, header string, allowPrivate bool, accept string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("the URL is not valid")
	}
	req.Header.Set("Accept", accept)
	if name, value, ok := strings.Cut(header, ":"); ok && strings.TrimSpace(name) != "" {
		req.Header.Set(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	send := t.Outbound
	if send == nil {
		send = guarded
	}
	resp, err := send(req, allowPrivate)
	if err != nil {
		return nil, fmt.Errorf("the endpoint did not answer: %v", err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, construction.SourceBody+1))
	if readErr != nil {
		return nil, fmt.Errorf("the response body could not be read")
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("the endpoint answered %d", resp.StatusCode)
	}
	if len(body) > construction.SourceBody {
		return nil, fmt.Errorf("the answer exceeds %d bytes", construction.SourceBody)
	}
	return body, nil
}
