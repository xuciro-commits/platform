package records

import (
	"encoding/json"
	persistence "platformserver/kernel/store"
)

type forkOrigin struct {
	typ  *Type
	rows Rows
}

type ForkCache struct {
	source  *Store
	store   *Store
	origins map[string]forkOrigin
	view    persistence.StateReadView
}

func (c *ForkCache) Fork(source *Store, build func(store *Store, original func(typ string) Rows) error) (*Store, error) {
	source.Lock()
	valid := c.source == source && c.store != nil && c.view == source.Current().View()
	for typ, et := range source.Types() {
		origin, exists := c.origins[typ]
		if !exists || origin.typ != et || origin.rows != et.Rows() {
			valid = false
			break
		}
	}
	valid = valid && len(c.origins) == source.TypeCount()
	store := c.store
	var origins map[string]forkOrigin
	if !valid {
		store = source.ForkRecordsLocked()
		origins = map[string]forkOrigin{}
		for typ, et := range source.Types() {
			origins[typ] = forkOrigin{typ: et, rows: et.Rows()}
		}
	}
	source.Unlock()
	if !valid {
		if err := build(store, func(typ string) Rows { return origins[typ].rows }); err != nil {
			return nil, err
		}
		c.source, c.store, c.origins, c.view = source, store, origins, store.Current().View()
	}
	return store, nil
}

func (s *Store) VisitImages(typ string, visit func(json.RawMessage) error) (bool, error) {
	s.Lock()
	defer s.Unlock()
	entity := s.Type(typ)
	if entity == nil {
		return false, nil
	}
	for _, row := range entity.Rows().All() {
		image, err := row.Image()
		if err != nil {
			return true, err
		}
		if err := visit(image); err != nil {
			return true, err
		}
		current, err := json.Marshal(row.Value.Interface())
		if err != nil {
			return true, err
		}
		if err := visit(current); err != nil {
			return true, err
		}
		for _, change := range row.RecordHistory() {
			for _, field := range change.Fields {
				if err := visit(field.Before); err != nil {
					return true, err
				}
				if err := visit(field.After); err != nil {
					return true, err
				}
			}
		}
	}
	return true, nil
}
