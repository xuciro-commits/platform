package platformserver

import records "platformserver/kernel/records"

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) namedQuery(app, name string) (platform.NamedQuery, bool) {
	for _, d := range t.definitions {
		if d.Ref == (platform.AssetRef{App: app, Kind: platform.AssetQuery, Name: name}) && d.Query != nil {
			return *d.Query, true
		}
	}
	return platform.NamedQuery{}, false
}

func (t *Tenant) queryWindowRead(m platform.Member, app, name, of, version string, window platform.Query, now time.Time) (RecordPage, *kernel.Error) {
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

func (t *Tenant) runDeclaredQueryFrom(store *records.Store, m platform.Member, q platform.NamedQuery, of string, now time.Time) (RecordPage, *kernel.Error) {
	return t.runDeclaredQueryWindowFrom(store, m, q, of, platform.Query{}, now)
}

func (t *Tenant) runDeclaredQueryWindowFrom(store *records.Store, m platform.Member, q platform.NamedQuery, of string, window platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	if q.By != "" && of == "" {
		return RecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{query} needs the record it is run for", q.Title)
	}
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	if window.Limit > 0 {
		limit = min(window.Limit, limit)
	}
	set := q.ObjectSet(of)
	set.Search = window.Search
	page, _, err := t.loadObjectSet(store, platform.NewCaller(runtime{t}, m, "", false, false), set, objectWindow{Sort: q.Sort, Offset: window.Offset, Limit: limit}, now, q)
	return page, err
}
