package objects

import (
	"reflect"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func FindRows(s *records.Store, c platform.Caller, t reflect.Type, q platform.Query, now time.Time) ([]any, int, *kernel.Error) {
	s.Lock()
	defer s.Unlock()
	et := s.Of(c, t)
	if et == nil {
		return nil, 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if len(q.Sort) == 0 {
		q.Sort = []string{"id"}
	}
	page, total, err := s.Find(et, BusinessQuery(et.Info(), q, c.Member, now), nil)
	out := make([]any, len(page))
	for i, v := range page {
		out[i] = records.Copy(t, v.Interface()).Interface()
	}
	return out, total, err
}
