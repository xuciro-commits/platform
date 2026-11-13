package records

import "platformserver/platform"

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
