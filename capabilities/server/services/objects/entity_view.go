package objects

import (
	"reflect"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func EntityView(store *records.Store, m platform.Member, hidden func(*records.Type) bool, authorization platform.AuthorizationEngine) []platform.RecordTypeInfo {
	store.Lock()
	defer store.Unlock()
	out := []platform.RecordTypeInfo{}
	for _, et := range store.Types() {
		if hidden(et) {
			continue
		}
		roles := m.RolesIn(et.Info().Type)
		if len(roles) > 0 && et.Info().Scope.LevelFor(roles, authorization) == platform.ScopeNone && et.Info().Scope.Participants == nil {
			continue
		}
		if len(roles) > 0 || et.Info().Scope.Participants != nil || et.Info().Scope.Through != nil {
			view, _ := ViewOf(m, et)
			info := view.Info()
			info.Fields = slices.DeleteFunc(slices.Clone(info.Fields), func(field platform.FieldInfo) bool { return field.Internal })
			out = append(out, info)
		}
	}
	slices.SortFunc(out, func(a, b platform.RecordTypeInfo) int { return strings.Compare(a.Type, b.Type) })
	return out
}

func EnterpriseReferencesRead(store *records.Store, m platform.Member, element string, offset, limit int, admit func(platform.Member) *platform.Error, visible func(*records.Type) (func(reflect.Value) bool, *platform.Error)) ([]records.EnterpriseReferenceGroup, *platform.Error) {
	if element == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A reference read names the element")
	}
	if err := admit(m); err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return records.EnterpriseReferences(store, element, offset, limit, func(et *records.Type) (func(reflect.Value) bool, []platform.FieldInfo, bool) {
		visibleRows, err := visible(et)
		if err != nil {
			return nil, nil, false
		}
		_, hiddenFields := ViewOf(m, et)
		return visibleRows, hiddenFields, true
	}, m.RolesIn), nil
}

func ProtocolRecords(page RecordPage, refusal *platform.Error) ([]any, *platform.Error) {
	if refusal != nil {
		return nil, refusal
	}
	if page.Total > 2000 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Protocol record reads support at most 2000 visible records")
	}
	return page.Records, nil
}
