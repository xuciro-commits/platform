package platformserver

import (
	"encoding/json"
	"slices"

	"platformserver/platform"
)

// The source owner's volatile reference index is rebuilt from accepted rows.
// Store generations invalidate it on Put, promotion, restore or schema install;
// a private fork owns an independent cache and never changes the live index.
type referenceIndex struct {
	generation uint64
	ids        map[string][]string
}

func (s *recordStore) indexedRecordCandidates(et *entityType, q platform.Query) ([]string, bool) {
	var terms []json.RawMessage
	if len(q.Domain) == 0 || json.Unmarshal(q.Domain, &terms) != nil {
		return nil, false
	}
	for _, term := range terms {
		if len(term) == 0 || term[0] != '[' {
			return nil, false
		}
	}
	for _, term := range terms {
		var parts []json.RawMessage
		if json.Unmarshal(term, &parts) != nil || len(parts) != 3 {
			continue
		}
		var field, op string
		if json.Unmarshal(parts[0], &field) != nil || json.Unmarshal(parts[1], &op) != nil {
			continue
		}
		if field == "id" && op == "in" {
			var ids []string
			if json.Unmarshal(parts[2], &ids) == nil {
				slices.Sort(ids)
				ids = slices.Compact(ids)
				return ids, true
			}
		}
		if op != "=" {
			continue
		}
		var target string
		if json.Unmarshal(parts[2], &target) != nil {
			continue
		}
		if field == "id" {
			return []string{target}, true
		}
		info, ok := et.info.Field(field)
		if !ok || info.Type != "reference" || info.Ref == "" || info.Inverse == "" {
			continue
		}
		key := et.info.Type + "#" + field
		index, exists := s.referenceIndexes[key]
		if !exists || index.generation != s.generation {
			index = referenceIndex{generation: s.generation, ids: map[string][]string{}}
			for id, row := range et.rows {
				value := row.value.FieldByIndex(info.Index).String()
				index.ids[value] = append(index.ids[value], id)
			}
			for _, ids := range index.ids {
				slices.Sort(ids)
			}
			if s.referenceIndexes == nil {
				s.referenceIndexes = map[string]referenceIndex{}
			}
			s.referenceIndexes[key] = index
		}
		return index.ids[target], true
	}
	return nil, false
}
