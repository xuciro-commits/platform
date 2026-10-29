package platformserver

import (
	"encoding/json"

	"platformserver/kernel/records"
	"reflect"
	"slices"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) aggregateRead(m platform.Member, typ string, q records.AggregateQuery, now time.Time) (records.Aggregate, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return records.Aggregate{}, err
	}
	if info, ok := t.entity(typ); ok {
		q.BusinessTime = businessQuery(info, platform.Query{BusinessTime: q.BusinessTime}, m, now).BusinessTime
	}
	if q.Collection != nil {
		if q.Window != nil || q.Histogram != nil || q.Traversal != nil || q.Set != nil || len(q.Domain) > 0 || q.Search != "" || q.Archived {
			return records.Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A declared collection cannot be combined with another selection")
		}
		root, ok := t.entity(q.Collection.Object)
		if !ok {
			return records.Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Collection owner is unavailable")
		}
		caller := platform.NewCaller(runtime{t}, m, root.App, false, false)
		page, err := caller.ReadRecordSet(root.Type, q.Collection.ID, q.Collection.Name, now)
		if err != nil {
			return records.Aggregate{}, err
		}
		if page.Object != typ {
			return records.Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Collection element differs from aggregate object")
		}
		t.records.Lock()
		defer t.records.Unlock()
		et := t.records.Type(typ)
		if et == nil {
			return records.Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Collection object is unavailable")
		}
		view := t.aggregateView(m, et)
		rows := make([]reflect.Value, len(page.Records))
		for i, raw := range page.Records {
			value := reflect.New(et.Info().Go).Elem()
			if json.Unmarshal(raw, value.Addr().Interface()) != nil {
				return records.Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Collection row is invalid")
			}
			rows[i] = value
		}
		out, err := t.records.Aggregate(view, q, nil, rows)
		out.Partial = page.Partial
		return out, err
	}
	if q.Traversal != nil {
		domain, err := t.linkTraversalDomain(m, typ, *q.Traversal, q.Domain, now)
		if err != nil {
			return records.Aggregate{}, err
		}
		q.Domain = domain
		q.Traversal = nil
	}
	s := t.records
	s.Lock()
	et := s.Type(typ)
	s.Unlock()
	if et == nil {
		return records.Aggregate{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	visible, err := t.visible(m, et, now)
	if err != nil {
		return records.Aggregate{}, err
	}
	view := t.aggregateView(m, et)
	s.Lock()
	defer s.Unlock()
	out, refusal := s.Aggregate(view, q, visible)
	if refusal == nil && q.Window == nil && q.Histogram == nil {
		query := platform.Query{Set: q.Set, Domain: q.Domain, Search: q.Search, Archived: q.Archived, BusinessTime: q.BusinessTime}
		all, err := s.MatchingQuery(et, query, nil)
		if err != nil {
			return records.Aggregate{}, err
		}
		shown, err := s.MatchingQuery(view, query, visible)
		if err != nil {
			return records.Aggregate{}, err
		}
		out.Partial = len(all) != len(shown)
	}
	return out, refusal
}

func (t *Tenant) aggregateView(member platform.Member, et *records.Type) *records.Type {
	view, _ := viewOf(member, et)
	if fields := t.narrowable(et); len(fields) > 0 {
		info := view.Info()
		info.Fields = slices.DeleteFunc(slices.Clone(info.Fields), func(field platform.FieldInfo) bool { return slices.Contains(fields, field.Name) })
		view = records.NewTypeView(info, view.Rows(), view.RetainedProjection())
	}
	return view
}
