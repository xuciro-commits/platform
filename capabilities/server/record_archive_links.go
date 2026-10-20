package platformserver

import (
	"fmt"
	"reflect"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (s *recordStore) validateArchiveLinkLocked(l platform.LinkType) error {
	parent, child := s.types[l.Parent.Name], s.types[l.Child.Name]
	if parent == nil || child == nil || l.Parent.App != l.Child.App {
		return fmt.Errorf("archive protection needs both objects of the same owner")
	}
	f, ok := child.info.Field(l.Via)
	if !ok || f.Type != "reference" || f.Ref != parent.info.Type {
		return fmt.Errorf("link reference is unavailable")
	}
	for id, bucket := range child.referenceIndex(f).All() {
		if bucket.Active == 0 {
			continue
		}
		target := parent.rows.Get(id)
		if target == nil || target.value.Field(0).Interface().(platform.Record).Archived {
			return fmt.Errorf("%s", linkArchiveFailure)
		}
	}
	return nil
}

func (s *recordStore) checkArchiveWriteLocked(et *entityType, value reflect.Value) *kernel.Error {
	rec := value.Field(0).Interface().(platform.Record)
	fail := func() *kernel.Error { return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, linkArchiveFailure) }
	for _, l := range s.archiveLinks {
		child, parent := s.types[l.Child.Name], s.types[l.Parent.Name]
		if child == nil || parent == nil {
			return fail()
		}
		f, ok := child.info.Field(l.Via)
		if !ok || f.Type != "reference" || f.Ref != parent.info.Type {
			return fail()
		}
		if et.info.Type == l.Parent.Name && rec.Archived {
			bucket := child.referenceIndex(f).Get(rec.ID)
			allowed := 0
			if et.info.Type == child.info.Type && bucket.IDs.Get(rec.ID) {
				allowed = 1 // prospective self is archived
			}
			if bucket.Active > allowed {
				return fail()
			}
		}
		if et.info.Type == l.Child.Name && !rec.Archived {
			id := value.FieldByIndex(f.Index).String()
			if id == "" {
				continue
			}
			if et.info.Type == l.Parent.Name && id == rec.ID {
				continue
			} // prospective self is active
			target := parent.rows.Get(id)
			if target == nil || target.value.Field(0).Interface().(platform.Record).Archived {
				return fail()
			}
		}
	}
	return nil
}
