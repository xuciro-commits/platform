package records

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"reflect"
	"strings"
	"time"
)

type Reference struct {
	ID, Target string
	Active     bool
}

type Bucket struct {
	IDs    Index[bool]
	Active int
}

type References struct{ targets Index[Bucket] }

func ReferencesFrom(entries []Reference) (References, error) {
	groups := map[string]*struct {
		ids    []Entry[bool]
		active int
	}{}
	for _, entry := range entries {
		if entry.Target == "" {
			continue
		}
		group := groups[entry.Target]
		if group == nil {
			group = &struct {
				ids    []Entry[bool]
				active int
			}{}
			groups[entry.Target] = group
		}
		group.ids = append(group.ids, Entry[bool]{Key: entry.ID, Value: entry.Active})
		if entry.Active {
			group.active++
		}
	}
	targets := make([]Entry[Bucket], 0, len(groups))
	for target, group := range groups {
		ids, err := From(group.ids)
		if err != nil {
			return References{}, err
		}
		targets = append(targets, Entry[Bucket]{Key: target, Value: Bucket{IDs: ids, Active: group.active}})
	}
	index, err := From(targets)
	return References{targets: index}, err
}

func (r References) Get(target string) Bucket { return r.targets.Get(target) }

func (r References) All() iter.Seq2[string, Bucket] { return r.targets.All() }

func (r *References) Replace(id, before, after string, active bool) {
	if before == after && (before == "" || r.targets.Get(before).IDs.Get(id) == active) {
		return
	}
	if before != "" {
		bucket := r.targets.Get(before)
		if bucket.IDs.Get(id) {
			bucket.Active--
		}
		bucket.IDs.Remove(id)
		if bucket.IDs.Len() == 0 {
			r.targets.Remove(before)
		} else {
			r.targets.Set(before, bucket)
		}
	}
	if after != "" {
		bucket := r.targets.Get(after)
		bucket.IDs.Set(id, active)
		if active {
			bucket.Active++
		}
		r.targets.Set(after, bucket)
	}
}

type UniqueIndex struct {
	Rule platform.Constraint
	Keys Index[Index[bool]]
	Rows Index[string]
}

func (index *UniqueIndex) Replace(id, key string) {
	before := index.Rows.Get(id)
	if before == key {
		return
	}
	if before != "" {
		bucket := index.Keys.Get(before)
		bucket.Remove(id)
		if bucket.Len() == 0 {
			index.Keys.Remove(before)
		} else {
			index.Keys.Set(before, bucket)
		}
		index.Rows.Remove(id)
	}
	if key != "" {
		bucket := index.Keys.Get(key)
		bucket.Set(id, true)
		index.Keys.Set(key, bucket)
		index.Rows.Set(id, key)
	}
}

type DerivedIndexes struct {
	references map[string]References
	unique     map[string]UniqueIndex
}

type IndexImage struct {
	Info               platform.EntityInfo
	Rows               Rows
	Indexes            *DerivedIndexes
	RetainedProjection bool
	Current            func() *CurrentScope
}

type ImageLookup func(string) (IndexImage, bool)

func (i DerivedIndexes) Fork() DerivedIndexes {
	return DerivedIndexes{references: maps.Clone(i.references), unique: maps.Clone(i.unique)}
}

func (i *DerivedIndexes) Reset() { i.references = nil; i.unique = nil }

func (i *DerivedIndexes) ResetUnique() { i.unique = nil }

func (i DerivedIndexes) Counts() (references, unique int) { return len(i.references), len(i.unique) }

func (i DerivedIndexes) CachedUnique(name string) UniqueIndex { return i.unique[name] }

func (i *DerivedIndexes) References(image IndexImage, field platform.FieldInfo) References {
	if index, exists := i.references[field.Name]; exists {
		return index
	}
	entries := make([]Reference, 0, image.Rows.Len())
	for id, row := range image.Rows.All() {
		target := row.Value.FieldByIndex(field.Index).String()
		if target != "" {
			entries = append(entries, Reference{ID: id, Target: target, Active: !Header(row.Value).Archived})
		}
	}
	index, err := ReferencesFrom(entries)
	if err != nil {
		panic(err)
	}
	if !image.Rows.Sourced() && i.references == nil {
		i.references = map[string]References{}
	}
	if !image.Rows.Sourced() {
		i.references[field.Name] = index
	}
	return index
}

func (i *DerivedIndexes) ReferenceBucket(image IndexImage, field platform.FieldInfo, target string) Bucket {
	if !image.Rows.Sourced() || image.RetainedProjection {
		return i.References(image, field).Get(target)
	}
	current := image.Current()
	filters := []persistence.StateFilter{{Path: []string{"value", field.Name}, Values: []string{target}}}
	if len(image.Info.RecordImages) > 0 {
		filters = nil
	}
	var bucket Bucket
	seen := map[string]bool{}
	visit := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		row := image.Rows.Get(id)
		if row == nil || row.Value.FieldByIndex(field.Index).String() != target {
			return
		}
		active := !Header(row.Value).Archived
		bucket.IDs.Set(id, active)
		if active {
			bucket.Active++
		}
	}
	after := ""
	for {
		ids, err := current.View().StateCandidates(context.Background(), current.Tenant(), image.Info.Type, filters, after, 256, current.Applied())
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
	for id := range image.Rows.Overlay().All() {
		visit(id)
	}
	return bucket
}

func UniqueKey(lookup ImageLookup, info platform.EntityInfo, rule platform.Constraint, value reflect.Value) (string, error) {
	values := make([]any, 0, len(rule.Fields)+2)
	for _, name := range rule.Fields {
		field, _ := info.Field(name)
		switch name {
		case "id":
			values = append(values, Header(value).ID)
		case "archived":
			values = append(values, Header(value).Archived)
		default:
			x := value.FieldByIndex(field.Index).Interface()
			if ConstraintEmpty(x) {
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
			if ConstraintEmpty(x.Interface()) {
				return "", fmt.Errorf("unique scope is missing")
			}
			if i == len(path)-1 {
				values = append(values, field.Ref, x.Interface())
				break
			}
			parent, exists := lookup(field.Ref)
			if !exists || parent.Info.App != info.App {
				return "", fmt.Errorf("unique scope reference is unavailable")
			}
			record := parent.Rows.Get(x.String())
			if record == nil {
				return "", fmt.Errorf("unique scope reference is unavailable")
			}
			current, currentType = record.Value, parent.Info
		}
	}
	raw, err := json.Marshal(values)
	return string(raw), err
}

func (i *DerivedIndexes) Unique(image IndexImage, lookup ImageLookup, rule platform.Constraint) UniqueIndex {
	if index, ok := i.unique[rule.Name]; ok {
		return index
	}
	index := UniqueIndex{Rule: rule}
	prepareUniqueScopeReferences(lookup, image.Info, rule)
	for id, r := range image.Rows.All() {
		key, err := UniqueKey(lookup, image.Info, rule, r.Value)
		if err == nil {
			index.Replace(id, key)
		}
	}
	if i.unique == nil {
		i.unique = map[string]UniqueIndex{}
	}
	i.unique[rule.Name] = index
	return index
}

func prepareUniqueScopeReferences(lookup ImageLookup, info platform.EntityInfo, rule platform.Constraint) {
	if rule.Scope == "" || rule.Scope == "tenant" {
		return
	}
	path, err := platform.ConstraintScopePath(info, rule.Scope)
	if err != nil {
		return
	}
	current, exists := lookup(info.Type)
	for i, name := range path {
		if !exists || i == len(path)-1 {
			return
		}
		field, ok := current.Info.Field(name)
		if !ok || field.Ref == "" {
			return
		}
		current.Indexes.References(current, field)
		current, exists = lookup(field.Ref)
	}
}

func ScopeDependents(lookup ImageLookup, info platform.EntityInfo, rule platform.Constraint, changedType, changedID string) Index[bool] {
	var out Index[bool]
	if rule.Scope == "" || rule.Scope == "tenant" {
		return out
	}
	path, err := platform.ConstraintScopePath(info, rule.Scope)
	if err != nil {
		return out
	}
	type edge struct {
		source IndexImage
		field  platform.FieldInfo
	}
	var edges []edge
	current, exists := lookup(info.Type)
	for i, name := range path {
		if !exists {
			break
		}
		field, ok := current.Info.Field(name)
		if !ok || field.Ref == "" {
			break
		}
		edges = append(edges, edge{current, field})
		if i < len(path)-1 && field.Ref == changedType {
			var ids Index[bool]
			ids.Set(changedID, true)
			for j := len(edges) - 1; j >= 0; j-- {
				var parents Index[bool]
				for target := range ids.All() {
					for id := range edges[j].source.Indexes.ReferenceBucket(edges[j].source, edges[j].field, target).IDs.All() {
						parents.Set(id, true)
					}
				}
				ids = parents
			}
			for id := range ids.All() {
				out.Set(id, true)
			}
		}
		current, exists = lookup(field.Ref)
	}
	return out
}

func ConstraintEmpty(value any) bool {
	v := reflect.ValueOf(value)
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return true
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return true
	}
	if v.Kind() == reflect.String {
		return strings.TrimSpace(v.String()) == ""
	}
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Map {
		return v.Len() == 0
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		return v.Interface().(time.Time).IsZero()
	}
	return false
}

func (i *DerivedIndexes) ReplaceReferences(info platform.EntityInfo, rows Rows, id string, value *Row) {
	previous := rows.Get(id)
	for name, index := range i.references {
		field, _ := info.Field(name)
		before := ""
		if previous != nil {
			before = previous.Value.FieldByIndex(field.Index).String()
		}
		after := value.Value.FieldByIndex(field.Index).String()
		index.Replace(id, before, after, !Header(value.Value).Archived)
		i.references[name] = index
	}
}

func (i *DerivedIndexes) RefreshUnique(image IndexImage, lookup ImageLookup, changedType, changedID string) {
	for name, index := range i.unique {
		ids := ScopeDependents(lookup, image.Info, index.Rule, changedType, changedID)
		if image.Info.Type == changedType {
			ids.Set(changedID, true)
		}
		for id := range ids.All() {
			key := ""
			if current := image.Rows.Get(id); current != nil {
				key, _ = UniqueKey(lookup, image.Info, index.Rule, current.Value)
			}
			index.Replace(id, key)
		}
		i.unique[name] = index
	}
}
