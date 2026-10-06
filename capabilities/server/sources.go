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
	} else if body, err := t.fetchSource(s); err != nil {
		pull.Error = err.Error()
	} else if rows, err := s.MapRows(body); err != nil {
		pull.Error = err.Error()
	} else {
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

// fetchSource reads the endpoint's answer with the guarded outbound sender:
// private addresses only when the source allows them, one bounded body.
func (t *Tenant) fetchSource(s build.Source) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("the URL is not valid")
	}
	req.Header.Set("Accept", "application/json")
	if name, value, ok := strings.Cut(s.Header, ":"); ok && strings.TrimSpace(name) != "" {
		req.Header.Set(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	send := t.Outbound
	if send == nil {
		send = guarded
	}
	resp, err := send(req, s.AllowPrivate)
	if err != nil {
		return nil, fmt.Errorf("the endpoint did not answer: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, build.SourceBody+1))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("the endpoint answered %d", resp.StatusCode)
	}
	if len(body) > build.SourceBody {
		return nil, fmt.Errorf("the answer exceeds %d bytes", build.SourceBody)
	}
	return body, nil
}
