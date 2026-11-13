package records

import (
	"encoding/json"
	"strings"

	"platformserver/platform"
)

func (s *Store) ObservationSourceActive(record string) bool {
	typ, id, valid := strings.Cut(record, "/")
	if !valid {
		return false
	}
	s.Lock()
	defer s.Unlock()
	owner := s.Type(typ)
	if owner == nil {
		return false
	}
	row := owner.Rows().Get(id)
	return row != nil && !Header(row.Value).Archived && BusinessField(owner.Info(), row.Value, "state") == "published"
}

func (s *Store) ObservationPublisher(record string) (string, []string, bool) {
	typ, id, valid := strings.Cut(record, "/")
	if !valid {
		return "", nil, false
	}
	s.Lock()
	defer s.Unlock()
	owner := s.Type(typ)
	row := owner.Rows().Get(id)
	if row == nil {
		return "", nil, true
	}
	publisher := BusinessField(owner.Info(), row.Value, "publisher")
	var scopes []string
	var value struct {
		Scopes string `json:"publisherScopes"`
	}
	if json.Unmarshal(platform.Raw(row.Value.Interface()), &value) == nil {
		if value.Scopes != "" && json.Unmarshal([]byte(value.Scopes), &scopes) != nil {
			publisher = ""
		}
	}
	return publisher, scopes, true
}
