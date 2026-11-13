package organization

import (
	"platformserver/platform"
	"slices"
	"strings"
)

func FromOrgSeed(seed platform.OrgSeed) Model {
	m := Model{UAF: "1.3", Kinds: []Kind{}, Elements: []Element{}, Relationships: []Relationship{}, Views: []View{}, Calendars: slices.Clone(seed.Calendars)}
	for _, s := range seed.Structures {
		m.Kinds = append(m.Kinds, Kind{ID: s.ID, Name: s.Name, Kind: s.Kind, Matrix: s.Matrix})
	}
	for _, u := range seed.Units {
		m.Elements = append(m.Elements, Element{ID: u.ID, Stereotype: Organization, Name: u.Name, Kind: u.Kind, Legal: u.Legal, External: u.External, From: u.From, Until: u.Until, Closed: u.Closed, Calendar: u.Calendar, Periods: slices.Clone(u.Periods)})
	}
	for i, e := range seed.Edges {
		m.Relationships = append(m.Relationships, Relationship{ID: RelID("pl", i), Stereotype: Placement, Kind: e.Structure, Source: e.Unit, Target: e.Parent, Relation: e.Relation, Share: e.Share, From: e.From, Until: e.Until, Periods: slices.Clone(e.Periods)})
	}
	for i, ms := range seed.Memberships {
		source := ms.Party
		if id, ok := CutPrefix(ms.Party, "unit:"); ok {
			source = id
		}
		m.Relationships = append(m.Relationships, Relationship{ID: RelID("mb", i), Stereotype: Membership, Source: source, Target: ms.Unit, Role: ms.Role, Primary: ms.Primary, From: ms.From, Until: ms.Until, Periods: slices.Clone(ms.Periods)})
	}
	return m
}

func (m Model) OrgSeed() platform.OrgSeed {
	out := platform.OrgSeed{Calendars: slices.Clone(m.Calendars), Structures: []platform.Structure{}, Units: []platform.Unit{}, Edges: []platform.Edge{}, Memberships: []platform.Membership{}}
	for _, k := range m.Kinds {
		out.Structures = append(out.Structures, platform.Structure{ID: k.ID, Name: k.Name, Kind: k.Kind, Matrix: k.Matrix})
	}
	for _, e := range m.Elements {
		if e.Stereotype == Organization && !e.Deleted {
			out.Units = append(out.Units, platform.Unit{ID: e.ID, Name: e.Name, Kind: e.Kind, Legal: e.Legal, External: e.External, From: e.From, Until: e.Until, Closed: e.Closed, Calendar: e.Calendar, Periods: slices.Clone(e.Periods)})
		}
	}
	for _, r := range m.Relationships {
		switch r.Stereotype {
		case Placement:
			out.Edges = append(out.Edges, platform.Edge{Structure: r.Kind, Unit: r.Source, Parent: r.Target, Relation: r.Relation, Share: r.Share, From: r.From, Until: r.Until, Periods: slices.Clone(r.Periods)})
		case Membership:
			party := r.Source
			if m.ElementOf(party) != nil {
				party = "unit:" + party
			}
			out.Memberships = append(out.Memberships, platform.Membership{Party: party, Unit: r.Target, Role: r.Role, Primary: r.Primary, From: r.From, Until: r.Until, Periods: slices.Clone(r.Periods)})
		}
	}
	return out
}

func (m *Model) Import(s Slice) { m.ImportOn(s, "") }

func (m *Model) ImportOn(s Slice, today platform.Date) {
	owner := "tenant:" + s.Tenant
	if today != "" {
		for _, kind := range s.Kinds {
			kind.Owner = owner
			if old := m.KindOf(kind.ID); old != nil {
				*old = kind
			} else {
				m.Kinds = append(m.Kinds, kind)
			}
		}
		seen := map[string]bool{}
		for _, in := range s.Elements {
			seen[in.ID] = true
			in.Owner, in.Published = owner, false
			if old := m.ElementOf(in.ID); old != nil {
				incomingUntil := in.Until
				if old.Until != "" && old.Closed == SyncClosed(owner) {
					_ = old.ReopenOn(today)
				}
				if len(old.Periods) > 0 {
					in.Periods = slices.Clone(old.Periods)
					in.From, in.Until = old.From, old.Until
					if incomingUntil != "" {
						in.Until = incomingUntil
						in.Periods[len(in.Periods)-1].Until = incomingUntil
					}
				}
				*old = in
			} else {
				m.Elements = append(m.Elements, in)
			}
		}
		for i := range m.Elements {
			el := &m.Elements[i]
			if el.Owner == owner && !seen[el.ID] && el.Until == "" {
				closeSourceElement(el, today, SyncClosed(owner))
			}
		}
		incoming := map[string]bool{}
		prefix := "fed:" + s.Tenant + ":"
		for _, in := range s.Relationships {
			if !strings.HasPrefix(in.ID, prefix) {
				in.ID = prefix + in.ID
			}
			incoming[in.ID] = true
			if old := m.RelationshipOf(in.ID); old != nil {
				if old.Until != "" {
					old.ReopenOn(today)
				}
				old.Kind, old.Role, old.Relation, old.Share, old.Primary = in.Kind, in.Role, in.Relation, in.Share, in.Primary
				if in.Until != "" {
					old.Until = in.Until
					if len(old.Periods) > 0 {
						old.Periods[len(old.Periods)-1].Until = in.Until
					}
				}
			} else {
				m.Relationships = append(m.Relationships, in)
			}
		}
		for i := range m.Relationships {
			r := &m.Relationships[i]
			if strings.HasPrefix(r.ID, prefix) && !incoming[r.ID] && r.Until == "" {
				_ = r.EndOn(today)
			}
		}
		return
	}
	kept := m.Elements[:0]
	for _, e := range m.Elements {
		if e.Owner != owner {
			kept = append(kept, e)
		}
	}
	m.Elements = append(kept, s.Elements...)
	ids := map[string]bool{}
	for _, e := range m.Elements {
		ids[e.ID] = true
	}
	prefix := "fed:" + s.Tenant + ":"
	rels := m.Relationships[:0]
	for _, r := range m.Relationships {
		if !strings.HasPrefix(r.ID, prefix) && ids[r.Source] && (ids[r.Target] || strings.HasPrefix(r.Source, "member:")) {
			rels = append(rels, r)
		}
	}
	m.Relationships = append(rels, s.Relationships...)
}

type SyncResult struct {
	Added, Changed, Closed, Reopened, Related, Ended int
}

func (m *Model) Sync(source string, elements []Element, relationships []Relationship, today platform.Date) SyncResult {
	return m.sync(source, elements, relationships, today, false)
}

func (m *Model) sync(source string, elements []Element, relationships []Relationship, today platform.Date, replay bool) SyncResult {
	owner, res := "source:"+source, SyncResult{}
	seen := map[string]bool{}
	for _, in := range elements {
		seen[in.ID] = true
		if cur := m.ElementOf(in.ID); cur != nil {
			changed := cur.Name != in.Name || cur.ShortName != in.ShortName || cur.Kind != in.Kind || cur.Legal != in.Legal || cur.External != in.External || !SameProperties(cur.Properties, in.Properties)
			if cur.Until != "" && cur.Closed == SyncClosed(source) {
				if replay {
					cur.Until, cur.Closed = "", ""
				} else {
					_ = cur.ReopenOn(today)
				}
				res.Reopened++
			} else if changed {
				res.Changed++
			}
			cur.Name, cur.ShortName, cur.Kind, cur.Legal, cur.External, cur.Properties = in.Name, in.ShortName, in.Kind, in.Legal, in.External, in.Properties
			if in.Until != "" {
				cur.Until = in.Until
				if len(cur.Periods) > 0 {
					cur.Periods[len(cur.Periods)-1].Until = in.Until
				}
			}
			continue
		}
		in.Owner, in.Published = owner, false
		if in.From == "" {
			in.From = today
		}
		m.Elements = append(m.Elements, in)
		res.Added++
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Owner == owner && !seen[e.ID] && e.Until == "" {
			if replay {
				e.Until, e.Closed = today, SyncClosed(source)
			} else {
				closeSourceElement(e, today, SyncClosed(source))
			}
			res.Closed++
		}
	}
	prefix := "sync:" + source + ":"
	seenRel := map[string]bool{}
	for _, in := range relationships {
		if !strings.HasPrefix(in.ID, prefix) {
			in.ID = prefix + in.ID
		}
		seenRel[in.ID] = true
		if cur := m.RelationshipOf(in.ID); cur != nil {
			if cur.Until != "" && in.Until == "" {
				if replay {
					cur.Until = ""
				} else {
					cur.ReopenOn(today)
				}
				res.Reopened++
			}
			cur.Kind, cur.Role, cur.Relation, cur.Share, cur.Primary = in.Kind, in.Role, in.Relation, in.Share, in.Primary
			if in.Until != "" {
				cur.Until = in.Until
				if len(cur.Periods) > 0 {
					cur.Periods[len(cur.Periods)-1].Until = in.Until
				}
			}
			continue
		}
		if in.From == "" {
			in.From = today
		}
		m.Relationships = append(m.Relationships, in)
		res.Related++
	}
	for i := range m.Relationships {
		r := &m.Relationships[i]
		if strings.HasPrefix(r.ID, prefix) && !seenRel[r.ID] && r.Until == "" {
			if replay {
				r.Until = today
			} else {
				_ = r.EndOn(today)
			}
			res.Ended++
		}
	}
	return res
}

func SyncClosed(source string) string { return "no longer in " + source }

func ImportedElementID(tenant, id string) string { return "tenant:" + tenant + ":" + id }

func closeSourceElement(e *Element, day platform.Date, reason string) {
	e.Until, e.Closed = day, reason
	if len(e.Periods) > 0 {
		e.Periods[len(e.Periods)-1].Until = day
	}
}
