package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/internal/records"
	"platformserver/journal"
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
		target := row.Value.FieldByIndex(field.Index).String()
		if target != "" {
			entries = append(entries, records.Reference{ID: id, Target: target, Active: !records.Header(row.Value).Archived})
		}
	}
	index, err := records.ReferencesFrom(entries)
	if err != nil {
		panic(err) // The installed record index already guarantees unique IDs.
	}
	if !et.rows.Sourced() && et.referenceIndexes == nil {
		et.referenceIndexes = map[string]records.References{}
	}
	if !et.rows.Sourced() {
		et.referenceIndexes[field.Name] = index
	}
	return index
}

// referenceBucket loads only actual matches in SQL mode. The immutable memory
// mode keeps its incremental buckets; SQL mode keeps no second resident index.
func (et *entityType) referenceBucket(field platform.FieldInfo, target string) records.Bucket {
	if !et.rows.Sourced() || et.retainedProjection {
		return et.referenceIndex(field).Get(target)
	}
	s := et.store
	filters := []journal.StateFilter{{Path: []string{"value", field.Name}, Values: []string{target}}}
	if len(et.info.RecordImages) > 0 {
		filters = nil
	}
	var bucket records.Bucket
	seen := map[string]bool{}
	visit := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		row := et.rows.Get(id)
		if row == nil || row.Value.FieldByIndex(field.Index).String() != target {
			return
		}
		active := !records.Header(row.Value).Archived
		bucket.IDs.Set(id, active)
		if active {
			bucket.Active++
		}
	}
	after := ""
	for {
		ids, err := s.readView.StateCandidates(context.Background(), s.tenant, et.info.Type, filters, after, 256, s.appliedPosition)
		if err != nil {
			panic(fmt.Errorf("record references: %w", err))
		}
		for _, id := range ids {
			visit(id)
		}
		if len(ids) < 256 {
			break
		}
		after = ids[len(ids)-1]
	}
	for id := range et.rows.Overlay().All() {
		visit(id)
	}
	return bucket
}

// All replacements, including pure accepted-result application, use this
// type owner. No decision rules run here; only derived lookup buckets change.
func (et *entityType) setRows(rows records.Index[*records.Row]) {
	et.rows, et.referenceIndexes, et.uniqueIndexes, et.fullImage = records.MemoryRows(rows), nil, nil, true
	if et.store != nil {
		// A schema/full-image replacement may change every referenced scope.
		// Bulk recovery may rebuild; ordinary setRow keeps its existing roots.
		for _, other := range et.store.types {
			other.uniqueIndexes = nil
		}
	}
}

func (et *entityType) setRow(id string, value *records.Row) {
	previous := et.rows.Get(id)
	for name, index := range et.referenceIndexes {
		field, _ := et.info.Field(name)
		before := ""
		if previous != nil {
			before = previous.Value.FieldByIndex(field.Index).String()
		}
		after := value.Value.FieldByIndex(field.Index).String()
		index.Replace(id, before, after, !records.Header(value.Value).Archived)
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
		bucket := source.referenceBucket(info, target)
		ids := make([]string, 0, bucket.IDs.Len())
		for id := range bucket.IDs.All() {
			ids = append(ids, id)
		}
		return ids, true
	}
	return nil, false
}
