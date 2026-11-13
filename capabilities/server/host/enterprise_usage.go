package host

import (
	"platformserver/kernel/records"
	controlowner "platformserver/services/security/control"
	"reflect"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	enterprisemodel "platformserver/services/security/organization"

	"platformserver/platform"
)

func (t *Tenant) enterpriseExternalUsage(c platform.Caller, store *records.Store, id, until string, now time.Time, details bool) enterprisemodel.EnterpriseUsage {
	result := enterprisemodel.EnterpriseUsage{CanDelete: true, CanClose: true, Items: []enterprisemodel.EnterpriseUsageItem{}}
	historicalPaths := t.enterpriseHistoricalReferencePaths(store)
	var types []*records.Type
	rows := map[string][]*records.Row{}
	func() {
		store.Lock()
		defer store.Unlock()
		types = make([]*records.Type, 0, store.TypeCount())
		for _, et := range store.Types() {
			if len(historicalPaths[et.Info().Type]) == 0 {
				continue
			}
			types = append(types, et)
			for _, r := range et.Rows().All() {
				rows[et.Info().Type] = append(rows[et.Info().Type], r)
			}
		}
	}()
	for _, et := range types {
		for _, r := range rows[et.Info().Type] {
			paths := records.MatchingRefsWithAccess(et.Info().Fields, "", r.Value, id, nil, true)
			historical := records.HistoryNamesEnterprise(historicalPaths[et.Info().Type], r.RecordHistory(), id)
			if len(paths) == 0 && !historical {
				continue
			}
			current := len(paths) > 0 && records.EnterpriseCurrent(et.Info(), r.Value, now, t.services.Rules)
			visible := false
			title, field := "", ""
			if details {
				if view, err := t.recordOf(c.Member, et.Info().Type, records.Header(r.Value).ID, now); err == nil {
					value := reflect.ValueOf(view.Record)
					if value.Kind() == reflect.Pointer {
						value = value.Elem()
					}
					allowed := records.MatchingRefs(et.Info().Fields, "", value, id, c.RolesIn(et.Info().App))
					if len(allowed) > 0 {
						visible = true
						title = records.RecordLabel(et.Info(), view.Record, records.Header(r.Value).ID)
						field = allowed[0].Path
					}
				}
			}
			result.Add(enterprisemodel.EnterpriseUsageItem{Kind: "record", Title: title, Ref: "record:" + et.Info().Type + "/" + records.Header(r.Value).ID, Field: field, Current: current}, visible)
		}
	}
	if directory, ok := t.App(controlowner.PlatformApp).(platform.IdentityDirectory); ok {
		visible := details && c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "action", Type: controlowner.MemberType, Action: controlowner.SchemaGrant}, now).Allow
		for _, usage := range directory.UnitGrantUsage(id, until) {
			ref := "member:" + usage.Member
			if strings.HasPrefix(usage.Member, "team:") {
				ref = usage.Member
			}
			result.Add(enterprisemodel.EnterpriseUsageItem{Kind: "grant", Title: usage.Member, Ref: ref, Current: usage.Current}, visible)
		}
	}
	if root := t.setting(platform.RouteCaller(c, controlowner.PlatformApp), "enterpriseRoot"); root == id {
		result.Add(enterprisemodel.EnterpriseUsageItem{Kind: "tenant", Title: t.ID, Ref: "tenant:" + t.ID, Current: true}, details)
	}
	return result
}

func (t *Tenant) checkEnterpriseUsage(c platform.Caller, store *records.Store, id, until string, deleting bool, now time.Time) *kernel.Error {
	if c.Replaying {
		return nil
	}
	result := t.enterpriseExternalUsage(c, store, id, until, now, false)
	if deleting && !result.CanDelete || !deleting && !result.CanClose {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The enterprise element is still in use; resolve its current uses before this change")
	}
	return nil
}

func (t *Tenant) enterpriseUsageRead(m platform.Member, id, until string, now time.Time) (enterprisemodel.EnterpriseUsage, *kernel.Error) {
	if err := t.Admits(m); err != nil {
		return enterprisemodel.EnterpriseUsage{}, err
	}
	if until == "" {
		until = now.UTC().Format(time.DateOnly)
	}
	if _, err := time.Parse(time.DateOnly, until); err != nil {
		return enterprisemodel.EnterpriseUsage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid effective period")
	}
	c := platform.NewCaller(runtime{t: t, scoped: true}, m, enterprisemodel.ID, false, false)
	result := t.enterpriseExternalUsage(c, t.Projection, id, until, now, true)
	model, err := t.tenantLinkModel()
	if err != nil {
		return result, err
	}
	result, found := model.Usage(id, until, result)
	if !found {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The enterprise element is unavailable")
	}
	return result, nil
}
