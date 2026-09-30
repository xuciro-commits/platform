package platformserver

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) readQueryFrom(store *recordStore, c platform.Caller, app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	values := struct {
		For string `json:"for,omitempty"`
	}{}
	if len(inputs) > 0 && string(inputs) != "null" {
		if _, err := platform.DecodeValue(inputs, 32<<10); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(inputs, &keys); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Query inputs must be an object")
		}
		for key := range keys {
			if key != "for" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Unknown query parameter {field}", key)
			}
		}
		if err := json.Unmarshal(inputs, &values); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Query record reference must be a string")
		}
	}
	page, refusal := t.runQueryFrom(store, c.Member, app, name, values.For, now)
	if refusal != nil {
		return nil, refusal
	}
	q, _ := t.namedQuery(app, name)
	sources := []string{}
	store.mu.Lock()
	et := store.types[q.Object]
	store.mu.Unlock()
	for _, value := range page.Records {
		row, _ := json.Marshal(value)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(row, &fields)
		var id string
		_ = json.Unmarshal(fields["id"], &id)
		if id == "" {
			continue
		}
		ref := q.Object + "/" + id
		sources = append(sources, ref)
		if et != nil {
			for _, field := range et.info.Fields {
				if field.Reads(c.Roles[et.info.App]) {
					sources = append(sources, ref+"#"+field.Name)
				}
			}
		}
	}
	raw, err := json.Marshal(struct {
		Records []any    `json:"records"`
		Total   int      `json:"total"`
		Sources []string `json:"sources"`
	}{page.Records, page.Total, sources})
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return raw, nil
}

func (t *Tenant) readRecordFrom(store *recordStore, c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	domain, _ := json.Marshal([]any{[]any{"id", "=", id}})
	page, refusal := t.recordsFrom(store, c.Member, typ, platform.Query{Domain: domain, Limit: 1}, now)
	if refusal != nil {
		return nil, refusal
	}
	if len(page.Records) != 1 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The record is not available")
	}
	raw, err := json.Marshal(page.Records[0])
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return raw, nil
}

func (r runtime) ReadQuery(c platform.Caller, app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return r.t.readQueryFrom(r.t.records, c, app, name, inputs, now)
}
func (d *stagedDecision) ReadQuery(c platform.Caller, app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return d.tenant.readQueryFrom(d.records, c, app, name, inputs, now)
}
func (r runtime) ReadRecord(c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	return r.t.readRecordFrom(r.t.records, c, typ, id, now)
}
func (d *stagedDecision) ReadRecord(c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	return d.tenant.readRecordFrom(d.records, c, typ, id, now)
}
