package platformserver

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"

	"time"
)

type RecordPage struct {
	Records []any `json:"records"`
	Total   int   `json:"total"`
	Partial bool  `json:"partial,omitempty"`
}

func (t *Tenant) recordsRead(m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.recordsFrom(t.records, m, typ, q, now)
}

func (t *Tenant) recordsFrom(s *records.Store, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	if q.Limit < 1 || q.Limit > 500 {
		q.Limit = 500
	}
	return t.recordSelectionFrom(s, m, typ, q, now)
}

func (t *Tenant) recordSelectionFrom(s *records.Store, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.recordSelectionForCallerFrom(s, platform.NewCaller(runtime{t}, m, "", false, false), typ, q, now)
}

func (t *Tenant) recordSelectionForCallerFrom(s *records.Store, c platform.Caller, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
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
	s.Lock()
	et := s.Type(typ)
	s.Unlock()
	if et == nil {
		return RecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	visible, err := t.visibleForCallerIn(s, c, et, now)
	if err != nil {
		return RecordPage{}, err
	}
	q = businessQuery(et.Info(), q, m, now)
	view, hidden := viewOf(m, et)
	s.Lock()
	defer s.Unlock()
	page, total, err := s.Find(view, q, visible)
	if err != nil {
		return RecordPage{}, err
	}
	out := RecordPage{Records: make([]any, len(page)), Total: total}
	probe := q
	probe.Offset, probe.Limit = 0, 1
	if _, unscoped, probeErr := s.Find(et, probe, nil); probeErr == nil {
		out.Partial = unscoped > total
	}
	var ids []string
	w := t.narrowerIn(s, m, now, true)
	for i, v := range page {
		out.Records[i] = records.Mask(et.Info(), v, hidden)
		if et.Info().HasReadSources() || len(et.Info().VersionPins) > 0 {
			narrowed, _ := w.derive(et, reflect.ValueOf(out.Records[i]))
			out.Records[i] = narrowed.Interface()
		}
		ids = append(ids, records.Header(v).ID)
	}
	t.readPersonal(m, view, ids, now)
	return out, nil
}
