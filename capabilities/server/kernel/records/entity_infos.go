package records

import "platformserver/platform"

func (s *Store) EntityInfos() map[string]platform.EntityInfo {
	s.Lock()
	defer s.Unlock()
	out := map[string]platform.EntityInfo{}
	for _, entity := range s.Types() {
		out[entity.Info().Type] = entity.Info()
	}
	return out
}
