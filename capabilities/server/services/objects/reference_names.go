package objects

import (
	"encoding/json"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"slices"
	"time"
)

func (t *Owner) referenceNames(m platform.Member, typ string, rows []any, now time.Time) (map[string]map[string]string, bool) {
	if len(rows) == 0 {
		return nil, false
	}
	source, ok := t.entity(typ)
	if !ok {
		return nil, false
	}
	m = t.authorizationMember(m)
	caller := t.caller(m, source.App)
	byField := map[string]map[string]bool{}
	fields := referenceFields(source)
	ids := make([]string, len(fields))
	for _, row := range rows {
		value := reflect.ValueOf(row)
		if value.Type() != source.Go {
			continue
		}
		for i, name := range fields {
			ids[i] = records.BusinessField(source, value, name)
		}
		addVisibleReferences(caller, typ, value.Field(0).Interface().(platform.Record).ID, fields, ids, now, byField)
	}
	return t.nameReferences(m, source, byField, now)
}

func referenceFields(source platform.RecordTypeInfo) []string {
	var fields []string
	for _, field := range source.Fields {
		if field.Type == "member" || field.Type == "reference" && field.Ref != "" {
			fields = append(fields, field.Name)
		}
	}
	return fields
}

func addVisibleReferences(caller platform.Caller, typ, record string, fields, ids []string, now time.Time, byField map[string]map[string]bool) {
	var populated []string
	for i, name := range fields {
		if ids[i] != "" {
			populated = append(populated, name)
		}
	}
	if len(populated) == 0 {
		return
	}
	visible := caller.Authorize(platform.AuthzRequest{Principal: caller.ID, Tenant: caller.Tenant, Kind: "read", Type: typ, Record: record, Fields: populated}, now)
	if !visible.Allow {
		return
	}
	for i, name := range fields {
		if ids[i] != "" && !slices.Contains(visible.Withheld, name) {
			if byField[name] == nil {
				byField[name] = map[string]bool{}
			}
			byField[name][ids[i]] = true
		}
	}
}

func (t *Owner) recordSetNames(m platform.Member, typ string, rows []json.RawMessage, now time.Time) (map[string]map[string]string, bool) {
	source, ok := t.entity(typ)
	if !ok || len(rows) == 0 {
		return nil, false
	}
	m = t.authorizationMember(m)
	caller := t.caller(m, source.App)
	byField := map[string]map[string]bool{}
	fields := referenceFields(source)
	ids := make([]string, len(fields))
	for _, raw := range rows {
		var row map[string]json.RawMessage
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		var record string
		if json.Unmarshal(row["id"], &record) != nil || record == "" {
			continue
		}
		clear(ids)
		for i, name := range fields {
			_ = json.Unmarshal(row[name], &ids[i])
		}
		addVisibleReferences(caller, typ, record, fields, ids, now, byField)
	}
	return t.nameReferences(m, source, byField, now)
}

func (t *Owner) historyNames(m platform.Member, typ string, history []records.RecordChange, now time.Time) (map[string]map[string]string, bool) {
	source, ok := t.entity(typ)
	if !ok || len(history) == 0 {
		return nil, false
	}
	byField := map[string]map[string]bool{}
	for _, change := range history {
		for _, entry := range change.Fields {
			field, ok := source.Field(entry.Field)
			if !ok || field.Type != "member" && (field.Type != "reference" || field.Ref == "") {
				continue
			}
			for _, raw := range []json.RawMessage{entry.Before, entry.After} {
				var value any
				if json.Unmarshal(raw, &value) != nil {
					continue
				}
				if typed, ok := value.(map[string]any); ok {
					value = typed["value"]
				}
				if id, ok := value.(string); ok && id != "" {
					if byField[field.Name] == nil {
						byField[field.Name] = map[string]bool{}
					}
					byField[field.Name][id] = true
				}
			}
		}
	}
	return t.nameReferences(t.authorizationMember(m), source, byField, now)
}

func (t *Owner) people(values ...any) map[string]string {
	out := map[string]string{}
	var walk func(any)
	walk = func(value any) {
		switch x := value.(type) {
		case map[string]any:
			for key, inner := range x {
				if id, ok := inner.(string); ok && key == "by" && id != "" {
					if _, seen := out[id]; !seen {
						out[id] = t.MemberDisplayName(id)
					}
				}
				walk(inner)
			}
		case []any:
			for _, inner := range x {
				walk(inner)
			}
		}
	}
	for _, value := range values {
		var tree any
		if raw, err := json.Marshal(value); err == nil && json.Unmarshal(raw, &tree) == nil {
			walk(tree)
		}
	}
	maps.DeleteFunc(out, func(_, name string) bool { return name == "" })
	if len(out) == 0 {
		return nil
	}
	return out
}

func (t *Owner) nameReferences(m platform.Member, source platform.RecordTypeInfo, byField map[string]map[string]bool, now time.Time) (map[string]map[string]string, bool) {
	byType := map[string]map[string]bool{}
	for _, field := range source.Fields {
		if field.Type == "reference" && field.Ref != "" {
			for id := range byField[field.Name] {
				if byType[field.Ref] == nil {
					byType[field.Ref] = map[string]bool{}
				}
				byType[field.Ref][id] = true
			}
		}
	}
	labels := map[string]map[string]string{}
	partial := false
	for target, ids := range byType {
		info, ok := t.entity(target)
		if !ok || info.Display == "" || info.Display == "id" {
			continue
		}
		caller := t.caller(m, info.App)
		labels[target] = map[string]string{}
		for batch := range slices.Chunk(slices.Collect(maps.Keys(ids)), 500) {
			set := platform.ObjectSet{Object: target, IDs: batch, Archived: true, AsOf: &platform.BusinessTime{All: true}}
			page, _, refusal := t.LoadObjectSet(t.Store, caller, set, Window{Limit: len(batch)}, now)
			if refusal != nil {
				partial = partial || (refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND)
				continue
			}
			for _, row := range page.Records {
				value := reflect.ValueOf(row)
				id := value.Field(0).Interface().(platform.Record).ID
				decision := caller.Authorize(platform.AuthzRequest{Principal: caller.ID, Tenant: caller.Tenant, Kind: "read", Type: target, Record: id, Fields: []string{info.Display}}, now)
				if decision.Allow && len(decision.Withheld) == 0 {
					if name := records.BusinessField(info, value, info.Display); name != "" {
						labels[target][id] = name
					}
				}
			}
		}
	}
	out := map[string]map[string]string{}
	memberNames := map[string]string{}
	for _, field := range source.Fields {
		for id := range byField[field.Name] {
			name := labels[field.Ref][id]
			if field.Type == "member" {
				if _, found := memberNames[id]; !found {
					memberNames[id] = t.MemberDisplayName(id)
				}
				name = memberNames[id]
			}
			if name != "" {
				if out[field.Name] == nil {
					out[field.Name] = map[string]string{}
				}
				out[field.Name][id] = name
			}
		}
	}
	return out, partial
}
