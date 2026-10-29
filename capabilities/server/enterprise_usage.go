package platformserver

import (
	controlowner "platformserver/kernel/control"
	"platformserver/kernel/records"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	enterprisemodel "platformserver/engine/organization"

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
func enterpriseRecordCurrent(info platform.EntityInfo, value reflect.Value, now time.Time, engines ...platform.RuleEngine) bool {
	if records.Header(value).Archived {
		return false
	}
	if info.EnterpriseUse == nil {
		return true
	}
	program, err := info.CompileEnterpriseUse(engines...)
	if err != nil {
		return true
	}
	context := platform.ExprContext{Refs: map[string]platform.ExprValue{}, Now: now, Today: now.UTC().Format(time.DateOnly)}
	context.Refs["archived"] = platform.ExprPresent("boolean", records.Header(value).Archived)
	for _, field := range info.Fields {
		context.Refs[field.Name] = platform.ExprValue{Kind: platform.RuleFieldKind(field.Type), Status: "value", Data: jsonOf(value.FieldByIndex(field.Index).Interface())}
	}
	result, err := program.Eval(context)
	return err != nil || string(result.Value.Data) != "false"
}

func (t *Tenant) enterpriseExternalUsage(c platform.Caller, store *records.Store, id, until string, now time.Time, details bool) EnterpriseUsage {
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
			historical := historyNamesEnterprisePaths(historicalPaths[et.Info().Type], r.RecordHistory(), id)
			if len(paths) == 0 && !historical {
				continue
			}
			current := len(paths) > 0 && enterpriseRecordCurrent(et.Info(), r.Value, now, t.services.Rules)
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
			add(EnterpriseUsageItem{Kind: "record", Title: title, Ref: "record:" + et.Info().Type + "/" + records.Header(r.Value).ID, Field: field, Current: current}, visible)
		}
	}
	if directory, ok := t.app(controlowner.PlatformApp).(platform.IdentityDirectory); ok {
		visible := details && c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "action", Type: controlowner.MemberType, Action: controlowner.SchemaGrant}, now).Allow
		for _, usage := range directory.UnitGrantUsage(id, until) {
			ref := "member:" + usage.Member
			if strings.HasPrefix(usage.Member, "team:") {
				ref = usage.Member
			}
			add(EnterpriseUsageItem{Kind: "grant", Title: usage.Member, Ref: ref, Current: usage.Current}, visible)
		}
	}
	if root := t.setting(platform.RouteCaller(c, controlowner.PlatformApp), "enterpriseRoot"); root == id {
		add(EnterpriseUsageItem{Kind: "tenant", Title: t.ID, Ref: "tenant:" + t.ID, Current: true}, details)
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

func (t *Tenant) enterpriseUsageRead(m platform.Member, id, until string, now time.Time) (EnterpriseUsage, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return EnterpriseUsage{}, err
	}
	if until == "" {
		until = now.UTC().Format(time.DateOnly)
	}
	if _, err := time.Parse(time.DateOnly, until); err != nil {
		return EnterpriseUsage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid effective period")
	}
	c := platform.NewCaller(runtime{t}, m, enterprisemodel.ID, false, false)
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
			current := slices.ContainsFunc(r.PeriodsFromLegacy(), func(p platform.Period) bool { return p.Until == "" || p.Until > until })
			add("relationship", r.Relation, r.ID, current)
		}
	}
	for _, v := range model.Views {
		_, placed := v.Layout[id]
		if placed || slices.Contains(v.Elements, id) || slices.Contains(v.Context, id) || slices.ContainsFunc(v.Pins, func(p enterprisemodel.Pin) bool { return p.Anchor == id || p.Ref == "record:enterprise.element/"+id }) {
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
