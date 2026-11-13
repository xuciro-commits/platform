package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"time"
)

func (t *Tenant) enterpriseReferencesRead(m platform.Member, element string, offset, limit int, now time.Time) ([]records.EnterpriseReferenceGroup, *kernel.Error) {
	if element == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A reference read names the element")
	}
	if err := t.Admits(m); err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return records.EnterpriseReferences(t.Projection, element, offset, limit, func(et *records.Type) (func(reflect.Value) bool, []platform.FieldInfo, bool) {
		visible, err := t.visibleIn(t.Projection, m, et, now)
		if err != nil {
			return nil, nil, false
		}
		_, hidden := viewOf(m, et)
		return visible, hidden, true
	}, m.RolesIn), nil
}
