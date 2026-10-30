package platformserver

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/kernel/records"
	"platformserver/platform"

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
	around, err := t.traversalAround(s, c.Member, typ, q)
	if err != nil {
		return RecordPage{}, err
	}
	if q.Set != nil && q.CheckSet() != nil {
		return RecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	page, _, err := t.loadObjectSet(s, c, q.ObjectSet(typ, around), objectWindow{Sort: q.Sort, Offset: q.Offset, Limit: q.Limit}, now)
	return page, err
}
