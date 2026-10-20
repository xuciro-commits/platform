package records

import (
	"encoding/json"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

// Copy is a deep copy of an entity struct, through its JSON.
func Copy(t reflect.Type, v any) reflect.Value {
	raw, _ := json.Marshal(v)
	out := reflect.New(t).Elem()
	json.Unmarshal(raw, out.Addr().Interface())
	return out
}

// EmptyLists makes every nil list of a record an empty one, nested ones too,
// so a record never reads null where its type says a list: a page reading
// levels[level] or start.join got null from a Go nil slice (the owner's
// testing, 2026-09-27). Lists marked omitempty are left out, as their tag says.
func EmptyLists(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		if _, ok := v.Interface().(time.Time); ok {
			return
		}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			x := v.Field(i)
			if x.Kind() == reflect.Slice && x.IsNil() && !strings.Contains(f.Tag.Get("json"), "omitempty") && x.CanSet() {
				x.Set(reflect.MakeSlice(x.Type(), 0, 0))
				continue
			}
			EmptyLists(x)
		}
	case reflect.Slice:
		for i := range v.Len() {
			EmptyLists(v.Index(i))
		}
	}
}

// Mask is a record with the hidden fields at their zero value.
func Mask(info platform.EntityInfo, v reflect.Value, hidden []platform.FieldInfo) any {
	c := Copy(info.Go, v.Interface())
	for _, f := range hidden {
		c.FieldByIndex(f.Index).SetZero()
	}
	return c.Interface()
}

// MaskHistory drops the hidden fields from a record's changes.
func MaskHistory(h []RecordChange, hidden []platform.FieldInfo) []RecordChange {
	if len(hidden) == 0 {
		return h
	}
	out := make([]RecordChange, 0, len(h))
	for _, c := range h {
		c.Fields = slices.DeleteFunc(slices.Clone(c.Fields), func(f FieldChange) bool {
			return slices.ContainsFunc(hidden, func(x platform.FieldInfo) bool { return x.Name == f.Field })
		})
		out = append(out, c)
	}
	return out
}
