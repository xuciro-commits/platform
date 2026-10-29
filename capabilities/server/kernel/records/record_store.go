package records

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"platformserver/journal"
	"platformserver/platform"

	"reflect"

	"slices"
	"strings"
	"sync"
)

type Store struct {
	current *CurrentScope
	rules   platform.RuleEngine

	mu sync.Mutex

	liveVersions   map[string]uint64
	uniqueLinks    map[string]platform.LinkType
	archiveLinks   map[string]platform.LinkType
	types          map[string]*Type
	byGo           map[reflect.Type]*Type
	parent         *Store
	generation     uint64
	baseGeneration uint64
	writes         map[string]bool
	sealed         bool
	touched        func(typ, id string)
	dirty          Index[bool]
	recentChanges  RecentChanges
}

type Type struct {
	store              *Store
	indexes            DerivedIndexes
	info               platform.EntityInfo
	rows               Rows
	fullImage          bool
	knowledge          bool
	retainedProjection bool
}

func NewStore() *Store {
	s := &Store{rules: platform.NativeRules{}, types: map[string]*Type{}, byGo: map[reflect.Type]*Type{}}
	s.current = NewCurrentScope(func(typ string) platform.EntityInfo { return s.types[typ].info })
	return s
}

func (s *Store) ForkRecords() *Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ForkRecordsLocked()
}

func (s *Store) ForkRecordsLocked() *Store {
	s.PrepareUniqueIndexes()
	draft := &Store{rules: s.rules, liveVersions: maps.Clone(s.liveVersions), uniqueLinks: maps.Clone(s.uniqueLinks), archiveLinks: maps.Clone(s.archiveLinks), parent: s, generation: s.generation, baseGeneration: s.generation, types: make(map[string]*Type, len(s.types)),
		byGo: make(map[reflect.Type]*Type, len(s.byGo)), writes: map[string]bool{},
		dirty: s.dirty, recentChanges: s.recentChanges.Fork()}
	draft.current = s.current.Fork(func(typ string) platform.EntityInfo { return draft.types[typ].info })
	for typ, et := range s.types {
		copied := &Type{store: draft, indexes: et.indexes.Fork(), info: et.info, knowledge: et.knowledge, rows: et.rows, fullImage: s.parent != nil && et.fullImage, retainedProjection: et.retainedProjection}
		if copied.rows.Sourced() {
			copied.rows = copied.rows.Rebind(draft.current.Source(typ, nil))
		}
		draft.types[typ] = copied
		draft.byGo[et.info.Go] = copied
	}
	return draft
}

func (s *Store) PromoteRecords(draft *Store) error {
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
	if err := draft.ValidateChangedLinkConstraintsLocked(); err != nil {
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
		s.LiveTouched(typ)
		if s.parent == nil && s.current.Backend() != nil {
			_, id, _ := strings.Cut(ref, "/")
			et := s.types[typ]
			s.AttachHistory(et, id, et.rows.Get(id), s.current.Backend(), s.current.Tenant())
		}
		if s.touched != nil {
			typ, id, _ := strings.Cut(ref, "/")
			s.touched(typ, id)
		}
	}
	return nil
}

func (s *Store) SortedTypes() []*Type {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Type, 0, len(s.types))
	for _, et := range s.types {
		out = append(out, et)
	}
	slices.SortFunc(out, func(a, b *Type) int { return strings.Compare(a.info.Type, b.info.Type) })
	return out
}

func (s *Store) LiveTouched(typ string) {
	if s.liveVersions == nil {
		s.liveVersions = map[string]uint64{}
	}
	s.liveVersions[typ]++
}

func (et *Type) SetRows(rows Index[*Row]) {
	et.rows, et.fullImage = MemoryRows(rows), true
	et.indexes.Reset()
	if et.store != nil {
		for _, other := range et.store.types {
			other.indexes.ResetUnique()
		}
	}
}

func (et *Type) SetRow(id string, value *Row) {
	et.indexes.ReplaceReferences(et.info, et.rows, id, value)
	et.rows.Set(id, value)
	if et.store != nil {
		et.store.RefreshUniqueIndexes(et.info.Type, id)
	}
}

func (et *Type) IndexImage() IndexImage {
	return IndexImage{Info: et.info, Rows: et.rows, Indexes: &et.indexes, RetainedProjection: et.retainedProjection, Current: func() *CurrentScope { return et.store.current }}
}

func (s *Store) IndexImage(typ string) (IndexImage, bool) {
	et := s.types[typ]
	if et == nil {
		return IndexImage{}, false
	}
	return et.IndexImage(), true
}

func (et *Type) ReferenceIndex(field platform.FieldInfo) References {
	return et.indexes.References(et.IndexImage(), field)
}

func (et *Type) ReferenceBucket(field platform.FieldInfo, target string) Bucket {
	return et.indexes.ReferenceBucket(et.IndexImage(), field, target)
}

func (et *Type) UniqueIndex(s *Store, rule platform.Constraint) UniqueIndex {
	return et.indexes.Unique(et.IndexImage(), s.IndexImage, rule)
}

func (s *Store) RefreshUniqueIndexes(changedType, changedID string) {
	for _, et := range s.types {
		et.indexes.RefreshUnique(et.IndexImage(), s.IndexImage, changedType, changedID)
	}
}

func (s *Store) UniqueKeys(typ, id string) map[string]string {
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
		key, err := UniqueKey(s.IndexImage, et.info, rule, r.Value)
		if err == nil && key != "" {
			keys[rule.Name] = key
		}
	}
	return keys
}

func (s *Store) UniqueChanges() map[string]map[string]string {
	var affected Index[bool]
	for ref := range s.writes {
		affected.Set(ref, true)
		typ, id, _ := strings.Cut(ref, "/")
		for _, et := range s.types {
			for _, rule := range et.info.Constraints {
				if rule.Kind != "unique" {
					continue
				}
				for child := range ScopeDependents(s.IndexImage, et.info, rule, typ, id).All() {
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
		changes[ref] = s.UniqueKeys(typ, id)
	}
	return changes
}

func (s *Store) PrepareUniqueIndexes() {
	if s.current.Backend() != nil {
		return
	}
	for _, et := range s.types {
		for _, rule := range et.info.Constraints {
			if rule.Kind == "unique" {
				et.UniqueIndex(s, rule)
			}
		}
	}
}

func (s *Store) UseCurrentRows() {
	for typ, et := range s.types {
		et.rows = SourceRows(s.current.Source(typ, nil))
		et.indexes.Reset()
		et.fullImage = false
	}
}

func (s *Store) AttachHistory(et *Type, id string, r *Row, store journal.RuntimeState, tenant string) {
	count := r.HistoryLength()
	copy := *r
	copy.History = nil
	copy.HistoryParent = nil
	copy.HistoryCount = count
	typ := et.info.Type
	copy.LoadHistory = func(after int) []RecordChange {
		list, err := store.StateHistory(context.Background(), tenant, typ, id, after, count)
		if err != nil {
			panic(fmt.Errorf("record history %s/%s: %w", typ, id, err))
		}
		out := make([]RecordChange, len(list))
		for i, raw := range list {
			if err := json.Unmarshal(raw, &out[i]); err != nil {
				panic(err)
			}
		}
		return out
	}
	et.SetRow(id, &copy)
}

func DecodeRowImage(info platform.EntityInfo, raw json.RawMessage, projection *Projection, release, format string) (json.RawMessage, error) {
	decode, err := DecodeFormattedImage(info, raw, projection, release, format)
	if err == nil {
		return decode, nil
	}
	pruned, pruneErr := PruneImage(info, raw)
	if pruneErr != nil {
		return nil, err
	}
	return DecodeFormattedImage(info, pruned, projection, release, format)
}

func (s *Store) Install(info platform.EntityInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if other := s.byGo[info.Go]; other != nil && other.info.Type != info.Type {
		return fmt.Errorf("%s: another type is already this Go type", info.Type)
	}
	et := &Type{store: s, info: info, rows: Rows{}, knowledge: KnowledgeBearing(info)}
	if before := s.types[info.Type]; before != nil {
		if before.info.App != info.App {
			return fmt.Errorf("%s is declared by %s", info.Type, before.info.App)
		}
		if before.info.Versioning != nil && !reflect.DeepEqual(before.info.Versioning, info.Versioning) || len(before.info.VersionPins) > 0 && !reflect.DeepEqual(before.info.VersionPins, info.VersionPins) {
			return fmt.Errorf("published business versioning and pin bindings are retained")
		}
		rows := before.rows
		if rows.Sourced() && s.current.Backend() != nil && !before.retainedProjection {
			rows = rows.Rebind(s.current.Source(info.Type, &info))
		}
		entries := make([]Entry[*Row], 0, rows.Len())
		for id, r := range rows.All() {
			raw, err := r.Image()
			if err != nil {
				return err
			}
			v := reflect.New(info.Go).Elem()
			decode, err := DecodeRowImage(info, raw, r.Projection, s.current.Release(), r.Format)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(decode, v.Addr().Interface()); err != nil {
				return err
			}
			platform.ApplyFieldAliases(info, v)
			EmptyLists(v)
			installed := &Row{Value: v, Format: r.Format, HistoryParent: r.HistoryParent, HistoryHead: r.HistoryHead, History: r.History, LoadHistory: r.LoadHistory, HistoryCount: r.HistoryCount, Projection: r.Projection, Extraction: r.Extraction}
			installed.RetainOriginal(raw)
			entries = append(entries, Entry[*Row]{Key: id, Value: installed})
		}
		index, err := From(entries)
		if err != nil {
			return err
		}
		et.SetRows(index)
		delete(s.byGo, before.info.Go)
	} else if s.current.Backend() != nil && s.current.View() != nil {
		et.rows = SourceRows(s.current.Source(info.Type, nil))
	}
	s.types[info.Type], s.byGo[info.Go] = et, et
	s.generation++
	return nil
}

func (s *Store) Lock()                              { s.mu.Lock() }
func (s *Store) Unlock()                            { s.mu.Unlock() }
func (s *Store) Current() *CurrentScope             { return s.current }
func (s *Store) SetRules(rules platform.RuleEngine) { s.rules = rules }
func (s *Store) Type(name string) *Type             { return s.types[name] }
func (s *Store) GoType(shape reflect.Type) *Type    { return s.byGo[shape] }
func (s *Store) TypeCount() int                     { return len(s.types) }
func (s *Store) Types() iter.Seq2[string, *Type]    { return maps.All(s.types) }
func (s *Store) Parent() *Store                     { return s.parent }
func (s *Store) Generation() uint64                 { return s.generation }
func (s *Store) BaseGeneration() uint64             { return s.baseGeneration }
func (s *Store) Writes() iter.Seq[string]           { return maps.Keys(s.writes) }
func (s *Store) Written(ref string) bool            { return s.writes[ref] }
func (s *Store) WriteCount() int                    { return len(s.writes) }
func (s *Store) TrackWrite(ref string)              { s.writes[ref] = true }
func (s *Store) Dirty() Index[bool]                 { return s.dirty }
func (s *Store) TakeDirty() Index[bool]             { dirty := s.dirty; s.dirty = Index[bool]{}; return dirty }
func (s *Store) MarkDirty(ref string)               { s.dirty.Set(ref, true) }
func (s *Store) RememberChange(change, ref string)  { s.recentChanges.Remember(change, ref) }
func (s *Store) Changed(change string) []string {
	return slices.Clone(s.recentChanges.Entries.Get(change))
}
func (s *Store) LiveVersion(typ string) uint64           { return s.liveVersions[typ] }
func (s *Store) SetTouched(touched func(string, string)) { s.touched = touched }
func (s *Store) InheritDecision(base *Store) {
	s.parent, s.baseGeneration, s.writes = base.parent, base.baseGeneration, maps.Clone(base.writes)
}
func (s *Store) Restored()                       { s.generation++ }
func (et *Type) Info() platform.EntityInfo       { return et.info }
func (et *Type) Rows() Rows                      { return et.rows }
func (et *Type) Knowledge() bool                 { return et.knowledge }
func (et *Type) RetainedProjection() bool        { return et.retainedProjection }
func (et *Type) DerivedSnapshot() DerivedIndexes { return et.indexes }
func NewTypeView(info platform.EntityInfo, rows Rows, retained bool) *Type {
	return &Type{info: info, rows: rows, retainedProjection: retained}
}

func (s *Store) ReplaceReadType(info platform.EntityInfo, rows Rows, retained bool) *Type {
	knowledge := KnowledgeBearing(info)
	if old := s.types[info.Type]; old != nil {
		knowledge = old.knowledge
		delete(s.byGo, old.info.Go)
	}
	et := &Type{store: s, info: info, rows: rows, knowledge: knowledge, retainedProjection: retained}
	s.types[info.Type], s.byGo[info.Go] = et, et
	return et
}

func (s *Store) WriteSnapshot() map[string]bool   { return maps.Clone(s.writes) }
func (s *Store) UseWrites(writes map[string]bool) { s.writes = writes }
func (s *Store) ExtendDecision(base *Store) {
	s.parent, s.baseGeneration = base.parent, base.baseGeneration
	maps.Copy(s.writes, base.writes)
}
func (s *Store) Anchor(parent *Store) error {
	writes := map[string]bool{}
	cursor := s
	var generation uint64
	for cursor != parent {
		if cursor == nil || cursor.parent == nil || cursor.sealed {
			return fmt.Errorf("release record view is not an unpromoted descendant")
		}
		predecessor := cursor.parent
		if predecessor.generation != cursor.baseGeneration {
			return fmt.Errorf("release record predecessor changed")
		}
		maps.Copy(writes, cursor.writes)
		generation = cursor.baseGeneration
		cursor = predecessor
	}
	s.parent, s.baseGeneration, s.writes = parent, generation, writes
	return nil
}

func (s *Store) ChangeRoot() Index[[]string] { return s.recentChanges.Entries }

type StoredState struct {
	HistoryHead string          `json:"historyHead,omitempty"`
	Value       json.RawMessage `json:"value"`
	Format      string          `json:"format,omitempty"`
	History     []RecordChange  `json:"history"`
	Projection  *Projection     `json:"projection,omitempty"`
	Extraction  *Extraction     `json:"extraction,omitempty"`
}

func (s *Store) Rules() platform.RuleEngine { return s.rules }
