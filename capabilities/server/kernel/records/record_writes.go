package records

import (
	"context"

	"fmt"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/journal"
	"platformserver/platform"

	"reflect"

	"strings"
	"time"
)

// declare registers an app's entity types; references resolve within the app.
func (s *Store) Declare(a platform.App) error {
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
		et := &Type{store: s, info: info, rows: Rows{}, knowledge: KnowledgeBearing(info)}
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
			if err := s.Check(seeder, v, time.Now()); err != nil {
				return fmt.Errorf("entity %s: a seed record does not hold: %v", e.Type, err)
			}
			if err := s.Put(seeder, seed, v); err != nil {
				return fmt.Errorf("entity %s: seed record: %v", e.Type, err)
			}
		}
	}
	return nil
}

// of is c's app's entity type for a Go type; other apps' types are not reachable (D3).
func (s *Store) Of(c platform.Caller, t reflect.Type) *Type {
	et := s.byGo[t]
	if et == nil || et.info.App != c.App {
		return nil
	}
	return et
}

func (s *Store) Put(c platform.Caller, r *pb.ChangeRecord, entity any) *kernel.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	et := s.Of(c, reflect.TypeOf(entity))
	if et == nil || r == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	v := Copy(et.info.Go, entity)
	platform.ApplyFieldAliases(et.info, v)
	EmptyLists(v)
	rec := Header(v)
	if rec.ID == "" {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := s.CheckLinkWriteLocked(et, v); err != nil {
		return err
	}
	prev := et.rows.Get(rec.ID)
	next, refusal := Write(et.info, r, prev, v)
	if refusal != nil {
		return refusal
	}
	et.SetRow(rec.ID, next)
	s.generation++
	s.LiveTouched(et.info.Type)
	if s.writes != nil {
		s.writes[et.info.Type+"/"+rec.ID] = true
	}
	s.recentChanges.Remember(c.App+"/"+r.GetChangeId(), et.info.Type+"/"+rec.ID)
	if et.knowledge {
		s.dirty.Set(et.info.Type+"/"+rec.ID, true)
	}
	if s.touched != nil {
		s.touched(et.info.Type, rec.ID)
	}
	return nil
}

func (s *Store) Get(c platform.Caller, t reflect.Type, id string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.Of(c, t)
	if et == nil {
		return nil, false
	}
	row := et.rows.Get(id)
	if row == nil {
		return nil, false
	}
	return Copy(t, row.Value.Interface()).Interface(), true
}

func (s *Store) Check(c platform.Caller, entity any, now time.Time) *kernel.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.Of(c, reflect.TypeOf(entity))
	if et == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	v := Copy(et.info.Go, entity)
	platform.ApplyFieldAliases(et.info, v)
	previous := reflect.Value{}
	if old := et.rows.Get(Header(v).ID); old != nil {
		previous = old.Value
	}
	if err := CheckEnterpriseReferences(c.Enterprise(), c.Replaying, et.info.Fields, v, previous, now); err != nil {
		return err
	}
	issues, refusal := CheckFields(et.info, entity, v, c.Replaying, s)
	if len(issues) > 0 {
		return c.RefuseFields(issues)
	}
	if refusal != nil {
		return refusal
	}
	return s.CheckLinkWriteLocked(et, v)
}

// HasRecord is called only under the owning store's lock during field checks.
func (s *Store) HasRecord(typ, id string) bool {
	et := s.types[typ]
	return et != nil && et.rows.Get(id) != nil
}

const linkArchiveFailure = "Active reference archive protection failed"

const LinkCardinalityFailure = "Reference cardinality constraint failed"

func (s *Store) ValidateUniqueLinkLocked(l platform.LinkType) error {
	et := s.types[l.Child.Name]
	if et == nil {
		return fmt.Errorf("link child is unavailable")
	}
	f, ok := et.info.Field(l.Via)
	if !ok || f.Type != "reference" || f.Ref != l.Parent.Name {
		return fmt.Errorf("link reference is unavailable")
	}
	for _, bucket := range et.ReferenceIndex(f).All() {
		if bucket.IDs.Len() > 1 {
			return fmt.Errorf("%s", LinkCardinalityFailure)
		}
	}
	return nil
}

// The parent admitted its complete image and generation must still match at
// promotion. Ordinary writes therefore need only their final relation keys;
// new declarations and replaced type images still receive full admission.
func (s *Store) ValidateChangedLinkConstraintsLocked() error {
	changed := map[string][]string{}
	for ref := range s.writes {
		typ, id, _ := strings.Cut(ref, "/")
		changed[typ] = append(changed[typ], id)
	}
	full := func(typ string) bool {
		et := s.types[typ]
		return s.parent == nil || et == nil || et.fullImage || s.parent.types[typ] == nil
	}
	for key, link := range s.uniqueLinks {
		if full(link.Child.Name) || !reflect.DeepEqual(s.parent.uniqueLinks[key], link) {
			if err := s.ValidateUniqueLinkLocked(link); err != nil {
				return err
			}
			continue
		}
		et := s.types[link.Child.Name]
		field, ok := et.info.Field(link.Via)
		if !ok || field.Type != "reference" || field.Ref != link.Parent.Name {
			return fmt.Errorf("link reference is unavailable")
		}
		for _, id := range changed[link.Child.Name] {
			if value := et.rows.Get(id); value != nil {
				target := value.Value.FieldByIndex(field.Index).String()
				if target != "" && et.ReferenceBucket(field, target).IDs.Len() > 1 {
					return fmt.Errorf("%s", LinkCardinalityFailure)
				}
			}
		}
	}
	for key, link := range s.archiveLinks {
		if full(link.Child.Name) || full(link.Parent.Name) || !reflect.DeepEqual(s.parent.archiveLinks[key], link) {
			if err := s.ValidateArchiveLinkLocked(link); err != nil {
				return err
			}
			continue
		}
		child, parent := s.types[link.Child.Name], s.types[link.Parent.Name]
		field, ok := child.info.Field(link.Via)
		if !ok || field.Type != "reference" || field.Ref != link.Parent.Name {
			return fmt.Errorf("link reference is unavailable")
		}
		for _, id := range changed[link.Child.Name] {
			row := child.rows.Get(id)
			if row == nil || row.Value.Field(0).Interface().(platform.Record).Archived {
				continue
			}
			target := row.Value.FieldByIndex(field.Index).String()
			if target != "" {
				value := parent.rows.Get(target)
				if value == nil || value.Value.Field(0).Interface().(platform.Record).Archived {
					return fmt.Errorf("%s", linkArchiveFailure)
				}
			}
		}
		for _, id := range changed[link.Parent.Name] {
			row := parent.rows.Get(id)
			if (row == nil || row.Value.Field(0).Interface().(platform.Record).Archived) && child.ReferenceBucket(field, id).Active > 0 {
				return fmt.Errorf("%s", linkArchiveFailure)
			}
		}
	}
	return nil
}

func (s *Store) ValidateLinkConstraintsLocked() error {
	for _, l := range s.uniqueLinks {
		if err := s.ValidateUniqueLinkLocked(l); err != nil {
			return err
		}
	}
	for _, l := range s.archiveLinks {
		if err := s.ValidateArchiveLinkLocked(l); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ValidateLinkConstraints() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ValidateLinkConstraintsLocked()
}

func (s *Store) ValidateChangedLinkConstraints() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ValidateChangedLinkConstraintsLocked()
}

func (s *Store) InstallLinkConstraint(l platform.LinkType) error {
	if l.Storage == "edge" {
		return nil
	}
	if l.Cardinality != "one-to-one" && l.DeletePolicy != "restrict-active" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.Cardinality == "one-to-one" {
		if err := s.ValidateUniqueLinkLocked(l); err != nil {
			return err
		}
	}
	if l.DeletePolicy == "restrict-active" {
		if err := s.ValidateArchiveLinkLocked(l); err != nil {
			return err
		}
	}
	key := l.Child.Name + "/" + l.Via
	if l.Cardinality == "one-to-one" {
		if s.uniqueLinks == nil {
			s.uniqueLinks = map[string]platform.LinkType{}
		}
		if _, ok := s.uniqueLinks[key]; !ok {
			s.uniqueLinks[key] = l
			s.generation++
		}
	}
	if l.DeletePolicy == "restrict-active" {
		if s.archiveLinks == nil {
			s.archiveLinks = map[string]platform.LinkType{}
		}
		if _, ok := s.archiveLinks[key]; !ok {
			s.archiveLinks[key] = l
			s.generation++
		}
	}
	return nil
}

// This check is under the record lock, across the owner's full store. Member
// read filters cannot hide a conflict; refusals expose no competing row data.
func (s *Store) CheckLinkWriteLocked(et *Type, value reflect.Value) *kernel.Error {
	id := value.Field(0).Interface().(platform.Record).ID
	for _, l := range s.uniqueLinks {
		if l.Child.Name != et.info.Type {
			continue
		}
		f, ok := et.info.Field(l.Via)
		if !ok || f.Type != "reference" || f.Ref != l.Parent.Name {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, LinkCardinalityFailure)
		}
		parent := value.FieldByIndex(f.Index).String()
		if parent == "" {
			continue
		}
		bucket := et.ReferenceBucket(f, parent).IDs
		allowed := 0
		if bucket.Has(id) {
			allowed = 1
		}
		if bucket.Len() > allowed {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, LinkCardinalityFailure)
		}
	}
	return s.CheckArchiveWriteLocked(et, value)
}

func (s *Store) ValidateArchiveLinkLocked(l platform.LinkType) error {
	parent, child := s.types[l.Parent.Name], s.types[l.Child.Name]
	if parent == nil || child == nil || l.Parent.App != l.Child.App {
		return fmt.Errorf("archive protection needs both objects of the same owner")
	}
	f, ok := child.info.Field(l.Via)
	if !ok || f.Type != "reference" || f.Ref != parent.info.Type {
		return fmt.Errorf("link reference is unavailable")
	}
	for id, bucket := range child.ReferenceIndex(f).All() {
		if bucket.Active == 0 {
			continue
		}
		target := parent.rows.Get(id)
		if target == nil || target.Value.Field(0).Interface().(platform.Record).Archived {
			return fmt.Errorf("%s", linkArchiveFailure)
		}
	}
	return nil
}

func (s *Store) CheckArchiveWriteLocked(et *Type, value reflect.Value) *kernel.Error {
	rec := value.Field(0).Interface().(platform.Record)
	fail := func() *kernel.Error { return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, linkArchiveFailure) }
	for _, l := range s.archiveLinks {
		child, parent := s.types[l.Child.Name], s.types[l.Parent.Name]
		if child == nil || parent == nil {
			return fail()
		}
		f, ok := child.info.Field(l.Via)
		if !ok || f.Type != "reference" || f.Ref != parent.info.Type {
			return fail()
		}
		if et.info.Type == l.Parent.Name && rec.Archived {
			bucket := child.ReferenceBucket(f, rec.ID)
			allowed := 0
			if et.info.Type == child.info.Type && bucket.IDs.Get(rec.ID) {
				allowed = 1 // prospective self is archived
			}
			if bucket.Active > allowed {
				return fail()
			}
		}
		if et.info.Type == l.Child.Name && !rec.Archived {
			id := value.FieldByIndex(f.Index).String()
			if id == "" {
				continue
			}
			if et.info.Type == l.Parent.Name && id == rec.ID {
				continue
			} // prospective self is active
			target := parent.rows.Get(id)
			if target == nil || target.Value.Field(0).Interface().(platform.Record).Archived {
				return fail()
			}
		}
	}
	return nil
}

// Unique checks the complete owner index; member read filters never hide a
// competing row. The tenant writer keeps this check and acceptance indivisible.
func (s *Store) UniqueConstraint(info platform.EntityInfo, rule platform.Constraint, value reflect.Value) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, err := UniqueKey(s.IndexImage, info, rule, value)
	if err != nil || key == "" {
		return err == nil, err
	}
	et := s.types[info.Type]
	if s.current.Backend() != nil {
		storage, ok := s.current.Reader().(journal.UniqueState)
		if !ok {
			panic("record storage does not provide derived unique keys")
		}
		changes := s.UniqueChanges()
		if !et.fullImage {
			ids, err := storage.StateUnique(context.Background(), s.current.Tenant(), info.Type, rule.Name, key, s.current.Applied())
			if err != nil {
				// As with candidate reads, a storage failure cannot become a
				// durable business refusal or silently pass a constraint.
				panic(fmt.Errorf("record unique keys: %w", err))
			}
			for _, id := range ids {
				if id == Header(value).ID {
					continue
				}
				if _, replaced := changes[info.Type+"/"+id]; !replaced {
					return false, nil
				}
			}
		}
		for ref, keys := range changes {
			typ, id, _ := strings.Cut(ref, "/")
			if typ == info.Type && id != Header(value).ID && keys[rule.Name] == key {
				return false, nil
			}
		}
		return true, nil
	}
	index := et.UniqueIndex(s, rule)
	bucket := index.Keys.Get(key)
	return bucket.Len() == 0 || bucket.Len() == 1 && bucket.Has(Header(value).ID), nil
}
