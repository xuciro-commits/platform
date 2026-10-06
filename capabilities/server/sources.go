package platformserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

// Data source pulls (ADR-0061, block C of ADR-0057). A pull is an import whose
// file comes from an endpoint: the host fetches the answer on the I/O lane,
// outside every lock and decision, then submits each mapped row as the
// publisher - the target's own generated create or edit, keyed by the source,
// the row and its content - and finally the pull's summary on the source. Each
// of those is an ordinary journaled input; a replay never fetches.

// PullSources pulls every due source of the tenant once; it runs on the
// five-second outside loop beside evaluations and embeddings.
func (t *Tenant) PullSources(now time.Time) {
	if t.quarantined() {
		return
	}
	t.mu.Lock()
	sources, _, _ := platform.Find[build.Source](t.automation(build.ID, false), platform.Query{Domain: json.RawMessage(`[["state","=","published"]]`), Sort: []string{"id"}, Limit: 200})
	t.mu.Unlock()
	for _, s := range sources {
		if s.Due(now) {
			t.pullSource(s, now)
		}
	}
}

func (t *Tenant) pullSource(s build.Source, now time.Time) {
	pull := build.SourcePull{At: now}
	member, ok := t.Member(s.Puller)
	if !ok {
		pull.Error = "The publishing member is no longer available"
	} else if raw, err := t.readSource(s); err != nil {
		pull.Error = err.Error()
	} else if s.Dataset != "" {
		pull.Cursor, pull.Rows = s.Advance(raw), len(raw)
		if len(raw) > 0 || s.Cursor == "" {
			if err := t.loadDataset(member, s.Dataset, "source:"+s.Name, t.connectionMarking(s.Connection), raw, now); err != nil {
				pull.Error = err.Message
			} else {
				pull.Applied = len(raw)
			}
		}
	} else if rows, err := s.MapRows(raw); err != nil {
		pull.Error = err.Error()
	} else if build.Guarded(t.connectionMarking(s.Connection)) {
		pull.Error = "A confidential or restricted connection feeds a dataset; use a pipeline to apply field access rules"
	} else {
		pull.Cursor = s.Advance(raw)
		pull.Rows, pull.Applied = len(rows), 0
		t.applyRows(member, s.Object, s.Name, rows, now, &pull)
	}
	payload, _ := json.Marshal(map[string]any{"pull": pull})
	if member.ID == "" {
		member = t.automation(build.ID, false).Member
	}
	t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "pulled:" + s.ID + ":" + now.UTC().Format(time.RFC3339Nano),
		Target: &pb.EntityRef{Type: build.SourceType, Id: s.ID}, Schema: &pb.SchemaRef{Name: build.SchemaSourcePulled, Version: 1}, Payload: payload}, now)
}

// applyRows decides each mapped row as the member: the object's own create,
// or edit on conflict, keyed by content; failures are counted on the summary.
func (t *Tenant) applyRows(member platform.Member, object, producer string, rows []build.SourceRow, now time.Time, pull *build.SourcePull) {
	t.records.mu.Lock()
	et := t.records.types[object]
	t.records.mu.Unlock()
	if resolver := t.resolver(member, object, producer, now); resolver != nil {
		for i := range rows {
			resolver.Resolve(&rows[i])
		}
		pull.Merged = resolver.Merged
	}
	for _, row := range rows {
		if row.Error != "" {
			pull.Fail(row.ID, row.Error)
			continue
		}
		if et == nil {
			pull.Fail(row.ID, "the target object is no longer installed")
			continue
		}
		try := func(verb, key string) *kernel.Error {
			_, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: et.info.App, IdempotencyKey: key,
				Target: &pb.EntityRef{Type: object, Id: row.ID}, Schema: &pb.SchemaRef{Name: object + verb, Version: 1}, Payload: row.Payload}, now)
			return err
		}
		err := try(".create", row.Key)
		if err != nil && err.Code == pb.ErrorCode_ERROR_CODE_CONFLICT {
			err = try(".edit", row.Key+":edit")
		}
		if err != nil {
			pull.Fail(row.ID, err.Code.String()+": "+err.Message)
			continue
		}
		pull.Applied++
	}
}

// connectionMarking is what a connection classifies everything read through it as (ADR-0075).
func (t *Tenant) connectionMarking(connection string) string {
	if connection == "" {
		return ""
	}
	conn, _ := platform.Get[build.Connection](t.automation(build.ID, false), connection)
	return conn.Marking
}

// resolver is the object's published matching rule (ADR-0074) over its
// records as they are now, or nil when rows land as they come.
func (t *Tenant) resolver(member platform.Member, object, producer string, now time.Time) *build.Resolver {
	rules, _, _ := platform.Find[build.Match](t.automation(build.ID, false), platform.Query{Limit: 500})
	i := slices.IndexFunc(rules, func(m build.Match) bool { return m.Object == object && m.State == "published" })
	if i < 0 {
		return nil
	}
	var records []map[string]any
	for offset := 0; ; offset += 500 {
		page, err := t.Records(member, object, platform.Query{Limit: 500, Offset: offset, Sort: []string{"id"}}, now)
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
	return build.NewResolver(rules[i], producer, records)
}

// loadDataset appends rows as the dataset's next version, one journaled input.
func (t *Tenant) loadDataset(member platform.Member, dataset, producer, marking string, rows []map[string]any, now time.Time) *kernel.Error {
	if rows == nil {
		rows = []map[string]any{}
	}
	payload, _ := json.Marshal(map[string]any{"rows": rows, "producer": producer, "marking": marking})
	_, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "load:" + dataset + ":" + producer + ":" + now.UTC().Format(time.RFC3339Nano),
		Target: &pb.EntityRef{Type: build.DatasetType, Id: dataset}, Schema: &pb.SchemaRef{Name: build.SchemaDatasetLoad, Version: 1}, Payload: payload}, now)
	return err
}

// readSource reads the source's rows by its profile (ADR-0070): json and csv
// fetch one body, odata walks an entity set's pages, table queries a database.
func (t *Tenant) readSource(s build.Source) ([]map[string]any, error) {
	var conn *build.Connection
	if s.Connection != "" {
		t.mu.Lock()
		c, ok := platform.Get[build.Connection](t.automation(build.ID, false), s.Connection)
		t.mu.Unlock()
		if !ok || c.State != "ready" {
			return nil, fmt.Errorf("the connection is not ready")
		}
		conn = &c
		if c.Secret != "" {
			if value, ok := t.secret(c.Secret); !ok || len(strings.TrimSpace(string(value))) == 0 {
				return nil, fmt.Errorf("the named connection secret is unavailable")
			}
		}
	}
	switch s.Profile {
	case "odata":
		return t.readOData(s, *conn)
	case "table":
		return t.readTable(s, *conn)
	}
	url, header, private := s.URL, s.Header, s.AllowPrivate
	if conn != nil {
		url, private = conn.Resolve(s.URL), conn.AllowPrivate
		header = t.authorization(*conn)
	}
	body, err := t.fetchSource(url, header, private, s.Profile == "csv")
	if err != nil {
		return nil, err
	}
	return s.Decode(body)
}

// authorization is the Authorization header a connection's secret gives, or none.
func (t *Tenant) authorization(c build.Connection) string {
	if c.Secret == "" {
		return ""
	}
	if v, ok := t.secret(c.Secret); ok && len(strings.TrimSpace(string(v))) > 0 {
		return "Authorization: " + strings.TrimSpace(string(v))
	}
	return ""
}

// fetchSource reads an answer with the guarded outbound sender: private
// addresses only when allowed, one bounded body.
func (t *Tenant) fetchSource(url, header string, allowPrivate, csv bool) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("the URL is not valid")
	}
	req.Header.Set("Accept", map[bool]string{false: "application/json", true: "text/csv, text/plain"}[csv])
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
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, build.SourceBody+1))
	if readErr != nil {
		return nil, fmt.Errorf("the response body could not be read")
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("the endpoint answered %d", resp.StatusCode)
	}
	if len(body) > build.SourceBody {
		return nil, fmt.Errorf("the answer exceeds %d bytes", build.SourceBody)
	}
	return body, nil
}
