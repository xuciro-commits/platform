package enterprise

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"platformserver/internal/enterprise/uaf"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func periodsOrLegacy(periods []platform.Period, from, until platform.Date) []platform.Period {
	if len(periods) > 0 {
		return periods
	}
	return []platform.Period{{From: from, Until: until}}
}
func periodsActive(periods []platform.Period, from, until, day platform.Date) bool {
	return slices.ContainsFunc(periodsOrLegacy(periods, from, until), func(p platform.Period) bool { return platform.ActiveOn(p.From, p.Until, day) })
}
func (e Element) ActiveOn(day platform.Date) bool {
	return !e.Deleted && periodsActive(e.Periods, e.From, e.Until, day)
}
func (r Relationship) ActiveOn(day platform.Date) bool {
	return periodsActive(r.Periods, r.From, r.Until, day)
}
func (e Element) Covers(from, until platform.Date) bool {
	if e.Deleted {
		return false
	}
	return slices.ContainsFunc(periodsOrLegacy(e.Periods, e.From, e.Until), func(p platform.Period) bool {
		return (p.From == "" || from >= p.From) && (p.Until == "" || until != "" && until <= p.Until)
	})
}
func (r Relationship) PeriodsFromLegacy() []platform.Period {
	return periodsOrLegacy(r.Periods, r.From, r.Until)
}
func (e Element) PeriodsFromLegacy() []platform.Period {
	return periodsOrLegacy(e.Periods, e.From, e.Until)
}
func CheckPeriods(periods []platform.Period) error {
	for i, p := range periods {
		if p.From != "" {
			if _, err := time.Parse(time.DateOnly, p.From); err != nil {
				return fmt.Errorf("Invalid effective period")
			}
		}
		if p.Until != "" {
			if _, err := time.Parse(time.DateOnly, p.Until); err != nil {
				return fmt.Errorf("Invalid effective period")
			}
			if p.Until <= p.From {
				return fmt.Errorf("The effective period must end after it starts")
			}
		}
		if i > 0 && (periods[i-1].Until == "" || p.From < periods[i-1].Until) {
			return fmt.Errorf("Effective periods must not overlap")
		}
	}
	return nil
}
func (e *Element) CloseOn(day platform.Date, reason string) error {
	periods := slices.Clone(e.PeriodsFromLegacy())
	last := &periods[len(periods)-1]
	if day == "" || day <= last.From || last.Until != "" && day > last.Until {
		return fmt.Errorf("The closing date cannot extend the effective period")
	}
	last.Until = day
	if err := CheckPeriods(periods); err != nil {
		return err
	}
	e.Until, e.Closed = day, reason
	if len(e.Periods) > 0 {
		e.Periods = periods
	}
	return nil
}
func (e *Element) ReopenOn(day platform.Date) error {
	periods := slices.Clone(e.PeriodsFromLegacy())
	last := periods[len(periods)-1]
	if e.Deleted || last.Until == "" || day == "" || day < last.Until {
		return fmt.Errorf("Reopening starts a new period after the previous one ends")
	}
	periods = append(periods, platform.Period{From: day})
	if err := CheckPeriods(periods); err != nil {
		return err
	}
	e.Periods = periods
	e.Until, e.Closed = "", ""
	return nil
}
func (e *Element) Reinstate() error {
	if e.Deleted || e.Until == "" {
		return fmt.Errorf("Only a closed element can be reinstated")
	}
	e.Until, e.Closed = "", ""
	if len(e.Periods) > 0 {
		e.Periods[len(e.Periods)-1].Until = ""
	}
	return nil
}
func (r *Relationship) EndOn(day platform.Date) error {
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return fmt.Errorf("Invalid effective period")
	}
	periods := slices.Clone(r.PeriodsFromLegacy())
	last := &periods[len(periods)-1]
	if day < last.From || last.Until != "" && day > last.Until {
		return fmt.Errorf("Ending a relationship cannot extend its effective period")
	}
	last.Until = day
	r.Until = day
	if len(r.Periods) > 0 {
		r.Periods = periods
	}
	return nil
}
func (r *Relationship) ReopenOn(day platform.Date) {
	periods := slices.Clone(r.PeriodsFromLegacy())
	last := periods[len(periods)-1]
	if last.Until != "" && day >= last.Until {
		r.Periods = append(periods, platform.Period{From: day})
		r.Until = ""
	}
}
func (m *Model) TenantLinkOf(id string) *TenantLink {
	if i := slices.IndexFunc(m.TenantLinks, func(link TenantLink) bool { return link.ID == id }); i >= 0 {
		return &m.TenantLinks[i]
	}
	return nil
}
func fingerprint(value any) string {
	raw, _ := json.Marshal(value)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}
func (m Model) Fingerprint() string { return fingerprint(m) }
func (s Slice) Fingerprint() string { return fingerprint(s) }

// InternalUsage includes historical declarations; deleting must not erase a named identity.
func (m Model) InternalUsage(id string) string {
	for _, r := range m.Relationships {
		if r.Source == id || r.Target == id {
			return "relationship " + r.ID
		}
	}
	for _, el := range m.Elements {
		if el.ID == id && el.Published {
			return "published element"
		}
	}
	for _, v := range m.Views {
		if slices.Contains(v.Elements, id) || slices.Contains(v.Context, id) {
			return "view " + v.ID
		}
		if _, ok := v.Layout[id]; ok {
			return "view " + v.ID
		}
		for _, pin := range v.Pins {
			if pin.Anchor == id || pin.Ref == "record:enterprise.element/"+id {
				return "view " + v.ID
			}
		}
	}
	if refs := m.ElementPropertyUses(id, ""); len(refs) > 0 {
		return "element " + refs[0]
	}
	for _, link := range m.TenantLinks {
		if link.Organization == id || ImportedElementID(link.Tenant, link.Root) == id {
			return "tenant link " + link.ID
		}
	}
	return ""
}

// ElementPropertyUses follows the metamodel's element-typed tagged values, never free text.
// Empty day includes every retained period; otherwise only use on or after day counts.
func (m Model) ElementPropertyUses(id string, day platform.Date) []string {
	mm := uaf.Current()
	out := []string{}
	for _, element := range m.Elements {
		if element.ID == id || element.Deleted && day != "" {
			continue
		}
		if day != "" && !slices.ContainsFunc(element.PeriodsFromLegacy(), func(period platform.Period) bool { return period.Until == "" || period.Until > day }) {
			continue
		}
		for _, property := range mm.Properties(element.Stereotype) {
			if mm.Stereotypes[property.Type] == nil || mm.Relationship(property.Type) {
				continue
			}
			match := func(value any) bool {
				text, ok := value.(string)
				return ok && (text == id || text == "enterprise.element/"+id)
			}
			value := element.Properties[property.Name]
			found := match(value)
			if property.Many {
				if values, ok := value.([]any); ok {
					found = slices.ContainsFunc(values, match)
				}
				if values, ok := value.([]string); ok {
					found = slices.ContainsFunc(values, func(value string) bool { return match(value) })
				}
			}
			if found {
				out = append(out, element.ID)
				break
			}
		}
	}
	return out
}
func ImportedElementID(tenant, id string) string { return "tenant:" + tenant + ":" + id }

// PreviewImport is the same pure model update used by preview and accepted import.
func (m Model) PreviewImport(s Slice, linkID, content, base string, today platform.Date) (Model, error) {
	if base != m.Fingerprint() {
		return Model{}, fmt.Errorf("The enterprise model changed; preview the import again")
	}
	if content != s.Fingerprint() {
		return Model{}, fmt.Errorf("The shared slice differs from its preview")
	}
	raw, _ := json.Marshal(m)
	var next Model
	json.Unmarshal(raw, &next)
	link := next.TenantLinkOf(linkID)
	if link == nil || link.Tenant != s.Tenant {
		return Model{}, fmt.Errorf("The tenant link does not match the shared slice")
	}
	if s.UAF != "" && s.UAF != m.UAF {
		return Model{}, fmt.Errorf("The shared slice uses a different UAF version")
	}
	root := slices.IndexFunc(s.Elements, func(e Element) bool {
		return e.ID == link.Root && e.Stereotype == Organization && !e.Deleted && e.ActiveOn(today)
	})
	if root < 0 {
		return Model{}, fmt.Errorf("The tenant link root is not a shared active organisation")
	}
	s, err := next.NamespaceSlice(s)
	if err != nil {
		return Model{}, err
	}
	next.ImportOn(s, today)
	link = next.TenantLinkOf(linkID)
	link.Content, link.UpdatedAt = content, today
	return next, nil
}

func (m Model) NamespaceSlice(s Slice) (Slice, error) {
	raw, _ := json.Marshal(s)
	var cloned Slice
	json.Unmarshal(raw, &cloned)
	s = cloned
	mm := uaf.Current()
	kinds := map[string]string{}
	for i := range s.Kinds {
		kind := &s.Kinds[i]
		if kind.ID == "" || kind.Name == "" || kind.Kind == "" || kinds[kind.ID] != "" {
			return Slice{}, fmt.Errorf("The shared slice contains an invalid relationship kind")
		}
		id := ImportedElementID(s.Tenant, kind.ID)
		legacy := false
		if old := m.KindOf(kind.ID); old != nil && old.Owner == "" && slices.ContainsFunc(m.Relationships, func(r Relationship) bool { return strings.HasPrefix(r.ID, "fed:"+s.Tenant+":") && r.Kind == kind.ID }) {
			id = kind.ID
			legacy = true
		}
		if old := m.KindOf(id); old != nil && !legacy && old.Owner != "tenant:"+s.Tenant {
			return Slice{}, fmt.Errorf("The relationship kind identity is already reserved")
		}
		kinds[kind.ID] = id
		kind.ID, kind.Owner = id, "tenant:"+s.Tenant
	}
	ids := map[string]string{}
	for _, e := range s.Elements {
		if e.ID == "" || e.Name == "" || e.Deleted {
			return Slice{}, fmt.Errorf("The shared slice contains an invalid element")
		}
		if st := mm.Stereotypes[e.Stereotype]; st == nil || st.Abstract || mm.Relationship(e.Stereotype) {
			return Slice{}, fmt.Errorf("Choose a concrete enterprise element type")
		}
		props := mm.Properties(e.Stereotype)
		for name, value := range e.Properties {
			index := slices.IndexFunc(props, func(p uaf.Property) bool { return p.Name == name })
			if index < 0 {
				return Slice{}, fmt.Errorf("The shared element contains an unknown tagged value")
			}
			if enumeration := mm.Enumerations[props[index].Type]; enumeration != nil {
				if text, ok := value.(string); !ok || !slices.Contains(enumeration.Literals, text) {
					return Slice{}, fmt.Errorf("The shared element contains an invalid tagged value")
				}
			}
		}
		if err := CheckPeriods(e.PeriodsFromLegacy()); err != nil {
			return Slice{}, err
		}
		id := ImportedElementID(s.Tenant, e.ID)
		if previous := m.ElementOf(e.ID); previous != nil && previous.Owner == "tenant:"+s.Tenant {
			id = e.ID
		}
		if previous := m.ElementOf(id); previous != nil && (previous.Owner != "tenant:"+s.Tenant || previous.Deleted) {
			return Slice{}, fmt.Errorf("The shared element identity is already reserved")
		}
		if previous := m.ElementOf(id); previous != nil && previous.Stereotype != e.Stereotype {
			return Slice{}, fmt.Errorf("A source element keeps its identity and type")
		}
		if ids[e.ID] != "" {
			return Slice{}, fmt.Errorf("The shared slice repeats an element")
		}
		ids[e.ID] = id
	}
	for i := range s.Elements {
		s.Elements[i].ID = ids[s.Elements[i].ID]
		for _, property := range mm.Properties(s.Elements[i].Stereotype) {
			if mm.Stereotypes[property.Type] == nil || mm.Relationship(property.Type) {
				continue
			}
			translate := func(value any) any {
				if ref, ok := value.(string); ok {
					if mapped := ids[ref]; mapped != "" {
						return mapped
					}
					if mapped := ids[strings.TrimPrefix(ref, "enterprise.element/")]; strings.HasPrefix(ref, "enterprise.element/") && mapped != "" {
						return "enterprise.element/" + mapped
					}
				}
				return value
			}
			value := s.Elements[i].Properties[property.Name]
			if property.Many {
				if values, ok := value.([]any); ok {
					for j := range values {
						values[j] = translate(values[j])
					}
				}
			} else if value != nil {
				s.Elements[i].Properties[property.Name] = translate(value)
			}
		}
	}
	relIDs := map[string]bool{}
	for i := range s.Relationships {
		r := &s.Relationships[i]
		if ref := kinds[r.Kind]; ref != "" {
			r.Kind = ref
		}
		if r.ID == "" || relIDs[r.ID] {
			return Slice{}, fmt.Errorf("The shared slice repeats a relationship")
		}
		relIDs[r.ID] = true
		if err := CheckPeriods(r.PeriodsFromLegacy()); err != nil {
			return Slice{}, err
		}
		r.Source, r.Target = ids[r.Source], ids[r.Target]
		if r.Source == "" || r.Target == "" {
			return Slice{}, fmt.Errorf("The shared relationship has an unavailable endpoint")
		}
		if !strings.HasPrefix(r.ID, "fed:"+s.Tenant+":") {
			r.ID = "fed:" + s.Tenant + ":" + r.ID
		}
		if old := m.RelationshipOf(r.ID); old != nil && (old.Source != r.Source || old.Target != r.Target || old.Stereotype != r.Stereotype) {
			return Slice{}, fmt.Errorf("A source relationship keeps its endpoints; use a new relationship ID")
		}
	}
	return s, nil
}

func closeSourceElement(e *Element, day platform.Date, reason string) {
	e.Until, e.Closed = day, reason
	if len(e.Periods) > 0 {
		e.Periods[len(e.Periods)-1].Until = day
	}
}
