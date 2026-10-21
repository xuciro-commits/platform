package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/internal/records"
	"platformserver/journal"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"sync"
)

type recordStore struct {
	historyStore    journal.RuntimeState
	readView        journal.StateReadView
	appliedPosition int64
	currentPosition func() int64
	tenant          string
	mu              sync.Mutex
	readRelease     string // decoder context, derived from the release owner's pointer
	liveVersions    map[string]uint64
	uniqueLinks     map[string]platform.LinkType
	archiveLinks    map[string]platform.LinkType
	types           map[string]*entityType
	byGo            map[reflect.Type]*entityType
	// A private 19a decision view has no projection callback. It may replace
	// its parent only if no direct write reached that parent in the meantime.
	parent         *recordStore
	generation     uint64
	baseGeneration uint64
	writes         map[string]bool
	sealed         bool
	// touched, when set, hears of each record put, under mu (the PostgreSQL projection, ADR-0024).
	touched func(typ, id string)
	// dirty are the records of knowledge-bearing types put since the index last
	// read it, under mu: knowledge is cut again for those records alone, not by
	// walking the tenant at every search (ADR-0009 14b, #130).
	dirty records.Index[bool]
	// changed are the records each recent decision put ("<app>/<change>" →
	// "<type>/<id>"), for flows started by a record's state (ADR-0038).
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
	store         *recordStore
	uniqueIndexes map[string]records.UniqueIndex
	info          platform.EntityInfo
	rows          records.Rows
	// Derived indexes share the same immutable rows. Writes update only built
	// reference buckets; schema replacement discards them with its type image.
	referenceIndexes map[string]records.References
	fullImage        bool // private schema/bulk replacement needs full admission once
	// knowledge says a record of this type may become knowledge: it declares a
	// knowledge field, it is a knowledge document, or it is a file that may be
	// attached to one (ADR-0009).
	knowledge          bool
	retainedProjection bool // query fields are interpreted through an older owner shape
}

// Public response names remain stable; internal consumers use the records owner.
type RecordChange = records.RecordChange

type FieldChange = records.FieldChange

func newRecordStore() *recordStore {
	return &recordStore{types: map[string]*entityType{}, byGo: map[reflect.Type]*entityType{}}
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
	draft := &recordStore{historyStore: s.historyStore, readView: s.readView, tenant: s.tenant, appliedPosition: s.appliedPosition, currentPosition: s.currentPosition, readRelease: s.readRelease, liveVersions: maps.Clone(s.liveVersions), uniqueLinks: maps.Clone(s.uniqueLinks), archiveLinks: maps.Clone(s.archiveLinks), parent: s, generation: s.generation, baseGeneration: s.generation, types: make(map[string]*entityType, len(s.types)),
		byGo: make(map[reflect.Type]*entityType, len(s.byGo)), writes: map[string]bool{},
		dirty: s.dirty, recentChanges: s.recentChanges.Fork()}
	for typ, et := range s.types {
		copied := &entityType{store: draft, uniqueIndexes: maps.Clone(et.uniqueIndexes), info: et.info, knowledge: et.knowledge, rows: et.rows, referenceIndexes: maps.Clone(et.referenceIndexes), fullImage: s.parent != nil && et.fullImage, retainedProjection: et.retainedProjection}
		if copied.rows.Sourced() {
			copied.rows = copied.rows.Rebind(&currentRows{store: draft, typ: typ})
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
			et.rows = et.rows.Rebind(&currentRows{store: s, typ: et.info.Type})
		}
	}
	if s.parent == nil {
		for _, et := range s.types {
			et.fullImage = false
		}
	}
	s.readRelease = draft.readRelease
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
		if s.parent == nil && s.historyStore != nil {
			_, id, _ := strings.Cut(ref, "/")
			et := s.types[typ]
			s.attachHistory(et, id, et.rows.Get(id), s.historyStore, s.tenant)
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
