package platformserver

import (
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	enterprise "platformserver/internal/host/enterpriseapi"
	"platformserver/platform"
)

type EnterpriseUsageItem struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Ref     string `json:"ref,omitempty"`
	Field   string `json:"field,omitempty"`
	Current bool   `json:"current"`
}

type EnterpriseUsage struct {
	CanDelete          bool                  `json:"canDelete"`
	CanClose           bool                  `json:"canClose"`
	HasRestrictedUsage bool                  `json:"hasRestrictedUsage"`
	Items              []EnterpriseUsageItem `json:"items"`
}

// Current-use rules are pure owner declarations over this record, evaluated by
// the existing expression engine. No record-name or state-name inference lives here.
func enterpriseRecordCurrent(info platform.EntityInfo, value reflect.Value, now time.Time) bool {
	if recordOf(value).Archived {
		return false
	}
	if info.EnterpriseUse == nil {
		return true
	}
	program, err := info.CompileEnterpriseUse()
	if err != nil {
		return true
	}
	context := platform.ExprContext{Refs: map[string]platform.ExprValue{}, Now: now, Today: now.UTC().Format(time.DateOnly)}
	context.Refs["archived"] = platform.ExprPresent("boolean", recordOf(value).Archived)
	for _, field := range info.Fields {
		context.Refs[field.Name] = platform.ExprValue{Kind: platform.RuleFieldKind(field.Type), Status: "value", Data: jsonOf(value.FieldByIndex(field.Index).Interface())}
	}
	result, err := program.Eval(context)
	return err != nil || string(result.Value.Data) != "false"
}

func (t *Tenant) enterpriseExternalUsage(c platform.Caller, store *recordStore, id, until string, now time.Time, details bool) EnterpriseUsage {
	result := EnterpriseUsage{CanDelete: true, CanClose: true, Items: []EnterpriseUsageItem{}}
	historicalPaths := t.enterpriseHistoricalReferencePaths(store)
	add := func(item EnterpriseUsageItem, visible bool) {
		result.CanDelete = false
		if item.Current {
			result.CanClose = false
		}
		if visible {
			result.Items = append(result.Items, item)
		} else {
			result.HasRestrictedUsage = true
		}
	}
	store.mu.Lock()
	types := make([]*entityType, 0, len(store.types))
	rows := map[string][]*row{}
	for _, et := range store.types {
		if len(historicalPaths[et.info.Type]) == 0 {
			continue
		}
		types = append(types, et)
		for _, r := range et.rows.all() {
			rows[et.info.Type] = append(rows[et.info.Type], r)
		}
	}
	store.mu.Unlock()
	for _, et := range types {
		for _, r := range rows[et.info.Type] {
			paths := matchingRefsWithAccess(et.info.Fields, "", r.value, id, nil, true)
			historical := historyNamesEnterprisePaths(historicalPaths[et.info.Type], r.history, id)
			if len(paths) == 0 && !historical {
				continue
			}
			current := len(paths) > 0 && enterpriseRecordCurrent(et.info, r.value, now)
			visible := false
			title, field := "", ""
			if details {
				if view, err := t.RecordOf(c.Member, et.info.Type, recordOf(r.value).ID, now); err == nil {
					value := reflect.ValueOf(view.Record)
					if value.Kind() == reflect.Pointer {
						value = value.Elem()
					}
					allowed := matchingRefs(et.info.Fields, "", value, id, c.RolesIn(et.info.App))
					if len(allowed) > 0 {
						visible = true
						title = recordLabel(et, view.Record, recordOf(r.value).ID)
						field = allowed[0].Path
					}
				}
			}
			add(EnterpriseUsageItem{Kind: "record", Title: title, Ref: "record:" + et.info.Type + "/" + recordOf(r.value).ID, Field: field, Current: current}, visible)
		}
	}
	if directory, ok := t.app(PlatformApp).(host.IdentityDirectory); ok {
		visible := details && c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "action", Type: MemberType, Action: SchemaGrant}, now).Allow
		for _, usage := range directory.UnitGrantUsage(id, until) {
			ref := "member:" + usage.Member
			if strings.HasPrefix(usage.Member, "team:") {
				ref = usage.Member
			}
			add(EnterpriseUsageItem{Kind: "grant", Title: usage.Member, Ref: ref, Current: usage.Current}, visible)
		}
	}
	if root := t.setting(platform.RouteCaller(c, PlatformApp), "enterpriseRoot"); root == id {
		add(EnterpriseUsageItem{Kind: "tenant", Title: t.ID, Ref: "tenant:" + t.ID, Current: true}, details)
	}
	return result
}

func (t *Tenant) checkEnterpriseUsage(c platform.Caller, store *recordStore, id, until string, deleting bool, now time.Time) *kernel.Error {
	if c.Replaying {
		return nil
	}
	result := t.enterpriseExternalUsage(c, store, id, until, now, false)
	if deleting && !result.CanDelete || !deleting && !result.CanClose {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The enterprise element is still in use; resolve its current uses before this change")
	}
	return nil
}

func (t *Tenant) EnterpriseUsage(m platform.Member, id, until string, now time.Time) (EnterpriseUsage, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return EnterpriseUsage{}, err
	}
	if until == "" {
		until = now.UTC().Format(time.DateOnly)
	}
	if _, err := time.Parse(time.DateOnly, until); err != nil {
		return EnterpriseUsage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid effective period")
	}
	c := platform.NewCaller(runtime{t}, m, enterprise.ID, false, false)
	result := t.enterpriseExternalUsage(c, t.records, id, until, now, true)
	model, err := t.tenantLinkModel()
	if err != nil {
		return result, err
	}
	element := model.ElementOf(id)
	if element == nil || element.Deleted {
		return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The enterprise element is unavailable")
	}
	if element.Owner != "" {
		result.CanDelete = false
		result.CanClose = false
	}
	add := func(kind, title, ref string, current bool) {
		result.CanDelete = false
		if current {
			result.CanClose = false
		}
		result.Items = append(result.Items, EnterpriseUsageItem{Kind: kind, Title: title, Ref: ref, Current: current})
	}
	if element.Published {
		add("publication", element.Name, element.ID, true)
	}
	for _, r := range model.Relationships {
		if r.Source == id || r.Target == id {
			current := slices.ContainsFunc(r.PeriodsFromLegacy(), func(p enterprise.Period) bool { return p.Until == "" || p.Until > until })
			add("relationship", r.Relation, r.ID, current)
		}
	}
	for _, v := range model.Views {
		_, placed := v.Layout[id]
		if placed || slices.Contains(v.Elements, id) || slices.Contains(v.Context, id) || slices.ContainsFunc(v.Pins, func(p enterprise.Pin) bool { return p.Anchor == id || p.Ref == "record:enterprise.element/"+id }) {
			add("view", v.Name, v.ID, false)
		}
	}
	for _, link := range model.TenantLinks {
		if link.Organization == id {
			add("tenant-link", link.Tenant, link.ID, true)
		}
	}

	currentProperties := model.ElementPropertyUses(id, until)
	for _, otherID := range model.ElementPropertyUses(id, "") {
		other := model.ElementOf(otherID)
		if other != nil {
			add("property", other.Name, other.ID, slices.Contains(currentProperties, other.ID))
		}
	}
	return result, nil
}
