package objects

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"

	"time"
)

type RecordPage struct {
	Records []any                                      `json:"records"`
	Total   int                                        `json:"total"`
	Partial bool                                       `json:"partial,omitempty"`
	Formats map[string]map[string]platform.FieldFormat `json:"formats,omitempty"`
}

func (t *Owner) recordsRead(m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.recordsFrom(t.Store, m, typ, q, now)
}

func (t *Owner) recordsFrom(s *records.Store, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	if q.Limit < 1 || q.Limit > 500 {
		q.Limit = 500
	}
	return t.recordSelectionFrom(s, m, typ, q, now)
}

func (t *Owner) recordSelectionFrom(s *records.Store, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.recordSelectionForCallerFrom(s, t.caller(m, ""), typ, q, now)
}

func (t *Owner) recordSelectionForCallerFrom(s *records.Store, c platform.Caller, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	c.Member = t.authorizationMember(c.Member)
	around, err := t.traversalAround(s, c.Member, typ, q)
	if err != nil {
		return RecordPage{}, err
	}
	if q.Set != nil && q.CheckSet() != nil {
		return RecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	page, _, err := t.LoadObjectSet(s, c, q.ObjectSet(typ, around), Window{Sort: q.Sort, Offset: q.Offset, Limit: q.Limit}, now)
	return page, err
}

func (t *Owner) Records(m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.recordsRead(m, typ, q, now)
}

func (t *Owner) RecordsFrom(store *records.Store, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.recordsFrom(store, m, typ, q, now)
}

func (t *Owner) SelectionFrom(store *records.Store, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.recordSelectionFrom(store, m, typ, q, now)
}

func (t *Owner) readRecordFrom(store *records.Store, c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
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

func (t *Owner) ReadRecordFrom(store *records.Store, c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	return t.readRecordFrom(store, c, typ, id, now)
}
