package platformserver

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

type EnterpriseReference struct {
	Type       string `json:"type"`
	Field      string `json:"field"`
	FieldTitle string `json:"fieldTitle"`
	Stereotype string `json:"stereotype,omitempty"`
	ID         string `json:"id"`
	Name       string `json:"name"`
}

type EnterpriseReferenceGroup struct {
	Type       string                `json:"type"`
	Title      string                `json:"title"`
	Field      string                `json:"field"`
	FieldTitle string                `json:"fieldTitle"`
	Stereotype string                `json:"stereotype,omitempty"`
	Total      int                   `json:"total"`
	Records    []EnterpriseReference `json:"records"`
}

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
	s.Lock()
	types := make([]*records.Type, 0, s.TypeCount())
	for _, et := range s.Types() {
		types = append(types, et)
	}
	s.Unlock()
	slices.SortFunc(types, func(a, b *records.Type) int { return strings.Compare(a.Info().Type, b.Info().Type) })
	groups := []EnterpriseReferenceGroup{}
	for _, et := range types {
		if !records.DeclaresEnterpriseRef(et.Info().Fields) {
			continue
		}
		visible, err := t.visibleIn(s, m, et, now)
		if err != nil {
			continue
		}
		_, hidden := viewOf(m, et)
		var rows []reflect.Value
		func() {
			s.Lock()
			defer s.Unlock()
			rows = make([]reflect.Value, 0, et.Rows().Len())
			for _, r := range et.Rows().All() {
				if visible == nil || visible(r.Value) {
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
			readable := records.Mask(et.Info(), r, hidden)
			seen := map[string]bool{}
			for _, f := range records.MatchingRefs(et.Info().Fields, "", reflect.ValueOf(readable), element, m.RolesIn(et.Info().App)) {
				if seen[f.Path] {
					continue
				}
				seen[f.Path] = true
				g, ok := byPath[f.Path]
				if !ok {
					g = &EnterpriseReferenceGroup{Type: et.Info().Type, Title: et.Info().Plural, Field: f.Path, FieldTitle: f.Title, Stereotype: f.Stereotype, Records: []EnterpriseReference{}}
					byPath[f.Path] = g
					order = append(order, f.Path)
				}
				g.Total++
				if g.Total > offset && len(g.Records) < limit {
					if name == "" {
						name = records.RecordLabel(et.Info(), readable, id)
					}
					g.Records = append(g.Records, EnterpriseReference{Type: et.Info().Type, Field: f.Path, FieldTitle: f.Title, Stereotype: f.Stereotype, ID: id, Name: name})
				}
			}
		}
		for _, path := range order {
			groups = append(groups, *byPath[path])
		}
	}
	return groups, nil
}
