package records

import (
	"encoding/json"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func LockedRows[T any](s *Store, typ string) []T {
	s.Lock()
	defer s.Unlock()
	var out []T
	if et := s.Type(typ); et != nil {
		for _, row := range et.Rows().All() {
			out = append(out, row.Value.Interface().(T))
		}
	}
	return out
}

func LockedRow[T any](s *Store, typ, id string) (T, bool) {
	s.Lock()
	defer s.Unlock()
	var out T
	if et := s.Type(typ); et != nil {
		if row := et.Rows().Get(id); row != nil {
			return row.Value.Interface().(T), true
		}
	}
	return out, false
}

func LockedFields(s *Store, typ string) ([]platform.FieldInfo, bool) {
	s.Lock()
	defer s.Unlock()
	if et := s.Type(typ); et != nil {
		return et.Info().Fields, true
	}
	return nil, false
}

func LockedInfo(s *Store, typ string) (platform.EntityInfo, bool) {
	s.Lock()
	defer s.Unlock()
	if et := s.Type(typ); et != nil {
		return et.Info(), true
	}
	return platform.EntityInfo{}, false
}

func LockedImage(s *Store, typ, id, change string) (json.RawMessage, bool) {
	s.Lock()
	var row *Row
	if et := s.Type(typ); et != nil {
		row = et.Rows().Get(id)
	}
	s.Unlock()
	if row == nil {
		return nil, false
	}
	return row.ImageAt(change)
}

func LockedActive(s *Store, typ, id string) (platform.EntityInfo, bool) {
	s.Lock()
	defer s.Unlock()
	et := s.Type(typ)
	if et == nil {
		return platform.EntityInfo{}, false
	}
	row := et.Rows().Get(id)
	return et.Info(), row != nil && !Header(row.Value).Archived
}

func LockedAISource(s *Store, f platform.AIFunction, id string, readable func() bool, prepare func(input map[string]any)) (uint32, map[string]json.RawMessage, *kernel.Error) {
	s.Lock()
	defer s.Unlock()
	et := s.Type(f.Object)
	if et == nil {
		return 0, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The AI function source is not readable")
	}
	row := et.Rows().Get(id)
	if row == nil || Header(row.Value).Archived || !readable() {
		return 0, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The AI function source is not readable")
	}
	input := map[string]any{}
	for _, name := range f.Fields {
		field, found := et.Info().Field(name)
		if !found || field.AIInput != nil && !*field.AIInput {
			return 0, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The AI function source is not readable")
		}
		input[name] = row.Value.FieldByIndex(field.Index).Interface()
	}
	values := map[string]json.RawMessage{}
	for field, value := range input {
		values[field] = platform.Raw(value)
	}
	prepare(input)
	return Header(row.Value).Revision, values, nil
}
