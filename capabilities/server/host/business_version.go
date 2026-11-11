package host

import (
	"encoding/json"

	"platformserver/kernel/records"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/objects"
)

func businessQuery(info platform.EntityInfo, q platform.Query, member platform.Member, at time.Time) platform.Query {
	return objects.BusinessQuery(info, q, member, at)
}

func (d *stagedDecision) PrepareBusinessRecord(c platform.Caller, entity any, at time.Time) (any, *kernel.Error) {
	d.Records.Lock()
	et := d.Records.Of(c, reflect.TypeOf(entity))
	if et == nil {
		d.Records.Unlock()
		return entity, nil
	}
	info := et.Info()
	value := records.Copy(info.Go, entity)
	record := records.Header(value)
	before := et.Rows().Get(record.ID)
	if before != nil {
		record.VersionPins = slices.Clone(records.Header(before.Value).VersionPins)
	} else {
		record.VersionPins = nil
	}
	d.Records.Unlock()
	if before == nil && len(info.VersionPins) > 0 && info.Lifecycle != nil && records.BusinessField(info, value, info.Lifecycle.Field) == "" {
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
		from, until := records.BusinessField(info, value, versioning.ValidFrom), records.BusinessField(info, value, versioning.ValidUntil)
		if _, err := platform.EffectiveAt(field.Type, from, until, from); err != nil {
			return refuse(err.Error())
		}
		if before != nil && (records.BusinessField(info, value, versioning.Key) != records.BusinessField(info, before.Value, versioning.Key) || records.BusinessField(info, value, versioning.Version) != records.BusinessField(info, before.Value, versioning.Version)) {
			return refuse("The business key and version are retained")
		}
	}
	for _, field := range info.Fields {
		if field.Type != "reference" {
			continue
		}
		id := records.BusinessField(info, value, field.Name)
		if id == "" {
			continue
		}
		var source *records.Type
		var selected *records.Row
		func() {
			d.Records.Lock()
			defer d.Records.Unlock()
			source = d.Records.Type(field.Ref)
			if source != nil {
				selected = source.Rows().Get(id)
			}
		}()
		if source == nil || source.Info().Versioning == nil {
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
		versioning := source.Info().Versioning
		bindingAt := slices.IndexFunc(info.VersionPins, func(binding platform.VersionPinBinding) bool { return binding.Reference == field.Name })
		ready := versioning.Pin == "on_create" && before == nil
		if strings.HasPrefix(versioning.Pin, "on_state:") && info.Lifecycle != nil {
			ready = records.BusinessField(info, value, info.Lifecycle.Field) == strings.TrimPrefix(versioning.Pin, "on_state:")
		}
		if before != nil && records.BusinessField(info, before.Value, field.Name) == id && (bindingAt < 0 || !ready) {
			continue
		}
		clock := businessQuery(source.Info(), platform.Query{}, c.Member, at).BusinessTime.At
		from, until := records.BusinessField(source.Info(), selected.Value, versioning.ValidFrom), records.BusinessField(source.Info(), selected.Value, versioning.ValidUntil)
		timeField, _ := source.Info().Field(versioning.ValidFrom)
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
		versionField, _ := source.Info().Field(versioning.Version)
		if !field.ReadsAny(c.RolesIn(info.App), c.Rules()) || !versionField.ReadsAny(c.RolesIn(source.Info().App), c.Rules()) {
			return refuse("The selected business version is not readable")
		}
		if _, readErr := c.ReadRecord(source.Info().Type, id, at); readErr != nil {
			return refuse("The selected business version is not readable")
		}
		raw := platform.Raw(selected.Value.FieldByIndex(versionField.Index).Interface())
		target, _ := info.Field(binding.Version)
		if json.Unmarshal(raw, value.FieldByIndex(target.Index).Addr().Interface()) != nil {
			return refuse("The pinned version has an incompatible field type")
		}
		record.VersionPins = append(record.VersionPins, platform.RecordVersionPin{Reference: field.Name, VersionField: binding.Version, Source: field.Ref + "/" + id, Version: raw, At: at})
	}
	for _, pin := range record.VersionPins {
		field, ok := info.Field(pin.Reference)
		if !ok || field.Ref+"/"+records.BusinessField(info, value, pin.Reference) != pin.Source {
			return refuse("A pinned business version cannot be changed")
		}
	}
	return value.Interface(), nil
}

func (d *stagedDecision) PrepareBusinessVersions(c platform.Caller, record any, at time.Time) *kernel.Error {
	value := reflect.ValueOf(record)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Business version preparation requires a record")
	}
	prepared, err := d.PrepareBusinessRecord(c, value.Elem().Interface(), at)
	if err == nil {
		value.Elem().Set(reflect.ValueOf(prepared))
	}
	return err
}
