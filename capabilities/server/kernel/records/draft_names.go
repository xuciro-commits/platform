package records

import (
	"fmt"
	"platformserver/platform"
	"reflect"
)

func (s *Store) DraftMatches(typ, name string, reopen, managed bool) (int, error) {
	et := s.Type(typ)
	nameField, ok := et.Info().Field("name")
	if !ok {
		return 0, fmt.Errorf("asset draft %s has no owner identity", typ)
	}
	matches := 0
	for id, row := range et.Rows().All() {
		archived := row.Value.FieldByName("Record").Interface().(platform.Record).Archived
		named := row.Value.FieldByIndex(nameField.Index).String() == name
		if reopen && archived && named {
			value := reflect.New(row.Value.Type()).Elem()
			value.Set(row.Value)
			Header(value).Archived = false
			copy := *row
			copy.Value = value
			et.SetRow(id, &copy)
			s.TrackWrite(et.Info().Type + "/" + id)
			archived = false
		}
		if (managed || !archived) && named {
			matches++
		}
	}
	return matches, nil
}
