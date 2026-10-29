package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"sync"
)

type recordStore struct {
	current *records.CurrentScope
	rules   platform.RuleEngine

	mu sync.Mutex

	liveVersions map[string]uint64
	uniqueLinks  map[string]platform.LinkType
	archiveLinks map[string]platform.LinkType
	types        map[string]*entityType
	byGo         map[reflect.Type]*entityType
	// A private 19a decision view has no projection callback. It may replace
	// its parent only if no direct write reached that parent in the meantime.
	parent         *recordStore
	generation     uint64
	baseGeneration uint64
	writes         map[string]bool
	sealed         bool
	// touched, when set, hears of each record put, under mu (the PostgreSQL projection).
	touched func(typ, id string)
	// dirty are the records of knowledge-bearing types put since the index last
	// read it, under mu: knowledge is cut again for those records alone, not by
	// walking the tenant at every search (#130).
	dirty records.Index[bool]
	// changed are the records each recent decision put ("<app>/<change>" →
	// "<type>/<id>"), for flows started by a record's state.
	recentChanges records.RecentChanges
}

// Changed are the records the decision of event e put.
func (t *Tenant) Changed(e platform.Event) []string {
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	return slices.Clone(t.records.recentChanges.Entries.Get(e.App + "/" + e.Record.GetChangeId()))
}

// Held is a record by "<type>/<id>", as its app holds it, for the platform's own apps.
func (t *Tenant) held(ref string) (any, bool) {
	typ, id, _ := strings.Cut(ref, "/")
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	et := t.records.types[typ]
	if et == nil {
		return nil, false
	}
	row := et.rows.Get(id)
	if row == nil {
		return nil, false
	}
	return records.Copy(et.info.Go, row.Value.Interface()).Interface(), true
}

type entityType struct {
	store   *recordStore
	indexes records.DerivedIndexes
	info    platform.EntityInfo
	rows    records.Rows
	// Derived indexes share the same immutable rows. Writes update only built
	// reference buckets; schema replacement discards them with its type image.
	fullImage bool // private schema/bulk replacement needs full admission once
	// knowledge says a record of this type may become knowledge: it declares a
	// knowledge field, it is a knowledge document, or it is a file that may be
	// attached to one.
	knowledge          bool
	retainedProjection bool // query fields are interpreted through an older owner shape
}

// Public response names remain stable; internal consumers use the records owner.
type RecordChange = records.RecordChange

type FieldChange = records.FieldChange

func newRecordStore() *recordStore {
	s := &recordStore{rules: platform.NativeRules{}, types: map[string]*entityType{}, byGo: map[reflect.Type]*entityType{}}
	s.current = records.NewCurrentScope(func(typ string) platform.EntityInfo { return s.types[typ].info })
	return s
}

// forkRecords snapshots immutable row, dirty and change indexes without
// visiting record values/history. Only type metadata and the bounded recent
// change order are copied; writes replace their own index paths and rows.
// The accepted-result path appends before promoting this isolated view.
func (s *recordStore) forkRecords() *recordStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.forkRecordsLocked()
}

func (s *recordStore) forkRecordsLocked() *recordStore {
	s.prepareUniqueIndexes()
	draft := &recordStore{rules: s.rules, liveVersions: maps.Clone(s.liveVersions), uniqueLinks: maps.Clone(s.uniqueLinks), archiveLinks: maps.Clone(s.archiveLinks), parent: s, generation: s.generation, baseGeneration: s.generation, types: make(map[string]*entityType, len(s.types)),
		byGo: make(map[reflect.Type]*entityType, len(s.byGo)), writes: map[string]bool{},
		dirty: s.dirty, recentChanges: s.recentChanges.Fork()}
	draft.current = s.current.Fork(func(typ string) platform.EntityInfo { return draft.types[typ].info })
	for typ, et := range s.types {
		copied := &entityType{store: draft, indexes: et.indexes.Fork(), info: et.info, knowledge: et.knowledge, rows: et.rows, fullImage: s.parent != nil && et.fullImage, retainedProjection: et.retainedProjection}
		if copied.rows.Sourced() {
			copied.rows = copied.rows.Rebind(draft.current.Source(typ, nil))
		}
		draft.types[typ] = copied
		draft.byGo[et.info.Go] = copied
	}
	return draft
}

// promoteRecords makes a staged view visible after its result is durable.
// The tenant lock must cover the decision and promotion. The projection is
// notified only for the rows the decision changed, never while staging.
func (s *recordStore) promoteRecords(draft *recordStore) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if draft == nil || draft.parent != s {
		return fmt.Errorf("record draft belongs to another store")
	}
	draft.mu.Lock()
	defer draft.mu.Unlock()
	if draft.sealed || s.generation != draft.baseGeneration {
		return fmt.Errorf("record draft is stale or already promoted")
	}
	if err := draft.validateChangedLinkConstraintsLocked(); err != nil {
		return err
	}
	s.uniqueLinks = draft.uniqueLinks
	s.archiveLinks = draft.archiveLinks
	s.types, s.byGo = draft.types, draft.byGo
	for _, et := range s.types {
		et.store = s
		if et.rows.Sourced() {
			et.rows = et.rows.Rebind(s.current.Source(et.info.Type, nil))
		}
	}
	if s.parent == nil {
		for _, et := range s.types {
			et.fullImage = false
		}
	}
	s.current.SelectRelease(draft.current.Release())
	s.dirty, s.recentChanges = draft.dirty, draft.recentChanges
	s.generation++
	draft.sealed = true
	refs := make([]string, 0, len(draft.writes))
	for ref := range draft.writes {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	for _, ref := range refs {
		typ, _, _ := strings.Cut(ref, "/")
		s.liveTouched(typ)
		if s.parent == nil && s.current.Backend() != nil {
			_, id, _ := strings.Cut(ref, "/")
			et := s.types[typ]
			s.attachHistory(et, id, et.rows.Get(id), s.current.Backend(), s.current.Tenant())
		}
		if s.touched != nil {
			typ, id, _ := strings.Cut(ref, "/")
			s.touched(typ, id)
		}
	}
	return nil
}

// sortedTypes are the declared entity types, by name.
func (s *recordStore) sortedTypes() []*entityType {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*entityType, 0, len(s.types))
	for _, et := range s.types {
		out = append(out, et)
	}
	slices.SortFunc(out, func(a, b *entityType) int { return strings.Compare(a.info.Type, b.info.Type) })
	return out
}

func jsonOf(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }

// liveTouched is ephemeral query invalidation metadata, protected by mu.
func (s *recordStore) liveTouched(typ string) {
	if s.liveVersions == nil {
		s.liveVersions = map[string]uint64{}
	}
	s.liveVersions[typ]++
}

// All replacements, including pure accepted-result application, use this
// type owner. No decision rules run here; only derived lookup buckets change.
func (et *entityType) setRows(rows records.Index[*records.Row]) {
	et.rows, et.fullImage = records.MemoryRows(rows), true
	et.indexes.Reset()
	if et.store != nil {
		// A schema/full-image replacement may change every referenced scope.
		// Bulk recovery may rebuild; ordinary setRow keeps its existing roots.
		for _, other := range et.store.types {
			other.indexes.ResetUnique()
		}
	}
}

func (et *entityType) setRow(id string, value *records.Row) {
	et.indexes.ReplaceReferences(et.info, et.rows, id, value)
	et.rows.Set(id, value)
	if et.store != nil {
		et.store.refreshUniqueIndexes(et.info.Type, id)
	}
}
func (et *entityType) indexImage() records.IndexImage {
	return records.IndexImage{Info: et.info, Rows: et.rows, Indexes: &et.indexes, RetainedProjection: et.retainedProjection, Current: func() *records.CurrentScope { return et.store.current }}
}
func (s *recordStore) indexImage(typ string) (records.IndexImage, bool) {
	et := s.types[typ]
	if et == nil {
		return records.IndexImage{}, false
	}
	return et.indexImage(), true
}
func (et *entityType) referenceIndex(field platform.FieldInfo) records.References {
	return et.indexes.References(et.indexImage(), field)
}
func (et *entityType) referenceBucket(field platform.FieldInfo, target string) records.Bucket {
	return et.indexes.ReferenceBucket(et.indexImage(), field, target)
}
func (et *entityType) uniqueIndex(s *recordStore, rule platform.Constraint) records.UniqueIndex {
	return et.indexes.Unique(et.indexImage(), s.indexImage, rule)
}

func (s *recordStore) refreshUniqueIndexes(changedType, changedID string) {
	for _, et := range s.types {
		et.indexes.RefreshUnique(et.indexImage(), s.indexImage, changedType, changedID)
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
		key, err := records.UniqueKey(s.indexImage, et.info, rule, r.Value)
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
				for child := range records.ScopeDependents(s.indexImage, et.info, rule, typ, id).All() {
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
	if s.current.Backend() != nil {
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

// useCurrentRows is called only after the existing images have been checked
// against SQL, or after the complete accepted changes have been applied there.
// A private decision keeps its immutable overlay until its parent accepts it.
func (s *recordStore) useCurrentRows() {
	for typ, et := range s.types {
		et.rows = records.SourceRows(s.current.Source(typ, nil))
		et.indexes.Reset()
		et.fullImage = false
	}
}
