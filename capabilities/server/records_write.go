package platformserver

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"time"
)

func (r runtime) Put(c platform.Caller, rec *pb.ChangeRecord, entity any) *kernel.Error {
	if !c.Replaying {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A new record write requires a private runtime")
	}
	return r.t.records.Put(c, rec, entity)
}

func (r runtime) Get(c platform.Caller, t reflect.Type, id string) (any, bool) {
	return r.t.records.Get(c, t, id)
}

func (r runtime) Find(c platform.Caller, t reflect.Type, q platform.Query) ([]any, int, *kernel.Error) {
	s := r.t.records
	s.Lock()
	defer s.Unlock()
	et := s.Of(c, t)
	if et == nil {
		return nil, 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if len(q.Sort) == 0 {
		q.Sort = []string{"id"}
	}
	q = businessQuery(et.Info(), q, c.Member, time.Now())
	page, total, err := s.Find(et, q, nil)
	out := make([]any, len(page))
	for i, v := range page {
		out[i] = records.Copy(t, v.Interface()).Interface()
	}
	return out, total, err
}

func (r runtime) Check(c platform.Caller, entity any) *kernel.Error {
	return r.t.records.Check(c, entity, time.Now())
}
