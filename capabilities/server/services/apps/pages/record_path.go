package pages

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type RecordPath struct {
	Record func(c platform.Caller, typ, id string, at time.Time) (json.RawMessage, *kernel.Error)
	Entity func(typ string) (platform.EntityInfo, bool)
}

func (h RecordPath) Read(c platform.Caller, typ, id string, path []string, at time.Time) (json.RawMessage, []string, *kernel.Error) {
	if len(path) == 0 || len(path) > 16 {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record paths need 1–16 fields")
	}
	var sources []string
	for i, name := range path {
		raw, refusal := h.Record(c, typ, id, at)
		if refusal != nil {
			return nil, nil, refusal
		}
		info, found := h.Entity(typ)
		if !found {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Record path object is unavailable")
		}
		field, declared := info.Field(name)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		value, visible := fields[name]
		if !declared || !field.ReadsAny(c.RolesIn(info.App), c.Rules()) {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Record input is unavailable to this member")
		}
		permission := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id, Fields: []string{name}}, at)
		if !permission.Allow || len(permission.Withheld) > 0 {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Record input is unavailable to this member")
		}
		ref := typ + "/" + id
		sources = append(sources, ref, ref+"#"+name)
		if !visible {
			return nil, sources, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input has no value")
		}
		if i == len(path)-1 {
			return value, sources, nil
		}
		if field.Type != "reference" || field.Ref == "" {
			result, err := (platform.Binding{Source: "subject", Path: path[i+1:]}).Resolve(&platform.Run{}, value)
			if err != nil {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return result, sources, nil
		}
		if json.Unmarshal(value, &id) != nil || id == "" {
			return nil, sources, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input reference is empty")
		}
		typ = field.Ref
	}
	return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input is unavailable")
}
