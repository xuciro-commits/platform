package enterprise

import (
	"fmt"
	"strings"
)

// Templates by scale (ADR-0067 D6): the first shape of an enterprise model,
// sized to the organisation, which people then rename, merge, close and
// extend like anything else. Nothing here is locked.

type SeedParams struct {
	Scale         string // S, M, L, XL; empty: from Headcount
	Name          string
	Headcount     int
	Sites         int
	LegalEntities int
	Industry      string
	Day           Date
}

// ScaleOf picks the scale a headcount suggests.
func ScaleOf(headcount int) string {
	switch {
	case headcount <= 100:
		return "S"
	case headcount <= 1000:
		return "M"
	case headcount <= 10000:
		return "L"
	default:
		return "XL"
	}
}

// Template is the model a template produces for the parameters.
func Template(p SeedParams) (Model, error) {
	if p.Scale == "" {
		p.Scale = ScaleOf(p.Headcount)
	}
	if p.Sites < 1 {
		p.Sites = map[string]int{"S": 1, "M": 1, "L": 3, "XL": 6}[p.Scale]
	}
	if p.LegalEntities < 1 {
		p.LegalEntities = map[string]int{"S": 1, "M": 1, "L": 1, "XL": 4}[p.Scale]
	}
	b := &builder{m: Model{UAF: "1.3", Scale: p.Scale, Kinds: []Kind{}, Elements: []Element{}, Relationships: []Relationship{}, Views: []View{}}, day: p.Day, ids: map[string]int{}}
	switch p.Scale {
	case "S":
		b.small(p)
	case "M":
		b.plant(p)
	case "L":
		b.divisions(p)
	case "XL":
		b.group(p)
	default:
		return Model{}, fmt.Errorf("the scale %q is not S, M, L or XL", p.Scale)
	}
	return b.m, nil
}

type builder struct {
	m   Model
	day Date
	ids map[string]int
}

func slug(s string) string {
	var out []rune
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			out = append(out, r)
		case r == ' ' || r == '-' || r == '_' || r == '/':
			if len(out) > 0 && out[len(out)-1] != '-' {
				out = append(out, '-')
			}
		}
	}
	if len(out) == 0 {
		return "x"
	}
	return strings.TrimRight(string(out), "-")
}

func (b *builder) id(prefix, name string) string {
	base := prefix + "-" + slug(name)
	n := b.ids[base]
	b.ids[base] = n + 1
	if n == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n+1)
}

func (b *builder) kind(id, name, kind string, matrix bool) {
	b.m.Kinds = append(b.m.Kinds, Kind{ID: id, Name: name, Kind: kind, Matrix: matrix})
}

func (b *builder) element(stereotype, name, kind string) string {
	prefix := map[string]string{Organization: "org", Post: "post", Person: "person", Location: "loc", Resource: "res", Capability: "cap", Goal: "goal", Project: "prj", "System": "sys", "Responsibility": "resp"}[stereotype]
	if prefix == "" {
		prefix = slug(stereotype)
	}
	id := b.id(prefix, name)
	b.m.Elements = append(b.m.Elements, Element{ID: id, Stereotype: stereotype, Name: name, Kind: kind, From: b.day})
	return id
}

func (b *builder) legal(name, kind string) string {
	id := b.element(Organization, name, kind)
	b.m.Elements[len(b.m.Elements)-1].Legal = true
	return id
}

func (b *builder) relate(stereotype, kind, source, target string) string {
	id := b.id("rel", stereotype+"-"+source+"-"+target)
	b.m.Relationships = append(b.m.Relationships, Relationship{ID: id, Stereotype: stereotype, Kind: kind, Source: source, Target: target, From: b.day})
	return id
}

func (b *builder) under(kind, child, parent string) { b.relate(Placement, kind, child, parent) }

// post adds a post in an organisation (the post is owned by it).
func (b *builder) post(org, name, kind string) string {
	p := b.element(Post, name, kind)
	b.relate(Owns, "", org, p)
	return p
}

func (b *builder) capability(org, name, kind string) string {
	c := b.element(Capability, name, kind)
	b.relate(Performs, "", org, c)
	return c
}

func (b *builder) goal(org, name string) {
	g := b.element(Goal, name, "")
	b.relate(Owns, "", org, g)
}

func (b *builder) location(name, kind, parent, owner string) string {
	l := b.element(Location, name, kind)
	if parent != "" {
		b.under("site", l, parent)
	}
	if owner != "" {
		b.relate(Owns, "", owner, l)
	}
	return l
}

func (b *builder) view(name, grid string, elements ...string) {
	b.m.Views = append(b.m.Views, View{ID: b.id("view", name), Name: name, Grid: grid, Elements: elements})
}

func (b *builder) org(kind string) []string {
	var out []string
	for _, e := range b.m.Elements {
		if e.Stereotype == Organization && (kind == "" || e.Kind == kind) {
			out = append(out, e.ID)
		}
	}
	return out
}

// --- S: ≤ 100 people. One company, a few teams, posts are roles, one site. ---
func (b *builder) small(p SeedParams) {
	b.kind("management", "Management", "management", false)
	b.kind("site", "Sites", "site", false)
	co := b.legal(p.Name, "company")
	b.post(co, "General manager", "executive")
	for _, team := range []string{"Operations", "Sales & service", "Finance & admin"} {
		t := b.element(Organization, team, "team")
		b.under("management", t, co)
		b.post(t, team+" lead", "manager")
		b.post(t, team+" staff", "staff")
	}
	site := b.location(p.Name+" office", "office", "", co)
	for _, c := range []string{"Sell", "Deliver", "Support customers", "Run the back office"} {
		b.capability(co, c, "business")
	}
	for _, g := range []string{"Grow revenue", "Keep customers", "Stay profitable"} {
		b.goal(co, g)
	}
	b.view("Organisation", "Pr-Sr", b.org("")...)
	b.view("Capabilities", "St-Tx", b.ofStereotype(Capability)...)
	b.view("Sites", "Rs-Sr", site)
}

// --- M: ≤ 1,000 people. A plant: departments, workshops, lines, stations, machines. ---
func (b *builder) plant(p SeedParams) {
	b.kind("management", "Management", "management", false)
	b.kind("site", "Sites", "site", false)
	b.kind("legal", "Legal", "legal", false)
	co := b.legal(p.Name, "company")
	b.post(co, "Plant manager", "executive")
	b.plantUnder(co, co, p, 1)
	for _, c := range []string{"Manufacture", "Assure quality", "Maintain equipment", "Store and ship", "Plan production", "Buy materials", "Sell and serve"} {
		b.capability(co, c, "operational")
	}
	for _, s := range []string{"ERP", "MES", "WMS"} {
		sys := b.element("System", s, s)
		b.relate(Owns, "", co, sys)
	}
	for _, g := range []string{"On-time delivery above 95%", "First-pass yield above 98%", "Zero lost-time accidents"} {
		b.goal(co, g)
	}
	b.view("Organisation", "Pr-Sr", b.org("")...)
	b.view("Capabilities", "St-Tx", b.ofStereotype(Capability)...)
	b.view("Sites and resources", "Rs-Sr", append(b.ofStereotype(Location), b.ofStereotype(Resource)...)...)
}

// plantUnder adds the departments and the physical plant(s) of an organisation.
func (b *builder) plantUnder(mgmt, owner string, p SeedParams, plants int) {
	depts := []string{"Production", "Quality", "Maintenance", "Warehouse & logistics", "Planning & purchasing", "Human resources", "Finance", "Sales"}
	if p.Industry == "hospitality" {
		depts = []string{"Front office", "Housekeeping", "Food & beverage", "Engineering", "Sales & marketing", "Human resources", "Finance"}
	}
	var production string
	for _, d := range depts {
		dept := b.element(Organization, d, "department")
		b.under("management", dept, mgmt)
		b.post(dept, d+" manager", "manager")
		if d == "Production" || d == "Front office" {
			production = dept
		}
	}
	for i := 1; i <= plants; i++ {
		name := p.Name + " plant"
		if plants > 1 {
			name = fmt.Sprintf("%s plant %d", p.Name, i)
		}
		plant := b.location(name, "plant", "", owner)
		for w := 1; w <= 2; w++ {
			shop := b.element(Organization, fmt.Sprintf("Workshop %d", w), "workshop")
			b.under("management", shop, production)
			b.post(shop, "Workshop supervisor", "supervisor")
			shopLoc := b.location(fmt.Sprintf("Workshop %d", w), "workshop", plant, shop)
			for l := 1; l <= 2; l++ {
				line := b.element(Organization, fmt.Sprintf("Line %d-%d", w, l), "line")
				b.under("management", line, shop)
				b.post(line, "Line lead", "supervisor")
				b.post(line, "Operator", "operator")
				lineLoc := b.location(fmt.Sprintf("Line %d-%d", w, l), "line", shopLoc, line)
				for s := 1; s <= 3; s++ {
					st := b.location(fmt.Sprintf("Station %d-%d-%d", w, l, s), "station", lineLoc, line)
					mach := b.element(Resource, fmt.Sprintf("Machine %d-%d-%d", w, l, s), "machine")
					b.relate(Owns, "", line, mach)
					b.relate(Placement, "site", mach, st)
				}
			}
		}
		b.location("Raw material warehouse", "warehouse", plant, owner)
		b.location("Finished goods warehouse", "warehouse", plant, owner)
	}
}

// --- L: ≤ 10,000 people. Divisions with their plants, shared services, a finance kind, projects. ---
func (b *builder) divisions(p SeedParams) {
	b.kind("management", "Management", "management", false)
	b.kind("site", "Sites", "site", false)
	b.kind("legal", "Legal", "legal", false)
	b.kind("finance", "Cost centres", "finance", false)
	b.kind("project", "Projects", "project", true)
	co := b.legal(p.Name, "company")
	b.post(co, "Chief executive", "executive")
	hq := b.element(Organization, "Headquarters", "division")
	b.under("management", hq, co)
	for _, f := range []string{"Strategy", "Finance", "Human resources", "IT", "Legal & compliance"} {
		d := b.element(Organization, f, "department")
		b.under("management", d, hq)
		b.under("finance", d, co)
		b.post(d, "Head of "+strings.ToLower(f), "manager")
	}
	for i := 1; i <= p.Sites; i++ {
		bu := b.element(Organization, fmt.Sprintf("Business unit %c", 'A'+i-1), "business unit")
		b.under("management", bu, co)
		b.under("finance", bu, co)
		b.post(bu, "Business unit director", "executive")
		b.plantUnder(bu, bu, SeedParams{Name: fmt.Sprintf("%s %c", p.Name, 'A'+i-1), Industry: p.Industry}, 1)
	}
	for _, c := range []string{"Set strategy", "Develop products", "Manufacture", "Sell and serve", "Run shared services", "Manage talent", "Govern finance"} {
		b.capability(co, c, "business")
	}
	for _, s := range []string{"ERP", "MES", "WMS", "PLM", "CRM"} {
		b.relate(Owns, "", hq, b.element("System", s, s))
	}
	for _, pr := range []string{"Digital factory programme", "New product line launch"} {
		prj := b.element(Project, pr, "improvement")
		b.relate(Owns, "", hq, prj)
	}
	for _, g := range []string{"Double revenue in five years", "Operating margin above 12%", "Employee engagement above 80%"} {
		b.goal(co, g)
	}
	b.view("Organisation", "Pr-Sr", b.org("")...)
	b.view("Capabilities", "St-Tx", b.ofStereotype(Capability)...)
	b.view("Projects", "Pj-Rm", b.ofStereotype(Project)...)
}

// --- XL: > 10,000 people. A group: subsidiaries with shares, regions, governance. ---
func (b *builder) group(p SeedParams) {
	b.kind("legal", "Ownership", "legal", false)
	b.kind("management", "Management", "management", false)
	b.kind("site", "Regions and sites", "site", false)
	b.kind("finance", "Cost centres", "finance", false)
	b.kind("governance", "Governance", "governance", false)
	b.kind("project", "Projects", "project", true)
	grp := b.legal(p.Name+" Group", "group")
	b.post(grp, "Group chief executive", "executive")
	board := b.element(Organization, "Board of directors", "committee")
	b.under("governance", grp, board)
	for _, c := range []string{"Audit committee", "Remuneration committee"} {
		b.under("governance", b.element(Organization, c, "committee"), board)
	}
	hq := b.element(Organization, "Group headquarters", "division")
	b.under("management", hq, grp)
	for _, f := range []string{"Group finance", "Group HR", "Group IT", "Strategy & M&A", "Legal & compliance", "Internal audit"} {
		d := b.element(Organization, f, "department")
		b.under("management", d, hq)
		b.under("finance", d, grp)
	}
	regions := []string{"North", "South", "East", "West", "Overseas"}
	for i := 1; i <= p.LegalEntities; i++ {
		subName := fmt.Sprintf("%s %s", p.Name, []string{"Industrial", "Technology", "Trading", "Services", "Energy", "Logistics"}[(i-1)%6])
		sub := b.legal(subName, "subsidiary")
		b.relate(Placement, "legal", sub, grp)
		b.m.Relationships[len(b.m.Relationships)-1].Share = 1
		b.under("management", sub, grp)
		b.under("finance", sub, grp)
		b.post(sub, "Managing director", "executive")
		region := b.location(regions[(i-1)%len(regions)]+" region", "region", "", sub)
		plants := max(1, p.Sites/p.LegalEntities)
		for j := 1; j <= plants; j++ {
			bu := b.element(Organization, fmt.Sprintf("Business unit %d", j), "business unit")
			b.under("management", bu, sub)
			b.under("finance", bu, sub)
			before := len(b.m.Elements)
			b.plantUnder(bu, bu, SeedParams{Name: fmt.Sprintf("%s %d", subName, j), Industry: p.Industry}, 1)
			for _, e := range b.m.Elements[before:] { // the plant sits in the region
				if e.Stereotype == Location && e.Kind == "plant" {
					b.under("site", e.ID, region)
				}
			}
		}
	}
	for _, c := range []string{"Allocate capital", "Govern subsidiaries", "Develop markets", "Manufacture", "Run shared services", "Manage group talent", "Assure compliance"} {
		b.capability(grp, c, "business")
	}
	for _, g := range []string{"Top three in every market served", "Return on capital above 10%", "Net zero by 2040"} {
		b.goal(grp, g)
	}
	b.view("Group structure", "Pr-Sr", b.org("")...)
	b.view("Capabilities", "St-Tx", b.ofStereotype(Capability)...)
	b.view("Regions and sites", "Rs-Sr", b.ofStereotype(Location)...)
}

func (b *builder) ofStereotype(st string) []string {
	var out []string
	for _, e := range b.m.Elements {
		if e.Stereotype == st {
			out = append(out, e.ID)
		}
	}
	return out
}
