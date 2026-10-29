package platformserver

import records "platformserver/kernel/records"

import (
	"encoding/json"
	automation "platformserver/engine/automation"
	"platformserver/studio/authoring"
	"reflect"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) readQueryFrom(store *records.Store, c platform.Caller, app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	q, ok := t.namedQuery(app, name)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Named query is unavailable")
	}
	return t.readDeclaredQueryFrom(store, c, q, inputs, now)
}

func (t *Tenant) readQueryVersionFrom(store *records.Store, c platform.Caller, app, name string, version int, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	q, _, ok := t.memberQueryVersion(c.Member, platform.AssetRef{App: app, Kind: platform.AssetQuery, Name: name}, version)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Retained query is unavailable to this member")
	}
	schema := authoring.QueryInputSchema(q)
	if err := schema.Validate(inputs, 32<<10); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return t.readDeclaredQueryFrom(store, c, q, inputs, now)
}

func (t *Tenant) readDeclaredQueryFrom(store *records.Store, c platform.Caller, q platform.NamedQuery, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
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
	page, refusal := t.runDeclaredQueryFrom(store, c.Member, q, values.For, now)
	if refusal != nil {
		return nil, refusal
	}
	sources := []string{}
	store.Lock()
	et := store.Type(q.Object)
	store.Unlock()
	for _, value := range page.Records {
		if row, ok := value.(InterfaceRecord); ok {
			ref := row.Type + "/" + row.ID
			sources = append(sources, ref)
			for _, field := range q.InterfaceShape.Fields {
				sources = append(sources, ref+"#"+field.Name)
			}
			continue
		}
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
			for _, field := range et.Info().Fields {
				if field.ReadsAny(c.RolesIn(et.Info().App)) {
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

func (t *Tenant) readRecordFrom(store *records.Store, c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	domain, _ := json.Marshal([]any{[]any{"id", "=", id}})
	page, refusal := t.recordSelectionForCallerFrom(store, c, typ, platform.Query{Domain: domain, Limit: 1, BusinessTime: &platform.BusinessTime{All: true}}, now)
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
	store.Lock()
	et := store.Type(typ)
	store.Unlock()
	value := reflect.ValueOf(page.Records[0])
	if et != nil && value.Type() == et.Info().Go {
		var fields map[string]json.RawMessage
		changed := false
		for _, field := range et.Info().Fields {
			if len(field.Index) == 0 || field.Index[0] == 0 || field.Aside {
				continue
			}
			// omitempty is a transport choice. Owner rules and flows still
			// read initialized scalar zero/false, while nil optional values
			// and fields withheld by their actual sources remain absent.
			original := value.FieldByIndex(field.Index)
			switch original.Kind() {
			case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
				if !original.IsZero() {
					continue
				}
				if fields == nil {
					_ = json.Unmarshal(raw, &fields)
				}
				if _, present := fields[field.Name]; present {
					continue
				}
				decision := t.authorizeFrom(store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id, Fields: []string{field.Name}}, now, false)
				if decision.Allow && len(decision.Withheld) == 0 {
					fields[field.Name], _ = json.Marshal(original.Interface())
					changed = true
				}
			}
		}
		if changed {
			raw, _ = json.Marshal(fields)
		}
	}
	return raw, nil
}

func (r runtime) ReadQuery(c platform.Caller, app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return r.t.readQueryFrom(r.t.records, c, app, name, inputs, now)
}
func (d *stagedDecision) ReadQuery(c platform.Caller, app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return d.tenant.readQueryFrom(d.records, c, app, name, inputs, now)
}
func (r runtime) ReadQueryVersion(c platform.Caller, app, name string, version int, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return r.t.readQueryVersionFrom(r.t.records, c, app, name, version, inputs, now)
}
func (d *stagedDecision) ReadQueryVersion(c platform.Caller, app, name string, version int, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return d.tenant.readQueryVersionFrom(d.records, c, app, name, version, inputs, now)
}
func (r runtime) ReadRecord(c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	return r.t.readRecordFrom(r.t.records, c, typ, id, now)
}
func (d *stagedDecision) ReadRecord(c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	return d.tenant.readRecordFrom(d.records, c, typ, id, now)
}

func (t *Tenant) readRecordPathFrom(store *records.Store, c platform.Caller, typ, id string, path []string, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	return automation.ReadRecordPath(c, typ, id, path, now, t.entity, func(typ, id string, at time.Time) (json.RawMessage, *kernel.Error) {
		return t.readRecordFrom(store, c, typ, id, at)
	})
}

func (r runtime) ReadRecordPath(c platform.Caller, typ, id string, path []string, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	return r.t.readRecordPathFrom(r.t.records, c, typ, id, path, now)
}
func (d *stagedDecision) ReadRecordPath(c platform.Caller, typ, id string, path []string, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	return d.tenant.readRecordPathFrom(d.records, c, typ, id, path, now)
}

func (t *Tenant) readRuleQueryFrom(store *records.Store, c platform.Caller, set platform.RuleCollection, input json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	q, _, ok := t.memberQueryVersion(c.Member, platform.AssetRef{App: set.App, Kind: platform.AssetQuery, Name: set.Query}, set.Version)
	if !ok || q.Object != set.Object || (q.By == "") != (set.Of == "") {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Rule collection declaration is unavailable")
	}
	info, known := t.entity(q.Object)
	if !known {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Rule collection object is unavailable")
	}
	if set.Policy == "complete" && (info.Scope.LevelFor(c.RolesIn(info.App)) != platform.ScopeTenant || info.Scope.Through != nil) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Complete collection requires permission to read its whole object / 完整集合需要整个对象的读取权限")
	}
	return t.readQueryVersionFrom(store, c, set.App, set.Query, set.Version, input, now)
}
func (r runtime) ReadRuleQuery(c platform.Caller, set platform.RuleCollection, input json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return r.t.readRuleQueryFrom(r.t.records, c, set, input, now)
}
func (d *stagedDecision) ReadRuleQuery(c platform.Caller, set platform.RuleCollection, input json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return d.tenant.readRuleQueryFrom(d.records, c, set, input, now)
}
