package platformserver

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// namedQuery is the query an app declares, by its name.
func (t *Tenant) namedQuery(app, name string) (platform.NamedQuery, bool) {
	a := t.app(app)
	if a == nil {
		return platform.NamedQuery{}, false
	}
	for _, q := range a.Manifest().Queries {
		if q.Name == name {
			return q, true
		}
	}
	return platform.NamedQuery{}, false
}

// RunQuery runs one declared query as m (ADR-0040 21c): its fixed conditions,
// and the record it is run for through its reference. It is the member's own
// read of the object, so a page and an agent tool see exactly what m may.
func (t *Tenant) RunQuery(m platform.Member, app, name, of string, now time.Time) (RecordPage, *kernel.Error) {
	q, ok := t.namedQuery(app, name)
	if !ok {
		return RecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if q.By != "" && of == "" {
		return RecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{query} needs the record it is run for", q.Title)
	}
	var terms []any
	if len(q.Domain) > 0 {
		_ = json.Unmarshal(q.Domain, &terms) // checked when the app was composed
	}
	if q.By != "" {
		terms = append(terms, []any{q.By, "=", of})
	}
	query := platform.Query{Sort: q.Sort, Limit: q.Limit}
	if query.Limit <= 0 || query.Limit > 200 {
		query.Limit = 200
	}
	if len(terms) > 0 {
		query.Domain, _ = json.Marshal(terms)
	}
	return t.Records(m, q.Object, query, now)
}
