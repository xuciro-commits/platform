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
	for _, d := range t.definitions {
		if d.Ref == (platform.AssetRef{App: app, Kind: platform.AssetQuery, Name: name}) && d.Query != nil {
			return *d.Query, true
		}
	}
	return platform.NamedQuery{}, false
}

// RunQuery runs one declared query as m (ADR-0040 21c): its fixed conditions,
// and the record it is run for through its reference. It is the member's own
// read of the object, so a page and an agent tool see exactly what m may.
func (t *Tenant) RunQuery(m platform.Member, app, name, of string, now time.Time) (RecordPage, *kernel.Error) {
	return t.runQueryFrom(t.records, m, app, name, of, now)
}

// RunQueryWindow retains the declaration's conditions/sort and published
// version while letting a reader search and page within its bounded window.
func (t *Tenant) RunQueryWindow(m platform.Member, app, name, of, version string, window platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	var declared *platform.NamedQuery
	for _, d := range t.Definitions(m) {
		if d.Ref != (platform.AssetRef{App: app, Kind: platform.AssetQuery, Name: name}) {
			continue
		}
		if version == "" {
			declared = d.Query
		} else if bound := d.QueryVersion(version); bound != nil {
			declared = bound.Query
		}
		break
	}
	if declared == nil {
		return RecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return t.runDeclaredQueryWindowFrom(t.records, m, *declared, of, window, now)
}

func (t *Tenant) runQueryFrom(store *recordStore, m platform.Member, app, name, of string, now time.Time) (RecordPage, *kernel.Error) {
	q, ok := t.namedQuery(app, name)
	if !ok {
		return RecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return t.runDeclaredQueryFrom(store, m, q, of, now)
}

func (t *Tenant) runDeclaredQueryFrom(store *recordStore, m platform.Member, q platform.NamedQuery, of string, now time.Time) (RecordPage, *kernel.Error) {
	return t.runDeclaredQueryWindowFrom(store, m, q, of, platform.Query{}, now)
}

func (t *Tenant) runDeclaredQueryWindowFrom(store *recordStore, m platform.Member, q platform.NamedQuery, of string, window platform.Query, now time.Time) (RecordPage, *kernel.Error) {
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
	query := platform.Query{Sort: q.Sort, Limit: q.Limit, Search: window.Search, Offset: window.Offset}
	if query.Limit <= 0 || query.Limit > 200 {
		query.Limit = 200
	}
	if window.Limit > 0 {
		query.Limit = min(window.Limit, query.Limit)
	}
	if len(terms) > 0 {
		query.Domain, _ = json.Marshal(terms)
	}
	if q.Interface != "" {
		page, err := t.interfaceRecordsFrom(store, m, q, query, now)
		out := RecordPage{Total: page.Total, Records: make([]any, len(page.Records))}
		for i, record := range page.Records {
			out.Records[i] = record
		}
		return out, err
	}
	return t.recordsFrom(store, m, q.Object, query, now)
}
