package enterprise

import "platformserver/apps/enterprise/uaf"

// The Enterprise Core Profile (ADR-0067 D1): the UAF stereotypes a tenant
// meets first, with the words people use for them and the description views
// they are drawn in. Everything else in the metamodel stays loadable and
// storable; it just has no palette entry until a profile opens it.

// ProfileEntry is one stereotype as the palette offers it.
type ProfileEntry struct {
	Stereotype string   `json:"stereotype"`
	Title      string   `json:"title"`
	Plural     string   `json:"plural"`
	Kinds      []string `json:"kinds,omitempty"`  // suggested Element.Kind values
	Scales     []string `json:"scales,omitempty"` // from which scale on it is offered; empty: always
	Icon       string   `json:"icon,omitempty"`
}

// Viewpoint is one ARIS description view (ADR-0093): the whole model seen from
// one question — who is where, what it holds, what it can do, what it offers,
// how it is run. Views are projections of the one model, never stores of their
// own: Elements declares the slice, Context names the stereotypes drawn only
// beside a slice element (the organisation is a fact of every view, not all of
// it in every drawing), and Relationships derive from the contracts just as a
// grid cell's did (ADR-0085 D2).
type Viewpoint struct {
	ID            string   `json:"id"` // "organization"
	Title         string   `json:"title"`
	Note          string   `json:"note"`               // the question the view answers
	Elements      []string `json:"elements"`           // the stereotypes it draws as its own
	Context       []string `json:"context,omitempty"`  // drawn only beside an element of the slice
	Relationships []string `json:"relationships"`      // derived: contracts among Elements+Context
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
		{Stereotype: "InformationElement", Title: "Information", Plural: "Information", Icon: "database", Scales: []string{"M", "L", "XL"}},
	}
}

// vocabulary are the relationships the Enterprise Core Profile draws: what a
// modeller of an organisation, its places, resources, capabilities, goals,
// processes and projects actually joins. Every other UAF relationship stays
// available through the contracts, and a model may hold one; it is just not
// offered by a view.
var vocabulary = map[string]bool{
	Placement: true, Membership: true, FillsPost: true, Performs: true, Exhibits: true,
	Owns: true, "OwnsProcess": true, "Enables": true, "MotivatedBy": true,
	"MilestoneDependency": true, "ProjectSequence": true,
}

// Views are the five description views (ADR-0093): one model, five questions.
// Each declares the stereotypes it draws as its own and the ones it draws only
// as context; the relationships are derived — a view offers a relation only
// when the contracts admit some pair of its own slice (ADR-0085 D2).
func Views(mm *uaf.Metamodel) []Viewpoint {
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
	views := []Viewpoint{
		{ID: "organization", Title: "Organisation and places", Note: "Who is where: organisations, posts, people, responsibilities, sites and equipment.",
			Elements: []string{Organization, Post, Person, "Responsibility", Location, Resource}},
		{ID: "data", Title: "Information", Note: "What the enterprise holds: information elements and where they belong.",
			Elements: []string{"InformationElement"}, Context: []string{Organization}},
		{ID: "function", Title: "Capabilities and goals", Note: "What it aims to do and can do: goals, objectives, opportunities, capabilities and the systems that support them.",
			Elements: []string{Capability, Goal, "EnterpriseObjective", "Opportunity", System}, Context: []string{Organization}},
		{ID: "output", Title: "Services delivered", Note: "What it offers outside: services and who provides them.",
			Elements: []string{"Service"}, Context: []string{Organization}},
		{ID: "control", Title: "Processes, projects and rules", Note: "How it is run: processes, projects and milestones, standards and risks — the view that joins the others.",
			Elements: []string{"OperationalActivity", Project, "ActualProjectMilestone", "Standard", "Risk"}, Context: []string{Organization}},
	}
	for i := range views {
		views[i].Relationships = drawable(append(append([]string{}, views[i].Elements...), views[i].Context...))
	}
	return views
}

// holds reports whether any of the view's element stereotypes is the named end
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
