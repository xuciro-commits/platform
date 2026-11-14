package objects

import (
	"reflect"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func EntityView(store *records.Store, m platform.Member, hidden func(*records.Type) bool, authorization platform.AuthorizationEngine) []platform.EntityInfo {
	store.Lock()
	defer store.Unlock()
	out := []platform.EntityInfo{}
	for _, et := range store.Types() {
		if hidden(et) {
			continue
		}
		roles := m.RolesIn(et.Info().App)
		if len(roles) > 0 && et.Info().Scope.LevelFor(roles, authorization) == platform.ScopeNone && et.Info().Scope.Participants == nil {
			continue
		}
		if len(roles) > 0 || et.Info().Scope.Participants != nil || et.Info().Scope.Through != nil {
			view, _ := ViewOf(m, et)
			out = append(out, view.Info())
		}
	}
	slices.SortFunc(out, func(a, b platform.EntityInfo) int { return strings.Compare(a.Type, b.Type) })
	return out
}

func EnterpriseReferencesRead(store *records.Store, m platform.Member, element string, offset, limit int, admit func(platform.Member) *kernel.Error, visible func(*records.Type) (func(reflect.Value) bool, *kernel.Error)) ([]records.EnterpriseReferenceGroup, *kernel.Error) {
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

func ProtocolRecords(page RecordPage, refusal *kernel.Error) ([]any, *kernel.Error) {
	if refusal != nil {
		return nil, refusal
	}
	if page.Total > 200 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Protocol record reads support at most 200 visible records")
	}
	return page.Records, nil
}
