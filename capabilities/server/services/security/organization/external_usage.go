package organization

import (
	"reflect"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	controlowner "platformserver/services/security/control"
)

type ExternalUsage struct {
	Tenant     string
	Store      *records.Store
	Paths      func(*records.Store) map[string][][]string
	Rules      platform.RuleEngine
	Admit      func(platform.Member) *kernel.Error
	Record     func(m platform.Member, typ, id string, now time.Time) (any, bool)
	GrantUsage func(id, until string) []platform.UnitGrantUsage
	Root       func(platform.Caller) string
	Caller     func(m platform.Member, app string) platform.Caller
	Model      func() (Model, *kernel.Error)
}

func (u ExternalUsage) External(c platform.Caller, id, until string, now time.Time, details bool) EnterpriseUsage {
	result := EnterpriseUsage{CanDelete: true, CanClose: true, Items: []EnterpriseUsageItem{}}
	for _, hit := range records.EnterpriseRecordHits(u.Store, u.Paths(u.Store), id, now, u.Rules) {
		et, r := hit.Type, hit.Row
		visible := false
		title, field := "", ""
		if details {
			if record, readable := u.Record(c.Member, et.Info().Type, records.Header(r.Value).ID, now); readable {
				value := reflect.ValueOf(record)
				if value.Kind() == reflect.Pointer {
					value = value.Elem()
				}
				allowed := records.MatchingRefs(et.Info().Fields, "", value, id, c.RolesIn(et.Info().App))
				if len(allowed) > 0 {
					visible = true
					title = records.RecordLabel(et.Info(), record, records.Header(r.Value).ID)
					field = allowed[0].Path
				}
			}
		}
		result.Add(EnterpriseUsageItem{Kind: "record", Title: title, Ref: "record:" + et.Info().Type + "/" + records.Header(r.Value).ID, Field: field, Current: hit.Current}, visible)
	}
	if u.GrantUsage != nil {
		visible := details && c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "action", Type: controlowner.MemberType, Action: controlowner.SchemaGrant}, now).Allow
		result.AddGrants(u.GrantUsage(id, until), visible)
	}
	if u.Root(platform.RouteCaller(c, controlowner.PlatformApp)) == id {
		result.Add(EnterpriseUsageItem{Kind: "tenant", Title: u.Tenant, Ref: "tenant:" + u.Tenant, Current: true}, details)
	}
	return result
}

func (u ExternalUsage) Check(c platform.Caller, id, until string, deleting bool, now time.Time) *kernel.Error {
	if c.Replaying {
		return nil
	}
	result := u.External(c, id, until, now, false)
	if deleting && !result.CanDelete || !deleting && !result.CanClose {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The enterprise element is still in use; resolve its current uses before this change")
	}
	return nil
}

func (u ExternalUsage) Read(m platform.Member, id, until string, now time.Time) (EnterpriseUsage, *kernel.Error) {
	if err := u.Admit(m); err != nil {
		return EnterpriseUsage{}, err
	}
	until, err := UsageUntil(until, now)
	if err != nil {
		return EnterpriseUsage{}, err
	}
	result := u.External(u.Caller(m, ID), id, until, now, true)
	model, err := u.Model()
	if err != nil {
		return result, err
	}
	result, found := model.Usage(id, until, result)
	if !found {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The enterprise element is unavailable")
	}
	return result, nil
}
