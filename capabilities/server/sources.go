package platformserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	} else if rows, err := s.MapRows(raw); err != nil {
		pull.Error = err.Error()
	} else {
		pull.Cursor = s.Advance(raw)
		t.records.mu.Lock()
		et := t.records.types[s.Object]
		t.records.mu.Unlock()
		for _, row := range rows {
			pull.Rows++
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
					Target: &pb.EntityRef{Type: s.Object, Id: row.ID}, Schema: &pb.SchemaRef{Name: s.Object + verb, Version: 1}, Payload: row.Payload}, now)
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
	payload, _ := json.Marshal(map[string]any{"pull": pull})
	if member.ID == "" {
		member = t.automation(build.ID, false).Member
	}
	t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "pulled:" + s.ID + ":" + now.UTC().Format(time.RFC3339Nano),
		Target: &pb.EntityRef{Type: build.SourceType, Id: s.ID}, Schema: &pb.SchemaRef{Name: build.SchemaSourcePulled, Version: 1}, Payload: payload}, now)
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
