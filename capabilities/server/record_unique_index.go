package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"platformserver/internal/records"
	"platformserver/platform"
)

// A rule index uses the same persistent AVL tree as records and references.
// Buckets retain every occupant, including old duplicate/archived rows; a
// derived index must not silently repair historical business data.
type uniqueIndex struct {
	rule platform.Constraint
	keys records.Index[records.Index[bool]]
	rows records.Index[string]
}

func (s *recordStore) uniqueKey(info platform.EntityInfo, rule platform.Constraint, value reflect.Value) (string, error) {
	values := make([]any, 0, len(rule.Fields)+2)
	for _, name := range rule.Fields {
		field, _ := info.Field(name)
		switch name {
		case "id":
			values = append(values, recordOf(value).ID)
		case "archived":
			values = append(values, recordOf(value).Archived)
		default:
			x := value.FieldByIndex(field.Index).Interface()
			if constraintEmpty(x) {
				return "", nil
			}
			values = append(values, x)
		}
	}
	if rule.Scope != "" && rule.Scope != "tenant" {
		path, err := platform.ConstraintScopePath(info, rule.Scope)
		if err != nil {
			return "", err
		}
		current, currentType := value, info
		for i, name := range path {
			field, ok := currentType.Field(name)
			if !ok {
				return "", fmt.Errorf("unique scope is unavailable")
			}
			x := current.FieldByIndex(field.Index)
			if constraintEmpty(x.Interface()) {
				return "", fmt.Errorf("unique scope is missing")
			}
			if i == len(path)-1 {
				values = append(values, field.Ref, x.Interface())
				break
			}
			parent := s.types[field.Ref]
			if parent == nil || parent.info.App != info.App || parent.rows.Get(x.String()) == nil {
				return "", fmt.Errorf("unique scope reference is unavailable")
			}
			current, currentType = parent.rows.Get(x.String()).value, parent.info
		}
	}
	raw, err := json.Marshal(values)
	return string(raw), err
}

func (index *uniqueIndex) replace(id, key string) {
	before := index.rows.Get(id)
	if before == key {
		return
	}
	if before != "" {
		bucket := index.keys.Get(before)
		bucket.Remove(id)
		if bucket.Len() == 0 {
			index.keys.Remove(before)
		} else {
			index.keys.Set(before, bucket)
		}
		index.rows.Remove(id)
	}
	if key != "" {
		bucket := index.keys.Get(key)
		bucket.Set(id, true)
		index.keys.Set(key, bucket)
		index.rows.Set(id, key)
	}
}

func (et *entityType) uniqueIndex(s *recordStore, rule platform.Constraint) uniqueIndex {
	if index, ok := et.uniqueIndexes[rule.Name]; ok {
		return index
	}
	index := uniqueIndex{rule: rule}
	s.prepareUniqueScopeReferences(et.info, rule)
	for id, r := range et.rows.All() {
		key, err := s.uniqueKey(et.info, rule, r.value)
		if err == nil {
			index.replace(id, key)
		}
	}
	if et.uniqueIndexes == nil {
		et.uniqueIndexes = map[string]uniqueIndex{}
	}
	et.uniqueIndexes[rule.Name] = index
	return index
}

// Construct reference roots alongside the initial unique-key image, rather
// than discovering a complete child table during the first parent edit.
func (s *recordStore) prepareUniqueScopeReferences(info platform.EntityInfo, rule platform.Constraint) {
	if rule.Scope == "" || rule.Scope == "tenant" {
		return
	}
	path, err := platform.ConstraintScopePath(info, rule.Scope)
	if err != nil {
		return
	}
	current := s.types[info.Type]
	for i, name := range path {
		if current == nil || i == len(path)-1 {
			return
		}
		field, ok := current.info.Field(name)
		if !ok || field.Ref == "" {
			return
		}
		current.referenceIndex(field)
		current = s.types[field.Ref]
	}
}

// scopeDependents traverses only reference buckets whose declaration is part
// of this rule's scope. A change to an intermediate parent never scans the
// child table, including when multiple scope segments use the same type.
func (s *recordStore) scopeDependents(info platform.EntityInfo, rule platform.Constraint, changedType, changedID string) records.Index[bool] {
	var out records.Index[bool]
	if rule.Scope == "" || rule.Scope == "tenant" {
		return out
	}
	path, err := platform.ConstraintScopePath(info, rule.Scope)
	if err != nil {
		return out
	}
	type edge struct {
		source *entityType
		field  platform.FieldInfo
	}
	var edges []edge
	current := s.types[info.Type]
	for i, name := range path {
		if current == nil {
			break
		}
		field, ok := current.info.Field(name)
		if !ok || field.Ref == "" {
			break
		}
		edges = append(edges, edge{current, field})
		// The final reference contributes its identity, not its contents. Editing
		// its target therefore cannot change this scope's derived key.
		if i < len(path)-1 && field.Ref == changedType {
			var ids records.Index[bool]
			ids.Set(changedID, true)
			for j := len(edges) - 1; j >= 0; j-- {
				var parents records.Index[bool]
				index := edges[j].source.referenceIndex(edges[j].field)
				for target := range ids.All() {
					for id := range index.Get(target).IDs.All() {
						parents.Set(id, true)
					}
				}
				ids = parents
			}
			for id := range ids.All() {
				out.Set(id, true)
			}
		}
		current = s.types[field.Ref]
	}
	return out
}

func (s *recordStore) refreshUniqueIndexes(changedType, changedID string) {
	for _, et := range s.types {
		for name, index := range et.uniqueIndexes {
			ids := s.scopeDependents(et.info, index.rule, changedType, changedID)
			if et.info.Type == changedType {
				ids.Set(changedID, true)
			}
			for id := range ids.All() {
				key := ""
				if current := et.rows.Get(id); current != nil {
					key, _ = s.uniqueKey(et.info, index.rule, current.value)
				}
				index.replace(id, key)
			}
			et.uniqueIndexes[name] = index
		}
	}
}

// uniqueKeys is the owner-derived SQL representation of one current row. A
// missing/incomplete historical scope occupies no key, just as the in-memory
// index. Storage persists these values without evaluating business rules.
func (s *recordStore) uniqueKeys(typ, id string) map[string]string {
	keys := map[string]string{}
	et := s.types[typ]
	if et == nil {
		return keys
	}
	r := et.rows.Get(id)
	if r == nil {
		return keys
	}
	for _, rule := range et.info.Constraints {
		if rule.Kind != "unique" {
			continue
		}
		key, err := s.uniqueKey(et.info, rule, r.value)
		if err == nil && key != "" {
			keys[rule.Name] = key
		}
	}
	return keys
}

// uniqueChanges contains complete per-row replacement key sets for a private
// decision, including unchanged children whose parent scope was edited. A
// caller applies an empty key set by deleting that row's former derived keys.
// The store lock belongs to the caller, as with accepted record collection.
func (s *recordStore) uniqueChanges() map[string]map[string]string {
	var affected records.Index[bool]
	for ref := range s.writes {
		affected.Set(ref, true)
		typ, id, _ := strings.Cut(ref, "/")
		for _, et := range s.types {
			for _, rule := range et.info.Constraints {
				if rule.Kind != "unique" {
					continue
				}
				for child := range s.scopeDependents(et.info, rule, typ, id).All() {
					affected.Set(et.info.Type+"/"+child, true)
				}
			}
		}
	}
	for typ, et := range s.types {
		if !et.fullImage {
			continue
		}
		for id := range et.rows.All() {
			affected.Set(typ+"/"+id, true)
		}
	}
	changes := map[string]map[string]string{}
	for ref := range affected.All() {
		typ, id, _ := strings.Cut(ref, "/")
		changes[ref] = s.uniqueKeys(typ, id)
	}
	return changes
}

// Retain roots on the authoritative image before copying a private view. A
// refusal must not make the next decision rebuild all the unique keys.
func (s *recordStore) prepareUniqueIndexes() {
	if s.historyStore != nil {
		return
	}
	for _, et := range s.types {
		for _, rule := range et.info.Constraints {
			if rule.Kind == "unique" {
				et.uniqueIndex(s, rule)
			}
		}
	}
}
