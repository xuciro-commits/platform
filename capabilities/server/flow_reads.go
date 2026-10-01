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

func (t *Tenant) readRecordPathFrom(store *recordStore, c platform.Caller, typ, id string, path []string, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	if len(path) == 0 || len(path) > 16 {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record paths need 1–16 fields")
	}
	var sources []string
	for i, name := range path {
		raw, refusal := t.readRecordFrom(store, c, typ, id, now)
		if refusal != nil {
			return nil, nil, refusal
		}
		info, known := t.entity(typ)
		field, declared := info.Field(name)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		value, visible := fields[name]
		if !known || !declared || !field.Reads(c.Roles[info.App]) {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Record input is unavailable to this member")
		}
		if !visible {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input has no value")
		}
		ref := typ + "/" + id
		sources = append(sources, ref, ref+"#"+name)
		if i == len(path)-1 {
			return value, sources, nil
		}
		if field.Type != "reference" || field.Ref == "" {
			// Opaque JSON fields retain ordinary JSON-path behavior; typed
			// form publications cannot infer a schema inside an opaque field.
			result, err := (platform.Binding{Source: "subject", Path: path[i+1:]}).Resolve(&platform.Run{}, value)
			if err != nil {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return result, sources, nil
		}
		if json.Unmarshal(value, &id) != nil || id == "" {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input reference is empty")
		}
		typ = field.Ref
	}
	return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input is unavailable")
}

func (r runtime) ReadRecordPath(c platform.Caller, typ, id string, path []string, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	return r.t.readRecordPathFrom(r.t.records, c, typ, id, path, now)
}
func (d *stagedDecision) ReadRecordPath(c platform.Caller, typ, id string, path []string, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	return d.tenant.readRecordPathFrom(d.records, c, typ, id, path, now)
}
