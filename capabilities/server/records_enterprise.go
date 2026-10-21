package platformserver

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/records"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

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
func (t *Tenant) enterpriseReferencesRead(m platform.Member, element string, offset, limit int, now time.Time) ([]EnterpriseReferenceGroup, *kernel.Error) {
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
		if !records.DeclaresEnterpriseRef(et.info.Fields) {
			continue
		}
		visible, err := t.visibleIn(s, m, et, now)
		if err != nil {
			continue // the member may not read this type
		}
		_, hidden := viewOf(m, et)
		var rows []reflect.Value
		func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			rows = make([]reflect.Value, 0, et.rows.Len())
			for _, r := range et.rows.All() {
				if visible == nil || visible(r.Value) { // nil: the member's roles read the whole type
					rows = append(rows, r.Value)
				}
			}

		}()
		slices.SortFunc(rows, func(a, b reflect.Value) int { return strings.Compare(records.Header(a).ID, records.Header(b).ID) })
		byPath := map[string]*EnterpriseReferenceGroup{}
		var order []string
		for _, r := range rows {
			id := records.Header(r).ID
			name := ""
			readable := records.Mask(et.info, r, hidden)
			seen := map[string]bool{}
			for _, f := range records.MatchingRefs(et.info.Fields, "", reflect.ValueOf(readable), element, m.RolesIn(et.info.App)) {
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
						name = records.RecordLabel(et.info, readable, id)
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
