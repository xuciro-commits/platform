package records

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"platformserver/platform"
)

func (s *Store) RowDigest(record string) (string, error) {
	typ, id, ok := strings.Cut(record, "/")
	if !ok {
		return "", fmt.Errorf("invalid installation record %s", record)
	}
	s.Lock()
	defer s.Unlock()
	et := s.Type(typ)
	if et == nil || et.Rows().Get(id) == nil {
		return "", fmt.Errorf("release metadata needs its predecessor %s", record)
	}
	image, err := et.Rows().Get(id).Image()
	if err != nil {
		return "", err
	}
	return platform.CanonicalDigest(image)
}

func (s *Store) InstallImage(record string, image json.RawMessage) error {
	typ, id, _ := strings.Cut(record, "/")
	et := s.Type(typ)
	value := reflect.New(et.Info().Go).Elem()
	if err := json.Unmarshal(image, value.Addr().Interface()); err != nil {
		return err
	}
	installed := *et.Rows().Get(id)
	installed.Value, installed.Original = value, nil
	installed.RetainOriginal(image)
	et.SetRow(id, &installed)
	s.TrackWrite(record)
	return nil
}

func (s *Store) ResetReshaped(shapes map[string]platform.EntityInfo) {
	for typ, shape := range shapes {
		if old := s.Type(typ); old != nil && old.Info().Go != shape.Go {
			old.SetRows(Index[*Row]{})
		}
	}
}
