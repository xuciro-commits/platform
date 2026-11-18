package records

import "platformserver/platform"

func (s *Store) EntityInfos() map[string]platform.RecordTypeInfo {
	s.Lock()
	defer s.Unlock()
	out := map[string]platform.RecordTypeInfo{}
	for _, entity := range s.Types() {
		out[entity.Info().Type] = entity.Info()
	}
	return out
}
