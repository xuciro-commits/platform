package records

import (
	"context"
	"fmt"
	"strings"

	persistence "platformserver/kernel/store"
)

type StateMatch struct {
	Path   []string
	Values []string
}

func FindRow[T any](store *Store, typ string, filters []StateMatch, match func(T) bool) (T, bool) {
	var none T
	rows := store.Type(typ)
	if rows == nil {
		return none, false
	}
	if view := store.Current().View(); view != nil {
		state := make([]persistence.StateFilter, len(filters))
		for i, filter := range filters {
			state[i] = persistence.StateFilter{Path: filter.Path, Values: filter.Values}
		}
		after := ""
		for {
			ids, err := view.StateCandidates(context.Background(), store.Current().Tenant(), typ, state, after, 256, store.Current().Applied())
			if err != nil {
				panic(fmt.Errorf("resource candidates: %w", err))
			}
			for _, id := range ids {
				if row := rows.Rows().Get(id); row != nil {
					if value := row.Value.Interface().(T); match(value) {
						return value, true
					}
				}
			}
			if len(ids) < 256 {
				break
			}
			after = ids[len(ids)-1]
		}
		return none, false
	}
	for _, row := range rows.Rows().All() {
		if value := row.Value.Interface().(T); match(value) {
			return value, true
		}
	}
	return none, false
}

func (s *Store) Held(ref string) (any, bool) {
	typ, id, _ := strings.Cut(ref, "/")
	s.Lock()
	defer s.Unlock()
	et := s.Type(typ)
	if et == nil {
		return nil, false
	}
	row := et.Rows().Get(id)
	if row == nil {
		return nil, false
	}
	return Copy(et.Info().Go, row.Value.Interface()).Interface(), true
}

func (s *Store) ReadScoped(typ string) (exists, scoped bool) {
	et := s.Type(typ)
	return et != nil, et != nil && (et.Info().Scope.Through != nil || et.Info().HasReadSources())
}
