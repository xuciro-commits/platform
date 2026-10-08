package enterprise

import "platformserver/apps/enterprise/uaf"

// The Enterprise Core Profile (ADR-0067 D1): the UAF stereotypes a tenant
// meets first, with the words people use for them and the grid cells they are
// drawn in. Everything else in the metamodel stays loadable and storable; it
// just has no palette entry until a profile opens it.

// ProfileEntry is one stereotype as the palette offers it.
type ProfileEntry struct {
	Stereotype string   `json:"stereotype"`
	Title      string   `json:"title"`
	Plural     string   `json:"plural"`
	Kinds      []string `json:"kinds,omitempty"`  // suggested Element.Kind values
	Scales     []string `json:"scales,omitempty"` // from which scale on it is offered; empty: always
	Icon       string   `json:"icon,omitempty"`
}

// GridCell is a UAF view: a domain × aspect cell with what it draws.
type GridCell struct {
	ID            string   `json:"id"` // "Pr-Sr"
	Title         string   `json:"title"`
	Domain        string   `json:"domain"`
	Aspect        string   `json:"aspect"`
	Elements      []string `json:"elements"`      // stereotypes placed in it
	Relationships []string `json:"relationships"` // relationship stereotypes drawn in it
	Scales        []string `json:"scales,omitempty"`
}

func Profile() []ProfileEntry {
	return []ProfileEntry{
		{Stereotype: Organization, Title: "Organisation", Plural: "Organisations", Icon: "building", Kinds: []string{"group", "company", "subsidiary", "division", "business unit", "factory", "plant", "department", "line", "shift", "team", "committee", "partner"}},
		{Stereotype: Post, Title: "Post", Plural: "Posts", Icon: "badge", Kinds: []string{"executive", "manager", "supervisor", "specialist", "operator", "staff"}},
		{Stereotype: Person, Title: "Person", Plural: "People", Icon: "user"},
		{Stereotype: "Responsibility", Title: "Responsibility", Plural: "Responsibilities", Icon: "clipboard", Scales: []string{"M", "L", "XL"}},
		{Stereotype: Capability, Title: "Capability", Plural: "Capabilities", Icon: "target", Kinds: []string{"business", "operational", "technical", "support"}},
		{Stereotype: Goal, Title: "Goal", Plural: "Goals", Icon: "flag"},
		{Stereotype: "EnterpriseObjective", Title: "Objective", Plural: "Objectives", Icon: "flag", Scales: []string{"L", "XL"}},
		{Stereotype: "Opportunity", Title: "Opportunity", Plural: "Opportunities", Icon: "sparkles", Scales: []string{"L", "XL"}},
		{Stereotype: Location, Title: "Location", Plural: "Locations", Icon: "map-pin", Kinds: []string{"site", "plant", "building", "warehouse", "workshop", "zone", "line", "workcenter", "station", "bin", "dock", "office"}},
		{Stereotype: Resource, Title: "Resource", Plural: "Resources", Icon: "wrench", Kinds: []string{"machine", "equipment", "tool", "vehicle", "fixture", "instrument"}},
		{Stereotype: "System", Title: "System", Plural: "Systems", Icon: "server", Kinds: []string{"ERP", "MES", "WMS", "PLM", "CRM", "custom"}, Scales: []string{"M", "L", "XL"}},
		{Stereotype: "Service", Title: "Service", Plural: "Services", Icon: "handshake", Scales: []string{"L", "XL"}},
		{Stereotype: "OperationalActivity", Title: "Process", Plural: "Processes", Icon: "workflow", Scales: []string{"M", "L", "XL"}},
		{Stereotype: Project, Title: "Project", Plural: "Projects", Icon: "calendar", Kinds: []string{"improvement", "investment", "product", "compliance"}},
		{Stereotype: "ActualProjectMilestone", Title: "Milestone", Plural: "Milestones", Icon: "milestone", Scales: []string{"L", "XL"}},
		{Stereotype: "Standard", Title: "Standard", Plural: "Standards", Icon: "book", Scales: []string{"L", "XL"}},
		{Stereotype: "Risk", Title: "Risk", Plural: "Risks", Icon: "alert", Scales: []string{"L", "XL"}},
	}
}

// vocabulary are the relationships the Enterprise Core Profile draws: what a
// modeller of an organisation, its places, resources, capabilities, goals,
// processes and projects actually joins. Every other UAF relationship stays
// available through the contracts, and a model may hold one; it is just not
// offered by a cell.
var vocabulary = map[string]bool{
	Placement: true, Membership: true, FillsPost: true, Performs: true, Exhibits: true,
	Owns: true, "OwnsProcess": true, "Enables": true, "MotivatedBy": true,
	"MilestoneDependency": true, "ProjectSequence": true,
}

// Grid are the UAF view cells: each draws a set of elements, and the
// relationships between them are derived — a relation appears in a cell only
// when the contracts admit some pair of the cell's own elements (ADR-0085 D2).
// A cell can therefore never offer a relation its elements cannot use.
func Grid(mm *uaf.Metamodel) []GridCell {
	contracts := Contracts(mm)
	drawable := func(elements []string) []string {
		var out []string
		for _, c := range contracts {
			if !vocabulary[c.Stereotype] {
				continue
			}
			for _, pair := range EndsFor(c) {
				if holds(mm, pair.Source, elements) && holds(mm, pair.Target, elements) {
					out = append(out, c.Stereotype)
					break
				}
			}
		}
		return out
	}
	cells := []GridCell{
		{ID: "Pr-Sr", Title: "Personnel structure", Domain: "Personnel", Aspect: "Structure", Elements: []string{Organization, Post, Person}},
		{ID: "Pr-Cn", Title: "Posts and responsibilities", Domain: "Personnel", Aspect: "Connectivity", Elements: []string{Post, "Responsibility", Person}, Scales: []string{"M", "L", "XL"}},
		{ID: "St-Tx", Title: "Capabilities", Domain: "Strategic", Aspect: "Taxonomy", Elements: []string{Capability}},
		{ID: "St-Sr", Title: "Goals and capabilities", Domain: "Strategic", Aspect: "Structure", Elements: []string{Goal, "EnterpriseObjective", "Opportunity", Capability, Organization}, Scales: []string{"L", "XL"}},
		{ID: "Rs-Sr", Title: "Sites and resources", Domain: "Resources", Aspect: "Structure", Elements: []string{Location, Resource, "System", Organization}},
		{ID: "Sv-Tx", Title: "Services", Domain: "Services", Aspect: "Taxonomy", Elements: []string{"Service", Organization}, Scales: []string{"L", "XL"}},
		{ID: "Op-Pr", Title: "Processes", Domain: "Operational", Aspect: "Processes", Elements: []string{"OperationalActivity", Organization, Capability}, Scales: []string{"M", "L", "XL"}},
		{ID: "Pj-Rm", Title: "Projects", Domain: "Projects", Aspect: "Roadmap", Elements: []string{Project, "ActualProjectMilestone", Organization}},
	}
	for i := range cells {
		cells[i].Relationships = drawable(cells[i].Elements)
	}
	return cells
}

// holds reports whether any of the cell's element stereotypes is the named end
// or a specialisation of it.
func holds(mm *uaf.Metamodel, end string, elements []string) bool {
	if end == "*" {
		return len(elements) > 0
	}
	for _, e := range elements {
		if mm.Is(e, end) {
			return true
		}
	}
	return false
}
