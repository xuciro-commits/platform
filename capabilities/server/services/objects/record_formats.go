package objects

import (
	"platformserver/kernel/records"
	"platformserver/platform"
	"time"
)

func (t *Owner) recordFormats(store *records.Store, c platform.Caller, et *records.Type, id string, record any, now time.Time, held bool) map[string]platform.FieldFormat {
	var checked map[string]bool
	return et.Formats().Evaluate(record, func(field string) bool {
		if allowed, ok := checked[field]; ok {
			return allowed
		}
		decision := t.authorizeFrom(store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: et.Info().Type, Record: id, Fields: []string{field}}, now, held)
		if checked == nil {
			checked = map[string]bool{}
		}
		checked[field] = decision.Allow && len(decision.Withheld) == 0
		return checked[field]
	})
}
