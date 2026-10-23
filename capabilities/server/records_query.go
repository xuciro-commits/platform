package platformserver

import (
	"context"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/records"
	"platformserver/journal"
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
	visit := func(r *records.Row) {
		if r == nil {
			return
		}
		v := r.Value
		if !q.Archived && records.Header(v).Archived || visible != nil && !visible(v) || effective != nil && !effective(v) || !match(v) {
			return
		}
		accept(v)
	}
	if ids, indexed := s.indexedRecordCandidates(et, q); indexed {
		for _, id := range ids {
			visit(et.rows.Get(id))
		}
	} else if s.historyStore != nil {
		var filters []journal.StateFilter
		// SQL retains the original image, while the owner may have renamed or
		// transformed its fields. Filter the converted values below instead.
		if !et.retainedProjection && len(et.info.RecordImages) == 0 {
			filters = sqlRecordFilters(et.info, q.Domain)
		}
		if et.retainedProjection {
			// Record identity is unchanged by aliases or historical field projections.
			for _, filter := range sqlRecordFilters(et.info, q.Domain) {
				if len(filter.Path) == 2 && filter.Path[0] == "value" && filter.Path[1] == "id" {
					filters = append(filters, filter)
				}
			}
		}
		pending := map[string]bool{}
		// During checkpoint recovery the fixed SQL archive predates accepted
		// tail writes promoted into the root overlay. They must participate in
		// owner inventories even when their IDs do not exist in that archive.
		for id := range et.rows.Overlay().All() {
			pending[id] = true
		}
		for overlay := s; overlay != nil; overlay = overlay.parent {
			for ref := range overlay.writes {
				typ, id, ok := strings.Cut(ref, "/")
				if ok && typ == et.info.Type {
					pending[id] = true
				}
			}
		}
		after := ""
		for {
			ids, readErr := s.stateReader().StateCandidates(context.Background(), s.tenant, et.info.Type, filters, after, 256, s.appliedPosition)
			if readErr != nil {
				panic(fmt.Errorf("record candidates: %w", readErr))
			}
			for _, id := range ids {
				delete(pending, id)
				visit(et.rows.Get(id))
			}
			if len(ids) < 256 {
				break
			}
			after = ids[len(ids)-1]
		}
		// Private writes may newly match a condition their committed predecessor
		// did not. Include this decision's overlay before applying the same rules.
		for id := range pending {
			visit(et.rows.Get(id))
		}
	} else {
		for _, r := range et.rows.All() {
			visit(r)
		}
	}

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
