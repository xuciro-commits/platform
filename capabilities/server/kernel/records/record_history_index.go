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
	"time"
)

type Row struct {
	Value         reflect.Value
	HistoryParent *Row
	HistoryHead   string
	Format        string
	History       []RecordChange
	LoadHistory   func(int) []RecordChange
	HistoryCount  int
	Original      json.RawMessage
	Projection    *Projection
	Extraction    *Extraction
}

type Extraction struct {
	Migration string `json:"migration"`
	Source    string `json:"source"`
	Step      int    `json:"step"`
}

type Projection struct {
	Format string          `json:"format,omitempty"`
	From   string          `json:"from"`
	Shape  string          `json:"shape"`
	Value  json.RawMessage `json:"value"`
	Prior  []Projection    `json:"prior,omitempty"`
}

func CloneProjection(p *Projection) *Projection {
	if p == nil {
		return nil
	}
	copy := &Projection{Format: p.Format, From: p.From, Shape: p.Shape, Value: slices.Clone(p.Value)}
	for _, prior := range p.Prior {
		copy.Prior = append(copy.Prior, *CloneProjection(&prior))
	}
	return copy
}

func ProjectionFor(p *Projection, release string) *Projection {
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

func RefreshProjection(p *Projection, value any) error {
	if p == nil {
		return nil
	}
	var header map[string]json.RawMessage
	if json.Unmarshal(rowJSON(value), &header) != nil {
		return fmt.Errorf("versioned record header is invalid")
	}
	refresh := func(image *Projection) error {
		var fields map[string]json.RawMessage
		if json.Unmarshal(image.Value, &fields) != nil {
			return fmt.Errorf("versioned source image is invalid")
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
		return err
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

func DecodeImage(info platform.EntityInfo, value json.RawMessage, projection *Projection, release string) (json.RawMessage, error) {
	if projection == nil {
		return value, nil
	}
	shape, err := platform.CanonicalDigest(info.Fields)
	if err != nil {
		return nil, err
	}
	var actual platform.Record
	if json.Unmarshal(value, &actual) != nil {
		return nil, fmt.Errorf("versioned record identity is invalid")
	}
	seen := map[string]bool{}
	images := append([]Projection{*projection}, projection.Prior...)
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
	if selected := ProjectionFor(projection, release); selected != nil && shape == selected.Shape {
		return selected.Value, nil
	}
	return value, nil
}

func (r *Row) Image() (json.RawMessage, error) {
	if len(r.Original) > 0 {
		return slices.Clone(r.Original), nil
	}
	return json.Marshal(r.Value.Interface())
}

func (r *Row) RetainOriginal(raw json.RawMessage) {
	decoded, err := json.Marshal(r.Value.Interface())
	before, beforeErr := platform.CanonicalDigest(raw)
	after, afterErr := platform.CanonicalDigest(json.RawMessage(decoded))
	if err == nil && beforeErr == nil && afterErr == nil && before != after {
		r.Original = slices.Clone(raw)
	}
}

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

type Image struct {
	Type        string          `json:"type"`
	Format      string          `json:"format,omitempty"`
	HistoryRef  *HistoryRef     `json:"historyRef,omitempty"`
	Predecessor string          `json:"predecessor,omitempty"`
	ID          string          `json:"id"`
	Value       json.RawMessage `json:"value"`
	History     []RecordChange  `json:"history"`
	Projection  *Projection     `json:"projection,omitempty"`
	Extraction  *Extraction     `json:"extraction,omitempty"`
}

func CopyHistory(history []RecordChange) []RecordChange {
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

func Header(v reflect.Value) *platform.Record {
	return v.Field(0).Addr().Interface().(*platform.Record)
}

func FullImage(typ, id string, r *Row) (Image, error) {
	value, err := r.Image()
	return Image{Type: typ, ID: id, Value: value, History: CopyHistory(r.RecordHistory()), Projection: CloneProjection(r.Projection), Extraction: r.Extraction}, err
}

func (r *Row) RecordHistory() []RecordChange { return r.HistoryAfter(0) }

func (r *Row) HistoryLength() int {
	if r.LoadHistory != nil || r.HistoryParent != nil || r.HistoryCount > 0 {
		return r.HistoryCount
	}
	return len(r.History)
}

func (r *Row) HistoryAfter(start int) []RecordChange {
	if start >= r.HistoryLength() {
		return nil
	}
	if start < 0 {
		start = 0
	}
	if r.LoadHistory != nil {
		return r.LoadHistory(start)
	}
	if r.HistoryParent == nil {
		return r.History[start:]
	}
	base := r.HistoryParent.HistoryLength()
	if start >= base {
		return r.History[start-base:]
	}
	out := make([]RecordChange, 0, r.HistoryLength()-start)
	var suffixes [][]RecordChange
	for r.HistoryParent != nil && start < r.HistoryParent.HistoryLength() {
		suffixes = append(suffixes, r.History)
		r = r.HistoryParent
	}
	if r.LoadHistory != nil {
		out = append(out, r.LoadHistory(start)...)
	} else if r.HistoryParent != nil {
		out = append(out, r.History[start-r.HistoryParent.HistoryLength():]...)
	} else {
		out = append(out, r.History[start:]...)
	}
	for i := len(suffixes) - 1; i >= 0; i-- {
		out = append(out, suffixes[i]...)
	}
	return out
}

type HistoryRef struct {
	Version int    `json:"version"`
	Base    int    `json:"base"`
	Prior   string `json:"prior"`
	Head    string `json:"head"`
}

func (r *Row) HistoryDigest() (string, error) {
	if r == nil {
		return platform.EmptyRecordHistory, nil
	}
	if r.HistoryHead != "" {
		return r.HistoryHead, nil
	}
	head := platform.EmptyRecordHistory
	for i, h := range r.RecordHistory() {
		var err error
		head, err = platform.AppendRecordHistory(head, i, rowJSON(h))
		if err != nil {
			return "", err
		}
	}
	return head, nil
}

func AppendRowHistory(r, prior *Row, changes []RecordChange) error {
	head, err := prior.HistoryDigest()
	if err != nil {
		return err
	}
	base := 0
	if prior != nil {
		base = prior.HistoryLength()
	}
	for i, h := range changes {
		head, err = platform.AppendRecordHistory(head, base+i, rowJSON(h))
		if err != nil {
			return err
		}
	}
	r.HistoryHead = head
	r.History = CopyHistory(changes)
	if prior != nil {
		prefix := *prior
		prefix.Value = reflect.Value{}
		prefix.Original = nil
		prefix.Projection = nil
		prefix.Extraction = nil
		r.HistoryParent = &prefix
	}
	r.HistoryCount = base + len(changes)
	return nil
}

func ChangeImage(typ, id string, r, prior *Row) (Image, error) {
	value, err := r.Image()
	if err != nil {
		return Image{}, err
	}
	base := 0
	if prior != nil {
		base = prior.HistoryLength()
	}
	changes := r.HistoryAfter(base)
	before, err := prior.HistoryDigest()
	if err != nil {
		return Image{}, err
	}
	head, err := r.HistoryDigest()
	if err != nil {
		return Image{}, err
	}
	out := Image{Type: typ, ID: id, Value: value, Format: r.Format, History: CopyHistory(changes), Projection: CloneProjection(r.Projection), Extraction: r.Extraction,
		HistoryRef: &HistoryRef{Version: 1, Base: base, Prior: before, Head: head}}
	return out, nil
}

func Predecessor(typ, id string, r *Row) (string, error) {
	if r == nil {
		return "", nil
	}
	value, err := r.Image()
	if err != nil {
		return "", err
	}
	head, err := r.HistoryDigest()
	if err != nil {
		return "", err
	}
	return platform.CanonicalDigest(struct {
		Version    int         `json:"version"`
		Type       string      `json:"type"`
		ID         string      `json:"id"`
		Value      any         `json:"value"`
		Format     string      `json:"format,omitempty"`
		Count      int         `json:"count"`
		Head       string      `json:"head"`
		Projection *Projection `json:"projection,omitempty"`
		Extraction *Extraction `json:"extraction,omitempty"`
	}{1, typ, id, value, r.Format, r.HistoryLength(), head, r.Projection, r.Extraction})
}

func ValidateHistoryRef(image Image) error {
	ref := image.HistoryRef
	if ref == nil || ref.Version != 1 || ref.Base < 0 || len(image.History) == 0 {
		return fmt.Errorf("record history reference is incomplete")
	}
	head := ref.Prior
	for i, h := range image.History {
		var err error
		head, err = platform.AppendRecordHistory(head, ref.Base+i, rowJSON(h))
		if err != nil {
			return err
		}
	}
	if head != ref.Head {
		return fmt.Errorf("record history chain differs")
	}
	return nil
}

func CheckHistoryPredecessor(image Image, prior *Row) error {
	if err := ValidateHistoryRef(image); err != nil {
		return err
	}
	head, err := prior.HistoryDigest()
	if err != nil {
		return err
	}
	count := 0
	if prior != nil {
		count = prior.HistoryLength()
	}
	if image.HistoryRef.Base != count || image.HistoryRef.Prior != head {
		return fmt.Errorf("record history predecessor differs")
	}
	return nil
}

func ImageAlreadyApplied(prior *Row, image Image) bool {
	if image.HistoryRef != nil {
		head, err := prior.HistoryDigest()
		return err == nil && prior.HistoryLength() == image.HistoryRef.Base+len(image.History) && head == image.HistoryRef.Head
	}
	a, err := platform.CanonicalDigest(prior.RecordHistory())
	b, err2 := platform.CanonicalDigest(image.History)
	return err == nil && err2 == nil && a == b
}

func DecodeFormattedImage(info platform.EntityInfo, value json.RawMessage, projection *Projection, release, format string) (json.RawMessage, error) {
	decoded, err := DecodeImage(info, value, projection, release)
	if err != nil {
		return nil, err
	}
	if selected := ProjectionFor(projection, release); selected != nil {
		shape, err := platform.CanonicalDigest(info.Fields)
		if err != nil {
			return nil, err
		}
		if shape == selected.Shape {
			format = selected.Format
		}
	}
	return platform.DecodeRecordImage(info, format, decoded)
}

func PruneImage(info platform.EntityInfo, raw json.RawMessage) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	kept := map[string]bool{"id": true, "revision": true, "created": true, "changed": true, "archived": true, "versionPins": true}
	for _, field := range info.Fields {
		kept[field.Name] = true
	}
	for name := range object {
		if !kept[name] {
			delete(object, name)
		}
	}
	return json.Marshal(object)
}

func rowJSON(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }

type Index[V any] struct{ root *indexNode[V] }

type Entry[V any] struct {
	Key   string
	Value V
}

func From[V any](entries []Entry[V]) (Index[V], error) {
	slices.SortFunc(entries, func(a, b Entry[V]) int { return strings.Compare(a.Key, b.Key) })
	for i, entry := range entries {
		if entry.Key == "" || i > 0 && entries[i-1].Key == entry.Key {
			return Index[V]{}, fmt.Errorf("record index needs unique nonempty keys")
		}
	}
	var build func(int, int) *indexNode[V]
	build = func(start, end int) *indexNode[V] {
		if start == end {
			return nil
		}
		middle := start + (end-start)/2
		entry := entries[middle]
		return recordBranch(entry.Key, entry.Value, build(start, middle), build(middle+1, end))
	}
	return Index[V]{root: build(0, len(entries))}, nil
}

type indexNode[V any] struct {
	key          string
	value        V
	left, right  *indexNode[V]
	height, size int
}

func (s Index[V]) Get(key string) V {
	if n := s.Find(key); n != nil {
		return n.value
	}
	var absent V
	return absent
}

func (s Index[V]) Has(key string) bool { return s.Find(key) != nil }

func (s Index[V]) Find(key string) *indexNode[V] {
	for n := s.root; n != nil; {
		switch {
		case key < n.key:
			n = n.left
		case key > n.key:
			n = n.right
		default:
			return n
		}
	}
	return nil
}

func (s Index[V]) Len() int { return recordSize(s.root) }

func recordSize[V any](n *indexNode[V]) int {
	if n == nil {
		return 0
	}
	return n.size
}

func recordHeight[V any](n *indexNode[V]) int {
	if n == nil {
		return 0
	}
	return n.height
}

func recordBranch[V any](key string, value V, left, right *indexNode[V]) *indexNode[V] {
	return &indexNode[V]{key: key, value: value, left: left, right: right,
		height: 1 + max(recordHeight(left), recordHeight(right)), size: 1 + recordSize(left) + recordSize(right)}
}

func recordRotateLeft[V any](n *indexNode[V]) *indexNode[V] {
	r := n.right
	return recordBranch(r.key, r.value, recordBranch(n.key, n.value, n.left, r.left), r.right)
}

func recordRotateRight[V any](n *indexNode[V]) *indexNode[V] {
	l := n.left
	return recordBranch(l.key, l.value, l.left, recordBranch(n.key, n.value, l.right, n.right))
}

func recordBalance[V any](n *indexNode[V]) *indexNode[V] {
	switch delta := recordHeight(n.left) - recordHeight(n.right); {
	case delta > 1:
		if recordHeight(n.left.left) < recordHeight(n.left.right) {
			n = recordBranch(n.key, n.value, recordRotateLeft(n.left), n.right)
		}
		return recordRotateRight(n)
	case delta < -1:
		if recordHeight(n.right.right) < recordHeight(n.right.left) {
			n = recordBranch(n.key, n.value, n.left, recordRotateRight(n.right))
		}
		return recordRotateLeft(n)
	}
	return n
}

func (s *Index[V]) Set(key string, value V) { s.root = recordSet(s.root, key, value) }

func recordSet[V any](n *indexNode[V], key string, value V) *indexNode[V] {
	if n == nil {
		return recordBranch(key, value, nil, nil)
	}
	switch {
	case key < n.key:
		return recordBalance(recordBranch(n.key, n.value, recordSet(n.left, key, value), n.right))
	case key > n.key:
		return recordBalance(recordBranch(n.key, n.value, n.left, recordSet(n.right, key, value)))
	default:
		return recordBranch(key, value, n.left, n.right)
	}
}

func (s *Index[V]) Remove(key string) { s.root = recordRemove(s.root, key) }

func recordRemove[V any](n *indexNode[V], key string) *indexNode[V] {
	if n == nil {
		return nil
	}
	switch {
	case key < n.key:
		return recordBalance(recordBranch(n.key, n.value, recordRemove(n.left, key), n.right))
	case key > n.key:
		return recordBalance(recordBranch(n.key, n.value, n.left, recordRemove(n.right, key)))
	case n.left == nil:
		return n.right
	case n.right == nil:
		return n.left
	default:
		next := n.right
		for next.left != nil {
			next = next.left
		}
		return recordBalance(recordBranch(next.key, next.value, n.left, recordRemove(n.right, next.key)))
	}
}

func (s Index[V]) All() iter.Seq2[string, V] {
	return func(yield func(string, V) bool) {
		var walk func(*indexNode[V]) bool
		walk = func(n *indexNode[V]) bool {
			return n == nil || walk(n.left) && yield(n.key, n.value) && walk(n.right)
		}
		walk(s.root)
	}
}

func (s Index[V]) after(key string) iter.Seq2[string, V] {
	return func(yield func(string, V) bool) {
		var walk func(*indexNode[V]) bool
		walk = func(n *indexNode[V]) bool {
			if n == nil {
				return true
			}
			if n.key <= key {
				return walk(n.right)
			}
			return walk(n.left) && yield(n.key, n.value) && walk(n.right)
		}
		walk(s.root)
	}
}

const changesKept = 10000

type RecentChanges struct {
	Entries Index[[]string]
	order   []string
}

func (r RecentChanges) Fork() RecentChanges {
	return RecentChanges{Entries: r.Entries, order: slices.Clone(r.order)}
}

func (r *RecentChanges) Remember(change, ref string) {
	if r.Entries.Get(change) == nil {
		r.order = append(r.order, change)
		if len(r.order) > changesKept {
			r.Entries.Remove(r.order[0])
			r.order = r.order[1:]
		}
	}
	if refs := r.Entries.Get(change); !slices.Contains(refs, ref) {
		r.Entries.Set(change, append(slices.Clone(refs), ref))
	}
}

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
func (i *DerivedIndexes) Reset()                              { i.references = nil; i.unique = nil }
func (i *DerivedIndexes) ResetUnique()                        { i.unique = nil }
func (i DerivedIndexes) Counts() (references, unique int)     { return len(i.references), len(i.unique) }
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
	filters := []journal.StateFilter{{Path: []string{"value", field.Name}, Values: []string{target}}}
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

func IndexedCandidates(image IndexImage, lookup ImageLookup, q platform.Query) ([]string, bool) {
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
		info, ok := image.Info.Field(field)
		if !ok || info.Type != "reference" || info.Ref == "" || info.Inverse == "" || target == "" {
			continue
		}
		source, ok := lookup(image.Info.Type)
		if !ok {
			continue
		}
		bucket := source.Indexes.ReferenceBucket(source, info, target)
		ids := make([]string, 0, bucket.IDs.Len())
		for id := range bucket.IDs.All() {
			ids = append(ids, id)
		}
		return ids, true
	}
	return nil, false
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
