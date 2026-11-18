package objects

import (
	"maps"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"slices"
	"time"
)

func (t *Owner) referenceNames(m platform.Member, typ string, rows []any, now time.Time) map[string]map[string]string {
	if len(rows) == 0 {
		return nil
	}
	source, ok := t.entity(typ)
	if !ok {
		return nil
	}
	byField := map[string]map[string]bool{}
	byType := map[string]map[string]bool{}
	m = t.authorizationMember(m)
	sourceCaller := t.caller(m, source.App)
	for _, field := range source.Fields {
		if field.Type != "reference" || field.Ref == "" {
			continue
		}
		for _, row := range rows {
			value := reflect.ValueOf(row)
			if value.Type() != source.Go {
				continue
			}
			if id := records.BusinessField(source, value, field.Name); id != "" {
				visible := sourceCaller.Authorize(platform.AuthzRequest{Principal: m.ID, Tenant: sourceCaller.Tenant, Kind: "read", Type: typ, Record: value.Field(0).Interface().(platform.Record).ID, Fields: []string{field.Name}}, now)
				if !visible.Allow || len(visible.Withheld) > 0 {
					continue
				}
				if byField[field.Name] == nil {
					byField[field.Name] = map[string]bool{}
				}
				if byType[field.Ref] == nil {
					byType[field.Ref] = map[string]bool{}
				}
				byField[field.Name][id], byType[field.Ref][id] = true, true
			}
		}
	}
	labels := map[string]map[string]string{}
	for target, ids := range byType {
		info, ok := t.entity(target)
		if !ok || info.Display == "" || info.Display == "id" {
			continue
		}
		caller := t.caller(m, info.App)
		labels[target] = map[string]string{}
		for batch := range slices.Chunk(slices.Collect(maps.Keys(ids)), 500) {
			set := platform.ObjectSet{Object: target, IDs: batch, Archived: true, AsOf: &platform.BusinessTime{All: true}}
			page, _, refusal := t.LoadObjectSet(t.Store, caller, set, Window{Limit: len(batch)}, now)
			if refusal != nil {
				continue
			}
			for _, row := range page.Records {
				value := reflect.ValueOf(row)
				id := value.Field(0).Interface().(platform.Record).ID
				decision := caller.Authorize(platform.AuthzRequest{Principal: caller.ID, Tenant: caller.Tenant, Kind: "read", Type: target, Record: id, Fields: []string{info.Display}}, now)
				if decision.Allow && len(decision.Withheld) == 0 {
					if name := records.BusinessField(info, value, info.Display); name != "" {
						labels[target][id] = name
					}
				}
			}
		}
	}
	out := map[string]map[string]string{}
	for _, field := range source.Fields {
		for id := range byField[field.Name] {
			if name := labels[field.Ref][id]; name != "" {
				if out[field.Name] == nil {
					out[field.Name] = map[string]string{}
				}
				out[field.Name][id] = name
			}
		}
	}
	return out
}
