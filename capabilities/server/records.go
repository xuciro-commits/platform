package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/journal"
	"platformserver/platform"
)

// The record store (ADR-0046): the records of every app's entity types,
// kept by the host in memory and rebuilt by replay, since every record is put
// by a decision inside its input. Apps read their own records unscoped;
// members read through one contract, scoped by each type's declaration.
// A projection into PostgreSQL (stage 3) will serve the same reads.

type recordStore struct {
	historyStore    journal.RuntimeState
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
	dirty recordIndex[bool]
	// changed are the records each recent decision put ("<app>/<change>" →
	// "<type>/<id>"), for flows started by a record's state (ADR-0038).
	changed recordIndex[[]string]
	order   []string
}

const changesKept = 10000

func (s *recordStore) remember(change, ref string) {
	if s.changed.get(change) == nil {
		s.order = append(s.order, change)
		if len(s.order) > changesKept {
			s.changed.remove(s.order[0])
			s.order = s.order[1:]
		}
	}
	if refs := s.changed.get(change); !slices.Contains(refs, ref) {
		s.changed.set(change, append(slices.Clone(refs), ref))
	}
}

// Changed are the records the decision of event e put.
func (t *Tenant) Changed(e platform.Event) []string {
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	return slices.Clone(t.records.changed.get(e.App + "/" + e.Record.GetChangeId()))
}

// Held is a record by "<type>/<id>", as its app holds it, for the platform's own apps.
func (t *Tenant) Held(ref string) (any, bool) {
	typ, id, _ := strings.Cut(ref, "/")
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	et := t.records.types[typ]
	if et == nil || et.rows.get(id) == nil {
		return nil, false
	}
	return copyOf(et.info.Go, et.rows.get(id).value.Interface()).Interface(), true
}

type entityType struct {
	info platform.EntityInfo
	rows recordIndex[*row]
	// Derived indexes share the same immutable rows. Writes update only built
	// reference buckets; schema replacement discards them with its type image.
	referenceIndexes map[string]referenceIndex
	fullImage        bool // private schema/bulk replacement needs full admission once
	// knowledge says a record of this type may become knowledge: it declares a
	// knowledge field, it is a knowledge document, or it is a file that may be
	// attached to one (ADR-0009).
	knowledge bool
}

// viewOf is a type as m may see it (ADR-0038): without the fields m's role
// in the app may not read, so search, filters, sort, grouping and forms never
// reach them; the rows are shared. hidden are those fields.
func viewOf(m platform.Member, et *entityType) (view *entityType, hidden []platform.FieldInfo) {
	roles := m.RolesIn(et.info.App)
	readable := func(f platform.FieldInfo) bool {
		if !f.ReadsAny(roles) || m.AIContext && f.AIInput != nil && !*f.AIInput {
			return false
		}
		if f.AliasOf != "" {
			target, found := et.info.Field(f.AliasOf)
			return found && target.ReadsAny(roles) && !(m.AIContext && target.AIInput != nil && !*target.AIInput)
		}
		return true
	}
	for _, f := range et.info.Fields {
		if !readable(f) {
			hidden = append(hidden, f)
		}
	}
	if len(hidden) == 0 {
		return et, nil
	}
	info := et.info
	info.Fields = slices.DeleteFunc(slices.Clone(info.Fields), func(f platform.FieldInfo) bool { return !readable(f) })
	return &entityType{info: info, rows: et.rows}, hidden
}

// masked is a record with the hidden fields at their zero value.
func masked(et *entityType, v reflect.Value, hidden []platform.FieldInfo) any {
	c := copyOf(et.info.Go, v.Interface())
	for _, f := range hidden {
		c.FieldByIndex(f.Index).SetZero()
	}
	return c.Interface()
}

// maskedHistory drops the hidden fields from a record's changes.
func maskedHistory(h []RecordChange, hidden []platform.FieldInfo) []RecordChange {
	if len(hidden) == 0 {
		return h
	}
	out := make([]RecordChange, 0, len(h))
	for _, c := range h {
		c.Fields = slices.DeleteFunc(slices.Clone(c.Fields), func(f FieldChange) bool {
			return slices.ContainsFunc(hidden, func(x platform.FieldInfo) bool { return x.Name == f.Field })
		})
		out = append(out, c)
	}
	return out
}

// A row and its nested values are immutable once stored. Writers replace it;
// readers that expose editable values copy them at the API boundary.
type row struct {
	value        reflect.Value // the entity struct, a private copy
	history      []RecordChange
	loadHistory  func(int) []RecordChange
	historyCount int
	// Recovery bytes preserve an accepted predecessor's identity when typed
	// decoding normalises its definition. The next mutation writes only the
	// current format; these bytes have no writable or executable API.
	original   json.RawMessage
	projection *recordProjection
	extraction *extractionOrigin
}

type extractionOrigin struct {
	Migration string `json:"migration"`
	Source    string `json:"source"`
	Step      int    `json:"step"`
}

// A migration retains the source release's read image while its target facts
// are accepted in batches. Only that exact shape can select this projection.
type recordProjection struct {
	From  string             `json:"from"`
	Shape string             `json:"shape"`
	Value json.RawMessage    `json:"value"`
	Prior []recordProjection `json:"prior,omitempty"`
}

func cloneRecordProjection(p *recordProjection) *recordProjection {
	if p == nil {
		return nil
	}
	copy := &recordProjection{From: p.From, Shape: p.Shape, Value: slices.Clone(p.Value)}
	for _, prior := range p.Prior {
		copy.Prior = append(copy.Prior, *cloneRecordProjection(&prior))
	}
	return copy
}

func projectionFor(p *recordProjection, release string) *recordProjection {
	if p == nil {
		return nil
	}
	if p.From == release {
		return p
	}
	for i := range p.Prior {
		if p.Prior[i].From == release {
			return &p.Prior[i]
		}
	}
	return nil
}

// Source values remain facts of their release. Only the common record header
// follows subsequent accepted writes, so every image names the same record.
func refreshRecordProjection(p *recordProjection, value any) *kernel.Error {
	if p == nil {
		return nil
	}
	var header map[string]json.RawMessage
	if json.Unmarshal(jsonOf(value), &header) != nil {
		return migrationRefusal(fmt.Errorf("versioned record header is invalid"))
	}
	refresh := func(image *recordProjection) *kernel.Error {
		var fields map[string]json.RawMessage
		if json.Unmarshal(image.Value, &fields) != nil {
			return migrationRefusal(fmt.Errorf("versioned source image is invalid"))
		}
		for _, name := range []string{"id", "revision", "created", "changed", "archived", "versionPins"} {
			if value, present := header[name]; present {
				fields[name] = value
			} else {
				delete(fields, name)
			}
		}
		raw, err := json.Marshal(fields)
		image.Value = raw
		return migrationRefusal(err)
	}
	if err := refresh(p); err != nil {
		return err
	}
	for i := range p.Prior {
		if err := refresh(&p.Prior[i]); err != nil {
			return err
		}
	}
	return nil
}

func rowDecodeImage(info platform.EntityInfo, value json.RawMessage, projection *recordProjection, release string) (json.RawMessage, error) {
	if projection == nil {
		return value, nil
	}
	shape, err := canonicalDigest(info.Fields)
	if err != nil {
		return nil, err
	}
	var actual platform.Record
	if json.Unmarshal(value, &actual) != nil {
		return nil, fmt.Errorf("versioned record identity is invalid")
	}
	seen := map[string]bool{}
	images := append([]recordProjection{*projection}, projection.Prior...)
	for i, image := range images {
		if image.From == "" || image.Shape == "" || len(image.Value) == 0 || seen[image.From] || i > 0 && len(image.Prior) != 0 {
			return nil, fmt.Errorf("incomplete or duplicate versioned record projection")
		}
		seen[image.From] = true
		var view platform.Record
		if json.Unmarshal(image.Value, &view) != nil || !reflect.DeepEqual(actual, view) {
			return nil, fmt.Errorf("versioned record projection identity differs")
		}
	}
	if selected := projectionFor(projection, release); selected != nil && shape == selected.Shape {
		return selected.Value, nil
	}
	return value, nil
}

func (r *row) image() (json.RawMessage, error) {
	if len(r.original) > 0 {
		return slices.Clone(r.original), nil
	}
	return json.Marshal(r.value.Interface())
}

func (r *row) retainOriginal(raw json.RawMessage) {
	decoded, err := json.Marshal(r.value.Interface())
	before, beforeErr := canonicalDigest(raw)
	after, afterErr := canonicalDigest(json.RawMessage(decoded))
	if err == nil && beforeErr == nil && afterErr == nil && before != after {
		r.original = slices.Clone(raw)
	}
}

// RecordChange is one decision's change to a record: the fields it set.
type RecordChange struct {
	Change string        `json:"change"`
	Schema string        `json:"schema"`
	By     string        `json:"by"`
	At     time.Time     `json:"at"`
	Fields []FieldChange `json:"fields"`
}

type FieldChange struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

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
	draft := &recordStore{historyStore: s.historyStore, tenant: s.tenant, appliedPosition: s.appliedPosition, currentPosition: s.currentPosition, readRelease: s.readRelease, liveVersions: maps.Clone(s.liveVersions), uniqueLinks: maps.Clone(s.uniqueLinks), archiveLinks: maps.Clone(s.archiveLinks), parent: s, generation: s.generation, baseGeneration: s.generation, types: make(map[string]*entityType, len(s.types)),
		byGo: make(map[reflect.Type]*entityType, len(s.byGo)), writes: map[string]bool{},
		dirty: s.dirty, changed: s.changed,
		order: slices.Clone(s.order)}
	for typ, et := range s.types {
		copied := &entityType{info: et.info, knowledge: et.knowledge, rows: et.rows, referenceIndexes: maps.Clone(et.referenceIndexes), fullImage: s.parent != nil && et.fullImage}
		draft.types[typ] = copied
		draft.byGo[et.info.Go] = copied
	}
	return draft
}

func copyHistory(history []RecordChange) []RecordChange {
	out := slices.Clone(history)
	for i := range out {
		out[i].Fields = slices.Clone(history[i].Fields)
		for j := range out[i].Fields {
			out[i].Fields[j].Before = slices.Clone(history[i].Fields[j].Before)
			out[i].Fields[j].After = slices.Clone(history[i].Fields[j].After)
		}
	}
	return out
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
	if s.parent == nil {
		for _, et := range s.types {
			et.fullImage = false
		}
	}
	s.readRelease = draft.readRelease
	s.dirty, s.changed, s.order = draft.dirty, draft.changed, draft.order
	s.generation++
	if s.parent == nil && s.currentPosition != nil {
		s.appliedPosition = s.currentPosition()
	}
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
			s.attachHistory(et, id, et.rows.get(id), s.historyStore, s.tenant)
		}
		if s.touched != nil {
			typ, id, _ := strings.Cut(ref, "/")
			s.touched(typ, id)
		}
	}
	return nil
}

// declare registers an app's entity types; references resolve within the app.
func (s *recordStore) declare(a platform.App) error {
	m := a.Manifest()
	own := map[reflect.Type]string{}
	for _, e := range m.Entities {
		own[reflect.TypeOf(e.Model)] = e.Type
	}
	classes := map[string]bool{}
	for _, d := range a.Declarations() {
		classes[d.GetDataClass()] = true
	}
	for _, e := range m.Entities {
		info, err := platform.Describe(m.ID, e, func(t reflect.Type) string { return own[t] })
		if err != nil {
			return err
		}
		if !classes[e.Type] {
			return fmt.Errorf("entity %s is not a data class the app is authority for", e.Type)
		}
		if s.types[e.Type] != nil || s.byGo[info.Go] != nil {
			return fmt.Errorf("entity %s is declared twice", e.Type)
		}
		et := &entityType{info: info, rows: recordIndex[*row]{}, knowledge: knowledgeBearing(info)}
		s.types[e.Type], s.byGo[info.Go] = et, et
		s.generation++
	}
	for _, e := range m.Entities { // seeds after every type is known, so references resolve
		seeder := platform.NewCaller(nil, platform.Member{ID: "seed"}, m.ID, false, false)
		seed := &pb.ChangeRecord{ChangeId: "seed", Submission: &pb.Submission{PrincipalId: "seed", Schema: &pb.SchemaRef{Name: "seed"}}}
		for _, v := range e.Seed {
			if reflect.TypeOf(v) != reflect.TypeOf(e.Model) {
				return fmt.Errorf("entity %s: a seed record is not a %s", e.Type, reflect.TypeOf(e.Model))
			}
			if err := s.check(seeder, v, time.Now()); err != nil {
				return fmt.Errorf("entity %s: a seed record does not hold: %v", e.Type, err)
			}
			if err := s.put(seeder, seed, v); err != nil {
				return fmt.Errorf("entity %s: seed record: %v", e.Type, err)
			}
		}
	}
	return nil
}

// knowledgeBearing says a record of this type may become a knowledge passage:
// a knowledge field of its own, the knowledge app's document, or a file that
// may be attached to either (ADR-0009). The index re-cuts only these.
func knowledgeBearing(info platform.EntityInfo) bool {
	return info.Type == platform.KnowledgeDocumentType || info.Type == platform.FileType || info.KnowledgeFiles ||
		slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool { return f.Knowledge })
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

// of is c's app's entity type for a Go type; other apps' types are not reachable (D3).
func (s *recordStore) of(c platform.Caller, t reflect.Type) *entityType {
	et := s.byGo[t]
	if et == nil || et.info.App != c.App {
		return nil
	}
	return et
}

// copyOf is a deep copy of an entity struct, through its JSON.
func copyOf(t reflect.Type, v any) reflect.Value {
	raw, _ := json.Marshal(v)
	out := reflect.New(t).Elem()
	json.Unmarshal(raw, out.Addr().Interface())
	return out
}

// emptyLists makes every nil list of a record an empty one, nested ones too,
// so a record never reads null where its type says a list: a page reading
// levels[level] or start.join got null from a Go nil slice (the owner's
// testing, 2026-09-27). Lists marked omitempty are left out, as their tag says.
func emptyLists(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		if _, ok := v.Interface().(time.Time); ok {
			return
		}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			x := v.Field(i)
			if x.Kind() == reflect.Slice && x.IsNil() && !strings.Contains(f.Tag.Get("json"), "omitempty") && x.CanSet() {
				x.Set(reflect.MakeSlice(x.Type(), 0, 0))
				continue
			}
			emptyLists(x)
		}
	case reflect.Slice:
		for i := range v.Len() {
			emptyLists(v.Index(i))
		}
	}
}

func recordOf(v reflect.Value) *platform.Record {
	return v.Field(0).Addr().Interface().(*platform.Record)
}

func (s *recordStore) put(c platform.Caller, r *pb.ChangeRecord, entity any) *kernel.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	et := s.of(c, reflect.TypeOf(entity))
	if et == nil || r == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := s.migrationWriteLocked(et.info, r); err != nil {
		return err
	}
	v := copyOf(et.info.Go, entity)
	platform.ApplyFieldAliases(et.info, v)
	emptyLists(v)
	rec := recordOf(v)
	if rec.ID == "" {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := s.checkLinkWriteLocked(et, v); err != nil {
		return err
	}
	stamp := platform.Stamp{By: r.GetSubmission().GetPrincipalId(), At: r.GetRecordedTime().AsTime(), Change: r.GetChangeId()}
	prev := et.rows.get(rec.ID)
	change := RecordChange{Change: r.GetChangeId(), Schema: r.GetSubmission().GetSchema().GetName(), By: stamp.By, At: stamp.At, Fields: []FieldChange{}}
	// The revision is the kernel's (K4 C12): the decisions naming the record as
	// their target, which clients send back as the revision they saw.
	rec.Changed, rec.Created, rec.Revision = stamp, stamp, 0
	target := r.GetSubmission().GetTarget()
	named := target.GetType() == et.info.Type && target.GetId() == rec.ID
	if l := et.info.Lifecycle; prev == nil && l != nil { // a new record starts in the initial state
		if f, _ := et.info.Field(l.Field); v.FieldByIndex(f.Index).String() == "" {
			v.FieldByIndex(f.Index).SetString(l.Initial)
		}
	}
	if owner := et.info.Scope.Owner; prev == nil && owner != "" && owner != platform.OwnerCreated { // a new record belongs to its creator unless the rules say who
		if f, _ := et.info.Field(owner); v.FieldByIndex(f.Index).String() == "" {
			v.FieldByIndex(f.Index).SetString(stamp.By)
		}
	}
	var history []RecordChange
	if prev != nil {
		old := recordOf(prev.value)
		rec.Created, history, rec.Revision = old.Created, copyHistory(prev.recordHistory()), old.Revision
		if old.Archived != rec.Archived {
			change.Fields = append(change.Fields, FieldChange{Field: "archived", Before: jsonOf(old.Archived), After: jsonOf(rec.Archived)})
		}
	}
	for _, f := range et.info.Fields {
		after := jsonOf(v.FieldByIndex(f.Index).Interface())
		var before json.RawMessage
		if prev != nil {
			before = jsonOf(prev.value.FieldByIndex(f.Index).Interface())
		}
		if string(before) != string(after) && !(prev == nil && v.FieldByIndex(f.Index).IsZero()) {
			change.Fields = append(change.Fields, FieldChange{Field: f.Name, Before: before, After: after})
		}
	}
	if named {
		rec.Revision = r.GetRevision()
	}
	var projection *recordProjection
	var extraction *extractionOrigin
	if prev != nil {
		extraction = prev.extraction
		projection = cloneRecordProjection(prev.projection)
		if err := refreshRecordProjection(projection, v.Interface()); err != nil {
			return err
		}
	}
	et.setRow(rec.ID, &row{value: v, history: append(history, change), projection: projection, extraction: extraction})
	s.generation++
	s.liveTouched(et.info.Type)
	if s.writes != nil {
		s.writes[et.info.Type+"/"+rec.ID] = true
	}
	s.remember(c.App+"/"+r.GetChangeId(), et.info.Type+"/"+rec.ID)
	if et.knowledge {
		s.dirty.set(et.info.Type+"/"+rec.ID, true)
	}
	if s.touched != nil {
		s.touched(et.info.Type, rec.ID)
	}
	return nil
}

func jsonOf(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }

func (s *recordStore) get(c platform.Caller, t reflect.Type, id string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.of(c, t)
	if et == nil || et.rows.get(id) == nil {
		return nil, false
	}
	return copyOf(t, et.rows.get(id).value.Interface()).Interface(), true
}

// check validates an entity against its declaration (Caller.Check).
func (s *recordStore) check(c platform.Caller, entity any, now time.Time) *kernel.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	et := s.of(c, reflect.TypeOf(entity))
	if et == nil {
		return invalid
	}
	v := copyOf(et.info.Go, entity)
	platform.ApplyFieldAliases(et.info, v)
	previous := reflect.Value{}
	if old := et.rows.get(recordOf(v).ID); old != nil {
		previous = old.value
	}
	if err := checkEnterpriseReferences(c, et.info.Fields, v, previous, now); err != nil {
		return err
	}
	// A conditional field (FieldInfo.When) follows its condition's value.
	stringOf := func(name string) string {
		i := slices.IndexFunc(et.info.Fields, func(x platform.FieldInfo) bool { return x.Name == name })
		if i < 0 {
			return ""
		}
		cv := v.FieldByIndex(et.info.Fields[i].Index)
		if cv.Kind() == reflect.Pointer {
			if cv.IsNil() {
				return ""
			}
			cv = cv.Elem()
		}
		if cv.Kind() == reflect.Bool {
			return strconv.FormatBool(cv.Bool())
		}
		return cv.String()
	}
	// Check the resulting record too: a partial edit must not bypass a
	// declared relationship by omitting its unchanged endpoint.
	if !c.Replaying && slices.ContainsFunc(et.info.Fields, func(f platform.FieldInfo) bool { return f.Constraints != nil }) {
		raw, marshalErr := json.Marshal(entity)
		if marshalErr != nil {
			return invalid
		}
		var values map[string]any
		if json.Unmarshal(raw, &values) != nil {
			return invalid
		}
		fields := []platform.Field{}
		for _, f := range et.info.Fields {
			if f.When != nil && !f.Active(stringOf) {
				continue
			}
			// omitempty is a transport detail; a stored zero or false is a value.
			if _, present := values[f.Name]; !present {
				raw, err := json.Marshal(v.FieldByIndex(f.Index).Interface())
				if err != nil {
					return invalid
				}
				var value any
				if json.Unmarshal(raw, &value) != nil {
					return invalid
				}
				values[f.Name] = value
			}
			typ := map[string]string{"integer": "integer", "decimal": "number", "boolean": "boolean", "date": "date", "datetime": "datetime"}[f.Type]
			if typ == "" {
				typ = "string"
			}
			fields = append(fields, platform.Field{Name: f.Name, Type: typ, Required: f.Required, Description: f.Title, Constraints: f.Constraints, Choices: f.Choices})
		}
		if issues := platform.InputIssues(fields, values); len(issues) > 0 {
			return c.RefuseFields(issues)
		}
	}
	for _, f := range et.info.Fields {
		fv := v.FieldByIndex(f.Index)
		if f.When != nil {
			on, _ := et.info.Field(f.When.Field)
			switch active := f.Active(stringOf); {
			case active && f.Required && f.Constraints == nil && fv.IsZero():
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} is required when {condition} is {values}", f.Title, on.Title, strings.Join(f.When.In, ", "))
			case !active && !fv.IsZero():
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} applies only when {condition} is {values}", f.Title, on.Title, strings.Join(f.When.In, ", "))
			case !active:
				continue
			}
		} else if f.Required && f.Constraints == nil && fv.IsZero() {
			return invalid
		}
		if fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}
		switch f.Type {
		case "choice":
			if !fv.IsZero() && !slices.Contains(f.Choices, fv.String()) {
				return invalid
			}
		}
		if l := et.info.Lifecycle; l != nil && f.Name == l.Field && !fv.IsZero() && !slices.ContainsFunc(l.States, func(st platform.State) bool { return st.Name == fv.String() }) {
			return invalid // not a state of the lifecycle
		}
		switch f.Type {
		case "":
		case "json":
			raw, err := json.Marshal(fv.Interface())
			if err != nil {
				return invalid
			}
			if _, err := platform.DecodeValue(raw, 64<<10); err != nil {
				return invalid
			}
		case "date":
			if _, err := time.Parse(time.DateOnly, fv.String()); !fv.IsZero() && err != nil {
				return invalid
			}
		case "text", "longtext":
			if f.Required && strings.TrimSpace(fv.String()) == "" {
				return invalid
			}
		case "money":
			if m := fv.Interface().(platform.Money); m.Amount != 0 && len(m.Currency) != 3 {
				return invalid
			}
		case "reference", "references":
			if f.Ref == "enterprise.element" {
				continue // checked through the directory, including nested lines
			}
			ids := []string{fv.String()}
			if f.Type == "references" {
				ids = ids[:0]
				for i := range fv.Len() {
					ids = append(ids, fv.Index(i).String())
				}
			}
			for _, id := range ids {
				if id != "" && (s.types[f.Ref] == nil || s.types[f.Ref].rows.get(id) == nil) {
					return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE}
				}
			}
		}
	}
	return s.checkLinkWriteLocked(et, v)
}

// Enterprise references resolve through the model rather than the record store.
// Lines carry the same declaration and must not bypass its validity/type check.
func checkEnterpriseReferences(c platform.Caller, fields []platform.FieldInfo, value, previous reflect.Value, now time.Time) *kernel.Error {
	if c.Replaying {
		return nil
	}
	for _, f := range fields {
		v := value.FieldByIndex(f.Index)
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				continue
			}
			v = v.Elem()
		}
		if f.Type == "lines" {
			used := map[int]bool{}
			for i := 0; i < v.Len(); i++ {
				if err := checkEnterpriseReferences(c, f.Fields, v.Index(i), previousLine(previous, f, v.Index(i), used), now); err != nil {
					return err
				}
			}
		}
		if f.Ref != "enterprise.element" || v.IsZero() {
			continue
		}
		if previous.IsValid() && reflect.DeepEqual(value.FieldByIndex(f.Index).Interface(), previous.FieldByIndex(f.Index).Interface()) {
			continue
		}
		el, ok := c.Enterprise().Element(v.String(), now)
		if !ok {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE, "{field} names an enterprise element that is not active: {element}", f.Title, v.String())
		}
		families := slices.Clone(f.Stereotypes)
		if f.Stereotype != "" {
			families = append(families, f.Stereotype)
		}
		if len(families) > 0 && !slices.Contains(families, el.Stereotype) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} names an enterprise element of an unsupported type", f.Title)
		}
	}
	return nil
}

func previousLine(record reflect.Value, field platform.FieldInfo, current reflect.Value, used map[int]bool) reflect.Value {
	if !record.IsValid() {
		return reflect.Value{}
	}
	lines := fieldValue(record, field.Index)
	if !lines.IsValid() || (lines.Kind() != reflect.Slice && lines.Kind() != reflect.Array) {
		return reflect.Value{}
	}
	// A line has no separate identity. Match its reference fields once, allowing
	// reorder or edits to other columns without allowing another retired use.
	for i := 0; i < lines.Len(); i++ {
		if used[i] {
			continue
		}
		same := true
		for _, column := range field.Fields {
			if column.Ref == "enterprise.element" && !reflect.DeepEqual(fieldValue(lines.Index(i), column.Index).Interface(), fieldValue(current, column.Index).Interface()) {
				same = false
				break
			}
		}
		if same {
			used[i] = true
			return lines.Index(i)
		}
	}
	return reflect.Value{}
}

// find selects records of a type; visible, when set, is the caller's scope.
func (s *recordStore) find(et *entityType, q platform.Query, visible func(reflect.Value) bool) ([]reflect.Value, int, *kernel.Error) {
	var sorts []sortKey
	for _, name := range q.Sort {
		desc := strings.HasPrefix(name, "-")
		bare := strings.TrimPrefix(name, "-")
		f, ok := et.info.Field(bare)
		if !ok && bare != "id" && bare != "created" && bare != "changed" {
			return nil, 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		sorts = append(sorts, sortKey{field: f, stamp: map[bool]string{true: bare, false: ""}[!ok], desc: desc})
	}
	out, err := s.matchingQuery(et, q, visible)
	if err != nil {
		return nil, 0, err
	}
	keys := make([][]any, len(out)) // sort keys read once, then compared
	for i, v := range out {
		for _, k := range sorts {
			keys[i] = append(keys[i], k.of(v))
		}
		keys[i] = append(keys[i], recordOf(v).ID)
	}
	index := make([]int, len(out))
	for i := range index {
		index[i] = i
	}
	slices.SortFunc(index, func(a, b int) int {
		for j := range keys[a] {
			c := compareValues(keys[a][j], keys[b][j])
			if j < len(sorts) && sorts[j].desc {
				c = -c
			}
			if c != 0 {
				return c
			}
		}
		return 0
	})
	total := len(out)
	start := min(max(q.Offset, 0), total)
	end := total
	if q.Limit > 0 {
		end = min(start+q.Limit, total)
	}
	page := make([]reflect.Value, 0, end-start)
	for _, i := range index[start:end] {
		page = append(page, out[i])
	}
	return page, total, nil
}

func (s *recordStore) matchingQuery(et *entityType, q platform.Query, visible func(reflect.Value) bool) ([]reflect.Value, *kernel.Error) {
	if err := q.CheckSet(); err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	match, err := compileRecordSet(et.info, platform.RecordSetPredicate{Domain: q.Domain, Search: q.Search, Set: q.Set})
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	effective, refusal := s.effectivePredicate(et, q)
	if refusal != nil {
		return nil, refusal
	}
	var out []reflect.Value
	visit := func(r *row) {
		if r == nil {
			return
		}
		v := r.value
		if !q.Archived && recordOf(v).Archived || visible != nil && !visible(v) || effective != nil && !effective(v) || !match(v) {
			return
		}
		out = append(out, v)
	}
	if s.historyStore != nil {
		filters := sqlRecordFilters(et.info, q.Domain)
		seen := map[string]bool{}
		after := ""
		for {
			ids, readErr := s.historyStore.StateCandidates(context.Background(), s.tenant, et.info.Type, filters, after, 256, s.appliedPosition)
			if readErr != nil {
				panic(fmt.Errorf("record candidates: %w", readErr))
			}
			for _, id := range ids {
				seen[id] = true
				visit(et.rows.get(id))
			}
			if len(ids) < 256 {
				break
			}
			after = ids[len(ids)-1]
		}
		// Private writes may newly match a condition their committed predecessor
		// did not. Include this decision's overlay before applying the same rules.
		for overlay := s; overlay != nil; overlay = overlay.parent {
			for ref := range overlay.writes {
				typ, id, ok := strings.Cut(ref, "/")
				if ok && typ == et.info.Type && !seen[id] {
					seen[id] = true
					visit(et.rows.get(id))
				}
			}
		}
	} else if ids, indexed := s.indexedRecordCandidates(et, q); indexed {
		for _, id := range ids {
			visit(et.rows.get(id))
		}
	} else {
		for _, r := range et.rows.all() {
			visit(r)
		}
	}

	return out, nil
}

// One predicate evaluates all source sets against one authorized row. Every
// branch is compiled using the member's field projection before any row runs.
func compileRecordSet(info platform.EntityInfo, p platform.RecordSetPredicate) (func(reflect.Value) bool, error) {
	domain, err := compileDomain(info, p.Domain)
	if err != nil {
		return nil, err
	}
	search := strings.ToLower(strings.TrimSpace(p.Search))
	var searched []platform.FieldInfo
	for _, f := range info.Fields {
		if f.Search {
			searched = append(searched, f)
		}
	}
	base := func(v reflect.Value) bool {
		return domain(v) && (search == "" || strings.Contains(strings.ToLower(recordOf(v).ID), search) || slices.ContainsFunc(searched, func(f platform.FieldInfo) bool {
			return strings.Contains(strings.ToLower(searchable(f, v.FieldByIndex(f.Index).Interface())), search)
		}))
	}
	if p.Set == nil {
		return base, nil
	}
	// CheckSet bounds the shape before this compiler is called.
	left, err := compileRecordSet(info, *p.Set.Inputs[0])
	if err != nil {
		return nil, err
	}
	right, err := compileRecordSet(info, *p.Set.Inputs[1])
	if err != nil {
		return nil, err
	}
	switch p.Set.Op {
	case "union":
		return func(v reflect.Value) bool { return base(v) && (left(v) || right(v)) }, nil
	case "intersect":
		return func(v reflect.Value) bool { return base(v) && left(v) && right(v) }, nil
	case "subtract":
		return func(v reflect.Value) bool { return base(v) && left(v) && !right(v) }, nil
	}
	return nil, fmt.Errorf("unsupported record set operation")
}

type sortKey struct {
	field platform.FieldInfo
	stamp string // id, created, changed: the record's own
	desc  bool
}

func (k sortKey) of(v reflect.Value) any {
	switch k.stamp {
	case "id":
		return recordOf(v).ID
	case "created":
		return recordOf(v).Created.At
	case "changed":
		return recordOf(v).Changed.At
	}
	return comparable(k.field, v.FieldByIndex(k.field.Index).Interface())
}

// comparable is a field value as it is compared and sorted: strings, numbers,
// times; money by its amount.
func comparable(f platform.FieldInfo, v any) any {
	if v != nil {
		rv := reflect.ValueOf(v)
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil
			}
			rv = rv.Elem()
		}
		v = rv.Interface()
	}
	switch x := v.(type) {
	case platform.Money:
		return float64(x.Amount)
	case time.Time:
		return x
	case bool:
		if x {
			return 1.0
		}
		return 0.0
	}
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.String:
		return rv.String()
	}
	return fmt.Sprint(v)
}

func searchable(f platform.FieldInfo, v any) string {
	x := comparable(f, v)
	if x == nil {
		return ""
	}
	return fmt.Sprint(x)
}

func compareValues(a, b any) int {
	if a == nil {
		if b == nil {
			return 0
		}
		return -1
	}
	if b == nil {
		return 1
	}
	switch x := a.(type) {
	case float64:
		y, _ := b.(float64)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	case time.Time:
		y, _ := b.(time.Time)
		return x.Compare(y)
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
}

// compileDomain turns a domain in Odoo's prefix form into a predicate (D7):
// [["field", "op", value], "|", [...], [...], "!", [...]]; terms not joined
// by an operator are joined by "&". Operators: = != < <= > >= in "not in" like.
func compileDomain(info platform.EntityInfo, raw json.RawMessage) (func(reflect.Value) bool, error) {
	all := func(reflect.Value) bool { return true }
	if len(raw) == 0 || string(raw) == "null" {
		return all, nil
	}
	var terms []json.RawMessage
	if err := json.Unmarshal(raw, &terms); err != nil {
		return nil, err
	}
	type pred = func(reflect.Value) bool
	var stack []pred
	for i := len(terms) - 1; i >= 0; i-- {
		var op string
		if json.Unmarshal(terms[i], &op) == nil {
			switch {
			case op == "!" && len(stack) >= 1:
				p := stack[len(stack)-1]
				stack[len(stack)-1] = func(v reflect.Value) bool { return !p(v) }
			case (op == "&" || op == "|") && len(stack) >= 2:
				a, b := stack[len(stack)-1], stack[len(stack)-2]
				stack = stack[:len(stack)-2]
				if op == "&" {
					stack = append(stack, func(v reflect.Value) bool { return a(v) && b(v) })
				} else {
					stack = append(stack, func(v reflect.Value) bool { return a(v) || b(v) })
				}
			default:
				return nil, fmt.Errorf("operator %q without its terms", op)
			}
			continue
		}
		p, err := condition(info, terms[i])
		if err != nil {
			return nil, err
		}
		stack = append(stack, p)
	}
	return func(v reflect.Value) bool {
		for _, p := range stack {
			if !p(v) {
				return false
			}
		}
		return true
	}, nil
}

func condition(info platform.EntityInfo, raw json.RawMessage) (func(reflect.Value) bool, error) {
	var term []json.RawMessage
	var name, op string
	if json.Unmarshal(raw, &term) != nil || len(term) != 3 || json.Unmarshal(term[0], &name) != nil || json.Unmarshal(term[1], &op) != nil {
		return nil, fmt.Errorf("a condition is [field, operator, value]")
	}
	f, ok := info.Field(name)
	read := func(v reflect.Value) any { return v.FieldByIndex(f.Index).Interface() }
	switch {
	case name == "id":
		f, read = platform.FieldInfo{Name: "id", Type: "text"}, func(v reflect.Value) any { return recordOf(v).ID }
	case name == "created" || name == "changed": // the record's stamps, as datetimes
		f, read = platform.FieldInfo{Name: name, Type: "datetime"}, func(v reflect.Value) any {
			if name == "created" {
				return recordOf(v).Created.At
			}
			return recordOf(v).Changed.At
		}
	case !ok:
		return nil, fmt.Errorf("unknown field %s", name)
	}
	if value, ok := platform.DecimalLiteral(term[2]); ok {
		return value.Condition(f, op, read)
	}
	var tagged map[string]json.RawMessage
	if json.Unmarshal(term[2], &tagged) == nil && tagged != nil {
		return nil, fmt.Errorf("invalid typed condition value")
	}
	var value any
	if json.Unmarshal(term[2], &value) != nil {
		return nil, fmt.Errorf("bad value")
	}
	if b, ok := value.(bool); ok { // compared as booleans are: 1 and 0
		value = map[bool]float64{true: 1, false: 0}[b]
	}
	if f.Type == "datetime" {
		if s, ok := value.(string); ok { // a time, or a day from its midnight (UTC)
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				if t, err = time.Parse(time.DateOnly, s); err != nil {
					return nil, err
				}
			}
			value = t
		}
	}
	list := f.Type == "references" || f.Type == "tags"
	items := func(v reflect.Value) []string {
		var out []string
		fv := v.FieldByIndex(f.Index)
		for i := range fv.Len() {
			out = append(out, fv.Index(i).String())
		}
		return out
	}
	values, _ := value.([]any)
	in := func(x any) bool {
		return slices.ContainsFunc(values, func(y any) bool { return compareValues(comparable(f, x), y) == 0 })
	}
	switch op {
	case "=", "!=":
		eq := func(v reflect.Value) bool {
			if list {
				return slices.Contains(items(v), fmt.Sprint(value))
			}
			return compareValues(comparable(f, read(v)), value) == 0
		}
		if op == "!=" {
			return func(v reflect.Value) bool { return !eq(v) }, nil
		}
		return eq, nil
	case "<", "<=", ">", ">=":
		textual := f.Type != "integer" && f.Type != "decimal" && f.Type != "money" && f.Type != "boolean"
		return func(v reflect.Value) bool {
			x := comparable(f, read(v))
			if x == nil || textual && reflect.ValueOf(x).IsZero() {
				return false // an empty value is neither before nor after anything
			}
			c := compareValues(x, value)
			return op == "<" && c < 0 || op == "<=" && c <= 0 || op == ">" && c > 0 || op == ">=" && c >= 0
		}, nil
	case "in", "not in":
		if values == nil {
			return nil, fmt.Errorf("%s needs a list", op)
		}
		has := func(v reflect.Value) bool {
			if list {
				return slices.ContainsFunc(items(v), func(x string) bool { return in(x) })
			}
			return in(read(v))
		}
		if op == "not in" {
			return func(v reflect.Value) bool { return !has(v) }, nil
		}
		return has, nil
	case "like":
		s := strings.ToLower(fmt.Sprint(value))
		return func(v reflect.Value) bool { return strings.Contains(strings.ToLower(searchable(f, read(v))), s) }, nil
	}
	return nil, fmt.Errorf("unknown operator %s", op)
}

// Runtime: apps put and read their own records.

func (r runtime) Put(c platform.Caller, rec *pb.ChangeRecord, entity any) *kernel.Error {
	if !c.Replaying {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A new record write requires a private runtime")
	}
	return r.t.records.put(c, rec, entity)
}

func (r runtime) Get(c platform.Caller, t reflect.Type, id string) (any, bool) {
	return r.t.records.get(c, t, id)
}

func (r runtime) Find(c platform.Caller, t reflect.Type, q platform.Query) ([]any, int, *kernel.Error) {
	s := r.t.records
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.of(c, t)
	if et == nil {
		return nil, 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if len(q.Sort) == 0 {
		q.Sort = []string{"id"}
	}
	q = businessQuery(et.info, q, c.Member, time.Now())
	page, total, err := s.find(et, q, nil)
	out := make([]any, len(page))
	for i, v := range page {
		out[i] = copyOf(t, v.Interface()).Interface()
	}
	return out, total, err
}

func (r runtime) Check(c platform.Caller, entity any) *kernel.Error {
	return r.t.records.check(c, entity, time.Now())
}

// Members' reads (the HTTP contract).

// Entities are the entity types m may read: those of apps m holds a role in.
func (t *Tenant) Entities(m platform.Member) []platform.EntityInfo {
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	out := []platform.EntityInfo{}
	for _, et := range t.records.types {
		roles := m.RolesIn(et.info.App)
		if len(roles) > 0 && et.info.Scope.LevelFor(roles) == platform.ScopeNone && et.info.Scope.Participants == nil {
			continue // a type none of these roles sees at all (ADR-0044 18b)
		}
		if len(roles) > 0 || et.info.Scope.Participants != nil || et.info.Scope.Through != nil { // participants, and what belongs to a record, are read without a role
			view, _ := viewOf(m, et)
			out = append(out, view.info)
		}
	}
	slices.SortFunc(out, func(a, b platform.EntityInfo) int { return strings.Compare(a.Type, b.Type) })
	return out
}

// readableLocked reports whether m may read the record ref ("<type>/<id>"),
// with the store's lock held.
func (t *Tenant) readableLocked(m platform.Member, ref string, now time.Time) bool {
	return t.readableIn(t.records, m, ref, now)
}

func (t *Tenant) readableIn(store *recordStore, m platform.Member, ref string, now time.Time) bool {
	typ, id, _ := strings.Cut(ref, "/")
	if typ == "enterprise.element" && t.directory != nil { // the model is every member's to read (ADR-0022 D3)
		_, ok := t.directory.Element(id, now.UTC().Format(time.DateOnly))
		return ok
	}
	et := store.types[typ]
	if et == nil {
		return false
	}
	r := et.rows.get(id)
	if r == nil {
		return false
	}
	visible, err := t.visibleIn(store, m, et, now)
	return err == nil && (visible == nil || visible(r.value))
}

// Readable reports whether m may read the record ref ("<type>/<id>").
func (t *Tenant) Readable(m platform.Member, ref string, now time.Time) bool {
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	return t.readableLocked(m, ref, now)
}

// visible is m's scope over a type's records on now's day (D4).
func (t *Tenant) visible(m platform.Member, et *entityType, now time.Time) (func(reflect.Value) bool, *kernel.Error) {
	return t.visibleIn(t.records, m, et, now)
}

func (t *Tenant) scopeVisibility(store *recordStore, m platform.Member, et *entityType, now time.Time) (func(reflect.Value) bool, *kernel.Error) {
	roles, scope := m.RolesIn(et.info.App), et.info.Scope
	participant := func(v reflect.Value) bool {
		return scope.Participants != nil && slices.Contains(scope.Participants(v.Interface()), m.ID)
	}
	if len(roles) == 0 {
		if scope.Participants == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return participant, nil
	}
	base, err := t.scoped(m, et, roles, now)
	if base == nil || err != nil || scope.Participants == nil {
		return base, err
	}
	return func(v reflect.Value) bool { return base(v) || participant(v) }, nil
}

// scoped is the widest scope m's roles give over a type's records (nil: all of them).
func (t *Tenant) scoped(m platform.Member, et *entityType, roles []string, now time.Time) (func(reflect.Value) bool, *kernel.Error) {
	// Each role contributes its own predicate; a wider level in one unit must
	// not discard a narrower role's independent unit.
	if len(roles) > 1 {
		var predicates []func(reflect.Value) bool
		for _, role := range roles {
			predicate, err := t.scoped(m, et, []string{role}, now)
			if err != nil {
				return nil, err
			}
			if predicate == nil {
				return nil, nil
			}
			predicates = append(predicates, predicate)
		}
		return func(v reflect.Value) bool {
			return slices.ContainsFunc(predicates, func(p func(reflect.Value) bool) bool { return p(v) })
		}, nil
	}
	scope := et.info.Scope
	level := scope.LevelFor(roles)
	field := func(name string) func(reflect.Value) string {
		f, _ := et.info.Field(name)
		return func(v reflect.Value) string { return v.FieldByIndex(f.Index).String() }
	}
	switch level {
	case platform.ScopeNone:
		return func(reflect.Value) bool { return false }, nil
	case platform.ScopeOwn:
		if scope.Owner == platform.OwnerCreated { // whoever's decision created it (ADR-0044)
			return func(v reflect.Value) bool { return recordOf(v).Created.By == m.ID }, nil
		}
		owner := field(scope.Owner)
		return func(v reflect.Value) bool { return owner(v) == m.ID }, nil
	case platform.ScopeUnit, platform.ScopeBelow:
		var units []string
		if t.directory != nil {
			structure := scope.Structure
			if level == platform.ScopeUnit {
				structure = ""
			}
			// Combine membership-based grants and explicit unit grants of this role.
			held, unbound := false, false
			for _, g := range m.Grants {
				if g.App != et.info.App || !slices.Contains(roles, g.Role) {
					continue
				}
				held = true
				if g.Unit == "" {
					unbound = true
					continue
				}
				if level == platform.ScopeBelow && g.Structure != "" && g.Structure != scope.Structure {
					continue
				}
				units = append(units, t.directory.Below([]string{g.Unit}, structure, m.Today(now))...)
			}
			if !held || unbound {
				units = append(units, t.directory.Units("member:"+m.ID, structure, m.Today(now))...)
			}
		}
		unit := field(scope.Unit)
		return func(v reflect.Value) bool { return slices.Contains(units, unit(v)) }, nil
	}
	return nil, nil
}

// RecordPage is a page of records and how many match in all.
type RecordPage struct {
	Records []any `json:"records"`
	Total   int   `json:"total"`
	Partial bool  `json:"partial,omitempty"`
}

// Records serves a member's query over a type, within the member's scope.
func (t *Tenant) Records(m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.recordsFrom(t.records, m, typ, q, now)
}

// recordsFrom is the same member projection over a staged or installed store.
// Flow queries must see prior accepted-decision writes without acquiring a
// second live runtime or widening the member's record/field scope.
func (t *Tenant) recordsFrom(s *recordStore, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	if q.Limit < 1 || q.Limit > 500 {
		q.Limit = 500
	}
	return t.recordSelectionFrom(s, m, typ, q, now)
}

// Internal aggregations select an owner's authorized inventory once. They
// reuse the same masks and source checks without repeatedly sorting each page.
// Public record reads still enter through the bounded recordsFrom above.
func (t *Tenant) recordSelectionFrom(s *recordStore, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	m = t.authorizationMember(m)
	if q.Traversal != nil {
		if s != t.records {
			return RecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Link traversal is unavailable in a staged read")
		}
		domain, err := t.linkTraversalDomain(m, typ, *q.Traversal, q.Domain, now)
		if err != nil {
			return RecordPage{}, err
		}
		q.Domain = domain
		q.Traversal = nil
	}
	if err := t.admits(m); err != nil {
		return RecordPage{}, err
	}
	s.mu.Lock()
	et := s.types[typ]
	s.mu.Unlock()
	if et == nil {
		return RecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	visible, err := t.visibleIn(s, m, et, now)
	if err != nil {
		return RecordPage{}, err
	}
	q = businessQuery(et.info, q, m, now)
	view, hidden := viewOf(m, et)
	s.mu.Lock()
	defer s.mu.Unlock()
	page, total, err := s.find(view, q, visible)
	if err != nil {
		return RecordPage{}, err
	}
	out := RecordPage{Records: make([]any, len(page)), Total: total}
	// The owner exposes a scope marker, never the hidden count or identities.
	probe := q
	probe.Offset, probe.Limit = 0, 1
	if _, unscoped, probeErr := s.find(et, probe, nil); probeErr == nil {
		out.Partial = unscoped > total
	}
	var ids []string
	w := t.narrowerIn(s, m, now, true)
	for i, v := range page {
		out.Records[i] = masked(et, v, hidden)
		if et.info.HasReadSources() || len(et.info.VersionPins) > 0 { // checked against its sources again (ADR-0009)
			narrowed, _ := w.derive(et, reflect.ValueOf(out.Records[i]))
			out.Records[i] = narrowed.Interface()
		}
		ids = append(ids, recordOf(v).ID)
	}
	t.readPersonal(m, view, ids, now)
	return out, nil
}

// EnterpriseReference is a record that names an enterprise element through a
// declared reference, at whatever depth the reference sits: a top-level field
// or a line of a line (ADR-0031 D4). The path is the record's own, so a reader
// can go and look.
type EnterpriseReference struct {
	Type       string `json:"type"`
	Field      string `json:"field"` // dotted path: "plant", "lines.machine"
	FieldTitle string `json:"fieldTitle"`
	Stereotype string `json:"stereotype,omitempty"` // the element stereotype the field declares, when it names one
	ID         string `json:"id"`
	Name       string `json:"name"` // the record's label, so a list is readable without a second call
}

// EnterpriseReferenceGroup is the references of one entity type, one column of
// them: how many were found and one page.
type EnterpriseReferenceGroup struct {
	Type       string                `json:"type"`
	Title      string                `json:"title"` // the type's plural
	Field      string                `json:"field"`
	FieldTitle string                `json:"fieldTitle"`
	Stereotype string                `json:"stereotype,omitempty"`
	Total      int                   `json:"total"`
	Records    []EnterpriseReference `json:"records"`
}

// EnterpriseReferences are the records that name an enterprise element, across
// every entity type the member may read, at any nesting depth, paged by column.
// The modeler's "Used by" asks this instead of querying one field at a time
// (ADR-0031 D4): a reference inside a line is found, a page says how many it
// left out, and a type the member may not read is not mentioned.
func (t *Tenant) EnterpriseReferences(m platform.Member, element string, offset, limit int, now time.Time) ([]EnterpriseReferenceGroup, *kernel.Error) {
	if element == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A reference read names the element")
	}
	if err := t.admits(m); err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	s := t.records
	s.mu.Lock()
	types := make([]*entityType, 0, len(s.types))
	for _, et := range s.types {
		types = append(types, et)
	}
	s.mu.Unlock()
	slices.SortFunc(types, func(a, b *entityType) int { return strings.Compare(a.info.Type, b.info.Type) })
	groups := []EnterpriseReferenceGroup{}
	for _, et := range types {
		if !declaresEnterpriseRef(et.info.Fields) {
			continue
		}
		visible, err := t.visibleIn(s, m, et, now)
		if err != nil {
			continue // the member may not read this type
		}
		_, hidden := viewOf(m, et)
		s.mu.Lock()
		rows := make([]reflect.Value, 0, et.rows.len())
		for _, r := range et.rows.all() {
			if visible == nil || visible(r.value) { // nil: the member's roles read the whole type
				rows = append(rows, r.value)
			}
		}
		s.mu.Unlock()
		slices.SortFunc(rows, func(a, b reflect.Value) int { return strings.Compare(recordOf(a).ID, recordOf(b).ID) })
		byPath := map[string]*EnterpriseReferenceGroup{}
		var order []string
		for _, r := range rows {
			id := recordOf(r).ID
			name := ""
			readable := masked(et, r, hidden)
			seen := map[string]bool{}
			for _, f := range matchingRefs(et.info.Fields, "", reflect.ValueOf(readable), element, m.RolesIn(et.info.App)) {
				if seen[f.Path] {
					continue
				}
				seen[f.Path] = true
				g, ok := byPath[f.Path]
				if !ok {
					g = &EnterpriseReferenceGroup{Type: et.info.Type, Title: et.info.Plural, Field: f.Path, FieldTitle: f.Title, Stereotype: f.Stereotype, Records: []EnterpriseReference{}}
					byPath[f.Path] = g
					order = append(order, f.Path)
				}
				g.Total++
				if g.Total > offset && len(g.Records) < limit {
					if name == "" {
						name = recordLabel(et, readable, id)
					}
					g.Records = append(g.Records, EnterpriseReference{Type: et.info.Type, Field: f.Path, FieldTitle: f.Title, Stereotype: f.Stereotype, ID: id, Name: name})
				}
			}
		}
		for _, path := range order {
			groups = append(groups, *byPath[path])
		}
	}
	return groups, nil
}

// refPath is one declared enterprise reference and where it sits.
type refPath struct {
	Path       string
	Title      string
	Stereotype string
}

// declaresEnterpriseRef reports whether a type names enterprise elements, at
// any depth.
func declaresEnterpriseRef(fields []platform.FieldInfo) bool {
	for _, f := range fields {
		if len(f.Fields) > 0 && declaresEnterpriseRef(f.Fields) {
			return true
		}
		if f.Ref == "enterprise.element" {
			return true
		}
	}
	return false
}

// matchingRefs are the declared references through which the record names the
// element; a "lines" field is descended into, and its path is dotted.
func matchingRefs(fields []platform.FieldInfo, prefix string, v reflect.Value, element string, roles []string) []refPath {
	return matchingRefsWithAccess(fields, prefix, v, element, roles, false)
}

func matchingRefsWithAccess(fields []platform.FieldInfo, prefix string, v reflect.Value, element string, roles []string, all bool) []refPath {
	var out []refPath
	for _, f := range fields {
		if !all && !f.ReadsAny(roles) {
			continue
		}
		path := f.Name
		if prefix != "" {
			path = prefix + "." + f.Name
		}
		fv := fieldValue(v, f.Index)
		if len(f.Fields) > 0 {
			if !fv.IsValid() || fv.Kind() != reflect.Slice && fv.Kind() != reflect.Array {
				continue
			}
			for i := 0; i < fv.Len(); i++ {
				out = append(out, matchingRefsWithAccess(f.Fields, path, fv.Index(i), element, roles, all)...)
			}
			continue
		}
		if f.Ref != "enterprise.element" {
			continue
		}
		if s, ok := referenceValue(fv); ok && s == element {
			out = append(out, refPath{Path: path, Title: f.Title, Stereotype: f.Stereotype})
		}
	}
	return out
}

// fieldValue reads a field, dereferencing the pointers on the way.
func fieldValue(v reflect.Value, index []int) reflect.Value {
	for _, at := range index {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct || at >= v.NumField() {
			return reflect.Value{}
		}
		v = v.Field(at)
	}
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

// referenceValue is a reference field's id: a plain string, or a reference
// type that names its target.
func referenceValue(v reflect.Value) (string, bool) {
	if !v.IsValid() {
		return "", false
	}
	if v.Kind() == reflect.String {
		return v.String(), true
	}
	if r, ok := v.Interface().(interface{ String() string }); ok {
		return r.String(), true
	}
	return "", false
}

// recordLabel is the words a person recognises a record by: its first
// searchable text, else its id.
func recordLabel(et *entityType, value any, id string) string {
	v := reflect.ValueOf(value)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return id
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return id
	}
	for _, f := range et.info.Fields {
		if !f.Search {
			continue
		}
		fv := fieldValue(v, f.Index)
		if fv.IsValid() && fv.Kind() == reflect.String && fv.String() != "" {
			return fv.String()
		}
	}
	return id
}

// RecordView is one record with its history, newest first, the records of
// the app's other types that refer to it, and the processes about it the
// member may read (ADR-0006 D4).
type RecordView struct {
	Effective   *bool             `json:"effective,omitempty"`
	Values      map[string]any    `json:"values,omitempty"`
	ValueErrors map[string]string `json:"valueErrors,omitempty"`
	Record      any               `json:"record"`
	History     []RecordChange    `json:"history"`
	Related     []Related         `json:"related"`
	Processes   []any             `json:"processes"`
	Approvals   []any             `json:"approvals"` // requests to move it, newest first (F-38)
	Tasks       []any             `json:"tasks"`     // open tasks about it the member may take, to answer from its page
	Files       []any             `json:"files"`     // attached to it (ADR-0038)
	Comments    []any             `json:"comments"`  // on it, oldest first (ADR-0038)
	Following   bool              `json:"following"` // the member follows it
	Linked      []Related         `json:"linked"`    // records of any app linked to it (the relations app), by type, that the member may read
	Activity    []any             `json:"activity"`  // what apps told about it through protocols (platform.RelationNote), oldest first
}

type Related struct {
	Type  string `json:"type"`
	Field string `json:"field"`
	// Relation is the reference field's declared inverse name, when it has one (ADR-0047 21b).
	Relation string `json:"relation,omitempty"`
	Title    string `json:"title"`
	Records  []any  `json:"records"`
	Total    int    `json:"total"`
}

func (t *Tenant) RecordOf(m platform.Member, typ, id string, now time.Time) (RecordView, *kernel.Error) {
	m = t.authorizationMember(m)
	notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	if err := t.admits(m); err != nil {
		return RecordView{}, err
	}
	s := t.records
	s.mu.Lock()
	et := s.types[typ]
	s.mu.Unlock()
	if et == nil {
		return RecordView{}, notFound
	}
	visible, err := t.visible(m, et, now)
	if err != nil {
		return RecordView{}, err
	}
	s.mu.Lock()
	r := et.rows.get(id)
	readable := r != nil && (visible == nil || visible(r.value))
	s.mu.Unlock()
	if !readable {
		return RecordView{}, notFound
	}
	seen, hidden := viewOf(m, et)
	record := masked(et, r.value, hidden)
	if et.info.HasReadSources() || len(et.info.VersionPins) > 0 {
		narrowed, _ := t.narrower(m, now, false).derive(et, reflect.ValueOf(record))
		record = narrowed.Interface()
	}
	values, valueErrors := propertyValues(seen.info, record)
	view := RecordView{Values: values, ValueErrors: valueErrors, Record: record, History: []RecordChange{}, Related: []Related{}, Processes: []any{}, Approvals: []any{}, Tasks: []any{}, Files: []any{}, Comments: []any{}, Linked: []Related{}, Activity: []any{}}
	if versioning := et.info.Versioning; versioning != nil {
		field, _ := et.info.Field(versioning.ValidFrom)
		clock := businessQuery(et.info, platform.Query{}, m, now).BusinessTime.At
		active, _ := platform.EffectiveAt(field.Type, businessField(et.info, r.value, versioning.ValidFrom), businessField(et.info, r.value, versioning.ValidUntil), clock)
		view.Effective = &active
	}
	if c := t.app(platform.RelationsApp); c != nil && typ != platform.CommentType && typ != platform.FollowType {
		about, _ := json.Marshal([]any{[]any{"target", "=", typ + "/" + id}})
		if page, err := t.Records(m, platform.CommentType, platform.Query{Domain: about, Sort: []string{"created"}, Limit: 200}, now); err == nil {
			view.Comments = t.narrowed(m, page.Records, now, false).([]any)
		}
		if f, ok := platform.Get[platform.RecordFollow](t.automation(platform.RelationsApp, false), platform.FollowID(m.ID, typ+"/"+id)); ok && !f.Archived {
			view.Following = true
		}
		view.Linked, view.Activity = t.linked(m, c, typ+"/"+id, now)
	}
	t.readPersonal(m, seen, []string{id}, now)
	if typ != platform.FileType && t.app(platform.FileApp) != nil {
		domain, _ := json.Marshal([]any{[]any{"target", "=", typ + "/" + id}})
		if page, err := t.Records(m, platform.FileType, platform.Query{Domain: domain, Sort: []string{"id"}, Limit: 100}, now); err == nil {
			view.Files = page.Records
		}
	}
	if t.procs != nil {
		domain, _ := json.Marshal([]any{[]any{"subject", "=", typ + "/" + id}})
		if page, err := t.Records(m, platform.FlowInstanceType, platform.Query{Domain: domain, Sort: []string{"-id"}, Limit: 20, Archived: true}, now); err == nil {
			view.Processes = page.Records
		}
	}
	if typ != platform.WorkTaskType && t.app(platform.WorkApp) != nil {
		domain, _ := json.Marshal([]any{[]any{"ref", "=", typ + "/" + id}, []any{"state", "=", "open"}})
		if page, err := t.Records(m, platform.WorkTaskType, platform.Query{Domain: domain, Sort: []string{"due"}, Limit: 20}, now); err == nil {
			view.Tasks = page.Records
		}
	}
	if typ != platform.WorkApprovalType && t.app(platform.WorkApp) != nil {
		domain, _ := json.Marshal([]any{[]any{"target", "=", typ + "/" + id}})
		if page, err := t.Records(m, platform.WorkApprovalType, platform.Query{Domain: domain, Sort: []string{"-created"}, Limit: 20}, now); err == nil {
			view.Approvals = page.Records
		}
	}
	history := maskedHistory(copyHistory(r.recordHistory()), hidden)
	for i := len(history) - 1; i >= 0; i-- {
		view.History = append(view.History, history[i])
	}
	s.mu.Lock()
	var referring []*entityType
	for _, other := range s.types {
		// The same app's referring types, and any type whose reference names an
		// inverse relation: those are declared to be seen from here (21b). The
		// member's own read of each type still decides what is listed.
		named := slices.ContainsFunc(other.info.Fields, func(f platform.FieldInfo) bool { return f.Ref == typ && f.Inverse != "" })
		if other.info.App == et.info.App || named {
			referring = append(referring, other)
		}
	}
	s.mu.Unlock()
	slices.SortFunc(referring, func(a, b *entityType) int { return strings.Compare(a.info.Type, b.info.Type) })
	for _, other := range referring {
		for _, f := range other.info.Fields {
			if f.Ref != typ {
				continue
			}
			domain, _ := json.Marshal([]any{[]any{f.Name, "=", id}})
			page, err := t.Records(m, other.info.Type, platform.Query{Domain: domain, Sort: []string{"-id"}, Limit: 20}, now)
			if err != nil {
				continue // the member may not read that type
			}
			if other.info.App != et.info.App && f.Inverse == "" {
				continue // another app's reference without a declared relation stays private to it
			}
			view.Related = append(view.Related, Related{Type: other.info.Type, Field: f.Name, Relation: f.Inverse,
				Title: other.info.Plural, Records: page.Records, Total: page.Total})
		}
	}
	return view, nil
}

// linked are the records linked to entity that m may read, grouped by type in
// the order they were linked, and the protocol events told about it: what a
// record page shows of other apps without the app composing it (#129). A link
// to what is not a record of this tenant is left out.
func (t *Tenant) linked(m platform.Member, rel platform.App, entity string, now time.Time) ([]Related, []any) {
	c := t.caller(m, rel, false)
	out, activity := []Related{}, []any{}
	for _, e := range c.Links(entity) {
		typ, id, _ := strings.Cut(e, "/")
		domain, _ := json.Marshal([]any{[]any{"id", "=", id}})
		page, err := t.Records(m, typ, platform.Query{Domain: domain, Limit: 1, Archived: true}, now)
		if err != nil || len(page.Records) == 0 {
			continue
		}
		i := slices.IndexFunc(out, func(r Related) bool { return r.Type == typ })
		if i < 0 {
			t.records.mu.Lock()
			plural := t.records.types[typ].info.Plural
			t.records.mu.Unlock()
			out = append(out, Related{Type: typ, Field: "link", Title: plural, Records: []any{}})
			i = len(out) - 1
		}
		out[i].Records = append(out[i].Records, page.Records[0])
		out[i].Total++
	}
	if notes, err := readApp(rel, c, "timeline"); err == nil {
		for _, n := range notes.([]platform.RelationNote) {
			if n.Entity == entity {
				activity = append(activity, n)
			}
		}
	}
	return out, activity
}

// PersonalRead is one read of personal data (ADR-0038): who read which
// records' personal fields, and when. Kept outside the journal, like transcripts.
type PersonalRead struct {
	At     time.Time `json:"at"`
	Member string    `json:"member"`
	Type   string    `json:"type"`
	IDs    []string  `json:"ids"`
	Fields []string  `json:"fields"`
}

// readPersonal notes that m read the personal fields view shows of records ids.
func (t *Tenant) readPersonal(m platform.Member, view *entityType, ids []string, now time.Time) {
	var fields []string
	for _, f := range view.info.Fields {
		if f.Personal != "" {
			fields = append(fields, f.Name)
		}
	}
	if len(fields) == 0 || len(ids) == 0 || strings.HasPrefix(m.ID, "app:") {
		return
	}
	t.audit.readPersonal(PersonalRead{At: now, Member: m.ID, Type: view.info.Type, IDs: ids, Fields: fields})
}

// PersonalReads are the latest reads of personal data, newest first.
func (t *Tenant) PersonalReads() []PersonalRead { return t.audit.personalReads() }

// liveTouched is ephemeral query invalidation metadata, protected by mu.
func (s *recordStore) liveTouched(typ string) {
	if s.liveVersions == nil {
		s.liveVersions = map[string]uint64{}
	}
	s.liveVersions[typ]++
}
