package platformserver

import (
	"encoding/json"
	"slices"

	"platformserver/platform"
)

// Reference buckets are derived, immutable indexes of the original type's
// complete rows, including rows the current reader cannot see. Read filters
// still run after candidate selection; constraints never use member filters.
type referenceBucket struct {
	ids    recordIndex[bool]
	active int // ids' values say which references are active
}
type referenceIndex struct {
	field   platform.FieldInfo
	targets recordIndex[referenceBucket]
}

func (et *entityType) referenceIndex(field platform.FieldInfo) referenceIndex {
	if index, exists := et.referenceIndexes[field.Name]; exists {
		return index
	}
	buckets := map[string]*struct {
		ids    []recordEntry[bool]
		active int
	}{}
	for id, row := range et.rows.all() {
		target := row.value.FieldByIndex(field.Index).String()
		if target == "" {
			continue
		}
		bucket := buckets[target]
		if bucket == nil {
			bucket = &struct {
				ids    []recordEntry[bool]
				active int
			}{}
			buckets[target] = bucket
		}
		active := !row.value.Field(0).Interface().(platform.Record).Archived
		bucket.ids = append(bucket.ids, recordEntry[bool]{id, active})
		if active {
			bucket.active++
		}
	}
	entries := make([]recordEntry[referenceBucket], 0, len(buckets))
	for target, rows := range buckets {
		ids, _ := recordIndexFrom(rows.ids)
		entries = append(entries, recordEntry[referenceBucket]{target, referenceBucket{ids, rows.active}})
	}
	targets, _ := recordIndexFrom(entries)
	index := referenceIndex{field: field, targets: targets}
	if et.referenceIndexes == nil {
		et.referenceIndexes = map[string]referenceIndex{}
	}
	et.referenceIndexes[field.Name] = index
	return index
}

// All replacements, including pure accepted-result application, use this
// type owner. No decision rules run here; only derived lookup buckets change.
func (et *entityType) setRows(rows recordIndex[*row]) {
	et.rows, et.referenceIndexes, et.fullImage = rows, nil, true
}

func (et *entityType) setRow(id string, value *row) {
	previous := et.rows.get(id)
	for name, index := range et.referenceIndexes {
		before := ""
		beforeActive := false
		if previous != nil {
			before = previous.value.FieldByIndex(index.field.Index).String()
			beforeActive = !previous.value.Field(0).Interface().(platform.Record).Archived
		}
		after := value.value.FieldByIndex(index.field.Index).String()
		afterActive := !value.value.Field(0).Interface().(platform.Record).Archived
		if before == after && (before == "" || beforeActive == afterActive) {
			continue
		}
		if before != "" {
			bucket := index.targets.get(before)
			if bucket.ids.get(id) {
				bucket.active--
			}
			bucket.ids.remove(id)
			if bucket.ids.len() == 0 {
				index.targets.remove(before)
			} else {
				index.targets.set(before, bucket)
			}
		}
		if after != "" {
			bucket := index.targets.get(after)
			bucket.ids.set(id, afterActive)
			if afterActive {
				bucket.active++
			}
			index.targets.set(after, bucket)
		}
		et.referenceIndexes[name] = index
	}
	et.rows.set(id, value)
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
		bucket := source.referenceIndex(info).targets.get(target)
		ids := make([]string, 0, bucket.ids.len())
		for id := range bucket.ids.all() {
			ids = append(ids, id)
		}
		return ids, true
	}
	return nil, false
}
