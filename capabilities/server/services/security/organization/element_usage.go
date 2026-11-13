package organization

import (
	"slices"

	"platformserver/platform"
)

type EnterpriseUsageItem struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Ref     string `json:"ref,omitempty"`
	Field   string `json:"field,omitempty"`
	Current bool   `json:"current"`
}

type EnterpriseUsage struct {
	CanDelete          bool                  `json:"canDelete"`
	CanClose           bool                  `json:"canClose"`
	HasRestrictedUsage bool                  `json:"hasRestrictedUsage"`
	Items              []EnterpriseUsageItem `json:"items"`
}

func (u *EnterpriseUsage) Add(item EnterpriseUsageItem, visible bool) {
	u.CanDelete = false
	if item.Current {
		u.CanClose = false
	}
	if visible {
		u.Items = append(u.Items, item)
	} else {
		u.HasRestrictedUsage = true
	}
}

func (m Model) Usage(id, until string, result EnterpriseUsage) (EnterpriseUsage, bool) {
	element := m.ElementOf(id)
	if element == nil || element.Deleted {
		return result, false
	}
	if element.Owner != "" {
		result.CanDelete = false
		result.CanClose = false
	}
	add := func(kind, title, ref string, current bool) {
		result.CanDelete = false
		if current {
			result.CanClose = false
		}
		result.Items = append(result.Items, EnterpriseUsageItem{Kind: kind, Title: title, Ref: ref, Current: current})
	}
	if element.Published {
		add("publication", element.Name, element.ID, true)
	}
	for _, r := range m.Relationships {
		if r.Source == id || r.Target == id {
			current := slices.ContainsFunc(r.PeriodsFromLegacy(), func(p platform.Period) bool { return p.Until == "" || p.Until > until })
			add("relationship", r.Relation, r.ID, current)
		}
	}
	for _, v := range m.Views {
		_, placed := v.Layout[id]
		if placed || slices.Contains(v.Elements, id) || slices.Contains(v.Context, id) || slices.ContainsFunc(v.Pins, func(p Pin) bool { return p.Anchor == id || p.Ref == "record:enterprise.element/"+id }) {
			add("view", v.Name, v.ID, false)
		}
	}
	for _, link := range m.TenantLinks {
		if link.Organization == id {
			add("tenant-link", link.Tenant, link.ID, true)
		}
	}

	currentProperties := m.ElementPropertyUses(id, until)
	for _, otherID := range m.ElementPropertyUses(id, "") {
		other := m.ElementOf(otherID)
		if other != nil {
			add("property", other.Name, other.ID, slices.Contains(currentProperties, other.ID))
		}
	}
	return result, true
}
