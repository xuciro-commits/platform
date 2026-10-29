package platformserver

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"strings"
	"time"
)

// find selects records of a type; visible, when set, is the caller's scope.
func (s *recordStore) find(et *entityType, q platform.Query, visible func(reflect.Value) bool) ([]reflect.Value, int, *kernel.Error) {
	order, err := records.CompileOrder(et.info, q.Sort)
	if err != nil {
		return nil, 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	collector := order.Page(q.Offset, q.Limit)
	if refusal := s.visitMatching(et, q, visible, collector.Add); refusal != nil {
		return nil, 0, refusal
	}
	page, total := collector.Result()
	return page, total, nil
}

func (s *recordStore) matchingQuery(et *entityType, q platform.Query, visible func(reflect.Value) bool) ([]reflect.Value, *kernel.Error) {
	var out []reflect.Value
	refusal := s.visitMatching(et, q, visible, func(value reflect.Value) { out = append(out, value) })
	return out, refusal
}

func (s *recordStore) visitMatching(et *entityType, q platform.Query, visible func(reflect.Value) bool, accept func(reflect.Value)) *kernel.Error {

	if err := q.CheckSet(); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	match, err := records.CompileSet(et.info, platform.RecordSetPredicate{Domain: q.Domain, Search: q.Search, Set: q.Set})
	if err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	effective, refusal := s.effectivePredicate(et, q)
	if refusal != nil {
		return refusal
	}

	image := et.indexImage()
	image.Current = func() *records.CurrentScope { return s.current }
	writes := func(yield func(string) bool) {
		for overlay := s; overlay != nil; overlay = overlay.parent {
			for ref := range overlay.writes {
				typ, id, ok := strings.Cut(ref, "/")
				if ok && typ == et.info.Type && !yield(id) {
					return
				}
			}
		}
	}
	records.VisitMatching(image, s.indexImage, q, visible, effective, match, writes, accept)

	return nil
}

// RecordPage is a page of records and how many match in all.
type RecordPage struct {
	Records []any `json:"records"`
	Total   int   `json:"total"`
	Partial bool  `json:"partial,omitempty"`
}

// Records serves a member's query over a type, within the member's scope.
func (t *Tenant) recordsRead(m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.recordsFrom(t.records, m, typ, q, now)
}

// recordsFrom is the same member projection over a staged or installed store.
// Flow queries must see prior accepted-decision writes without acquiring a
// second live runtime or widening the member's record/field scope.
func (t *Tenant) recordsFrom(s *recordStore, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	if q.Limit < 1 || q.Limit > 500 {
		q.Limit = 500
	}
	return t.recordSelectionFrom(s, m, typ, q, now)
}

// Internal aggregations select an owner's authorized inventory once. They
// reuse the same masks and source checks without repeatedly sorting each page.
// Public record reads still enter through the bounded recordsFrom above.
func (t *Tenant) recordSelectionFrom(s *recordStore, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.recordSelectionForCallerFrom(s, platform.NewCaller(runtime{t}, m, "", false, false), typ, q, now)
}

func (t *Tenant) recordSelectionForCallerFrom(s *recordStore, c platform.Caller, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	c.Member = t.authorizationMember(c.Member)
	m := c.Member
	if q.Traversal != nil {
		if s != t.records {
			return RecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Link traversal is unavailable in a staged read")
		}
		domain, err := t.linkTraversalDomain(m, typ, *q.Traversal, q.Domain, now)
		if err != nil {
			return RecordPage{}, err
		}
		q.Domain = domain
		q.Traversal = nil
	}
	if err := t.admits(m); err != nil {
		return RecordPage{}, err
	}
	s.mu.Lock()
	et := s.types[typ]
	s.mu.Unlock()
	if et == nil {
		return RecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	visible, err := t.visibleForCallerIn(s, c, et, now)
	if err != nil {
		return RecordPage{}, err
	}
	q = businessQuery(et.info, q, m, now)
	view, hidden := viewOf(m, et)
	s.mu.Lock()
	defer s.mu.Unlock()
	page, total, err := s.find(view, q, visible)
	if err != nil {
		return RecordPage{}, err
	}
	out := RecordPage{Records: make([]any, len(page)), Total: total}
	// The owner exposes a scope marker, never the hidden count or identities.
	probe := q
	probe.Offset, probe.Limit = 0, 1
	if _, unscoped, probeErr := s.find(et, probe, nil); probeErr == nil {
		out.Partial = unscoped > total
	}
	var ids []string
	w := t.narrowerIn(s, m, now, true)
	for i, v := range page {
		out.Records[i] = records.Mask(et.info, v, hidden)
		if et.info.HasReadSources() || len(et.info.VersionPins) > 0 { // checked against its sources again
			narrowed, _ := w.derive(et, reflect.ValueOf(out.Records[i]))
			out.Records[i] = narrowed.Interface()
		}
		ids = append(ids, records.Header(v).ID)
	}
	t.readPersonal(m, view, ids, now)
	return out, nil
}
