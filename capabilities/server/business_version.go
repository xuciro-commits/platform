package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/records"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func businessField(info platform.EntityInfo, value reflect.Value, name string) string {
	field, ok := info.Field(name)
	if !ok {
		return ""
	}
	v := value.FieldByIndex(field.Index)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		return v.Interface().(time.Time).Format(time.RFC3339Nano)
	}
	return fmt.Sprint(v.Interface())
}

func businessQuery(info platform.EntityInfo, q platform.Query, member platform.Member, at time.Time) platform.Query {
	if info.Versioning == nil || q.BusinessTime != nil && (q.BusinessTime.All || q.BusinessTime.At != "") {
		return q
	}
	field, _ := info.Field(info.Versioning.ValidFrom)
	point := member.Today(at)
	if field.Type == "datetime" {
		point = at.UTC().Format(time.RFC3339Nano)
	}
	q.BusinessTime = &platform.BusinessTime{At: point}
	return q
}

func (s *recordStore) effectivePredicate(et *entityType, q platform.Query) (func(reflect.Value) bool, *kernel.Error) {
	v := et.info.Versioning
	if q.BusinessTime != nil && q.BusinessTime.All && q.BusinessTime.At != "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Business time selects either an instant or all versions")
	}
	if v == nil || q.BusinessTime != nil && q.BusinessTime.All {
		return nil, nil
	}
	if q.BusinessTime == nil || q.BusinessTime.At == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Business time must be captured before reading versioned data")
	}
	info := s.types[et.info.Type].info
	field, _ := info.Field(v.ValidFrom)
	if _, err := platform.EffectiveAt(field.Type, q.BusinessTime.At, "", q.BusinessTime.At); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return func(value reflect.Value) bool {
		active, err := platform.EffectiveAt(field.Type, businessField(info, value, v.ValidFrom), businessField(info, value, v.ValidUntil), q.BusinessTime.At)
		return err == nil && active
	}, nil
}

func (d *stagedDecision) PrepareBusinessVersions(c platform.Caller, record any, at time.Time) *kernel.Error {
	value := reflect.ValueOf(record)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Business version preparation requires a record")
	}
	prepared, err := d.prepareBusinessRecord(c, value.Elem().Interface(), at)
	if err == nil {
		value.Elem().Set(reflect.ValueOf(prepared))
	}
	return err
}

func (d *stagedDecision) prepareBusinessRecord(c platform.Caller, entity any, at time.Time) (any, *kernel.Error) {
	d.records.mu.Lock()
	et := d.records.of(c, reflect.TypeOf(entity))
	if et == nil {
		d.records.mu.Unlock()
		return entity, nil
	}
	info := et.info
	value := copyOf(info.Go, entity)
	record := records.Header(value)
	before := et.rows.Get(record.ID)
	if before != nil {
		record.VersionPins = slices.Clone(records.Header(before.Value).VersionPins)
	} else {
		record.VersionPins = nil
	}
	d.records.mu.Unlock()
	if before == nil && len(info.VersionPins) > 0 && info.Lifecycle != nil && businessField(info, value, info.Lifecycle.Field) == "" {
		field, _ := info.Field(info.Lifecycle.Field)
		value.FieldByIndex(field.Index).SetString(info.Lifecycle.Initial)
	}
	refuse := func(message string) (any, *kernel.Error) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)
	}
	if versioning := info.Versioning; versioning != nil {
		if record.Archived {
			return refuse("Business versions are retired by valid-until and cannot be archived")
		}
		field, _ := info.Field(versioning.ValidFrom)
		from, until := businessField(info, value, versioning.ValidFrom), businessField(info, value, versioning.ValidUntil)
		if _, err := platform.EffectiveAt(field.Type, from, until, from); err != nil {
			return refuse(err.Error())
		}
		if before != nil && (businessField(info, value, versioning.Key) != businessField(info, before.Value, versioning.Key) || businessField(info, value, versioning.Version) != businessField(info, before.Value, versioning.Version)) {
			return refuse("The business key and version are retained")
		}
	}
	for _, field := range info.Fields {
		if field.Type != "reference" {
			continue
		}
		id := businessField(info, value, field.Name)
		if id == "" {
			continue
		}
		d.records.mu.Lock()
		source := d.records.types[field.Ref]
		var selected *records.Row
		if source != nil {
			selected = source.rows.Get(id)
		}
		d.records.mu.Unlock()
		if source == nil || source.info.Versioning == nil {
			continue
		}
		if selected == nil || records.Header(selected.Value).Archived {
			return refuse("The selected business version is unavailable")
		}
		pinAt := slices.IndexFunc(record.VersionPins, func(pin platform.RecordVersionPin) bool { return pin.Reference == field.Name })
		if pinAt >= 0 {
			pin := record.VersionPins[pinAt]
			versionField, ok := info.Field(pin.VersionField)
			if !ok || pin.Source != field.Ref+"/"+id || string(platform.Raw(value.FieldByIndex(versionField.Index).Interface())) != string(pin.Version) {
				return refuse("A pinned business version cannot be changed")
			}
			continue
		}
		versioning := source.info.Versioning
		bindingAt := slices.IndexFunc(info.VersionPins, func(binding platform.VersionPinBinding) bool { return binding.Reference == field.Name })
		ready := versioning.Pin == "on_create" && before == nil
		if strings.HasPrefix(versioning.Pin, "on_state:") && info.Lifecycle != nil {
			ready = businessField(info, value, info.Lifecycle.Field) == strings.TrimPrefix(versioning.Pin, "on_state:")
		}
		if before != nil && businessField(info, before.Value, field.Name) == id && (bindingAt < 0 || !ready) {
			continue
		}
		clock := businessQuery(source.info, platform.Query{}, c.Member, at).BusinessTime.At
		from, until := businessField(source.info, selected.Value, versioning.ValidFrom), businessField(source.info, selected.Value, versioning.ValidUntil)
		timeField, _ := source.info.Field(versioning.ValidFrom)
		active, err := platform.EffectiveAt(timeField.Type, from, until, clock)
		if err != nil || !active {
			return refuse("New references require a currently effective business version")
		}
		if bindingAt < 0 || versioning.Pin == "none" {
			continue
		}
		if !ready {
			continue
		}
		binding := info.VersionPins[bindingAt]
		versionField, _ := source.info.Field(versioning.Version)
		if !field.ReadsAny(c.RolesIn(info.App)) || !versionField.ReadsAny(c.RolesIn(source.info.App)) {
			return refuse("The selected business version is not readable")
		}
		if _, readErr := c.ReadRecord(source.info.Type, id, at); readErr != nil {
			return refuse("The selected business version is not readable")
		}
		raw := platform.Raw(selected.Value.FieldByIndex(versionField.Index).Interface())
		target, _ := info.Field(binding.Version)
		if json.Unmarshal(raw, value.FieldByIndex(target.Index).Addr().Interface()) != nil {
			return refuse("The pinned version has an incompatible field type")
		}
		record.VersionPins = append(record.VersionPins, platform.RecordVersionPin{Reference: field.Name, VersionField: binding.Version, Source: field.Ref + "/" + id, Version: raw, At: at})
	}
	// Removing a pinned reference is also a mutation, even though the empty
	// value did not enter the ordinary reference-validation loop above.
	for _, pin := range record.VersionPins {
		field, ok := info.Field(pin.Reference)
		if !ok || field.Ref+"/"+businessField(info, value, pin.Reference) != pin.Source {
			return refuse("A pinned business version cannot be changed")
		}
	}
	return value.Interface(), nil
}

func (d *stagedDecision) checkBusinessVersionPins() *kernel.Error {
	types := d.records.sortedTypes()
	if !slices.ContainsFunc(types, func(et *entityType) bool { return et.info.Versioning != nil }) {
		return nil
	}
	pinned := map[string]bool{}
	// Only versions already used before this decision freeze their contents.
	// A new version, its children and its first consumer may be accepted together.
	for _, et := range d.records.parent.sortedTypes() {
		for _, current := range et.rows.All() {
			for _, pin := range records.Header(current.Value).VersionPins {
				pinned[pin.Source] = true
			}
		}
	}
	refs := make([]string, 0, len(d.constraintWrites))
	for ref := range d.constraintWrites {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	for _, et := range types {
		if et.info.Versioning == nil {
			continue
		}
		for id, current := range et.rows.All() {
			rootRef := et.info.Type + "/" + id
			if !pinned[rootRef] {
				continue
			}
			if write, touched := d.constraintWrites[rootRef]; touched && write.before != nil {
				for _, field := range et.info.Fields {
					if field.Name == et.info.Versioning.ValidUntil {
						continue
					}
					if !reflect.DeepEqual(current.Value.FieldByIndex(field.Index).Interface(), write.before.Value.FieldByIndex(field.Index).Interface()) {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Pinned business version content is retained; create another version")
					}
				}
			}
			for _, ref := range refs {
				if ref == rootRef {
					continue
				}
				write := d.constraintWrites[ref]
				typ, changed, _ := strings.Cut(ref, "/")
				for _, set := range et.info.Sets {
					if d.declaredSetDepends(et.info, current, set, typ, changed, write.before) {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A pinned business version's declared records are retained; create another version")
					}
				}
			}
		}
	}
	return nil
}
