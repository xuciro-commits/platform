package enterprise

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

func Grid() []GridCell {
	return []GridCell{
		{ID: "Pr-Sr", Title: "Personnel structure", Domain: "Personnel", Aspect: "Structure", Elements: []string{Organization, Post, Person}, Relationships: []string{Placement, Membership, FillsPost}},
		{ID: "Pr-Cn", Title: "Posts and responsibilities", Domain: "Personnel", Aspect: "Connectivity", Elements: []string{Post, "Responsibility", Person}, Relationships: []string{FillsPost, Owns}, Scales: []string{"M", "L", "XL"}},
		{ID: "St-Tx", Title: "Capabilities", Domain: "Strategic", Aspect: "Taxonomy", Elements: []string{Capability}, Relationships: []string{Performs}},
		{ID: "St-Sr", Title: "Goals and capabilities", Domain: "Strategic", Aspect: "Structure", Elements: []string{Goal, "EnterpriseObjective", "Opportunity", Capability}, Relationships: []string{"MapsToGoal", "MapsToCapability", Performs}, Scales: []string{"L", "XL"}},
		{ID: "Rs-Sr", Title: "Sites and resources", Domain: "Resources", Aspect: "Structure", Elements: []string{Location, Resource, "System", Organization}, Relationships: []string{Placement, Owns, Performs}},
		{ID: "Sv-Tx", Title: "Services", Domain: "Services", Aspect: "Taxonomy", Elements: []string{"Service", Organization}, Relationships: []string{Owns}, Scales: []string{"L", "XL"}},
		{ID: "Op-Pr", Title: "Processes", Domain: "Operational", Aspect: "Processes", Elements: []string{"OperationalActivity", Organization, Capability}, Relationships: []string{Performs, Owns}, Scales: []string{"M", "L", "XL"}},
		{ID: "Pj-Rm", Title: "Projects", Domain: "Projects", Aspect: "Roadmap", Elements: []string{Project, "ActualProjectMilestone", Organization}, Relationships: []string{Owns, "MilestoneDependency"}},
	}
}
