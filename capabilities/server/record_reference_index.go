package platformserver

import (
	"encoding/json"
	"slices"

	"platformserver/internal/records"
	"platformserver/platform"
)

// The schema owner extracts primitive references; the records component owns
// their immutable buckets, archive counts and incremental replacement rules.
func (et *entityType) referenceIndex(field platform.FieldInfo) records.References {
	if index, exists := et.referenceIndexes[field.Name]; exists {
		return index
	}
	entries := make([]records.Reference, 0, et.rows.Len())
	for id, row := range et.rows.All() {
		target := row.value.FieldByIndex(field.Index).String()
		if target != "" {
			entries = append(entries, records.Reference{ID: id, Target: target, Active: !recordOf(row.value).Archived})
		}
	}
	index, err := records.ReferencesFrom(entries)
	if err != nil {
		panic(err) // The installed record index already guarantees unique IDs.
	}
	if et.referenceIndexes == nil {
		et.referenceIndexes = map[string]records.References{}
	}
	et.referenceIndexes[field.Name] = index
	return index
}

// All replacements, including pure accepted-result application, use this
// type owner. No decision rules run here; only derived lookup buckets change.
func (et *entityType) setRows(rows records.Index[*row]) {
	et.rows, et.referenceIndexes, et.uniqueIndexes, et.fullImage = rows, nil, nil, true
	if et.store != nil {
		// A schema/full-image replacement may change every referenced scope.
		// Bulk recovery may rebuild; ordinary setRow keeps its existing roots.
		for _, other := range et.store.types {
			other.uniqueIndexes = nil
		}
	}
}

func (et *entityType) setRow(id string, value *row) {
	previous := et.rows.Get(id)
	for name, index := range et.referenceIndexes {
		field, _ := et.info.Field(name)
		before := ""
		if previous != nil {
			before = previous.value.FieldByIndex(field.Index).String()
		}
		after := value.value.FieldByIndex(field.Index).String()
		index.Replace(id, before, after, !recordOf(value.value).Archived)
		et.referenceIndexes[name] = index
	}
	et.rows.Set(id, value)
	if et.store != nil {
		et.store.refreshUniqueIndexes(et.info.Type, id)
	}
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
				return slices.Compact(ids), true
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
		if !ok || info.Type != "reference" || info.Ref == "" || info.Inverse == "" || target == "" {
			continue
		}
		// Permission masking can make a temporary type view. The original source
		// owns its cache; the field above must still be readable in that view.
		source := s.types[et.info.Type]
		if source == nil {
			continue
		}
		bucket := source.referenceIndex(info).Get(target)
		ids := make([]string, 0, bucket.IDs.Len())
		for id := range bucket.IDs.All() {
			ids = append(ids, id)
		}
		return ids, true
	}
	return nil, false
}
