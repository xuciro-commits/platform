package objects

import (
	"encoding/json"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"time"
)

func (t *Owner) liveLookup(store *records.Store) func(string) (platform.RecordTypeInfo, bool) {
	return func(typ string) (platform.RecordTypeInfo, bool) {
		store.Lock()
		defer store.Unlock()
		et := store.Type(typ)
		if et == nil {
			return platform.RecordTypeInfo{}, false
		}
		return et.Info(), true
	}
}

// projectLive never mutates a stored row and is only called outside the store
// lock: evaluation may read another record or a declared inverse collection.
func (t *Owner) liveValue(store *records.Store, c platform.Caller, info platform.RecordTypeInfo, field platform.FieldInfo, record any, lookup func(string) (platform.RecordTypeInfo, bool), at time.Time) platform.ExprValue {
	if info.Ledger != nil && field.Name == "reversed" {
		var header struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(platform.Raw(record), &header)
		if field.ReadsAny(c.RolesIn(info.Type), c.Rules()) {
			d := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: info.Type, Record: header.ID, Fields: []string{field.Name}}, at)
			if d.Allow && len(d.Withheld) == 0 {
				if reversed, err := store.LedgerReversed(info.Type, header.ID); err == nil {
					return platform.ExprPresent("boolean", reversed)
				}
			}
		}
		return platform.ExprValue{Kind: "boolean", Status: "withheld"}
	}
	v, _ := protocol.LiveField(c, info, field, record, lookup, at)
	return v
}

func (t *Owner) projectLive(store *records.Store, c platform.Caller, typ string, record any, at time.Time) (any, []string) {
	lookup := t.liveLookup(store)
	info, ok := lookup(typ)
	if !ok || len(info.LiveRules) == 0 && info.Ledger == nil {
		return record, nil
	}
	rv := reflect.ValueOf(record)
	if rv.Type() != info.Go {
		return record, nil
	}
	out := records.Copy(info.Go, record)
	var restricted []string
	for _, field := range info.Fields {
		if field.Provenance != "live" {
			continue
		}
		dest := out.FieldByIndex(field.Index)
		dest.SetZero()
		value := t.liveValue(store, c, info, field, record, lookup, at)
		switch value.Status {
		case "value":
			v, err := protocol.ExprGoValue(value)
			if err == nil {
				if raw, e := json.Marshal(v); e == nil {
					if json.Unmarshal(raw, dest.Addr().Interface()) == nil {
						continue
					}
				}
			}
			restricted = append(restricted, field.Name)
		case "null", "missing":
		default:
			restricted = append(restricted, field.Name)
		}
	}
	return out.Interface(), restricted
}

func (t *Owner) projectLiveRaw(store *records.Store, c platform.Caller, typ string, raw json.RawMessage, at time.Time) json.RawMessage {
	lookup := t.liveLookup(store)
	info, ok := lookup(typ)
	if !ok || len(info.LiveRules) == 0 && info.Ledger == nil {
		return raw
	}
	var row map[string]json.RawMessage
	if json.Unmarshal(raw, &row) != nil {
		return raw
	}
	var restricted []string
	for _, field := range info.Fields {
		if field.Provenance != "live" {
			continue
		}
		delete(row, field.Name)
		value := t.liveValue(store, c, info, field, raw, lookup, at)
		switch value.Status {
		case "value":
			v, err := protocol.ExprGoValue(value)
			if err == nil {
				row[field.Name] = platform.Raw(v)
				continue
			}
			restricted = append(restricted, field.Name)
		case "null", "missing":
			row[field.Name] = json.RawMessage("null")
		default:
			restricted = append(restricted, field.Name)
		}
	}
	if len(restricted) > 0 {
		row["livewithheld"] = platform.Raw(restricted)
	}
	return platform.Raw(row)
}
