package platformserver

import (
	"fmt"
	"platformserver/internal/host"
	"reflect"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

const linkArchiveFailure = "Active reference archive protection failed"

const linkCardinalityFailure = "Reference cardinality constraint failed"

func (s *recordStore) validateUniqueLinkLocked(l platform.LinkType) error {
	et := s.types[l.Child.Name]
	if et == nil {
		return fmt.Errorf("link child is unavailable")
	}
	f, ok := et.info.Field(l.Via)
	if !ok || f.Type != "reference" || f.Ref != l.Parent.Name {
		return fmt.Errorf("link reference is unavailable")
	}
	for _, bucket := range et.referenceIndex(f).All() {
		if bucket.IDs.Len() > 1 {
			return fmt.Errorf("%s", linkCardinalityFailure)
		}
	}
	return nil
}

// The parent admitted its complete image and generation must still match at
// promotion. Ordinary writes therefore need only their final relation keys;
// new declarations and replaced type images still receive full admission.
func (s *recordStore) validateChangedLinkConstraintsLocked() error {
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
			if err := s.validateUniqueLinkLocked(link); err != nil {
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
				if target != "" && et.referenceIndex(field).Get(target).IDs.Len() > 1 {
					return fmt.Errorf("%s", linkCardinalityFailure)
				}
			}
		}
	}
	for key, link := range s.archiveLinks {
		if full(link.Child.Name) || full(link.Parent.Name) || !reflect.DeepEqual(s.parent.archiveLinks[key], link) {
			if err := s.validateArchiveLinkLocked(link); err != nil {
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
			if (row == nil || row.Value.Field(0).Interface().(platform.Record).Archived) && child.referenceIndex(field).Get(id).Active > 0 {
				return fmt.Errorf("%s", linkArchiveFailure)
			}
		}
	}
	return nil
}

func (s *recordStore) validateLinkConstraintsLocked() error {
	for _, l := range s.uniqueLinks {
		if err := s.validateUniqueLinkLocked(l); err != nil {
			return err
		}
	}
	for _, l := range s.archiveLinks {
		if err := s.validateArchiveLinkLocked(l); err != nil {
			return err
		}
	}
	return nil
}
func (s *recordStore) validateLinkConstraints() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validateLinkConstraintsLocked()
}
func (s *recordStore) validateChangedLinkConstraints() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validateChangedLinkConstraintsLocked()
}
func (s *recordStore) installLinkConstraint(l platform.LinkType) error {
	if l.Storage == "edge" {
		return nil
	}
	if l.Cardinality != "one-to-one" && l.DeletePolicy != "restrict-active" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.Cardinality == "one-to-one" {
		if err := s.validateUniqueLinkLocked(l); err != nil {
			return err
		}
	}
	if l.DeletePolicy == "restrict-active" {
		if err := s.validateArchiveLinkLocked(l); err != nil {
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
func (s *recordStore) checkLinkWriteLocked(et *entityType, value reflect.Value) *kernel.Error {
	id := value.Field(0).Interface().(platform.Record).ID
	for _, l := range s.uniqueLinks {
		if l.Child.Name != et.info.Type {
			continue
		}
		f, ok := et.info.Field(l.Via)
		if !ok || f.Type != "reference" || f.Ref != l.Parent.Name {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, linkCardinalityFailure)
		}
		parent := value.FieldByIndex(f.Index).String()
		if parent == "" {
			continue
		}
		bucket := et.referenceIndex(f).Get(parent).IDs
		allowed := 0
		if bucket.Has(id) {
			allowed = 1
		}
		if bucket.Len() > allowed {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, linkCardinalityFailure)
		}
	}
	return s.checkArchiveWriteLocked(et, value)
}

// Publications derive their constraint from the same frozen Build descriptor;
// staging and accepted replay never maintain a second writable link declaration.
func (t *Tenant) installPublicationLinkConstraint(store *recordStore, schema string, image []byte) error {
	if schema != construction.SchemaLinkType {
		return nil
	}
	owner, ok := t.app(construction.ID).(host.LinkPublicationReader)
	if !ok {
		return fmt.Errorf("link publication owner is unavailable")
	}
	declaration, err := owner.LinkPublication(image)
	if err != nil {
		return err
	}
	return store.installLinkConstraint(declaration)
}
