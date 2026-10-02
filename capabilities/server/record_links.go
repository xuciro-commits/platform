package platformserver

import (
	"fmt"
	"reflect"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

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
	seen := map[string]bool{}
	for _, row := range et.rows {
		id := row.value.FieldByIndex(f.Index).String()
		if id == "" {
			continue
		}
		if seen[id] {
			return fmt.Errorf("%s", linkCardinalityFailure)
		}
		seen[id] = true
	}
	return nil
}

func (s *recordStore) validateLinkConstraintsLocked() error {
	for _, l := range s.uniqueLinks {
		if err := s.validateUniqueLinkLocked(l); err != nil {
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
func (s *recordStore) installLinkConstraint(l platform.LinkType) error {
	if l.Cardinality != "one-to-one" {
		return nil
	} // Historical weaker versions never erase a retained constraint.
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.validateUniqueLinkLocked(l); err != nil {
		return err
	}
	if s.uniqueLinks == nil {
		s.uniqueLinks = map[string]platform.LinkType{}
	}
	key := l.Child.Name + "/" + l.Via
	if _, installed := s.uniqueLinks[key]; !installed {
		s.uniqueLinks[key] = l
		s.generation++
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
		for otherID, row := range et.rows {
			if otherID != id && row.value.FieldByIndex(f.Index).String() == parent {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, linkCardinalityFailure)
			}
		}
	}
	return nil
}

// Publications derive their constraint from the same frozen Build descriptor;
// staging and accepted replay never maintain a second writable link declaration.
func (t *Tenant) installPublicationLinkConstraint(store *recordStore, schema string, image []byte) error {
	if schema != build.SchemaLinkType {
		return nil
	}
	owner, ok := t.app(build.ID).(*build.Build)
	if !ok {
		return fmt.Errorf("link publication has no owner")
	}
	decl, err := owner.ReleaseDeclaration(build.ReleasePublication{Schema: schema, Image: image})
	if err != nil {
		return err
	}
	if decl.LinkType == nil {
		return fmt.Errorf("link publication has no declaration")
	}
	return store.installLinkConstraint(*decl.LinkType)
}
